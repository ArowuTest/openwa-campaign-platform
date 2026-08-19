package audit

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
)

type forcedAuditStateError struct{ state string }

func (e forcedAuditStateError) Error() string    { return "forced audit transaction retry" }
func (e forcedAuditStateError) SQLState() string { return e.state }

type forcedAuditRetryDriver struct{ attempts atomic.Int32 }
type forcedAuditRetryConn struct{ owner *forcedAuditRetryDriver }

func (d *forcedAuditRetryDriver) Open(string) (driver.Conn, error) {
	return &forcedAuditRetryConn{owner: d}, nil
}
func (c *forcedAuditRetryConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *forcedAuditRetryConn) Close() error { return nil }
func (c *forcedAuditRetryConn) Begin() (driver.Tx, error) {
	c.owner.attempts.Add(1)
	return nil, forcedAuditStateError{state: "40001"}
}
func (c *forcedAuditRetryConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.owner.attempts.Add(1)
	return nil, forcedAuditStateError{state: "40001"}
}

var forcedAuditDriverSequence atomic.Uint64

func TestPostgreSQLAppendNormalisesRetryExhaustionToChainConflict(t *testing.T) {
	drv := &forcedAuditRetryDriver{}
	name := fmt.Sprintf("forced-audit-retry-%d", forcedAuditDriverSequence.Add(1))
	sql.Register(name, drv)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repo := &PostgreSQLRepository{DB: db}
	_, err = repo.Append(context.Background(), Event{}, 0, "")
	if !errors.Is(err, ErrChainConflict) {
		t.Fatalf("retry exhaustion error=%v want=%v", err, ErrChainConflict)
	}
	if attempts := drv.attempts.Load(); attempts != 4 {
		t.Fatalf("retry attempts=%d want=4", attempts)
	}
}
