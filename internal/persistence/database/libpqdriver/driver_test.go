//go:build cgo

package libpqdriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDriverIsRegistered(t *testing.T) {
	for _, name := range sql.Drivers() {
		if name == driverName {
			return
		}
	}
	t.Fatalf("driver %q is not registered: %v", driverName, sql.Drivers())
}

func TestParameterEncoding(t *testing.T) {
	instant := time.Date(2026, 8, 6, 18, 30, 15, 123456000, time.FixedZone("test", 3600))
	var absentTime *time.Time
	tests := []struct {
		name  string
		value any
		want  string
		null  bool
	}{
		{name: "null", value: nil, null: true},
		{name: "typed nil time pointer", value: absentTime, null: true},
		{name: "bytea", value: []byte{0, 0xff}, want: `\x00ff`},
		{name: "time", value: instant, want: "2026-08-06 17:30:15.123456Z"},
		{name: "string array", value: []string{"SEND_TEXT", "value,with,comma", `quote"and\\slash`, "NULL", ""}, want: `{"SEND_TEXT","value,with,comma","quote\"and\\\\slash","NULL",""}`},
		{name: "empty array", value: []string{}, want: `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, null, err := encodeParameter(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if null != test.null || got != test.want {
				t.Fatalf("got (%q,%v), want (%q,%v)", got, null, test.want, test.null)
			}
		})
	}
}

func TestArrayRoundTrip(t *testing.T) {
	encoded, _, err := encodeParameter([]string{"SEND_TEXT", "a,b", `a"b`, `a\\b`, "NULL", ""})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStringArray(encoded)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"SEND_TEXT", "a,b", `a"b`, `a\\b`, "NULL", ""}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded=%#v want=%#v encoded=%q", decoded, want, encoded)
	}
}

func TestDecodePostgreSQLTypes(t *testing.T) {
	value, err := decodeValue(1184, []byte("2026-08-06 17:30:15.123456+00"))
	if err != nil {
		t.Fatal(err)
	}
	if got := value.(time.Time); !got.Equal(time.Date(2026, 8, 6, 17, 30, 15, 123456000, time.UTC)) {
		t.Fatalf("time=%s", got)
	}
	value, err = decodeValue(1009, []byte(`{"SEND_TEXT","a,b","a\"b"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.([]string), []string{"SEND_TEXT", "a,b", `a"b`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("array=%#v want=%#v", got, want)
	}
}

func TestErrorCarriesSQLState(t *testing.T) {
	err := &Error{State: "40001", Message: "serialization failure"}
	var carrier interface{ SQLState() string }
	if !errors.As(err, &carrier) || carrier.SQLState() != "40001" {
		t.Fatalf("SQLSTATE not preserved: %#v", err)
	}
}

func TestConnectTimeoutIsAddedWithoutOverridingExplicitValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	got := withConnectTimeout(ctx, "postgres://user:pass@example/db?sslmode=require")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if value := parsed.Query().Get("connect_timeout"); value != "3" {
		t.Fatalf("connect_timeout=%q dsn=%q", value, got)
	}
	got = withConnectTimeout(context.Background(), "host=example dbname=db connect_timeout=17")
	if !strings.Contains(got, "connect_timeout=17") || strings.Count(got, "connect_timeout=") != 1 {
		t.Fatalf("explicit timeout changed: %q", got)
	}
}

func TestParameterNULIsRejected(t *testing.T) {
	_, err := buildParameters([]driver.NamedValue{{Ordinal: 1, Value: "unsafe\x00value"}})
	if err == nil || !strings.Contains(err.Error(), "contains NUL") {
		t.Fatalf("err=%v", err)
	}
}

type invalidSliceValuer []string

func (value invalidSliceValuer) Value() (driver.Value, error) { return value, nil }

type validStringValuer string

func (value validStringValuer) Value() (driver.Value, error) { return string(value), nil }

func TestValuerMustReturnDriverValue(t *testing.T) {
	if _, _, err := encodeParameter(invalidSliceValuer{"unsafe"}); err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("invalid valuer err=%v", err)
	}
	got, null, err := encodeParameter(validStringValuer("safe"))
	if err != nil || null || got != "safe" {
		t.Fatalf("valid valuer got=(%q,%v) err=%v", got, null, err)
	}
}

func TestNestedArraysAreRejected(t *testing.T) {
	if _, _, err := encodeParameter([][]string{{"a"}, {"b"}}); err == nil || !strings.Contains(err.Error(), "nested arrays") {
		t.Fatalf("nested array err=%v", err)
	}
}

type namedStringParameter string

type namedIntParameter int64

func TestNamedScalarParametersUseUnderlyingValues(t *testing.T) {
	got, null, err := encodeParameter(namedStringParameter("ACTIVE"))
	if err != nil || null || got != "ACTIVE" {
		t.Fatalf("named string got=(%q,%v) err=%v", got, null, err)
	}
	got, null, err = encodeParameter(namedIntParameter(42))
	if err != nil || null || got != "42" {
		t.Fatalf("named int got=(%q,%v) err=%v", got, null, err)
	}
}
