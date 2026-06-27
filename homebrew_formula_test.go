package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestGenerateHomebrewFormula(t *testing.T) {
	cmd := exec.Command(
		"bash",
		"scripts/generate-homebrew-formula.sh",
		"v5.4.0",
		"darwin-amd64-sha",
		"darwin-arm64-sha",
		"linux-amd64-sha",
		"linux-arm64-sha",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generate-homebrew-formula.sh failed: %v\n%s", err, out)
	}

	got := string(out)
	wantContains := []string{
		`class Pgferry < Formula`,
		`desc "Migrate MySQL, MariaDB, SQLite, or MSSQL databases to PostgreSQL"`,
		`homepage "https://www.pgferry.com"`,
		`version "5.4.0"`,
		`license "Apache-2.0"`,
		`url "https://github.com/Limetric/pgferry/releases/download/v5.4.0/pgferry-darwin-arm64"`,
		`sha256 "darwin-arm64-sha"`,
		`url "https://github.com/Limetric/pgferry/releases/download/v5.4.0/pgferry-darwin-amd64"`,
		`sha256 "darwin-amd64-sha"`,
		`url "https://github.com/Limetric/pgferry/releases/download/v5.4.0/pgferry-linux-arm64"`,
		`sha256 "linux-arm64-sha"`,
		`url "https://github.com/Limetric/pgferry/releases/download/v5.4.0/pgferry-linux-amd64"`,
		`sha256 "linux-amd64-sha"`,
		`binary = Dir["pgferry-*"].first`,
		`chmod 0755, binary`,
		`bin.install binary => "pgferry"`,
		`system "#{bin}/pgferry", "version"`,
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("generated formula missing %q\n%s", want, got)
		}
	}
}
