package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type PoolConfig struct {
	Driver          string
	DSN             string
	MaxOpen         int
	MaxIdle         int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	PingTimeout     time.Duration
}

// Open uses database/sql so repositories remain driver-neutral. A production
// binary must explicitly link a PostgreSQL driver; the function refuses to
// start when the requested driver is absent instead of running with memory
// persistence or failing on the first business request.
func Open(ctx context.Context, cfg PoolConfig) (*sql.DB, error) {
	driver := strings.TrimSpace(cfg.Driver)
	if driver == "" {
		driver = "pgx"
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("database DSN is required")
	}
	if !registered(driver) {
		drivers := sql.Drivers()
		sort.Strings(drivers)
		return nil, fmt.Errorf("PostgreSQL driver %q is not linked into this binary; registered drivers: %v", driver, drivers)
	}
	db, err := sql.Open(driver, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	maxOpen := cfg.MaxOpen
	if maxOpen <= 0 || maxOpen > 500 {
		maxOpen = 30
	}
	maxIdle := cfg.MaxIdle
	if maxIdle < 0 || maxIdle > maxOpen {
		maxIdle = maxOpen / 3
	}
	if maxIdle == 0 {
		maxIdle = 5
		if maxIdle > maxOpen {
			maxIdle = maxOpen
		}
	}
	lifetime := cfg.ConnMaxLifetime
	if lifetime <= 0 {
		lifetime = 30 * time.Minute
	}
	idleTime := cfg.ConnMaxIdleTime
	if idleTime <= 0 {
		idleTime = 5 * time.Minute
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(lifetime)
	db.SetConnMaxIdleTime(idleTime)

	pingTimeout := cfg.PingTimeout
	if pingTimeout <= 0 || pingTimeout > 30*time.Second {
		pingTimeout = 5 * time.Second
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

func registered(driver string) bool {
	for _, candidate := range sql.Drivers() {
		if candidate == driver {
			return true
		}
	}
	return false
}
