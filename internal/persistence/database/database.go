package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
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
	StartupTimeout  time.Duration
	Environment     string
	ServiceName     string
	ExpectedRole    string
}

// Open uses database/sql so repositories remain driver-neutral. A production
// binary must explicitly link a PostgreSQL driver; the function refuses to
// start when the requested driver is absent instead of running with memory
// persistence or failing on the first business request.
func Open(ctx context.Context, cfg PoolConfig) (*sql.DB, error) {
	driver := strings.TrimSpace(cfg.Driver)
	if driver == "" {
		driver = "postgres"
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("database DSN is required")
	}
	if err := ValidateDeploymentDSN(cfg.Environment, cfg.DSN); err != nil {
		return nil, err
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
	startupTimeout, err := resolveStartupTimeout(cfg.StartupTimeout)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := pingUntilReady(ctx, db, pingTimeout, startupTimeout, 250*time.Millisecond); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	roleCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := verifyServiceRole(roleCtx, db, cfg); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

type contextPinger interface {
	PingContext(context.Context) error
}

func resolveStartupTimeout(configured time.Duration) (time.Duration, error) {
	if configured > 0 {
		if configured > 5*time.Minute {
			return 0, errors.New("database startup timeout must not exceed 5 minutes")
		}
		return configured, nil
	}
	raw := strings.TrimSpace(os.Getenv("DATABASE_STARTUP_TIMEOUT"))
	if raw == "" {
		return 30 * time.Second, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 || parsed > 5*time.Minute {
		return 0, errors.New("DATABASE_STARTUP_TIMEOUT must be a positive duration no greater than 5 minutes")
	}
	return parsed, nil
}

func pingUntilReady(ctx context.Context, pinger contextPinger, attemptTimeout, startupTimeout, retryInterval time.Duration) error {
	if attemptTimeout <= 0 {
		attemptTimeout = 5 * time.Second
	}
	if startupTimeout <= 0 {
		startupTimeout = 30 * time.Second
	}
	if retryInterval <= 0 {
		retryInterval = 250 * time.Millisecond
	}
	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	attempts := 0
	var lastErr error
	for {
		attempts++
		attemptCtx, attemptCancel := context.WithTimeout(startupCtx, attemptTimeout)
		err := pinger.PingContext(attemptCtx)
		attemptCancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if startupCtx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("database not ready after %d attempts within %s: %w", attempts, startupTimeout, lastErr)
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-startupCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("database not ready after %d attempts within %s: %w", attempts, startupTimeout, lastErr)
		case <-timer.C:
		}
	}
}

func ValidateDeploymentDSN(environment, dsn string) error {
	environment = strings.ToLower(strings.TrimSpace(environment))
	if environment != "production" && environment != "staging" {
		return nil
	}
	sslMode := ""
	if parsed, err := url.Parse(strings.TrimSpace(dsn)); err == nil && parsed.Scheme != "" {
		sslMode = strings.ToLower(strings.TrimSpace(parsed.Query().Get("sslmode")))
	} else {
		for _, field := range strings.Fields(dsn) {
			key, value, ok := strings.Cut(field, "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "sslmode") {
				sslMode = strings.ToLower(strings.Trim(strings.TrimSpace(value), "'\""))
				break
			}
		}
	}
	switch sslMode {
	case "require", "verify-ca", "verify-full":
		return nil
	default:
		return errors.New("deployed PostgreSQL connections require sslmode=require, verify-ca or verify-full")
	}
}

func verifyServiceRole(ctx context.Context, db *sql.DB, cfg PoolConfig) error {
	expected := strings.TrimSpace(cfg.ExpectedRole)
	if override := strings.TrimSpace(os.Getenv("DATABASE_EXPECTED_ROLE")); override != "" {
		expected = override
	}
	environment := strings.ToLower(strings.TrimSpace(cfg.Environment))
	if expected == "" && (environment == "staging" || environment == "production") {
		expected = DefaultServiceRole(cfg.ServiceName)
	}
	if expected == "" {
		return nil
	}
	var actual string
	var superuser bool
	if err := db.QueryRowContext(ctx, `
SELECT current_user, role.rolsuper
FROM pg_roles AS role
WHERE role.rolname = current_user
`).Scan(&actual, &superuser); err != nil {
		return fmt.Errorf("verify database service identity: %w", err)
	}
	var expectedMember bool
	if err := db.QueryRowContext(ctx, `SELECT pg_has_role(current_user, $1, 'MEMBER')`, expected).Scan(&expectedMember); err != nil {
		return fmt.Errorf("verify database service-role membership: %w", err)
	}
	rows, err := db.QueryContext(ctx, `
SELECT role.rolname
FROM pg_roles AS role
WHERE role.rolname IN (
  'campaign_control_api',
  'campaign_audience_worker',
  'campaign_campaign_worker',
  'campaign_export_worker',
  'campaign_inbound_governance_worker',
  'campaign_metrics_worker',
  'campaign_platform_governance_worker'
)
AND pg_has_role(current_user, role.oid, 'MEMBER')
ORDER BY role.rolname
`)
	if err != nil {
		return fmt.Errorf("list database service-role memberships: %w", err)
	}
	defer rows.Close()
	memberships := make([]string, 0, 2)
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return fmt.Errorf("scan database service-role membership: %w", err)
		}
		memberships = append(memberships, role)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list database service-role memberships: %w", err)
	}
	if err := validateServiceIdentity(actual, expected, superuser, expectedMember, memberships); err != nil {
		return err
	}
	return nil
}

func validateServiceIdentity(actual, expected string, superuser, expectedMember bool, memberships []string) error {
	if superuser {
		return fmt.Errorf("database service identity %q must not be a superuser", actual)
	}
	if !expectedMember {
		return fmt.Errorf("database service identity mismatch: %q is not a member of expected role %q", actual, expected)
	}
	unexpected := make([]string, 0, len(memberships))
	for _, role := range memberships {
		if role != expected {
			unexpected = append(unexpected, role)
		}
	}
	if len(unexpected) > 0 {
		return fmt.Errorf("database service identity %q has cross-service memberships %v; expected only %q", actual, unexpected, expected)
	}
	return nil
}

func DefaultServiceRole(service string) string {
	service = strings.ToLower(strings.TrimSpace(service))
	service = strings.NewReplacer("-", "_", ".", "_", "/", "_").Replace(service)
	if service == "" {
		return ""
	}
	return "campaign_" + service
}

func registered(driver string) bool {
	for _, candidate := range sql.Drivers() {
		if candidate == driver {
			return true
		}
	}
	return false
}
