package main

import "testing"

func TestSourceObjectWarnings(t *testing.T) {
	objs := &SourceObjects{
		Views:    []string{"v_users"},
		Routines: []string{"FUNCTION calc_score", "PROCEDURE sync_data"},
		Triggers: []string{"trg_users_touch"},
	}

	warnings := sourceObjectWarnings(objs)
	if len(warnings) != 5 {
		t.Fatalf("warnings len = %d, want 5 (%v)", len(warnings), warnings)
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
