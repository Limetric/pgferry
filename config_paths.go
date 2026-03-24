package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var configPathsJSON bool
var configPathsConfigPath string

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect migration configuration files",
}

var configPathsCmd = &cobra.Command{
	Use:   "paths [migration.toml]",
	Short: "Print resolved paths for the config file, checkpoint, and hook SQL files",
	Long: `Load a migration TOML (same validation as migrate) and print absolute paths for
the config file, the config directory, the resume checkpoint file, and each hook
SQL path. Does not connect to any database.

Pass the config as a positional argument or via --config, not both.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runConfigPaths,
}

func init() {
	configPathsCmd.Flags().BoolVar(&configPathsJSON, "json", false, "print machine-readable JSON instead of text lines")
	configPathsCmd.Flags().StringVar(&configPathsConfigPath, "config", "", "path to migration TOML config file")
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configPathsCmd)
}

func runConfigPaths(cmd *cobra.Command, args []string) error {
	flagPath := strings.TrimSpace(configPathsConfigPath)
	var posPath string
	if len(args) > 0 {
		posPath = strings.TrimSpace(args[0])
	}
	if flagPath != "" && posPath != "" {
		return fmt.Errorf("provide either a positional migration.toml path or --config, not both")
	}
	cfgPath := flagPath
	if cfgPath == "" {
		cfgPath = posPath
	}
	if cfgPath == "" {
		return fmt.Errorf("migration config path required: pgferry config paths <migration.toml> or pgferry config paths --config <path>")
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	absCfg, err := filepath.Abs(cfgPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	cp := checkpointPath(cfg.configDir)

	out := cmd.OutOrStdout()
	if configPathsJSON {
		return writeConfigPathsJSON(out, absCfg, cfg, cp)
	}
	return writeConfigPathsText(out, absCfg, cfg, cp)
}

type hookPathEntry struct {
	ConfigPath string `json:"config_path"`
	Resolved   string `json:"resolved"`
	Exists     bool   `json:"exists"`
}

type configPathsJSONOut struct {
	ConfigFile string `json:"config_file"`
	ConfigDir  string `json:"config_dir"`
	Checkpoint struct {
		Path   string `json:"path"`
		Exists bool   `json:"exists"`
	} `json:"checkpoint"`
	Hooks struct {
		BeforeData []hookPathEntry `json:"before_data"`
		AfterData  []hookPathEntry `json:"after_data"`
		BeforeFk   []hookPathEntry `json:"before_fk"`
		AfterAll   []hookPathEntry `json:"after_all"`
	} `json:"hooks"`
}

func writeConfigPathsJSON(out io.Writer, absCfg string, cfg *MigrationConfig, checkpoint string) error {
	var doc configPathsJSONOut
	doc.ConfigFile = absCfg
	doc.ConfigDir = cfg.configDir
	doc.Checkpoint.Path = checkpoint
	doc.Checkpoint.Exists = pathExists(checkpoint)
	doc.Hooks.BeforeData = hookPathEntries(cfg, cfg.Hooks.BeforeData)
	doc.Hooks.AfterData = hookPathEntries(cfg, cfg.Hooks.AfterData)
	doc.Hooks.BeforeFk = hookPathEntries(cfg, cfg.Hooks.BeforeFk)
	doc.Hooks.AfterAll = hookPathEntries(cfg, cfg.Hooks.AfterAll)

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func hookPathEntries(cfg *MigrationConfig, rels []string) []hookPathEntry {
	if len(rels) == 0 {
		return nil
	}
	out := make([]hookPathEntry, 0, len(rels))
	for _, r := range rels {
		resolved := cfg.resolvePath(r)
		out = append(out, hookPathEntry{
			ConfigPath: r,
			Resolved:   resolved,
			Exists:     pathExists(resolved),
		})
	}
	return out
}

func writeConfigPathsText(out io.Writer, absCfg string, cfg *MigrationConfig, checkpoint string) error {
	fmt.Fprintf(out, "config_file: %s\n", absCfg)
	fmt.Fprintf(out, "config_dir: %s\n", cfg.configDir)
	fmt.Fprintf(out, "checkpoint: %s  exists: %s\n", checkpoint, yesNo(pathExists(checkpoint)))
	fmt.Fprintln(out)

	phases := []struct {
		name  string
		files []string
	}{
		{"hooks.before_data", cfg.Hooks.BeforeData},
		{"hooks.after_data", cfg.Hooks.AfterData},
		{"hooks.before_fk", cfg.Hooks.BeforeFk},
		{"hooks.after_all", cfg.Hooks.AfterAll},
	}
	for _, ph := range phases {
		fmt.Fprintf(out, "%s:\n", ph.name)
		if len(ph.files) == 0 {
			fmt.Fprint(out, "  (none)\n")
			continue
		}
		for _, f := range ph.files {
			resolved := cfg.resolvePath(f)
			fmt.Fprintf(out, "  %s  %s  exists: %s\n", f, resolved, yesNo(pathExists(resolved)))
		}
	}
	return nil
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
