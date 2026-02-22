package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// MigrationConfig holds the full TOML-driven migration configuration.
type MigrationConfig struct {
	MySQL          MySQLConfig    `toml:"mysql"`
	Postgres       PostgresConfig `toml:"postgres"`
	Schema         string         `toml:"schema"`
	OnSchemaExists string         `toml:"on_schema_exists"`
	UnloggedTables bool           `toml:"unlogged_tables"`
	Workers        int            `toml:"workers"`
	Hooks          HooksConfig    `toml:"hooks"`

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

// loadConfig reads a TOML config file and returns a MigrationConfig with defaults applied.
func loadConfig(path string) (*MigrationConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg MigrationConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	cfg.configDir = filepath.Dir(absPath)

	if cfg.Workers <= 0 {
		cfg.Workers = 4
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
