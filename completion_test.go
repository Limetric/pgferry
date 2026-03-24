package main

import (
	"bytes"
	"strings"
	"testing"
)

// Smoke-test Cobra's default completion command: pgferry does not register a custom
// "completion" command; Cobra still generates the standard shell script.
func TestCompletionBashOutput(t *testing.T) {
	var buf bytes.Buffer
	if err := rootCmd.GenBashCompletionV2(&buf, true); err != nil {
		t.Fatalf("GenBashCompletionV2() error = %v", err)
	}
	out := buf.String()
	if len(out) == 0 {
		t.Fatal("completion bash output is empty")
	}
	// GenBashCompletionV2 does not list subcommands in the script; it shells out to
	// `pgferry __complete ...`. These strings are stable markers of a valid V2 script.
	for _, want := range []string{
		"bash completion V2",
		"__complete",
		"pgferry",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("completion bash output missing %q", want)
		}
	}
}
