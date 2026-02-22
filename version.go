package main

import "strings"

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

func versionString() string {
	return formatVersion(buildVersion, buildCommit)
}

func formatVersion(version, commit string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		v = "dev"
	}
	if v != "dev" {
		return v
	}

	c := shortCommit(commit)
	if c == "" {
		return "dev"
	}
	return "dev-" + c
}

func shortCommit(commit string) string {
	c := strings.TrimSpace(commit)
	if c == "" || c == "unknown" {
		return ""
	}
	if len(c) > 7 {
		return c[:7]
	}
	return c
}
