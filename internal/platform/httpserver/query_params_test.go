package httpserver

import (
	"net/http/httptest"
	"testing"
)

func TestOptionalPositiveIntQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    int
		wantErr bool
	}{
		{name: "missing", query: "", want: 0},
		{name: "valid", query: "?limit=25", want: 25},
		{name: "zero", query: "?limit=0", wantErr: true},
		{name: "negative", query: "?limit=-1", wantErr: true},
		{name: "too large", query: "?limit=501", wantErr: true},
		{name: "non numeric", query: "?limit=many", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/resource"+tc.query, nil)
			got, err := optionalPositiveIntQuery(r, "limit", 500)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestOptionalCursorAndBooleanQueries(t *testing.T) {
	r := httptest.NewRequest("GET", "/resource?afterSequence=42&activeOnly=true", nil)
	sequence, err := optionalUint64Query(r, "afterSequence")
	if err != nil || sequence != 42 {
		t.Fatalf("sequence=%d err=%v", sequence, err)
	}
	active, err := optionalBoolQuery(r, "activeOnly")
	if err != nil || !active {
		t.Fatalf("active=%v err=%v", active, err)
	}

	badSequence := httptest.NewRequest("GET", "/resource?afterSequence=-1", nil)
	if _, err := optionalUint64Query(badSequence, "afterSequence"); err == nil {
		t.Fatal("expected negative cursor to fail")
	}
	badBoolean := httptest.NewRequest("GET", "/resource?activeOnly=perhaps", nil)
	if _, err := optionalBoolQuery(badBoolean, "activeOnly"); err == nil {
		t.Fatal("expected invalid boolean to fail")
	}
}
