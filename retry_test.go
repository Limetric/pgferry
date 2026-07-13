package main

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsTransientError(t *testing.T) {
	transient := map[string]error{
		"bad conn":          driver.ErrBadConn,
		"eof":               io.EOF,
		"unexpected eof":    io.ErrUnexpectedEOF,
		"connection reset":  syscall.ECONNRESET,
		"broken pipe":       syscall.EPIPE,
		"connection refuse": syscall.ECONNREFUSED,
		"wrapped reset":     fmt.Errorf("copy chunk: %w", syscall.ECONNRESET),
		"net timeout":       &net.OpError{Op: "read", Err: syscall.ETIMEDOUT},
		"admin shutdown":    &pgconn.PgError{Code: "57P01"},
		"cannot connect":    &pgconn.PgError{Code: "57P03"},
		"too many conns":    &pgconn.PgError{Code: "53300"},
		"conn exception":    &pgconn.PgError{Code: "08006"},
		"deadlock":          &pgconn.PgError{Code: "40P01"},
		"wrapped pg":        fmt.Errorf("copy: %w", &pgconn.PgError{Code: "08003"}),
	}
	for name, err := range transient {
		if !isTransientError(err) {
			t.Errorf("isTransientError(%s) = false, want true", name)
		}
	}

	permanent := map[string]error{
		"nil":                nil,
		"context canceled":   context.Canceled,
		"deadline exceeded":  context.DeadlineExceeded,
		"generic":            errors.New("column does not exist"),
		"undefined table":    &pgconn.PgError{Code: "42P01"},
		"unique violation":   &pgconn.PgError{Code: "23505"},
		"wrapped cancel":     fmt.Errorf("copy chunk: %w", context.Canceled),
		"not null violation": &pgconn.PgError{Code: "23502"},
	}
	for name, err := range permanent {
		if isTransientError(err) {
			t.Errorf("isTransientError(%s) = true, want false", name)
		}
	}
}

func TestCopyRetryBackoff_GrowsAndCaps(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 1; attempt <= 12; attempt++ {
		got := copyRetryBackoff(attempt)
		if got <= 0 {
			t.Fatalf("copyRetryBackoff(%d) = %v, want a positive duration", attempt, got)
		}
		if got > maxCopyRetryBackoff {
			t.Fatalf("copyRetryBackoff(%d) = %v, exceeds cap %v", attempt, got, maxCopyRetryBackoff)
		}
		if got < prev {
			t.Fatalf("copyRetryBackoff(%d) = %v, shrank from %v", attempt, got, prev)
		}
		prev = got
	}
	if got := copyRetryBackoff(1); got != initialCopyRetryBackoff {
		t.Errorf("copyRetryBackoff(1) = %v, want %v", got, initialCopyRetryBackoff)
	}
}

func TestRunParallelMigrationWorkers_RetriesTransientErrors(t *testing.T) {
	workItems := []migrationWorkItem{{Table: Table{SourceName: "orders"}}}

	var mu sync.Mutex
	openCalls := 0
	var attempts atomic.Int32

	mgr := &fakeMigrationCheckpointManager{}
	err := runParallelMigrationWorkers(
		context.Background(),
		1,
		func() (migrationWorkerSource, error) {
			mu.Lock()
			defer mu.Unlock()
			openCalls++
			return &fakeMigrationWorkerSource{id: openCalls, closeMu: &mu, closeCounts: map[int]int{}}, nil
		},
		workItems,
		mgr,
		func(ctx context.Context, _ dbQuerier, _ migrationWorkItem) (int64, error) {
			// Fail twice with a dropped connection, then succeed.
			if attempts.Add(1) <= 2 {
				return 0, fmt.Errorf("copy chunk: %w", syscall.ECONNRESET)
			}
			return 42, nil
		},
	)
	if err != nil {
		t.Fatalf("expected the transient failures to be retried, got: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("execute attempts = %d, want 3 (2 transient failures then success)", got)
	}
	// The possibly-dead connection must be dropped and redialed between attempts.
	if openCalls != 3 {
		t.Errorf("openSource calls = %d, want 3 (one redial per transient failure)", openCalls)
	}
}

func TestRunParallelMigrationWorkers_GivesUpAfterMaxAttempts(t *testing.T) {
	workItems := []migrationWorkItem{{Table: Table{SourceName: "orders"}}}

	var mu sync.Mutex
	var attempts atomic.Int32

	err := runParallelMigrationWorkers(
		context.Background(),
		1,
		func() (migrationWorkerSource, error) {
			mu.Lock()
			defer mu.Unlock()
			return &fakeMigrationWorkerSource{id: 1, closeMu: &mu, closeCounts: map[int]int{}}, nil
		},
		workItems,
		&fakeMigrationCheckpointManager{},
		func(ctx context.Context, _ dbQuerier, _ migrationWorkItem) (int64, error) {
			attempts.Add(1)
			return 0, fmt.Errorf("copy chunk: %w", syscall.ECONNRESET)
		},
	)
	if err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if got := attempts.Load(); got != maxCopyAttempts {
		t.Errorf("execute attempts = %d, want %d", got, maxCopyAttempts)
	}
}

func TestRunParallelMigrationWorkers_DoesNotRetryPermanentErrors(t *testing.T) {
	workItems := []migrationWorkItem{{Table: Table{SourceName: "orders"}}}

	var mu sync.Mutex
	var attempts atomic.Int32

	err := runParallelMigrationWorkers(
		context.Background(),
		1,
		func() (migrationWorkerSource, error) {
			mu.Lock()
			defer mu.Unlock()
			return &fakeMigrationWorkerSource{id: 1, closeMu: &mu, closeCounts: map[int]int{}}, nil
		},
		workItems,
		&fakeMigrationCheckpointManager{},
		func(ctx context.Context, _ dbQuerier, _ migrationWorkItem) (int64, error) {
			attempts.Add(1)
			return 0, &pgconn.PgError{Code: "42P01", Message: "relation does not exist"}
		},
	)
	if err == nil {
		t.Fatal("expected a permanent error to fail the migration")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("execute attempts = %d, want 1 (a schema error must not be retried)", got)
	}
}

func TestRunParallelMigrationWorkers_RetriedItemIsCheckpointedOnce(t *testing.T) {
	// A retried work item must be recorded exactly once, with the count from the
	// successful attempt — not once per attempt.
	workItems := []migrationWorkItem{{Table: Table{SourceName: "orders"}}}

	var mu sync.Mutex
	var attempts atomic.Int32
	mgr := &fakeMigrationCheckpointManager{}

	err := runParallelMigrationWorkers(
		context.Background(),
		1,
		func() (migrationWorkerSource, error) {
			mu.Lock()
			defer mu.Unlock()
			return &fakeMigrationWorkerSource{id: 1, closeMu: &mu, closeCounts: map[int]int{}}, nil
		},
		workItems,
		mgr,
		func(ctx context.Context, _ dbQuerier, _ migrationWorkItem) (int64, error) {
			if attempts.Add(1) == 1 {
				return 0, fmt.Errorf("copy chunk: %w", io.ErrUnexpectedEOF)
			}
			return 7, nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mgr.mu.Lock()
	recorded := append([]string(nil), mgr.recordedFull...)
	mgr.mu.Unlock()
	if len(recorded) != 1 || recorded[0] != "orders" {
		t.Errorf("checkpoint recorded %v, want exactly one entry for orders", recorded)
	}
}

func TestWithInterruptCancel_CancelsOnSignal(t *testing.T) {
	ctx, stop := withInterruptCancel(context.Background())
	defer stop()

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("context was not cancelled after SIGTERM; an interrupt would kill the run mid-COPY without flushing the checkpoint")
	}
}

func TestWithInterruptCancel_StopReleasesHandler(t *testing.T) {
	ctx, stop := withInterruptCancel(context.Background())
	stop()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stop() should cancel the context")
	}
}
