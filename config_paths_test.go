package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPaths_OutputContainsResolvedPaths(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "migration.toml")

	if err := os.WriteFile(filepath.Join(dir, "before.sql"), []byte("--"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "after.sql"), []byte("--"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fk.sql"), []byte("--"), 0644); err != nil {
		t.Fatal(err)
	}
	// missing.sql intentionally absent

	content := `
schema = "app"

[source]
type = "mysql"
dsn = "user:secret@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://other:secret@localhost:5432/pg"

[hooks]
before_data = ["before.sql"]
after_data = ["after.sql"]
before_fk = ["fk.sql", "missing.sql"]
after_all = []
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	// Reset flags that other tests might leave set
	prevJSON := configPathsJSON
	prevCfg := configPathsConfigPath
	if err := configPathsCmd.Flags().Set("json", "false"); err != nil {
		t.Fatal(err)
	}
	if err := configPathsCmd.Flags().Set("config", ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		configPathsJSON = prevJSON
		configPathsConfigPath = prevCfg
		if err := configPathsCmd.Flags().Set("json", "false"); err != nil {
			t.Fatal(err)
		}
		if err := configPathsCmd.Flags().Set("config", ""); err != nil {
			t.Fatal(err)
		}
	})

	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"config", "paths", cfgFile})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "secret") {
		t.Fatalf("output must not contain DSN secrets; got substring leak")
	}
	if !strings.Contains(out, cfgFile) {
		t.Fatalf("output should contain absolute config path %q", cfgFile)
	}
	if !strings.Contains(out, dir) {
		t.Fatalf("output should contain config dir %q", dir)
	}
	wantCP := filepath.Join(dir, "pgferry_checkpoint.json")
	if !strings.Contains(out, wantCP) {
		t.Fatalf("output should contain checkpoint path %q", wantCP)
	}
	if !strings.Contains(out, filepath.Join(dir, "before.sql")) {
		t.Fatalf("output should contain resolved before.sql path")
	}
	if !strings.Contains(out, "missing.sql") || !strings.Contains(out, "exists: no") {
		t.Fatalf("missing hook should be listed with exists: no")
	}
	if !strings.Contains(out, "exists: yes") {
		t.Fatalf("expected at least one exists: yes for present hook files")
	}
	if !strings.Contains(out, "hooks.after_all:") || !strings.Contains(out, "  (none)") {
		t.Fatalf("empty after_all should list (none); got:\n%s", out)
	}
}

func TestConfigPaths_JSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "m.toml")
	if err := os.WriteFile(filepath.Join(dir, "a.sql"), []byte("--"), 0644); err != nil {
		t.Fatal(err)
	}
	content := `
schema = "s"

[source]
type = "sqlite"
dsn = "` + filepath.Join(dir, "src.db") + `"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/t?sslmode=disable"

[hooks]
before_data = ["a.sql"]
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	prevJSON := configPathsJSON
	prevCfg := configPathsConfigPath
	if err := configPathsCmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		configPathsJSON = prevJSON
		configPathsConfigPath = prevCfg
		if err := configPathsCmd.Flags().Set("json", "false"); err != nil {
			t.Fatal(err)
		}
	})

	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"config", "paths", "--json", cfgFile})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var doc configPathsJSONOut
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("json decode: %v\n%s", err, buf.String())
	}
	if doc.ConfigFile != cfgFile {
		t.Fatalf("config_file = %q, want %q", doc.ConfigFile, cfgFile)
	}
	if doc.ConfigDir != dir {
		t.Fatalf("config_dir = %q, want %q", doc.ConfigDir, dir)
	}
	if doc.Checkpoint.Path != filepath.Join(dir, "pgferry_checkpoint.json") {
		t.Fatalf("checkpoint path = %q", doc.Checkpoint.Path)
	}
	if len(doc.Hooks.BeforeData) != 1 || doc.Hooks.BeforeData[0].ConfigPath != "a.sql" || !doc.Hooks.BeforeData[0].Exists {
		t.Fatalf("hooks.before_data = %+v", doc.Hooks.BeforeData)
	}
}

func TestConfigPaths_UnknownKeysRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad.toml")
	content := `
schema = "s"
unknown_top = true

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := configPathsCmd.Flags().Set("json", "false"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"config", "paths", cfgFile})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown keys")
	}
	if !strings.Contains(err.Error(), "unknown config keys") {
		t.Fatalf("error = %v, want unknown config keys", err)
	}
}
