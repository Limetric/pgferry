package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// MigrationConfig holds the full TOML-driven migration configuration.
type MigrationConfig struct {
	Source                            SourceConfig      `toml:"source"`
	Target                            TargetConfig      `toml:"target"`
	Schema                            string            `toml:"schema"`
	OnSchemaExists                    string            `toml:"on_schema_exists"`
	SchemaOnly                        bool              `toml:"schema_only"`
	DataOnly                          bool              `toml:"data_only"`
	SourceSnapshotMode                string            `toml:"source_snapshot_mode"` // none|single_tx
	UnloggedTables                    bool              `toml:"unlogged_tables"`
	PreserveDefaults                  bool              `toml:"preserve_defaults"`
	AddUnsignedChecks                 bool              `toml:"add_unsigned_checks"`
	CleanOrphans                      bool              `toml:"clean_orphans"`
	SnakeCaseIdentifiers              bool              `toml:"snake_case_identifiers"`
	ReplicateOnUpdateCurrentTimestamp bool              `toml:"replicate_on_update_current_timestamp"`
	Workers                           int               `toml:"workers"`
	Hooks                             HooksConfig       `toml:"hooks"`
	TypeMapping                       TypeMappingConfig `toml:"type_mapping"`

	// configDir is the directory containing the TOML file, used to resolve relative SQL paths.
	configDir string
}

// SourceConfig identifies the source database engine and connection string.
type SourceConfig struct {
	Type    string `toml:"type"`    // "mysql" or "sqlite"
	DSN     string `toml:"dsn"`
	Charset string `toml:"charset"` // character set for MySQL connection (default: "utf8mb4")
}

type TargetConfig struct {
	DSN string `toml:"dsn"`
}

type HooksConfig struct {
	BeforeData []string `toml:"before_data"`
	AfterData  []string `toml:"after_data"`
	BeforeFk   []string `toml:"before_fk"`
	AfterAll   []string `toml:"after_all"`
}

// TypeMappingConfig controls non-lossless type coercions.
type TypeMappingConfig struct {
	TinyInt1AsBoolean     bool              `toml:"tinyint1_as_boolean"`
	Binary16AsUUID        bool              `toml:"binary16_as_uuid"`
	DatetimeAsTimestamptz bool              `toml:"datetime_as_timestamptz"`
	JSONAsJSONB           bool              `toml:"json_as_jsonb"`
	EnumMode              string            `toml:"enum_mode"` // text|check
	SetMode               string            `toml:"set_mode"`  // text|text_array
	WidenUnsignedIntegers bool              `toml:"widen_unsigned_integers"`
	VarcharAsText         bool              `toml:"varchar_as_text"`
	SanitizeJSONNullBytes bool              `toml:"sanitize_json_null_bytes"`
	UnknownAsText         bool              `toml:"unknown_as_text"`
	CollationMode         string            `toml:"collation_mode"` // none|auto
	CollationMap          map[string]string `toml:"collation_map"`  // MySQL collation → PG collation overrides
}

// loadConfig reads a TOML config file and returns a MigrationConfig with defaults applied.
func loadConfig(path string) (*MigrationConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := MigrationConfig{
		OnSchemaExists:     "error",
		SourceSnapshotMode: "none",
		PreserveDefaults:     true,
		CleanOrphans:         true,
		SnakeCaseIdentifiers: true,
		TypeMapping:        defaultTypeMappingConfig(),
	}
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		keys := make([]string, len(unknown))
		for i, k := range unknown {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("unknown config keys: %s", strings.Join(keys, ", "))
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	cfg.configDir = filepath.Dir(absPath)

	if cfg.Workers <= 0 {
		cfg.Workers = defaultWorkers()
	}

	cfg.Schema = strings.TrimSpace(cfg.Schema)
	if cfg.Schema == "" {
		return nil, fmt.Errorf("schema is required")
	}

	if cfg.OnSchemaExists == "" {
		cfg.OnSchemaExists = "error"
	}
	switch cfg.OnSchemaExists {
	case "error", "recreate":
	default:
		return nil, fmt.Errorf("on_schema_exists must be one of: error, recreate")
	}
	switch cfg.SourceSnapshotMode {
	case "none", "single_tx":
	default:
		return nil, fmt.Errorf("source_snapshot_mode must be one of: none, single_tx")
	}
	switch cfg.TypeMapping.EnumMode {
	case "text", "check":
	default:
		return nil, fmt.Errorf("type_mapping.enum_mode must be one of: text, check")
	}
	switch cfg.TypeMapping.SetMode {
	case "text", "text_array":
	default:
		return nil, fmt.Errorf("type_mapping.set_mode must be one of: text, text_array")
	}
	switch cfg.TypeMapping.CollationMode {
	case "none", "auto":
	default:
		return nil, fmt.Errorf("type_mapping.collation_mode must be one of: none, auto")
	}

	if cfg.SchemaOnly && cfg.DataOnly {
		return nil, fmt.Errorf("schema_only and data_only are mutually exclusive")
	}

	// Source validation
	if cfg.Source.Type == "" {
		return nil, fmt.Errorf("source.type is required (must be mysql or sqlite)")
	}
	src, err := newSourceDB(cfg.Source.Type)
	if err != nil {
		return nil, err
	}
	if cfg.Source.Charset == "" {
		cfg.Source.Charset = "utf8mb4"
	}
	if cfg.Source.DSN == "" {
		return nil, fmt.Errorf("source.dsn is required")
	}

	// Source-specific snapshot validation
	if cfg.SourceSnapshotMode == "single_tx" && !src.SupportsSnapshotMode() {
		return nil, fmt.Errorf("source_snapshot_mode \"single_tx\" is not supported for %s sources", cfg.Source.Type)
	}

	// Source-specific charset validation (charset is MySQL-only)
	if cfg.Source.Type == "sqlite" && cfg.Source.Charset != "utf8mb4" {
		return nil, fmt.Errorf("source.charset is a MySQL-only option")
	}

	// Source-specific type mapping validation
	if err := src.ValidateTypeMapping(cfg.TypeMapping); err != nil {
		return nil, err
	}

	// Cap workers based on source limits
	if max := src.MaxWorkers(); max > 0 && cfg.Workers > max {
		cfg.Workers = max
	}

	if cfg.Target.DSN == "" {
		return nil, fmt.Errorf("target.dsn is required")
	}

	return &cfg, nil
}

// resolvePath resolves a path relative to the config file directory.
func (c *MigrationConfig) resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.configDir, p)
}

func defaultWorkers() int {
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	if n > 8 {
		return 8
	}
	return n
}

func defaultTypeMappingConfig() TypeMappingConfig {
	return TypeMappingConfig{
		TinyInt1AsBoolean:     false,
		Binary16AsUUID:        false,
		DatetimeAsTimestamptz: false,
		JSONAsJSONB:           false,
		EnumMode:              "text",
		SetMode:               "text",
		WidenUnsignedIntegers: true,
		SanitizeJSONNullBytes: true,
		UnknownAsText:         false,
		CollationMode:         "none",
	}
}
