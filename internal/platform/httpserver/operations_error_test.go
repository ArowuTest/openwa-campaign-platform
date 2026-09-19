package httpserver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"campaign-platform/internal/operations"
)

func TestUnknownOperationsErrorDoesNotLeakInternalDetail(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operations/delivery-exceptions/recipient/resolve", nil)
	res := httptest.NewRecorder()
	writeOperationsError(res, req, errors.New("pq: password=super-secret relation=campaign_recipients"))
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want %d", res.Code, http.StatusInternalServerError)
	}
	body := res.Body.String()
	for _, forbidden := range []string{"super-secret", "password=", "campaign_recipients", "pq:"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("internal error leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "OPERATIONS_INTERNAL") {
		t.Fatalf("safe error code missing: %s", body)
	}
}

func TestDuplicateRiskApprovalErrorExposesOperatorRiskAndApprovalField(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operations/delivery-exceptions/recipient/resolve", nil)
	res := httptest.NewRecorder()
	writeOperationsError(res, req, operations.ErrApprovalRequired)
	if res.Code != http.StatusConflict {
		t.Fatalf("status=%d", res.Code)
	}
	body := res.Body.String()
	for _, want := range []string{"DUPLICATE_RISK_APPROVAL_REQUIRED", "POSSIBLE_DUPLICATE_SEND", "duplicateRiskAccepted"} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q: %s", want, body)
		}
	}
}
