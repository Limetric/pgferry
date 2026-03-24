package main

import (
	"strings"
	"testing"
)

func TestFormatVersionVerbose(t *testing.T) {
	tests := []struct {
		name    string
		version string
		commit  string
		date    string
		goVer   string
		want    []string
	}{
		{
			name:    "release omits commit line uses version only",
			version: "v1.0.0",
			commit:  "abc123",
			date:    "",
			goVer:   "go1.22.0",
			want: []string{
				"Version: v1.0.0",
				"Commit: abc123",
				"Go: go1.22.0",
			},
		},
		{
			name:    "unknown commit skips commit line",
			version: "dev",
			commit:  "unknown",
			date:    "",
			goVer:   "go1.22.0",
			want: []string{
				"Version: dev",
				"Go: go1.22.0",
			},
		},
		{
			name:    "build date included when set",
			version: "dev",
			commit:  "0123456789abcdef",
			date:    "2024-06-01",
			goVer:   "go1.23.0",
			want: []string{
				"Version: dev-0123456",
				"Commit: 0123456789abcdef",
				"Build date: 2024-06-01",
				"Go: go1.23.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVersionVerbose(tt.version, tt.commit, tt.date, tt.goVer)
			lines := strings.Split(strings.TrimSpace(got), "\n")
			if len(lines) != len(tt.want) {
				t.Fatalf("got %d lines %q, want %d lines %q", len(lines), got, len(tt.want), tt.want)
			}
			for i := range lines {
				if lines[i] != tt.want[i] {
					t.Fatalf("line %d = %q, want %q", i, lines[i], tt.want[i])
				}
			}
		})
	}
}

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		commit  string
		want    string
	}{
		{
			name:    "release tag returned as-is",
			version: "v1.2.3",
			commit:  "0123456789abcdef",
			want:    "v1.2.3",
		},
		{
			name:    "dev with commit uses short sha",
			version: "dev",
			commit:  "0123456789abcdef",
			want:    "dev-0123456",
		},
		{
			name:    "dev with unknown commit",
			version: "dev",
			commit:  "unknown",
			want:    "dev",
		},
		{
			name:    "empty version falls back to dev",
			version: "",
			commit:  "abcdef1",
			want:    "dev-abcdef1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVersion(tt.version, tt.commit)
			if got != tt.want {
				t.Fatalf("formatVersion(%q, %q) = %q, want %q", tt.version, tt.commit, got, tt.want)
			}
		})
	}
}
