package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// postMigrate runs all post-migration steps in order:
// 1. SET LOGGED, 2. PKs, 3. Indexes, 4. before_fk hooks, 5. orphan cleanup, 6. FKs, 7. Sequences, 8. optional triggers, 9. after_all hooks
func postMigrate(ctx context.Context, pool *pgxpool.Pool, schema *Schema, cfg *MigrationConfig) error {
	pgSchema := cfg.Schema

	// data_only: skip all DDL steps, only reset sequences + after_all hooks
	if cfg.DataOnly {
		log.Printf("  sequences...")
		if err := resetSequences(ctx, pool, schema, pgSchema); err != nil {
			return fmt.Errorf("sequences: %w", err)
		}
		if err := loadAndExecSQLFiles(ctx, pool, cfg, cfg.Hooks.AfterAll, "after_all"); err != nil {
			return fmt.Errorf("after_all hooks: %w", err)
		}
		return nil
	}

	// schema_only: skip SET LOGGED (tables are already LOGGED)
	if !cfg.SchemaOnly {
		log.Printf("  SET LOGGED...")
		if err := setLogged(ctx, pool, schema, pgSchema); err != nil {
			return fmt.Errorf("SET LOGGED: %w", err)
		}
	}

	log.Printf("  primary keys...")
	if err := addPrimaryKeys(ctx, pool, schema, pgSchema); err != nil {
		return fmt.Errorf("primary keys: %w", err)
	}

	log.Printf("  indexes...")
	if err := addIndexes(ctx, pool, schema, pgSchema); err != nil {
		return fmt.Errorf("indexes: %w", err)
	}

	// before_fk hooks
	if err := loadAndExecSQLFiles(ctx, pool, cfg, cfg.Hooks.BeforeFk, "before_fk"); err != nil {
		return fmt.Errorf("before_fk hooks: %w", err)
	}

	// schema_only: skip orphan cleanup (no data to clean)
	if !cfg.SchemaOnly {
		log.Printf("  orphan cleanup...")
		if err := cleanOrphans(ctx, pool, schema, pgSchema); err != nil {
			return fmt.Errorf("orphan cleanup: %w", err)
		}
	}

	log.Printf("  foreign keys...")
	if err := addForeignKeys(ctx, pool, schema, pgSchema); err != nil {
		return fmt.Errorf("foreign keys: %w", err)
	}

	log.Printf("  sequences...")
	if err := resetSequences(ctx, pool, schema, pgSchema); err != nil {
		return fmt.Errorf("sequences: %w", err)
	}

	if cfg.AddUnsignedChecks {
		log.Printf("  unsigned checks...")
		if err := addUnsignedChecks(ctx, pool, schema, pgSchema, cfg.TypeMapping); err != nil {
			return fmt.Errorf("unsigned checks: %w", err)
		}
	} else {
		log.Printf("  unsigned checks skipped (add_unsigned_checks=false)")
	}

	if cfg.ReplicateOnUpdateCurrentTimestamp {
		log.Printf("  triggers...")
		if err := createTriggers(ctx, pool, schema, pgSchema); err != nil {
			return fmt.Errorf("triggers: %w", err)
		}
	} else {
		log.Printf("  triggers skipped (replicate_on_update_current_timestamp=false)")
	}

	// after_all hooks
	if err := loadAndExecSQLFiles(ctx, pool, cfg, cfg.Hooks.AfterAll, "after_all"); err != nil {
		return fmt.Errorf("after_all hooks: %w", err)
	}

	return nil
}

func addUnsignedChecks(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string, typeMap TypeMappingConfig) error {
	for _, t := range schema.Tables {
		for _, col := range t.Columns {
			expr, ok := unsignedCheckExpr(col, typeMap)
			if !ok {
				continue
			}

			constraintName := unsignedConstraintName(t.PGName, col.PGName)
			addSQL := fmt.Sprintf(
				"ALTER TABLE %s.%s ADD CONSTRAINT %s CHECK (%s) NOT VALID",
				pgIdent(pgSchema), pgIdent(t.PGName), pgIdent(constraintName), expr,
			)
			if err := execSQL(ctx, pool, constraintName, addSQL); err != nil {
				return err
			}

			validateSQL := fmt.Sprintf(
				"ALTER TABLE %s.%s VALIDATE CONSTRAINT %s",
				pgIdent(pgSchema), pgIdent(t.PGName), pgIdent(constraintName),
			)
			if err := execSQL(ctx, pool, constraintName, validateSQL); err != nil {
				return err
			}

			log.Printf("    constraint %s on %s.%s", constraintName, pgSchema, t.PGName)
		}
	}
	return nil
}

func unsignedCheckExpr(col Column, typeMap TypeMappingConfig) (string, bool) {
	if !strings.Contains(col.ColumnType, "unsigned") {
		return "", false
	}
	if col.DataType == "tinyint" && col.Precision == 1 && typeMap.TinyInt1AsBoolean {
		return "", false
	}

	ident := pgIdent(col.PGName)
	switch col.DataType {
	case "tinyint":
		return fmt.Sprintf("%s >= 0 AND %s <= 255", ident, ident), true
	case "smallint":
		return fmt.Sprintf("%s >= 0 AND %s <= 65535", ident, ident), true
	case "mediumint":
		return fmt.Sprintf("%s >= 0 AND %s <= 16777215", ident, ident), true
	case "int":
		return fmt.Sprintf("%s >= 0 AND %s <= 4294967295", ident, ident), true
	case "bigint":
		return fmt.Sprintf("%s >= 0 AND %s <= 18446744073709551615", ident, ident), true
	case "decimal", "float", "double":
		return fmt.Sprintf("%s >= 0", ident), true
	default:
		return "", false
	}
}

func unsignedConstraintName(table, col string) string {
	base := fmt.Sprintf("ck_%s_%s", table, col)
	suffix := "_unsigned"
	full := base + suffix
	if len(full) <= 63 {
		return full
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(full))
	hashSuffix := fmt.Sprintf("_%08x", h.Sum32())
	maxBase := 63 - len(suffix) - len(hashSuffix)
	if maxBase < 1 {
		maxBase = 1
	}
	return base[:maxBase] + suffix + hashSuffix
}

// execSQL is a helper that runs a single statement and logs errors with context.
func execSQL(ctx context.Context, pool *pgxpool.Pool, desc, query string) error {
	if _, err := pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("%s: %w\nSQL: %s", desc, err, query)
	}
	return nil
}

// setLogged converts all UNLOGGED tables back to LOGGED.
func setLogged(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string) error {
	for _, t := range schema.Tables {
		q := fmt.Sprintf("ALTER TABLE %s.%s SET LOGGED", pgIdent(pgSchema), pgIdent(t.PGName))
		if err := execSQL(ctx, pool, t.PGName, q); err != nil {
			return err
		}
	}
	return nil
}

// addPrimaryKeys adds PRIMARY KEY constraints from introspected data.
func addPrimaryKeys(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string) error {
	for _, t := range schema.Tables {
		if t.PrimaryKey == nil {
			continue
		}
		cols := quotedColumnList(t.PrimaryKey.Columns)
		q := fmt.Sprintf("ALTER TABLE %s.%s ADD PRIMARY KEY (%s)",
			pgIdent(pgSchema), pgIdent(t.PGName), cols)
		if err := execSQL(ctx, pool, t.PGName+" PK", q); err != nil {
			return err
		}
		log.Printf("    pk %s on %s.%s", cols, pgSchema, t.PGName)
	}
	return nil
}

// addIndexes adds all non-primary indexes.
func addIndexes(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string) error {
	for _, t := range schema.Tables {
		for _, idx := range t.Indexes {
			if reason, unsupported := indexUnsupportedReason(idx); unsupported {
				log.Printf("    skipping index %s on %s.%s: %s", idx.MySQLName, pgSchema, t.PGName, reason)
				continue
			}

			cols := quotedOrderedColumnList(idx.Columns, idx.ColumnOrders)
			unique := ""
			if idx.Unique {
				unique = "UNIQUE "
			}
			idxName := fmt.Sprintf("%s_%s", t.PGName, idx.Name)
			q := fmt.Sprintf("CREATE %sINDEX %s ON %s.%s (%s)",
				unique, pgIdent(idxName), pgIdent(pgSchema), pgIdent(t.PGName), cols)
			if err := execSQL(ctx, pool, idxName, q); err != nil {
				return err
			}
			log.Printf("    index %s on %s.%s (%s)", idxName, pgSchema, t.PGName, cols)
		}
	}
	return nil
}

// addForeignKeys adds all foreign key constraints from introspected data.
func addForeignKeys(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string) error {
	for _, t := range schema.Tables {
		for _, fk := range t.ForeignKeys {
			localCols := quotedColumnList(fk.Columns)
			refCols := quotedColumnList(fk.RefColumns)
			q := fmt.Sprintf(
				"ALTER TABLE %s.%s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s(%s) ON UPDATE %s ON DELETE %s",
				pgIdent(pgSchema), pgIdent(t.PGName),
				pgIdent(fk.Name),
				localCols,
				pgIdent(pgSchema), pgIdent(fk.RefPGTable),
				refCols,
				fk.UpdateRule, fk.DeleteRule,
			)
			if err := execSQL(ctx, pool, fk.Name, q); err != nil {
				return err
			}
			log.Printf("    fk %s on %s.%s → %s", fk.Name, pgSchema, t.PGName, fk.RefPGTable)
		}
	}
	return nil
}

// resetSequences resets auto-increment sequences by finding columns with auto_increment
// and setting the sequence to max(col)+1.
func resetSequences(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string) error {
	for _, t := range schema.Tables {
		for _, col := range t.Columns {
			if !strings.Contains(col.Extra, "auto_increment") {
				continue
			}
			// PG serial sequence naming convention: schema.table_col_seq
			// But since we created raw integer columns, we need to create the sequence and attach it
			seqName := fmt.Sprintf("%s_%s_seq", t.PGName, col.PGName)

			stmts := []string{
				fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s.%s", pgIdent(pgSchema), pgIdent(seqName)),
				fmt.Sprintf("SELECT setval('%s.%s', COALESCE((SELECT MAX(%s) FROM %s.%s), 0) + 1, false)",
					pgSchema, seqName,
					pgIdent(col.PGName), pgIdent(pgSchema), pgIdent(t.PGName)),
				fmt.Sprintf("ALTER TABLE %s.%s ALTER COLUMN %s SET DEFAULT nextval('%s.%s')",
					pgIdent(pgSchema), pgIdent(t.PGName), pgIdent(col.PGName),
					pgSchema, seqName),
			}
			for _, q := range stmts {
				if err := execSQL(ctx, pool, seqName, q); err != nil {
					return err
				}
			}
			log.Printf("    sequence %s.%s reset", pgSchema, seqName)
		}
	}
	return nil
}

// createTriggers creates trigger functions and triggers for columns with ON UPDATE CURRENT_TIMESTAMP.
func createTriggers(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string) error {
	// Track which trigger functions we've already created
	createdFuncs := make(map[string]bool)

	for _, t := range schema.Tables {
		for _, col := range t.Columns {
			if !strings.Contains(strings.ToLower(col.Extra), "on update current_timestamp") {
				continue
			}

			funcName := fmt.Sprintf("set_%s", col.PGName)

			// Create trigger function if not yet created
			if !createdFuncs[funcName] {
				q := fmt.Sprintf(
					`CREATE OR REPLACE FUNCTION %s.%s() RETURNS TRIGGER AS $fn$ BEGIN NEW.%s = CURRENT_TIMESTAMP; RETURN NEW; END; $fn$ LANGUAGE plpgsql`,
					pgIdent(pgSchema), pgIdent(funcName), pgIdent(col.PGName))
				if err := execSQL(ctx, pool, funcName, q); err != nil {
					return err
				}
				createdFuncs[funcName] = true
			}

			// Create trigger
			trigName := fmt.Sprintf("trg_%s_%s", t.PGName, col.PGName)
			q := fmt.Sprintf(
				"CREATE TRIGGER %s BEFORE UPDATE ON %s.%s FOR EACH ROW EXECUTE FUNCTION %s.%s()",
				pgIdent(trigName), pgIdent(pgSchema), pgIdent(t.PGName),
				pgIdent(pgSchema), pgIdent(funcName))
			if err := execSQL(ctx, pool, trigName, q); err != nil {
				return err
			}
			log.Printf("    trigger %s on %s.%s", trigName, pgSchema, t.PGName)
		}
	}
	return nil
}

// cleanOrphans removes or nullifies rows that reference non-existent parent rows.
// MySQL allows orphaned data via SET FOREIGN_KEY_CHECKS=0; PostgreSQL rejects it
// when adding FK constraints. The action mirrors the FK's ON DELETE rule:
// SET NULL → null out the columns, anything else → delete the rows.
func cleanOrphans(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string) error {
	for _, t := range schema.Tables {
		for _, fk := range t.ForeignKeys {
			child := fmt.Sprintf("%s.%s", pgIdent(pgSchema), pgIdent(t.PGName))
			parent := fmt.Sprintf("%s.%s", pgIdent(pgSchema), pgIdent(fk.RefPGTable))

			// Build the NOT EXISTS join condition
			var joinConds []string
			for i, col := range fk.Columns {
				joinConds = append(joinConds,
					fmt.Sprintf("p.%s = c.%s", pgIdent(fk.RefColumns[i]), pgIdent(col)))
			}
			notExists := fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s p WHERE %s)",
				parent, strings.Join(joinConds, " AND "))

			// At least one FK column must be non-null for a violation to exist
			var notNulls []string
			for _, col := range fk.Columns {
				notNulls = append(notNulls, fmt.Sprintf("c.%s IS NOT NULL", pgIdent(col)))
			}
			whereNotNull := strings.Join(notNulls, " OR ")

			var q string
			if strings.EqualFold(fk.DeleteRule, "SET NULL") {
				// Null out the FK columns
				var setClauses []string
				for _, col := range fk.Columns {
					setClauses = append(setClauses, fmt.Sprintf("%s = NULL", pgIdent(col)))
				}
				q = fmt.Sprintf("UPDATE %s c SET %s WHERE (%s) AND %s",
					child, strings.Join(setClauses, ", "), whereNotNull, notExists)
			} else {
				// DELETE for CASCADE, RESTRICT, NO ACTION
				q = fmt.Sprintf("DELETE FROM %s c WHERE (%s) AND %s",
					child, whereNotNull, notExists)
			}

			tag, err := pool.Exec(ctx, q)
			if err != nil {
				return fmt.Errorf("clean orphans %s.%s → %s: %w\nSQL: %s",
					t.PGName, fk.Name, fk.RefPGTable, err, q)
			}
			if tag.RowsAffected() > 0 {
				action := "deleted"
				if strings.EqualFold(fk.DeleteRule, "SET NULL") {
					action = "nullified"
				}
				log.Printf("    %s %d orphaned rows in %s.%s (fk: %s → %s)",
					action, tag.RowsAffected(), pgSchema, t.PGName, fk.Name, fk.RefPGTable)
			}
		}
	}
	return nil
}

// setTriggers enables or disables all triggers on every table in the schema.
// Disabling triggers suspends FK enforcement, allowing parallel COPY in data_only mode.
func setTriggers(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string, enable bool) error {
	action := "DISABLE"
	if enable {
		action = "ENABLE"
	}
	for _, t := range schema.Tables {
		q := fmt.Sprintf("ALTER TABLE %s.%s %s TRIGGER ALL", pgIdent(pgSchema), pgIdent(t.PGName), action)
		if err := execSQL(ctx, pool, t.PGName, q); err != nil {
			return err
		}
	}
	return nil
}

// quotedColumnList joins column names with proper quoting.
func quotedColumnList(cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = pgIdent(c)
	}
	return strings.Join(quoted, ", ")
}

func quotedOrderedColumnList(cols, orders []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		dir := ""
		if i < len(orders) && strings.EqualFold(orders[i], "DESC") {
			dir = " DESC"
		}
		quoted[i] = pgIdent(c) + dir
	}
	return strings.Join(quoted, ", ")
}
