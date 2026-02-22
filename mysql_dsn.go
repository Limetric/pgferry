package main

import (
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

func mysqlDSNWithReadOptions(baseDSN string) (string, error) {
	cfg, err := mysql.ParseDSN(baseDSN)
	if err != nil {
		return "", fmt.Errorf("parse mysql dsn: %w", err)
	}
	cfg.ParseTime = true
	cfg.InterpolateParams = true
	cfg.Loc = time.UTC
	return cfg.FormatDSN(), nil
}
