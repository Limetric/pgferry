package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

type pingContextDB interface {
	PingContext(context.Context) error
}

func resolveOptionalConfigPath(flagPath string, args []string) (string, error) {
	posPath := ""
	if len(args) > 0 {
		posPath = strings.TrimSpace(args[0])
	}
	flagPath = strings.TrimSpace(flagPath)
	if flagPath != "" && posPath != "" {
		return "", fmt.Errorf("provide either a positional migration.toml path or --config, not both")
	}
	if posPath != "" {
		return posPath, nil
	}
	return flagPath, nil
}

func pingSourceAndTarget(ctx context.Context, src SourceDB, sourceDB pingContextDB, pgPool *pgxpool.Pool) error {
	srcLabel := strings.ToLower(src.Name())
	log.Printf("pinging %s and PostgreSQL...", srcLabel)

	pingCtx, pingCancel := context.WithTimeout(ctx, connectivityPingTimeout)
	defer pingCancel()

	var srcPingErr error
	var pgPingErr error
	var g errgroup.Group
	g.Go(func() error {
		if err := sourceDB.PingContext(pingCtx); err != nil {
			srcPingErr = fmt.Errorf("ping %s: %w", srcLabel, err)
		}
		return nil
	})
	g.Go(func() error {
		if err := pgPool.Ping(pingCtx); err != nil {
			pgPingErr = fmt.Errorf("ping postgres: %w", err)
		}
		return nil
	})
	g.Wait()

	if err := errors.Join(srcPingErr, pgPingErr); err != nil {
		return err
	}
	return nil
}
