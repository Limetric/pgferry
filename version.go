package main

import (
	"runtime"
	"strings"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	// buildDate is set at link time via -ldflags '-X main.buildDate=...' (optional).
	buildDate = ""
)

func versionString() string {
	return formatVersion(buildVersion, buildCommit)
}

func versionVerboseString() string {
	return formatVersionVerbose(buildVersion, buildCommit, buildDate, runtime.Version())
}

func formatVersionVerbose(version, commit, date, goVer string) string {
	var b strings.Builder
	b.WriteString("Version: ")
	b.WriteString(formatVersion(version, commit))
	b.WriteByte('\n')
	c := strings.TrimSpace(commit)
	if c != "" && c != "unknown" {
		b.WriteString("Commit: ")
		b.WriteString(c)
		b.WriteByte('\n')
	}
	if d := strings.TrimSpace(date); d != "" {
		b.WriteString("Build date: ")
		b.WriteString(d)
		b.WriteByte('\n')
	}
	b.WriteString("Go: ")
	b.WriteString(goVer)
	return b.String()
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
