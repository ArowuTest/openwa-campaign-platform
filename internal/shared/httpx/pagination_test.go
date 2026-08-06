package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePageBoundsAndCursor(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/?limit=250&cursor=abc-123", nil)
	page, err := ParsePage(request, 100, 500)
	if err != nil || page.Limit != 250 || page.Cursor != "abc-123" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := ParsePage(httptest.NewRequest(http.MethodGet, "/?limit=501", nil), 100, 500); err == nil {
		t.Fatal("oversized page accepted")
	}
}

func TestWriteListUsesUniformEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteList(recorder, http.StatusOK, []string{"a"}, 1, "next")
	var decoded ListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Count != 1 || decoded.NextCursor != "next" || !decoded.HasMore {
		t.Fatalf("response=%+v", decoded)
	}
}
