package main

import (
	"strings"
	"testing"
)

func TestSourceObjectWarnings(t *testing.T) {
	objs := &SourceObjects{
		Views:    []SourceView{{Name: "v_users"}},
		Routines: []SourceRoutine{{Name: "calc_score", Type: "FUNCTION"}, {Name: "sync_data", Type: "PROCEDURE"}},
		Triggers: []SourceTrigger{{Name: "trg_users_touch", Table: "users"}},
	}

	warnings := sourceObjectWarnings(objs)
	if len(warnings) != 5 {
		t.Fatalf("warnings len = %d, want 5 (%v)", len(warnings), warnings)
	}
	if !strings.Contains(warnings[4], "trg_users_touch") || !strings.Contains(warnings[4], "users") {
		t.Fatalf("trigger warning should name table, got %q", warnings[4])
	}
	if warnings[0] == "" {
		t.Fatal("summary warning should not be empty")
	}
}

func TestSourceObjectWarnings_Empty(t *testing.T) {
	warnings := sourceObjectWarnings(&SourceObjects{})
	if len(warnings) != 0 {
		t.Fatalf("warnings len = %d, want 0 (%v)", len(warnings), warnings)
	}
}
