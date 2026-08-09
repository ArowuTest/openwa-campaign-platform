package privacy

import (
	"context"
	"testing"
)

func TestPrivacyCaseRequiresCompleteExactMSISDN(t *testing.T) {
	service, _, _ := testService(t)
	_, err := service.Create(context.Background(), CreateInput{
		Type: CaseAccess, MSISDN: "+2348012", Reason: "authorised exact subject lookup", CreatedBy: "privacy-operator",
	}, "correlation-1")
	if err == nil {
		t.Fatal("partial plaintext MSISDN was accepted for an authorised lookup workflow")
	}
}
