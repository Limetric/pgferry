package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// postMigrate runs all post-migration steps in order:
// 1. SET LOGGED, 2. PKs, 3. Indexes, 4. before_fk hooks, 5. FKs, 6. Sequences, 7. Triggers, 8. after_all hooks
func postMigrate(ctx context.Context, pool *pgxpool.Pool, schema *Schema, cfg *MigrationConfig) error {
	pgSchema := cfg.Schema

	steps := []struct {
		name string
		fn   func(context.Context, *pgxpool.Pool, *Schema, string) error
	}{
		{"SET LOGGED", setLogged},
		{"primary keys", addPrimaryKeys},
		{"indexes", addIndexes},
	}

	for _, step := range steps {
		log.Printf("  %s...", step.name)
		if err := step.fn(ctx, pool, schema, pgSchema); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	// before_fk hooks (orphan cleanup goes here)
	if err := loadAndExecSQLFiles(ctx, pool, cfg, cfg.Hooks.BeforeFk, "before_fk"); err != nil {
		return fmt.Errorf("before_fk hooks: %w", err)
	}

	steps2 := []struct {
		name string
		fn   func(context.Context, *pgxpool.Pool, *Schema, string) error
	}{
		{"foreign keys", addForeignKeys},
		{"sequences", resetSequences},
		{"triggers", createTriggers},
	}

	for _, step := range steps2 {
		log.Printf("  %s...", step.name)
		if err := step.fn(ctx, pool, schema, pgSchema); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	// after_all hooks
	if err := loadAndExecSQLFiles(ctx, pool, cfg, cfg.Hooks.AfterAll, "after_all"); err != nil {
		return fmt.Errorf("after_all hooks: %w", err)
	}

	return nil
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
	}
	return nil
}

// addIndexes adds all non-primary indexes.
func addIndexes(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string) error {
	for _, t := range schema.Tables {
		for _, idx := range t.Indexes {
			cols := quotedColumnList(idx.Columns)
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

// quotedColumnList joins column names with proper quoting.
func quotedColumnList(cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = pgIdent(c)
	}
	return strings.Join(quoted, ", ")
}
