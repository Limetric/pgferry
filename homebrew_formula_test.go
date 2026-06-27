package main

import (
	"os"
	"os/exec"
	"path/filepath"
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

func TestHomebrewFormulaHasChangesDetectsUntrackedFormula(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "Formula", "pgferry.rb"), "class Pgferry < Formula\nend\n")

	cmd := exec.Command("bash", "scripts/homebrew-formula-has-changes.sh", repo, "Formula/pgferry.rb")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected untracked formula to be detected as changed: %v\n%s", err, out)
	}
}

func TestHomebrewFormulaHasChangesIgnoresCleanTrackedFormula(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "Formula", "pgferry.rb"), "class Pgferry < Formula\nend\n")
	runGit(t, repo, "add", "Formula/pgferry.rb")
	runGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Add formula")

	cmd := exec.Command("bash", "scripts/homebrew-formula-has-changes.sh", repo, "Formula/pgferry.rb")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected clean tracked formula to be detected as unchanged\n%s", out)
	}
}

func TestHomebrewFormulaHasChangesDetectsModifiedFormula(t *testing.T) {
	repo := initGitRepo(t)
	formula := filepath.Join(repo, "Formula", "pgferry.rb")
	writeFile(t, formula, "class Pgferry < Formula\nend\n")
	runGit(t, repo, "add", "Formula/pgferry.rb")
	runGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Add formula")
	writeFile(t, formula, "class Pgferry < Formula\n  version \"6.0.1\"\nend\n")

	cmd := exec.Command("bash", "scripts/homebrew-formula-has-changes.sh", repo, "Formula/pgferry.rb")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected modified formula to be detected as changed: %v\n%s", err, out)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	cmd := exec.Command("git", "init", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	return dir
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()

	cmdArgs := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}
