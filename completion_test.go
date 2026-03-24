package main

import (
	"bytes"
	"strings"
	"testing"
)

// Smoke-test Cobra's InitDefaultCompletionCmd: pgferry does not register a custom
// "completion" command; Cobra injects completion bash|zsh|fish|powershell on Execute.
func TestCompletionBashOutput(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"completion", "bash"})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
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
