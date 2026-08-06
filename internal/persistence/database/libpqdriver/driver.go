//go:build cgo

// Package libpqdriver exposes PostgreSQL's maintained native libpq client as a
// database/sql driver. It deliberately keeps the adapter small: TLS, SCRAM,
// channel binding, protocol negotiation and server compatibility remain the
// responsibility of libpq, while this package provides Go context, transaction,
// parameter and row semantics required by the platform.
package libpqdriver

/*
#cgo pkg-config: libpq
#include <stdlib.h>
#include <libpq-fe.h>

static PGresult* campaign_pq_exec_params(
    PGconn *conn,
    const char *command,
    int nParams,
    char **paramValues,
    int *paramLengths,
    int *paramFormats
) {
    return PQexecParams(
        conn,
        command,
        nParams,
        NULL,
        (const char * const *)paramValues,
        paramLengths,
        paramFormats,
        0
    );
}

static char campaign_diag_sqlstate(void) { return PG_DIAG_SQLSTATE; }
static char campaign_diag_severity(void) { return PG_DIAG_SEVERITY; }
static char campaign_diag_message_primary(void) { return PG_DIAG_MESSAGE_PRIMARY; }
static char campaign_diag_message_detail(void) { return PG_DIAG_MESSAGE_DETAIL; }
static char campaign_diag_constraint_name(void) { return PG_DIAG_CONSTRAINT_NAME; }
static char campaign_diag_table_name(void) { return PG_DIAG_TABLE_NAME; }
static char campaign_diag_column_name(void) { return PG_DIAG_COLUMN_NAME; }
*/
import "C"

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const driverName = "postgres"

func init() {
	sql.Register(driverName, &Driver{})
}

// Driver implements database/sql/driver.DriverContext so connections are
// opened lazily by database/sql and honour the pool lifecycle.
type Driver struct{}

func (*Driver) Open(name string) (driver.Conn, error) {
	return open(context.Background(), name)
}

func (*Driver) OpenConnector(name string) (driver.Connector, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("libpq: connection string is required")
	}
	return &connector{dsn: name}, nil
}

type connector struct{ dsn string }

func (c *connector) Connect(ctx context.Context) (driver.Conn, error) { return open(ctx, c.dsn) }
func (c *connector) Driver() driver.Driver                            { return &Driver{} }

type conn struct {
	mu     sync.Mutex
	pg     *C.PGconn
	closed bool
}

func open(ctx context.Context, dsn string) (*conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("libpq: connection string is required")
	}
	if strings.IndexByte(dsn, 0) >= 0 {
		return nil, errors.New("libpq: connection string contains NUL")
	}
	dsn = withConnectTimeout(ctx, dsn)
	cdsn := C.CString(dsn)
	defer C.free(unsafe.Pointer(cdsn))
	pg := C.PQconnectdb(cdsn)
	if pg == nil {
		return nil, errors.New("libpq: PQconnectdb returned nil")
	}
	if C.PQstatus(pg) != C.CONNECTION_OK {
		message := connectionError(pg)
		C.PQfinish(pg)
		return nil, fmt.Errorf("libpq: connect: %s", message)
	}
	initialiseSQL := C.CString("SET TIME ZONE 'UTC'")
	result := C.PQexec(pg, initialiseSQL)
	C.free(unsafe.Pointer(initialiseSQL))
	if result == nil {
		message := connectionError(pg)
		C.PQfinish(pg)
		return nil, fmt.Errorf("libpq: initialise session: %s", message)
	}
	status := C.PQresultStatus(result)
	if status != C.PGRES_COMMAND_OK {
		err := resultError(result)
		C.PQclear(result)
		C.PQfinish(pg)
		return nil, fmt.Errorf("libpq: initialise session: %w", err)
	}
	C.PQclear(result)
	return &conn{pg: pg}, nil
}

func withConnectTimeout(ctx context.Context, dsn string) string {
	timeout := 10 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	seconds := int(math.Ceil(timeout.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	if parsed, err := url.Parse(dsn); err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		query := parsed.Query()
		if strings.TrimSpace(query.Get("connect_timeout")) == "" {
			query.Set("connect_timeout", strconv.Itoa(seconds))
			parsed.RawQuery = query.Encode()
		}
		return parsed.String()
	}
	for _, field := range strings.Fields(dsn) {
		key, _, ok := strings.Cut(field, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "connect_timeout") {
			return dsn
		}
	}
	return dsn + " connect_timeout=" + strconv.Itoa(seconds)
}

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("libpq: prepared query is empty")
	}
	if !c.IsValid() {
		return nil, driver.ErrBadConn
	}
	return &stmt{conn: c, query: query}, nil
}

func (c *conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.pg != nil {
		C.PQfinish(c.pg)
		c.pg = nil
	}
	return nil
}

func (c *conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *conn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	clauses := []string{"BEGIN"}
	switch options.Isolation {
	case driver.IsolationLevel(sql.LevelDefault):
	case driver.IsolationLevel(sql.LevelReadCommitted):
		clauses = append(clauses, "ISOLATION LEVEL READ COMMITTED")
	case driver.IsolationLevel(sql.LevelRepeatableRead):
		clauses = append(clauses, "ISOLATION LEVEL REPEATABLE READ")
	case driver.IsolationLevel(sql.LevelSerializable):
		clauses = append(clauses, "ISOLATION LEVEL SERIALIZABLE")
	case driver.IsolationLevel(sql.LevelReadUncommitted):
		// PostgreSQL treats READ UNCOMMITTED as READ COMMITTED, but spelling it
		// explicitly keeps the caller's intent auditable.
		clauses = append(clauses, "ISOLATION LEVEL READ UNCOMMITTED")
	default:
		return nil, fmt.Errorf("libpq: unsupported isolation level %d", options.Isolation)
	}
	if options.ReadOnly {
		clauses = append(clauses, "READ ONLY")
	} else {
		clauses = append(clauses, "READ WRITE")
	}
	if _, err := c.exec(ctx, strings.Join(clauses, " "), nil); err != nil {
		return nil, err
	}
	return &tx{conn: c}, nil
}

func (c *conn) Ping(ctx context.Context) error {
	rows, err := c.query(ctx, "SELECT 1", nil)
	if err != nil {
		return err
	}
	return rows.Close()
}

func (c *conn) ResetSession(ctx context.Context) error {
	c.mu.Lock()
	if c.closed || c.pg == nil || C.PQstatus(c.pg) != C.CONNECTION_OK {
		c.mu.Unlock()
		return driver.ErrBadConn
	}
	transactionStatus := C.PQtransactionStatus(c.pg)
	c.mu.Unlock()
	switch transactionStatus {
	case C.PQTRANS_IDLE:
		return nil
	case C.PQTRANS_INTRANS, C.PQTRANS_INERROR:
		_, err := c.exec(ctx, "ROLLBACK", nil)
		return err
	default:
		return driver.ErrBadConn
	}
}

func (c *conn) IsValid() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && c.pg != nil && C.PQstatus(c.pg) == C.CONNECTION_OK
}

func (c *conn) CheckNamedValue(value *driver.NamedValue) error {
	if value == nil {
		return errors.New("libpq: nil named value")
	}
	// Conversion is intentionally delayed until execution so TextMarshaler,
	// driver.Valuer and domain-specific Stringer types remain supported.
	return nil
}

func (c *conn) Exec(query string, values []driver.Value) (driver.Result, error) {
	named := make([]driver.NamedValue, len(values))
	for index, value := range values {
		named[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	return c.exec(context.Background(), query, named)
}

func (c *conn) ExecContext(ctx context.Context, query string, values []driver.NamedValue) (driver.Result, error) {
	return c.exec(ctx, query, values)
}

func (c *conn) Query(query string, values []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(values))
	for index, value := range values {
		named[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	return c.query(context.Background(), query, named)
}

func (c *conn) QueryContext(ctx context.Context, query string, values []driver.NamedValue) (driver.Rows, error) {
	return c.query(ctx, query, values)
}

func (c *conn) exec(ctx context.Context, query string, values []driver.NamedValue) (driver.Result, error) {
	result, err := c.execute(ctx, query, values)
	if err != nil {
		return nil, err
	}
	defer C.PQclear(result)
	switch C.PQresultStatus(result) {
	case C.PGRES_COMMAND_OK:
		affected := int64(0)
		if raw := C.GoString(C.PQcmdTuples(result)); raw != "" {
			parsed, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil {
				return nil, fmt.Errorf("libpq: parse affected-row count %q: %w", raw, parseErr)
			}
			affected = parsed
		}
		return resultValue{affected: affected}, nil
	case C.PGRES_TUPLES_OK:
		return nil, errors.New("libpq: ExecContext received a row-returning statement")
	default:
		return nil, resultError(result)
	}
}

func (c *conn) query(ctx context.Context, query string, values []driver.NamedValue) (*rows, error) {
	result, err := c.execute(ctx, query, values)
	if err != nil {
		return nil, err
	}
	switch C.PQresultStatus(result) {
	case C.PGRES_TUPLES_OK:
		return newRows(result), nil
	case C.PGRES_COMMAND_OK:
		C.PQclear(result)
		return nil, errors.New("libpq: QueryContext received a non-row-returning statement")
	default:
		err := resultError(result)
		C.PQclear(result)
		return nil, err
	}
}

func (c *conn) execute(ctx context.Context, query string, values []driver.NamedValue) (*C.PGresult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("libpq: query is empty")
	}
	if strings.IndexByte(query, 0) >= 0 {
		return nil, errors.New("libpq: query contains NUL")
	}
	params, err := buildParameters(values)
	if err != nil {
		return nil, err
	}
	defer params.free()
	cquery := C.CString(query)
	defer C.free(unsafe.Pointer(cquery))

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.pg == nil || C.PQstatus(c.pg) != C.CONNECTION_OK {
		return nil, driver.ErrBadConn
	}
	cancelHandle := C.PQgetCancel(c.pg)
	if cancelHandle != nil {
		defer C.PQfreeCancel(cancelHandle)
	}

	resultChannel := make(chan *C.PGresult, 1)
	go func() {
		resultChannel <- C.campaign_pq_exec_params(
			c.pg,
			cquery,
			C.int(len(values)),
			params.values,
			params.lengths,
			params.formats,
		)
	}()

	var result *C.PGresult
	select {
	case result = <-resultChannel:
	case <-ctx.Done():
		if cancelHandle != nil {
			errorBuffer := make([]byte, 256)
			C.PQcancel(cancelHandle, (*C.char)(unsafe.Pointer(&errorBuffer[0])), C.int(len(errorBuffer)))
		}
		result = <-resultChannel
		if result != nil {
			C.PQclear(result)
		}
		return nil, ctx.Err()
	}
	runtime.KeepAlive(params)
	if result == nil {
		if C.PQstatus(c.pg) != C.CONNECTION_OK {
			return nil, driver.ErrBadConn
		}
		return nil, fmt.Errorf("libpq: query returned no result: %s", connectionError(c.pg))
	}
	status := C.PQresultStatus(result)
	if status == C.PGRES_FATAL_ERROR || status == C.PGRES_BAD_RESPONSE || status == C.PGRES_NONFATAL_ERROR {
		err := resultError(result)
		C.PQclear(result)
		return nil, err
	}
	return result, nil
}

type parameters struct {
	values  **C.char
	lengths *C.int
	formats *C.int
	strings []*C.char
}

func buildParameters(values []driver.NamedValue) (*parameters, error) {
	if len(values) == 0 {
		return &parameters{}, nil
	}
	pointerSize := unsafe.Sizeof(uintptr(0))
	valuesMemory := C.calloc(C.size_t(len(values)), C.size_t(pointerSize))
	lengthsMemory := C.calloc(C.size_t(len(values)), C.size_t(unsafe.Sizeof(C.int(0))))
	formatsMemory := C.calloc(C.size_t(len(values)), C.size_t(unsafe.Sizeof(C.int(0))))
	if valuesMemory == nil || lengthsMemory == nil || formatsMemory == nil {
		if valuesMemory != nil {
			C.free(valuesMemory)
		}
		if lengthsMemory != nil {
			C.free(lengthsMemory)
		}
		if formatsMemory != nil {
			C.free(formatsMemory)
		}
		return nil, errors.New("libpq: allocate parameter arrays")
	}
	result := &parameters{
		values:  (**C.char)(valuesMemory),
		lengths: (*C.int)(lengthsMemory),
		formats: (*C.int)(formatsMemory),
	}
	valueSlots := unsafe.Slice((**C.char)(valuesMemory), len(values))
	lengthSlots := unsafe.Slice((*C.int)(lengthsMemory), len(values))
	formatSlots := unsafe.Slice((*C.int)(formatsMemory), len(values))
	for index, named := range values {
		encoded, null, err := encodeParameter(named.Value)
		if err != nil {
			result.free()
			return nil, fmt.Errorf("libpq: parameter %d: %w", named.Ordinal, err)
		}
		if null {
			valueSlots[index] = nil
			continue
		}
		if strings.IndexByte(encoded, 0) >= 0 {
			result.free()
			return nil, fmt.Errorf("libpq: parameter %d contains NUL", named.Ordinal)
		}
		cvalue := C.CString(encoded)
		result.strings = append(result.strings, cvalue)
		valueSlots[index] = cvalue
		lengthSlots[index] = C.int(len(encoded))
		formatSlots[index] = 0
	}
	return result, nil
}

func (p *parameters) free() {
	if p == nil {
		return
	}
	for _, value := range p.strings {
		C.free(unsafe.Pointer(value))
	}
	if p.values != nil {
		C.free(unsafe.Pointer(p.values))
	}
	if p.lengths != nil {
		C.free(unsafe.Pointer(p.lengths))
	}
	if p.formats != nil {
		C.free(unsafe.Pointer(p.formats))
	}
	p.values, p.lengths, p.formats, p.strings = nil, nil, nil, nil
}

func encodeParameter(value any) (string, bool, error) {
	if value == nil {
		return "", true, nil
	}
	if valuer, ok := value.(driver.Valuer); ok {
		converted, err := valuer.Value()
		if err != nil {
			return "", false, err
		}
		// database/sql requires Valuer implementations to return one of the
		// driver.Value types. Enforcing that contract prevents recursive self
		// returns and avoids comparing interfaces whose dynamic value is not
		// comparable (for example, a named slice).
		if !driver.IsValue(converted) {
			return "", false, fmt.Errorf("driver.Valuer returned unsupported type %T", converted)
		}
		return encodeParameter(converted)
	}
	switch typed := value.(type) {
	case string:
		return typed, false, nil
	case []byte:
		return `\x` + hex.EncodeToString(typed), false, nil
	case json.RawMessage:
		return string(typed), false, nil
	case bool:
		return strconv.FormatBool(typed), false, nil
	case int:
		return strconv.FormatInt(int64(typed), 10), false, nil
	case int8:
		return strconv.FormatInt(int64(typed), 10), false, nil
	case int16:
		return strconv.FormatInt(int64(typed), 10), false, nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), false, nil
	case int64:
		return strconv.FormatInt(typed, 10), false, nil
	case uint:
		return strconv.FormatUint(uint64(typed), 10), false, nil
	case uint8:
		return strconv.FormatUint(uint64(typed), 10), false, nil
	case uint16:
		return strconv.FormatUint(uint64(typed), 10), false, nil
	case uint32:
		return strconv.FormatUint(uint64(typed), 10), false, nil
	case uint64:
		if typed > math.MaxInt64 {
			return "", false, fmt.Errorf("unsigned integer %d exceeds PostgreSQL bigint range", typed)
		}
		return strconv.FormatUint(typed, 10), false, nil
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32), false, nil
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), false, nil
	case time.Time:
		return typed.UTC().Format("2006-01-02 15:04:05.999999999Z07:00"), false, nil
	case time.Duration:
		return typed.String(), false, nil
	}
	if marshaler, ok := value.(encoding.TextMarshaler); ok {
		encoded, err := marshaler.MarshalText()
		return string(encoded), false, err
	}
	if stringer, ok := value.(fmt.Stringer); ok {
		return stringer.String(), false, nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return "", true, nil
		}
		return encodeParameter(reflected.Elem().Interface())
	}
	if reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Array {
		encoded, err := encodeArray(reflected)
		return encoded, false, err
	}
	return "", false, fmt.Errorf("unsupported parameter type %T", value)
}

func encodeArray(value reflect.Value) (string, error) {
	if value.Kind() == reflect.Slice && value.IsNil() {
		return "{}", nil
	}
	items := make([]string, value.Len())
	for index := 0; index < value.Len(); index++ {
		item := value.Index(index)
		if item.Kind() == reflect.Interface {
			if item.IsNil() {
				items[index] = "NULL"
				continue
			}
			item = item.Elem()
		}
		if item.Kind() == reflect.Pointer {
			if item.IsNil() {
				items[index] = "NULL"
				continue
			}
			item = item.Elem()
		}
		if item.Kind() == reflect.Slice || item.Kind() == reflect.Array {
			return "", fmt.Errorf("array element %d: nested arrays are not supported", index)
		}
		encoded, null, err := encodeParameter(item.Interface())
		if err != nil {
			return "", fmt.Errorf("array element %d: %w", index, err)
		}
		if null {
			items[index] = "NULL"
			continue
		}
		items[index] = quoteArrayElement(encoded)
	}
	return "{" + strings.Join(items, ",") + "}", nil
}

func quoteArrayElement(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func parseArray(raw string) ([]*string, error) {
	if equals := strings.IndexByte(raw, '='); equals >= 0 && strings.HasPrefix(raw, "[") {
		raw = raw[equals+1:]
	}
	if len(raw) < 2 || raw[0] != '{' || raw[len(raw)-1] != '}' {
		return nil, fmt.Errorf("invalid PostgreSQL array %q", raw)
	}
	body := raw[1 : len(raw)-1]
	if body == "" {
		return []*string{}, nil
	}
	items := make([]*string, 0, 8)
	var builder strings.Builder
	quoted := false
	escaped := false
	wasQuoted := false
	flush := func() error {
		value := builder.String()
		builder.Reset()
		if !wasQuoted && strings.EqualFold(value, "NULL") {
			items = append(items, nil)
		} else {
			copyValue := value
			items = append(items, &copyValue)
		}
		wasQuoted = false
		return nil
	}
	for index := 0; index < len(body); index++ {
		character := body[index]
		if escaped {
			builder.WriteByte(character)
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			wasQuoted = true
			continue
		}
		if character == ',' && !quoted {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		builder.WriteByte(character)
	}
	if escaped || quoted {
		return nil, fmt.Errorf("unterminated PostgreSQL array %q", raw)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return items, nil
}

func decodeStringArray(raw string) ([]string, error) {
	items, err := parseArray(raw)
	if err != nil {
		return nil, err
	}
	result := make([]string, len(items))
	for index, item := range items {
		if item != nil {
			result[index] = *item
		}
	}
	return result, nil
}

func decodeIntArray(raw string) ([]int64, error) {
	items, err := parseArray(raw)
	if err != nil {
		return nil, err
	}
	result := make([]int64, len(items))
	for index, item := range items {
		if item == nil {
			return nil, errors.New("NULL is not supported in integer arrays")
		}
		value, err := strconv.ParseInt(*item, 10, 64)
		if err != nil {
			return nil, err
		}
		result[index] = value
	}
	return result, nil
}

func decodeFloatArray(raw string) ([]float64, error) {
	items, err := parseArray(raw)
	if err != nil {
		return nil, err
	}
	result := make([]float64, len(items))
	for index, item := range items {
		if item == nil {
			return nil, errors.New("NULL is not supported in floating-point arrays")
		}
		value, err := strconv.ParseFloat(*item, 64)
		if err != nil {
			return nil, err
		}
		result[index] = value
	}
	return result, nil
}

func decodeBoolArray(raw string) ([]bool, error) {
	items, err := parseArray(raw)
	if err != nil {
		return nil, err
	}
	result := make([]bool, len(items))
	for index, item := range items {
		if item == nil {
			return nil, errors.New("NULL is not supported in boolean arrays")
		}
		switch strings.ToLower(*item) {
		case "t", "true":
			result[index] = true
		case "f", "false":
			result[index] = false
		default:
			return nil, fmt.Errorf("invalid boolean %q", *item)
		}
	}
	return result, nil
}

type resultValue struct{ affected int64 }

func (resultValue) LastInsertId() (int64, error) {
	return 0, errors.New("libpq: LastInsertId is not supported; use RETURNING")
}
func (r resultValue) RowsAffected() (int64, error) { return r.affected, nil }

type stmt struct {
	mu     sync.Mutex
	conn   *conn
	query  string
	closed bool
}

func (s *stmt) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
func (s *stmt) NumInput() int { return -1 }
func (s *stmt) Exec(values []driver.Value) (driver.Result, error) {
	named := make([]driver.NamedValue, len(values))
	for index, value := range values {
		named[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	return s.ExecContext(context.Background(), named)
}
func (s *stmt) ExecContext(ctx context.Context, values []driver.NamedValue) (driver.Result, error) {
	if s.isClosed() {
		return nil, errors.New("libpq: statement is closed")
	}
	return s.conn.exec(ctx, s.query, values)
}
func (s *stmt) Query(values []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(values))
	for index, value := range values {
		named[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	return s.QueryContext(context.Background(), named)
}
func (s *stmt) QueryContext(ctx context.Context, values []driver.NamedValue) (driver.Rows, error) {
	if s.isClosed() {
		return nil, errors.New("libpq: statement is closed")
	}
	return s.conn.query(ctx, s.query, values)
}
func (s *stmt) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

type tx struct {
	conn *conn
	once sync.Once
}

func (t *tx) Commit() error {
	var err error
	t.once.Do(func() { _, err = t.conn.exec(context.Background(), "COMMIT", nil) })
	return err
}
func (t *tx) Rollback() error {
	var err error
	t.once.Do(func() { _, err = t.conn.exec(context.Background(), "ROLLBACK", nil) })
	return err
}

type rows struct {
	result  *C.PGresult
	columns []string
	oids    []uint32
	index   int
	total   int
	closed  bool
}

func newRows(result *C.PGresult) *rows {
	fieldCount := int(C.PQnfields(result))
	columns := make([]string, fieldCount)
	oids := make([]uint32, fieldCount)
	for index := 0; index < fieldCount; index++ {
		columns[index] = C.GoString(C.PQfname(result, C.int(index)))
		oids[index] = uint32(C.PQftype(result, C.int(index)))
	}
	return &rows{result: result, columns: columns, oids: oids, total: int(C.PQntuples(result))}
}

func (r *rows) Columns() []string { return append([]string(nil), r.columns...) }
func (r *rows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.result != nil {
		C.PQclear(r.result)
		r.result = nil
	}
	return nil
}
func (r *rows) Next(destination []driver.Value) error {
	if r.closed || r.result == nil {
		return io.EOF
	}
	if r.index >= r.total {
		return io.EOF
	}
	if len(destination) < len(r.columns) {
		return fmt.Errorf("libpq: destination has %d values for %d columns", len(destination), len(r.columns))
	}
	for column := range r.columns {
		if C.PQgetisnull(r.result, C.int(r.index), C.int(column)) != 0 {
			destination[column] = nil
			continue
		}
		rawPointer := C.PQgetvalue(r.result, C.int(r.index), C.int(column))
		rawLength := C.PQgetlength(r.result, C.int(r.index), C.int(column))
		raw := C.GoBytes(unsafe.Pointer(rawPointer), rawLength)
		value, err := decodeValue(r.oids[column], raw)
		if err != nil {
			return fmt.Errorf("libpq: decode column %q: %w", r.columns[column], err)
		}
		destination[column] = value
	}
	r.index++
	return nil
}

func (r *rows) ColumnTypeDatabaseTypeName(index int) string {
	if index < 0 || index >= len(r.oids) {
		return ""
	}
	return databaseTypeName(r.oids[index])
}
func (r *rows) ColumnTypeNullable(index int) (bool, bool) { return true, false }
func (r *rows) ColumnTypeLength(index int) (int64, bool) {
	if index < 0 || index >= len(r.columns) || r.result == nil {
		return 0, false
	}
	size := int64(C.PQfsize(r.result, C.int(index)))
	if size < 0 {
		return 0, false
	}
	return size, true
}
func (r *rows) ColumnTypeScanType(index int) reflect.Type {
	if index < 0 || index >= len(r.oids) {
		return reflect.TypeOf([]byte(nil))
	}
	switch r.oids[index] {
	case 16:
		return reflect.TypeOf(false)
	case 20, 21, 23, 26:
		return reflect.TypeOf(int64(0))
	case 700, 701:
		return reflect.TypeOf(float64(0))
	case 1082, 1114, 1184:
		return reflect.TypeOf(time.Time{})
	case 1000:
		return reflect.TypeOf([]bool(nil))
	case 1005, 1007, 1016:
		return reflect.TypeOf([]int64(nil))
	case 1021, 1022:
		return reflect.TypeOf([]float64(nil))
	case 1009, 1015, 2951:
		return reflect.TypeOf([]string(nil))
	default:
		return reflect.TypeOf([]byte(nil))
	}
}

func decodeValue(oid uint32, raw []byte) (driver.Value, error) {
	text := string(raw)
	switch oid {
	case 16: // bool
		switch text {
		case "t", "true":
			return true, nil
		case "f", "false":
			return false, nil
		default:
			return nil, fmt.Errorf("invalid boolean %q", text)
		}
	case 20, 21, 23, 26: // int8, int2, int4, oid
		value, err := strconv.ParseInt(text, 10, 64)
		return value, err
	case 700, 701: // float4, float8
		value, err := strconv.ParseFloat(text, 64)
		return value, err
	case 17: // bytea
		if strings.HasPrefix(text, `\x`) {
			decoded, err := hex.DecodeString(text[2:])
			return decoded, err
		}
		return append([]byte(nil), raw...), nil
	case 1082: // date
		value, err := time.Parse("2006-01-02", text)
		return value, err
	case 1114: // timestamp without time zone; sessions are forced to UTC.
		value, err := parseTimestamp(text, false)
		return value, err
	case 1184: // timestamp with time zone
		value, err := parseTimestamp(text, true)
		return value, err
	case 1000: // bool[]
		return decodeBoolArray(text)
	case 1005, 1007, 1016: // int2[], int4[], int8[]
		return decodeIntArray(text)
	case 1021, 1022: // float4[], float8[]
		return decodeFloatArray(text)
	case 1009, 1015, 2951: // text[], varchar[], uuid[]
		return decodeStringArray(text)
	default:
		return append([]byte(nil), raw...), nil
	}
}

func parseTimestamp(value string, withZone bool) (time.Time, error) {
	layouts := []string{}
	if withZone {
		layouts = append(layouts,
			"2006-01-02 15:04:05.999999999Z07:00",
			"2006-01-02 15:04:05.999999999Z0700",
			"2006-01-02 15:04:05.999999999Z07",
			"2006-01-02 15:04:05Z07:00",
			"2006-01-02 15:04:05Z0700",
			"2006-01-02 15:04:05Z07",
		)
	} else {
		layouts = append(layouts,
			"2006-01-02 15:04:05.999999999",
			"2006-01-02 15:04:05",
		)
	}
	for _, layout := range layouts {
		var parsed time.Time
		var err error
		if withZone {
			parsed, err = time.Parse(layout, value)
		} else {
			parsed, err = time.ParseInLocation(layout, value, time.UTC)
		}
		if err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported PostgreSQL timestamp %q", value)
}

func databaseTypeName(oid uint32) string {
	names := map[uint32]string{
		16: "BOOL", 17: "BYTEA", 18: "CHAR", 19: "NAME", 20: "INT8", 21: "INT2", 23: "INT4",
		25: "TEXT", 26: "OID", 114: "JSON", 700: "FLOAT4", 701: "FLOAT8", 1042: "BPCHAR",
		1043: "VARCHAR", 1082: "DATE", 1083: "TIME", 1114: "TIMESTAMP", 1184: "TIMESTAMPTZ",
		1186: "INTERVAL", 1266: "TIMETZ", 1700: "NUMERIC", 2950: "UUID", 3802: "JSONB",
		1000: "BOOL[]", 1005: "INT2[]", 1007: "INT4[]", 1009: "TEXT[]", 1015: "VARCHAR[]",
		1016: "INT8[]", 1021: "FLOAT4[]", 1022: "FLOAT8[]", 2951: "UUID[]",
	}
	if name, ok := names[oid]; ok {
		return name
	}
	return fmt.Sprintf("OID_%d", oid)
}

// Error preserves PostgreSQL SQLSTATE so the repository's bounded transaction
// retry policy can distinguish serialization/deadlock failures from business
// errors without depending on a particular third-party Go driver type.
type Error struct {
	State      string
	Severity   string
	Message    string
	Detail     string
	Constraint string
	Table      string
	Column     string
}

func (e *Error) Error() string {
	if e == nil {
		return "libpq: PostgreSQL error"
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "PostgreSQL error"
	}
	if e.State != "" {
		return fmt.Sprintf("libpq: %s (SQLSTATE %s)", message, e.State)
	}
	return "libpq: " + message
}
func (e *Error) SQLState() string {
	if e == nil {
		return ""
	}
	return e.State
}

func resultError(result *C.PGresult) error {
	if result == nil {
		return errors.New("libpq: nil PostgreSQL result")
	}
	field := func(code C.char) string {
		pointer := C.PQresultErrorField(result, C.int(code))
		if pointer == nil {
			return ""
		}
		return strings.TrimSpace(C.GoString(pointer))
	}
	message := field(C.campaign_diag_message_primary())
	if message == "" {
		message = strings.TrimSpace(C.GoString(C.PQresultErrorMessage(result)))
	}
	return &Error{
		State:      field(C.campaign_diag_sqlstate()),
		Severity:   field(C.campaign_diag_severity()),
		Message:    message,
		Detail:     field(C.campaign_diag_message_detail()),
		Constraint: field(C.campaign_diag_constraint_name()),
		Table:      field(C.campaign_diag_table_name()),
		Column:     field(C.campaign_diag_column_name()),
	}
}

func connectionError(pg *C.PGconn) string {
	if pg == nil {
		return "nil PostgreSQL connection"
	}
	return strings.TrimSpace(C.GoString(C.PQerrorMessage(pg)))
}

var (
	_ driver.Driver                         = (*Driver)(nil)
	_ driver.DriverContext                  = (*Driver)(nil)
	_ driver.Connector                      = (*connector)(nil)
	_ driver.Conn                           = (*conn)(nil)
	_ driver.ConnPrepareContext             = (*conn)(nil)
	_ driver.ConnBeginTx                    = (*conn)(nil)
	_ driver.Pinger                         = (*conn)(nil)
	_ driver.SessionResetter                = (*conn)(nil)
	_ driver.Validator                      = (*conn)(nil)
	_ driver.NamedValueChecker              = (*conn)(nil)
	_ driver.ExecerContext                  = (*conn)(nil)
	_ driver.QueryerContext                 = (*conn)(nil)
	_ driver.Stmt                           = (*stmt)(nil)
	_ driver.StmtExecContext                = (*stmt)(nil)
	_ driver.StmtQueryContext               = (*stmt)(nil)
	_ driver.Rows                           = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeLength           = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
)
