package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRunDataMigrationPhase_DataOnlySuccess(t *testing.T) {
	var calls []string
	err := runDataMigrationPhase(
		true,
		func(string, ...any) {},
		func(enable bool) error {
			if enable {
				calls = append(calls, "enable")
			} else {
				calls = append(calls, "disable")
			}
			return nil
		},
		func() error {
			calls = append(calls, "before")
			return nil
		},
		func() error {
			calls = append(calls, "migrate")
			return nil
		},
		func() error {
			calls = append(calls, "after")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("runDataMigrationPhase() error: %v", err)
	}

	want := []string{"disable", "before", "migrate", "after", "enable"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
}

func TestRunDataMigrationPhase_DataOnlyBeforeFailureStillEnables(t *testing.T) {
	beforeErr := errors.New("before failed")
	enableErr := errors.New("enable failed")
	var calls []string

	err := runDataMigrationPhase(
		true,
		func(string, ...any) {},
		func(enable bool) error {
			if enable {
				calls = append(calls, "enable")
				return enableErr
			}
			calls = append(calls, "disable")
			return nil
		},
		func() error {
			calls = append(calls, "before")
			return beforeErr
		},
		func() error {
			calls = append(calls, "migrate")
			return nil
		},
		func() error {
			calls = append(calls, "after")
			return nil
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, beforeErr) {
		t.Fatalf("expected error to wrap beforeErr, got %v", err)
	}
	if !errors.Is(err, enableErr) {
		t.Fatalf("expected error to wrap enableErr, got %v", err)
	}

	want := []string{"disable", "before", "enable"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
}

func TestRunDataMigrationPhase_DataOnlyMigrateFailureStillEnables(t *testing.T) {
	migrateErr := errors.New("copy failed")
	var calls []string

	err := runDataMigrationPhase(
		true,
		func(string, ...any) {},
		func(enable bool) error {
			if enable {
				calls = append(calls, "enable")
			} else {
				calls = append(calls, "disable")
			}
			return nil
		},
		func() error {
			calls = append(calls, "before")
			return nil
		},
		func() error {
			calls = append(calls, "migrate")
			return migrateErr
		},
		func() error {
			calls = append(calls, "after")
			return nil
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, migrateErr) {
		t.Fatalf("expected error to wrap migrateErr, got %v", err)
	}

	want := []string{"disable", "before", "migrate", "enable"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
}

func TestRunDataMigrationPhase_DataOnlyAfterFailureStillEnables(t *testing.T) {
	afterErr := errors.New("after failed")
	var calls []string

	err := runDataMigrationPhase(
		true,
		func(string, ...any) {},
		func(enable bool) error {
			if enable {
				calls = append(calls, "enable")
			} else {
				calls = append(calls, "disable")
			}
			return nil
		},
		func() error {
			calls = append(calls, "before")
			return nil
		},
		func() error {
			calls = append(calls, "migrate")
			return nil
		},
		func() error {
			calls = append(calls, "after")
			return afterErr
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, afterErr) {
		t.Fatalf("expected error to wrap afterErr, got %v", err)
	}

	want := []string{"disable", "before", "migrate", "after", "enable"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
}

func TestRunDataMigrationPhase_DataOnlyDisableFailureStopsEarly(t *testing.T) {
	disableErr := errors.New("disable failed")
	var calls []string

	err := runDataMigrationPhase(
		true,
		func(string, ...any) {},
		func(enable bool) error {
			if enable {
				calls = append(calls, "enable")
			} else {
				calls = append(calls, "disable")
			}
			return disableErr
		},
		func() error {
			calls = append(calls, "before")
			return nil
		},
		func() error {
			calls = append(calls, "migrate")
			return nil
		},
		func() error {
			calls = append(calls, "after")
			return nil
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, disableErr) {
		t.Fatalf("expected error to wrap disableErr, got %v", err)
	}

	want := []string{"disable"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
}

func TestRunDataMigrationPhase_NonDataOnlySkipsTriggerCalls(t *testing.T) {
	var calls []string

	err := runDataMigrationPhase(
		false,
		func(string, ...any) {},
		func(enable bool) error {
			if enable {
				calls = append(calls, "enable")
			} else {
				calls = append(calls, "disable")
			}
			return nil
		},
		func() error {
			calls = append(calls, "before")
			return nil
		},
		func() error {
			calls = append(calls, "migrate")
			return nil
		},
		func() error {
			calls = append(calls, "after")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("runDataMigrationPhase() error: %v", err)
	}

	want := []string{"before", "migrate", "after"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
}

func TestRunDataMigrationPhase_CleanupLogsOnFailure(t *testing.T) {
	beforeErr := errors.New("before failed")
	var logs []string

	err := runDataMigrationPhase(
		true,
		func(format string, args ...any) {
			logs = append(logs, format)
		},
		func(bool) error { return nil },
		func() error { return beforeErr },
		func() error { return nil },
		func() error { return nil },
	)
	if err == nil {
		t.Fatal("expected error")
	}

	if len(logs) != 2 {
		t.Fatalf("log count = %d, want 2", len(logs))
	}
	if !strings.Contains(logs[0], "disabling triggers") {
		t.Fatalf("first log = %q, want disable message", logs[0])
	}
	if !strings.Contains(logs[1], "attempting to re-enable triggers") {
		t.Fatalf("second log = %q, want cleanup message", logs[1])
	}
}
