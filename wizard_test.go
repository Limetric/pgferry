package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunGenerateWizardWritesConfig(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "migration.toml")
	out := &bytes.Buffer{}

	input := strings.Join([]string{
		"sqlite",
		"/tmp/source.db",
		"n",
		"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable",
		"n",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		outputPath,
		"stop",
	}, "\n") + "\n"

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(out)

	if err := runGenerateWizard(cmd, nil); err != nil {
		t.Fatalf("runGenerateWizard() error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, `schema = "source"`) {
		t.Fatalf("generated config missing schema, got:\n%s", text)
	}
	if !strings.Contains(text, "[source]\ntype = \"sqlite\"") {
		t.Fatalf("generated config missing source section, got:\n%s", text)
	}
	if !strings.Contains(text, "[target]\ndsn = \"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable\"") {
		t.Fatalf("generated config missing target section, got:\n%s", text)
	}
	output := out.String()
	if !strings.Contains(output, "Example: /path/to/database.db") {
		t.Fatalf("wizard output missing sqlite DSN example, got:\n%s", output)
	}
	if !strings.Contains(output, "Example: postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable") {
		t.Fatalf("wizard output missing postgres DSN example, got:\n%s", output)
	}
	if !strings.Contains(output, "Advanced options not covered by the wizard:") {
		t.Fatalf("wizard output missing advanced options note, got:\n%s", output)
	}
}

func TestRunGenerateWizardRunsGeneratedConfig(t *testing.T) {
	dir := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prevWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	var gotCfg *MigrationConfig
	prevRunner := generatedConfigRunner
	generatedConfigRunner = func(cfg *MigrationConfig) error {
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() {
		generatedConfigRunner = prevRunner
	})

	input := strings.Join([]string{
		"",                                     // source type = mysql
		"root:root@tcp(127.0.0.1:3306)/sakila", // source DSN
		"n",                                    // skip source connection test
		"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable", // target DSN
		"n",   // skip target connection test
		"",    // schema = sakila
		"",    // migration mode = full
		"",    // on_schema_exists = error
		"",    // source_snapshot_mode = none
		"",    // unlogged_tables = false
		"",    // preserve_defaults = true
		"",    // snake_case_identifiers = true
		"",    // clean_orphans = true
		"",    // workers = default
		"",    // json_as_jsonb = false
		"",    // unknown_as_text = false
		"",    // tinyint1_as_boolean = false
		"",    // datetime_as_timestamptz = false
		"",    // binary16_as_uuid = false (skips binary16_uuid_mode)
		"",    // string_uuid_as_uuid = false
		"",    // enum_mode = text
		"",    // set_mode = text
		"",    // bit_mode = bytea
		"",    // time_mode = time
		"",    // zero_date_mode = null
		"",    // spatial_mode = off
		"",    // add_unsigned_checks = false
		"",    // replicate_on_update_current_timestamp = false
		"n",   // do not save generated config
		"run", // next step
	}, "\n") + "\n"

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)

	if err := runGenerateWizard(cmd, nil); err != nil {
		t.Fatalf("runGenerateWizard() error: %v", err)
	}
	if gotCfg == nil {
		t.Fatal("expected generated config runner to be called")
	}
	if gotCfg.Source.Type != "mysql" {
		t.Fatalf("Source.Type = %q, want mysql", gotCfg.Source.Type)
	}
	if gotCfg.Schema != "sakila" {
		t.Fatalf("Schema = %q, want sakila", gotCfg.Schema)
	}
	if gotCfg.Workers != effectiveDefaultWorkers("mysql") {
		t.Fatalf("Workers = %d, want %d", gotCfg.Workers, effectiveDefaultWorkers("mysql"))
	}
	if gotCfg.configDir != dir {
		t.Fatalf("configDir = %q, want %q", gotCfg.configDir, dir)
	}
	output := out.String()
	if !strings.Contains(output, "If target schema already exists (default: error)\n  error: Safest default. Stops instead of touching an existing schema.\n  recreate: Drops and recreates the target schema. Fast for clean reruns, but destructive.\nChoice [error/recreate] [error]: ") {
		t.Fatalf("wizard output missing improved schema-exists choice layout, got:\n%s", output)
	}
	if !strings.Contains(output, "Map JSON columns to jsonb\n  jsonb is usually the better PostgreSQL type for indexing and querying.") {
		t.Fatalf("wizard output missing jsonb guidance block, got:\n%s", output)
	}
	if !strings.Contains(output, "Answer [y/N]: \nMap unknown source types to text instead of failing") {
		t.Fatalf("wizard output missing blank-line separation between prompt blocks, got:\n%s", output)
	}
}

func TestRunGenerateWizardRePromptsInvalidSourceDSN(t *testing.T) {
	var out bytes.Buffer
	lines := []string{
		"",           // source type = mysql
		"://bad-dsn", // invalid source DSN
		"root:root@tcp(127.0.0.1:3306)/sakila",
		"n",
		"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable",
		"n",
	}
	lines = append(lines, wizardBlankInputs(23)...)
	lines = append(lines, "n", "stop")
	input := strings.Join(lines, "\n") + "\n"

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)

	if err := runGenerateWizard(cmd, nil); err != nil {
		t.Fatalf("runGenerateWizard() error: %v", err)
	}

	if !strings.Contains(out.String(), "invalid MySQL DSN") {
		t.Fatalf("expected invalid DSN message, got:\n%s", out.String())
	}
}

func TestRunGenerateWizardTestsConnectionsByDefault(t *testing.T) {
	dir := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prevWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	prevSourceTester := wizardSourceConnectionTester
	prevTargetTester := wizardTargetConnectionTester
	wizardSourceConnectionTester = func(sourceType, dsn string) error {
		if sourceType != "mysql" {
			t.Fatalf("sourceType = %q, want mysql", sourceType)
		}
		if dsn != "root:root@tcp(127.0.0.1:3306)/sakila" {
			t.Fatalf("source dsn = %q", dsn)
		}
		return nil
	}
	wizardTargetConnectionTester = func(dsn string) error {
		if dsn != "postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable" {
			t.Fatalf("target dsn = %q", dsn)
		}
		return nil
	}
	t.Cleanup(func() {
		wizardSourceConnectionTester = prevSourceTester
		wizardTargetConnectionTester = prevTargetTester
	})

	prevRunner := generatedConfigRunner
	generatedConfigRunner = func(cfg *MigrationConfig) error { return nil }
	t.Cleanup(func() {
		generatedConfigRunner = prevRunner
	})

	var out bytes.Buffer
	lines := []string{
		"",
		"root:root@tcp(127.0.0.1:3306)/sakila",
		"",
		"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable",
		"",
	}
	lines = append(lines, wizardBlankInputs(23)...)
	lines = append(lines, "n", "run")
	input := strings.Join(lines, "\n") + "\n"

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)

	if err := runGenerateWizard(cmd, nil); err != nil {
		t.Fatalf("runGenerateWizard() error: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "Source connection OK.") {
		t.Fatalf("expected source connection success message, got:\n%s", text)
	}
	if !strings.Contains(text, "PostgreSQL connection OK.") {
		t.Fatalf("expected target connection success message, got:\n%s", text)
	}
}

func TestRunGenerateWizardRunsPlanFromGeneratedConfig(t *testing.T) {
	dir := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prevWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	var gotCfg *MigrationConfig
	var gotOut string
	prevPlanner := generatedConfigPlanner
	generatedConfigPlanner = func(cfg *MigrationConfig, out io.Writer) error {
		gotCfg = cfg
		buf := new(bytes.Buffer)
		if _, err := io.Copy(buf, strings.NewReader("plan ok\n")); err != nil {
			return err
		}
		gotOut = buf.String()
		_, err := fmt.Fprint(out, gotOut)
		return err
	}
	t.Cleanup(func() {
		generatedConfigPlanner = prevPlanner
	})

	var out bytes.Buffer
	input := strings.Join([]string{
		"sqlite",
		"/tmp/source.db",
		"n",
		"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable",
		"n",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"n",
		"plan",
	}, "\n") + "\n"

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)

	if err := runGenerateWizard(cmd, nil); err != nil {
		t.Fatalf("runGenerateWizard() error: %v", err)
	}
	if gotCfg == nil {
		t.Fatal("expected generated config planner to be called")
	}
	if !strings.Contains(out.String(), "Generating migration plan...") {
		t.Fatalf("expected plan start message, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "plan ok") {
		t.Fatalf("expected planner output, got:\n%s", out.String())
	}
}

func TestRenderConfigTOMLIncludesOnlyConfiguredOverrides(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mysql"
	cfg.Source.DSN = "root:root@tcp(127.0.0.1:3306)/sakila"
	cfg.Target.DSN = "postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable"
	cfg.Schema = "sakila"
	cfg.OnSchemaExists = "recreate"
	cfg.TypeMapping.JSONAsJSONB = true
	cfg.TypeMapping.UnknownAsText = true
	cfg.TypeMapping.TinyInt1AsBoolean = true
	cfg.Workers = 2

	rendered := renderConfigTOML(&cfg)

	if !strings.Contains(rendered, `on_schema_exists = "recreate"`) {
		t.Fatalf("expected on_schema_exists override, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "workers = 2") {
		t.Fatalf("expected workers override, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[type_mapping]") {
		t.Fatalf("expected type_mapping section, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "json_as_jsonb = true") {
		t.Fatalf("expected json_as_jsonb override, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "tinyint1_as_boolean = true") {
		t.Fatalf("expected tinyint1_as_boolean override, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "preserve_defaults = true") {
		t.Fatalf("did not expect default preserve_defaults in output, got:\n%s", rendered)
	}
}

func TestSuggestSchemaNameFallsBackWhenItMatchesTargetDatabase(t *testing.T) {
	got := suggestSchemaName(
		"mysql",
		"root:root@tcp(127.0.0.1:3306)/sakila",
		"postgres://postgres:postgres@127.0.0.1:5432/sakila?sslmode=disable",
	)
	if got != "app" {
		t.Fatalf("suggestSchemaName() = %q, want app", got)
	}
}

func TestSuggestSchemaNameUsesSourceDatabaseWhenDifferentFromTargetDatabase(t *testing.T) {
	got := suggestSchemaName(
		"mysql",
		"root:root@tcp(127.0.0.1:3306)/sakila",
		"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable",
	)
	if got != "sakila" {
		t.Fatalf("suggestSchemaName() = %q, want sakila", got)
	}
}

func wizardBlankInputs(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = ""
	}
	return lines
}

func TestRenderConfigTOMLIncludesNewTypeMappingOptions(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mysql"
	cfg.Source.DSN = "root:root@tcp(127.0.0.1:3306)/sakila"
	cfg.Target.DSN = "postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable"
	cfg.Schema = "sakila"
	cfg.TypeMapping.EnumMode = "native"
	cfg.TypeMapping.SetMode = "text_array_check"
	cfg.TypeMapping.BitMode = "varbit"
	cfg.TypeMapping.StringUUIDAsUUID = true
	cfg.TypeMapping.TimeMode = "interval"
	cfg.TypeMapping.ZeroDateMode = "error"
	cfg.TypeMapping.SpatialMode = "wkt_text"

	rendered := renderConfigTOML(&cfg)

	for _, want := range []string{
		`enum_mode = "native"`,
		`set_mode = "text_array_check"`,
		`bit_mode = "varbit"`,
		"string_uuid_as_uuid = true",
		`time_mode = "interval"`,
		`zero_date_mode = "error"`,
		`spatial_mode = "wkt_text"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expected %q in rendered config, got:\n%s", want, rendered)
		}
	}

	// Defaults should not appear
	for _, notWant := range []string{
		`bit_mode = "bytea"`,
		`time_mode = "time"`,
		`zero_date_mode = "null"`,
		`spatial_mode = "off"`,
	} {
		if strings.Contains(rendered, notWant) {
			t.Errorf("did not expect default %q in rendered config, got:\n%s", notWant, rendered)
		}
	}
}
