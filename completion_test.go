package main

import (
	"bytes"
	"strings"
	"testing"
)

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
	for _, want := range []string{"migrate", "plan", "wizard", "version", "completion", "--config"} {
		if !strings.Contains(out, want) {
			t.Fatalf("completion bash output missing %q", want)
		}
	}
}
