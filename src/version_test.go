package main

import "testing"

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
