package main

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// maxCopyAttempts bounds how many times a single work item (one chunk, or one
	// full-table copy) is attempted before the migration fails. Each chunk COPY runs
	// in its own transaction and rolls back cleanly on error, so a retry re-runs the
	// whole chunk and cannot double-insert rows.
	maxCopyAttempts = 4

	initialCopyRetryBackoff = 500 * time.Millisecond
	maxCopyRetryBackoff     = 8 * time.Second
)

// withInterruptCancel returns a context cancelled on SIGINT/SIGTERM, so a
// long-running migration unwinds through its normal error path — workers observe
// the cancellation and the checkpoint is flushed — instead of being killed
// mid-COPY by Go's default handler, which discards everything not yet written to
// the checkpoint file. A second signal restores default handling and terminates
// immediately, so an operator can always force the issue.
func withInterruptCancel(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case sig := <-sigCh:
			log.Printf("received %s: stopping workers and saving checkpoint (press Ctrl+C again to abort immediately)", sig)
			signal.Stop(sigCh)
			cancel()
		case <-ctx.Done():
		}
	}()

	return ctx, func() {
		signal.Stop(sigCh)
		cancel()
	}
}

// isTransientError reports whether err is a connection-level failure worth
// retrying: a dropped socket, a managed-database failover, or a target that is
// briefly refusing connections. A momentary blip should not kill a multi-hour
// migration. Context cancellation is never transient.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Connection torn down mid-statement.
	if errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, mysql.ErrInvalidConn) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// PostgreSQL-side conditions that resolve on their own.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "57P01", // admin_shutdown
			"57P02", // crash_shutdown
			"57P03", // cannot_connect_now (server starting up / failing over)
			"53300", // too_many_connections
			"53400", // configuration_limit_exceeded
			"40001", // serialization_failure
			"40P01": // deadlock_detected
			return true
		}
		// Class 08 — connection exception.
		if len(pgErr.Code) == 5 && pgErr.Code[:2] == "08" {
			return true
		}
	}

	return false
}

// copyRetryBackoff returns how long to wait before the given attempt (1-based),
// growing exponentially and capped at maxCopyRetryBackoff.
func copyRetryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	backoff := initialCopyRetryBackoff << (attempt - 1)
	if backoff > maxCopyRetryBackoff || backoff <= 0 {
		return maxCopyRetryBackoff
	}
	return backoff
}

// waitBeforeRetry sleeps for the attempt's backoff, returning early if the
// context is cancelled.
func waitBeforeRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(copyRetryBackoff(attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
