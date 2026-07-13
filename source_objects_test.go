package main

import (
	"encoding/json"
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

func TestSourceObjectWarnings_ReportsScheduledEvents(t *testing.T) {
	// MySQL/MariaDB scheduled events have no PostgreSQL equivalent. They used not to
	// be introspected at all, so a source using the event scheduler lost them with no
	// warning at all.
	warnings := sourceObjectWarnings(&SourceObjects{
		Events: []SourceEvent{{Name: "nightly_rollup"}, {Name: "purge_sessions"}},
	})
	if len(warnings) != 3 {
		t.Fatalf("warnings len = %d, want 3 (summary + 2 events): %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "2 events") {
		t.Errorf("summary should count events, got %q", warnings[0])
	}
	joined := strings.Join(warnings, "\n")
	for _, name := range []string{"nightly_rollup", "purge_sessions"} {
		if !strings.Contains(joined, name) {
			t.Errorf("warnings should name event %q, got:\n%s", name, joined)
		}
	}
}

func TestPlanSourceObjects_EventsRoundTripAndLegacyReportsLoad(t *testing.T) {
	// plan --input re-reads saved JSON reports. Reports saved before events existed
	// have no "events" key and must still decode.
	legacy := []byte(`{"views":[],"routines":[],"triggers":[]}`)
	var objs PlanSourceObjects
	if err := json.Unmarshal(legacy, &objs); err != nil {
		t.Fatalf("legacy report without an events key failed to decode: %v", err)
	}
	if objs.Events == nil || len(objs.Events) != 0 {
		t.Errorf("legacy report Events = %v, want an empty slice", objs.Events)
	}

	withEvents := PlanSourceObjects{
		Events: []PlanSourceEvent{{Name: "nightly_rollup", Dialect: "mysql", Definition: "-- body"}},
	}
	data, err := json.Marshal(withEvents)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"events"`) {
		t.Fatalf("marshaled report is missing the events key: %s", data)
	}

	var round PlanSourceObjects
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(round.Events) != 1 || round.Events[0].Name != "nightly_rollup" {
		t.Fatalf("events did not round-trip: %+v", round.Events)
	}
}
