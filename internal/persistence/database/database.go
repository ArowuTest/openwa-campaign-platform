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
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := verifyServiceRole(pingCtx, db, cfg); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
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
