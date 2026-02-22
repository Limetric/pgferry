package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type Config struct {
	MySQLDSN    string
	PostgresDSN string
	Workers     int
	BatchSize   int
	Schema      string
}

var cfg Config

var rootCmd = &cobra.Command{
	Use:   "pgferry",
	Short: "MySQL to PostgreSQL migration tool",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("pgferry — MySQL → PostgreSQL migration")
		return nil
	},
}

func init() {
	rootCmd.Flags().StringVar(&cfg.MySQLDSN, "mysql", "", "MySQL DSN (e.g. root:root@tcp(127.0.0.1:3306)/dbname)")
	rootCmd.Flags().StringVar(&cfg.PostgresDSN, "postgres", "", "PostgreSQL DSN (e.g. postgres://user:pass@host:5432/dbname)")
	rootCmd.Flags().IntVar(&cfg.Workers, "workers", 4, "number of parallel table migrations")
	rootCmd.Flags().IntVar(&cfg.BatchSize, "batch-size", 50000, "rows per SELECT batch")
	rootCmd.Flags().StringVar(&cfg.Schema, "schema", "app", "target PostgreSQL schema")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
