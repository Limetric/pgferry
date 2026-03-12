package main

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

type extensionRequirement struct {
	Name            string
	Feature         string
	CreateIfMissing bool
	CreateHint      string
}

func collectRequiredExtensions(schema *Schema, src SourceDB, cfg *MigrationConfig, typeMap TypeMappingConfig) []extensionRequirement {
	var reqs []extensionRequirement

	if schemaUsesCitext(schema, src, typeMap) {
		reqs = append(reqs, extensionRequirement{
			Name:            "citext",
			Feature:         "ci_as_citext",
			CreateIfMissing: true,
		})
	}

	if cfg.PostGIS.Enabled && schemaUsesMySQLSpatial(schema) {
		reqs = append(reqs, extensionRequirement{
			Name:            "postgis",
			Feature:         "postgis",
			CreateIfMissing: cfg.PostGIS.CreateExtension,
			CreateHint:      "or set [postgis].create_extension = true",
		})
	}

	sort.Slice(reqs, func(i, j int) bool {
		return reqs[i].Name < reqs[j].Name
	})

	return reqs
}

func schemaUsesCitext(schema *Schema, src SourceDB, typeMap TypeMappingConfig) bool {
	if !typeMap.CIAsCitext || schema == nil || src == nil {
		return false
	}

	for _, t := range schema.Tables {
		for _, col := range t.Columns {
			pgType, err := src.MapType(col, typeMap)
			if err != nil {
				continue
			}
			if pgTypeForCollation(col, pgType, typeMap) == "citext" {
				return true
			}
		}
	}

	return false
}

func schemaUsesMySQLSpatial(schema *Schema) bool {
	if schema == nil {
		return false
	}

	for _, t := range schema.Tables {
		for _, col := range t.Columns {
			if isMySQLSpatialType(col.DataType) {
				return true
			}
		}
	}

	return false
}

func ensureRequiredExtensions(ctx context.Context, pool *pgxpool.Pool, reqs []extensionRequirement) error {
	for _, req := range reqs {
		installed, available, err := queryExtensionStatus(ctx, pool, req.Name)
		if err != nil {
			return fmt.Errorf("check extension %s: %w", req.Name, err)
		}

		if installed {
			log.Printf("extension %s already installed (%s)", req.Name, req.Feature)
			continue
		}
		if !available {
			return fmt.Errorf("%s feature requires PostgreSQL extension %q, but it is not available on the target server", req.Feature, req.Name)
		}
		if !req.CreateIfMissing {
			msg := fmt.Sprintf("%s feature requires PostgreSQL extension %q to be installed before running pgferry; install it first", req.Feature, req.Name)
			if req.CreateHint != "" {
				msg += " " + req.CreateHint
			}
			return fmt.Errorf("%s", msg)
		}

		log.Printf("creating PostgreSQL extension %s for %s...", req.Name, req.Feature)
		if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", pgIdent(req.Name))); err != nil {
			return fmt.Errorf("create extension %s for %s: %w", req.Name, req.Feature, err)
		}
		log.Printf("extension %s created (%s)", req.Name, req.Feature)
	}

	return nil
}

func queryExtensionStatus(ctx context.Context, pool *pgxpool.Pool, name string) (installed bool, available bool, err error) {
	err = pool.QueryRow(ctx, `
		SELECT
			EXISTS(SELECT 1 FROM pg_extension WHERE extname = $1),
			EXISTS(SELECT 1 FROM pg_available_extensions WHERE name = $1)
	`, name).Scan(&installed, &available)
	return installed, available, err
}
