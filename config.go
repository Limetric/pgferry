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
	MySQL                             MySQLConfig       `toml:"mysql"`
	Postgres                          PostgresConfig    `toml:"postgres"`
	Schema                            string            `toml:"schema"`
	OnSchemaExists                    string            `toml:"on_schema_exists"`
	UnloggedTables                    bool              `toml:"unlogged_tables"`
	ReplicateOnUpdateCurrentTimestamp bool              `toml:"replicate_on_update_current_timestamp"`
	Workers                           int               `toml:"workers"`
	Hooks                             HooksConfig       `toml:"hooks"`
	TypeMapping                       TypeMappingConfig `toml:"type_mapping"`

	// configDir is the directory containing the TOML file, used to resolve relative SQL paths.
	configDir string
}

type MySQLConfig struct {
	DSN string `toml:"dsn"`
}

type PostgresConfig struct {
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
	TinyInt1AsBoolean     bool `toml:"tinyint1_as_boolean"`
	Binary16AsUUID        bool `toml:"binary16_as_uuid"`
	DatetimeAsTimestamptz bool `toml:"datetime_as_timestamptz"`
	JSONAsJSONB           bool `toml:"json_as_jsonb"`
	SanitizeJSONNullBytes bool `toml:"sanitize_json_null_bytes"`
	UnknownAsText         bool `toml:"unknown_as_text"`
}

// loadConfig reads a TOML config file and returns a MigrationConfig with defaults applied.
func loadConfig(path string) (*MigrationConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := MigrationConfig{
		OnSchemaExists: "error",
		TypeMapping:    defaultTypeMappingConfig(),
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
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

	if cfg.MySQL.DSN == "" {
		return nil, fmt.Errorf("mysql.dsn is required")
	}
	if cfg.Postgres.DSN == "" {
		return nil, fmt.Errorf("postgres.dsn is required")
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
		SanitizeJSONNullBytes: true,
		UnknownAsText:         false,
	}
}
