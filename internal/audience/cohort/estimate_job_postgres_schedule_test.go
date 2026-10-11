package cohort

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
)

func TestPostgreSQLEstimateScheduleRetriesRejectedTransactions(t *testing.T) {
	for _, phase := range []string{"enqueue", "evidence", "commit"} {
		for _, state := range []string{"40001", "40P01"} {
			t.Run(phase+"/"+state, func(t *testing.T) {
				rejection := estimateScheduleStateError{state: state}
				probe := &estimateScheduleDBProbe{phase: phase, failure: rejection, failures: 1}
				repo := estimateScheduleProbeRepository(t, probe)
				now := time.Date(2099, 3, 2, 12, 0, 0, 0, time.FixedZone("request", 3600))
				request := estimateScheduleProbeRequest()

				record, created, err := repo.Schedule(context.Background(), request, now)
				if err != nil || !created || record.ID == "" {
					t.Fatalf("schedule record=%+v created=%v err=%v", record, created, err)
				}
				if probe.attempts != 2 {
					t.Fatalf("transaction attempts=%d want 2", probe.attempts)
				}
				if len(probe.jobs) != 2 || !reflect.DeepEqual(probe.jobs[0], probe.jobs[1]) {
					t.Fatalf("retry changed queued job identity or request time: %+v", probe.jobs)
				}
				if len(probe.evidence) == 0 {
					t.Fatal("schedule committed without estimate evidence")
				}
				for _, args := range probe.evidence {
					if args[5] != now.UTC() || args[7] != request.ClientRequestID || args[9] != now.UTC() {
						t.Fatalf("retry changed estimate request identity or timestamps: %+v", args)
					}
					if !reflect.DeepEqual(args, probe.evidence[0]) {
						t.Fatalf("retry changed estimate evidence or fingerprint: %+v", probe.evidence)
					}
				}
				if record.ID != probe.jobs[0][0] || !record.AsOf.Equal(now) || !record.CreatedAt.Equal(now) {
					t.Fatalf("schedule result changed immutable request identity or time: %+v", record)
				}
				if record.RequestFingerprint == "" || record.RequestFingerprint != probe.evidence[0][8] {
					t.Fatalf("returned fingerprint differs from persisted evidence: %q != %v", record.RequestFingerprint, probe.evidence[0][8])
				}
				wantRollbacks := 1
				if phase == "commit" {
					wantRollbacks = 0 // database/sql marks a committed transaction done even when Commit fails.
				}
				if probe.rollbacks != wantRollbacks {
					t.Fatalf("rollback count=%d want %d", probe.rollbacks, wantRollbacks)
				}
			})
		}
	}
}

func TestPostgreSQLEstimateScheduleBoundsRejectedTransactionRetries(t *testing.T) {
	rejection := estimateScheduleStateError{state: "40001"}
	probe := &estimateScheduleDBProbe{phase: "evidence", failure: rejection, failures: 100}
	record, created, err := estimateScheduleProbeRepository(t, probe).Schedule(context.Background(), estimateScheduleProbeRequest(), time.Now())
	if !errors.Is(err, rejection) || created || record.ID != "" || probe.attempts != 4 || probe.rollbacks != 4 {
		t.Fatalf("exhaustion record=%+v created=%v attempts=%d rollbacks=%d err=%v", record, created, probe.attempts, probe.rollbacks, err)
	}
}

func TestPostgreSQLEstimateScheduleDoesNotRetryPermanentOrAmbiguousFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		phase   string
		failure error
	}{
		{"constraint", "evidence", estimateScheduleStateError{state: "23503"}},
		{"business", "evidence", ErrEstimateReplayConflict},
		{"connection", "enqueue", io.ErrUnexpectedEOF},
		{"ambiguous commit", "commit", io.ErrUnexpectedEOF},
		{"untyped SQLSTATE text", "commit", errors.New("SQLSTATE 40001")},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &estimateScheduleDBProbe{phase: test.phase, failure: test.failure, failures: 100}
			record, created, err := estimateScheduleProbeRepository(t, probe).Schedule(context.Background(), estimateScheduleProbeRequest(), time.Now())
			if !errors.Is(err, test.failure) || created || record.ID != "" || probe.attempts != 1 {
				t.Fatalf("nonretryable result record=%+v created=%v attempts=%d err=%v", record, created, probe.attempts, err)
			}
		})
	}
}

func TestPostgreSQLEstimateScheduleHonoursCancellationBeforeRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &estimateScheduleDBProbe{
		phase: "evidence", failure: estimateScheduleStateError{state: "40001"},
		failures: 100, onFailure: cancel,
	}
	record, created, err := estimateScheduleProbeRepository(t, probe).Schedule(ctx, estimateScheduleProbeRequest(), time.Now())
	if !errors.Is(err, context.Canceled) || created || record.ID != "" || probe.attempts != 1 {
		t.Fatalf("canceled result record=%+v created=%v attempts=%d err=%v", record, created, probe.attempts, err)
	}
}

type estimateScheduleStateError struct{ state string }

func (e estimateScheduleStateError) Error() string    { return "transaction rejected: " + e.state }
func (e estimateScheduleStateError) SQLState() string { return e.state }

// This driver double replaces only the external database boundary. Schedule,
// database/sql transaction lifecycle, durable-job enqueue and retry policy stay real.
type estimateScheduleDBProbe struct {
	phase     string
	failure   error
	failures  int
	attempts  int
	rollbacks int
	jobs      [][]driver.Value
	evidence  [][]driver.Value
	onFailure func()
}

func estimateScheduleProbeRepository(t *testing.T, probe *estimateScheduleDBProbe) *PostgreSQLEstimateRepository {
	t.Helper()
	db := sql.OpenDB(estimateScheduleConnector{probe: probe})
	t.Cleanup(func() { db.Close() })
	return &PostgreSQLEstimateRepository{DB: db}
}

func estimateScheduleProbeRequest() EstimateJobRequest {
	return EstimateJobRequest{
		OrganisationID:  "00000000-0000-0000-0000-000000000001",
		PurposeID:       "00000000-0000-0000-0000-000000000002",
		RequestedBy:     "00000000-0000-0000-0000-000000000003",
		Channel:         "WHATSAPP",
		ClientRequestID: "schedule-retry-request",
		Definition: audiencefilter.Group{
			Join:  audiencefilter.JoinAnd,
			Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}}},
		},
	}
}

func (p *estimateScheduleDBProbe) reject(phase string) error {
	if p.phase != phase || p.attempts > p.failures {
		return nil
	}
	if p.onFailure != nil {
		p.onFailure()
	}
	return fmt.Errorf("database %s: %w", phase, p.failure)
}

type estimateScheduleConnector struct{ probe *estimateScheduleDBProbe }

func (c estimateScheduleConnector) Connect(context.Context) (driver.Conn, error) {
	return &estimateScheduleConn{probe: c.probe}, nil
}
func (c estimateScheduleConnector) Driver() driver.Driver { return estimateScheduleDriver{} }

type estimateScheduleDriver struct{}

func (estimateScheduleDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use the schedule probe connector")
}

type estimateScheduleConn struct {
	probe *estimateScheduleDBProbe
	inTx  bool
}

func (*estimateScheduleConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (*estimateScheduleConn) Close() error { return nil }
func (c *estimateScheduleConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{Isolation: driver.IsolationLevel(sql.LevelSerializable)})
}
func (c *estimateScheduleConn) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	if c.inTx || options.Isolation != driver.IsolationLevel(sql.LevelSerializable) {
		return nil, errors.New("schedule must start a fresh serializable transaction")
	}
	c.inTx = true
	c.probe.attempts++
	return c, nil
}
func (c *estimateScheduleConn) Commit() error {
	c.inTx = false
	return c.probe.reject("commit")
}
func (c *estimateScheduleConn) Rollback() error {
	c.inTx = false
	c.probe.rollbacks++
	return nil
}
func (c *estimateScheduleConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(query, "FROM audience_cohort_estimates WHERE organisation_id=") || c.inTx {
		return nil, errors.New("unexpected schedule lookup")
	}
	return estimateScheduleEmptyRows{}, nil
}
func (c *estimateScheduleConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !c.inTx {
		return nil, errors.New("schedule writes must be transactional")
	}
	values := make([]driver.Value, len(args))
	for i := range args {
		values[i] = args[i].Value
	}
	switch {
	case strings.Contains(query, "INSERT INTO durable_jobs"):
		c.probe.jobs = append(c.probe.jobs, values)
		if err := c.probe.reject("enqueue"); err != nil {
			return nil, err
		}
	case strings.Contains(query, "INSERT INTO audience_cohort_estimates"):
		c.probe.evidence = append(c.probe.evidence, values)
		if err := c.probe.reject("evidence"); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unexpected schedule write")
	}
	return driver.RowsAffected(1), nil
}

type estimateScheduleEmptyRows struct{}

func (estimateScheduleEmptyRows) Columns() []string         { return []string{"id"} }
func (estimateScheduleEmptyRows) Close() error              { return nil }
func (estimateScheduleEmptyRows) Next([]driver.Value) error { return io.EOF }
