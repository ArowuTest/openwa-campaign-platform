package httpserver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"campaign-platform/internal/metacloud"
)

func TestCouncilMetaTemplateSyncDoesNotRehabilitateSenderHealth(t *testing.T) {
	h := newMetaAdminHarness(t)
	maker := "00000000-0000-4000-8000-000000000311"
	submitter := "00000000-0000-4000-8000-000000000312"
	checker := "00000000-0000-4000-8000-000000000313"
	draft, err := h.senders.CreateDraft(context.Background(), metacloud.Sender{OrganisationID: "00000000-0000-4000-8000-000000000401", SenderPoolID: "00000000-0000-4000-8000-000000000402", WABAID: "waba-health", PhoneNumberID: "phone-health", DisplayName: "Meta Health", BusinessPhoneDisplay: "+234 801 234 5678", CredentialKey: "meta-ng", GraphAPIVersion: "v23.0"}, maker, "create sender for health regression")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := h.senders.Submit(context.Background(), draft.ID, draft.Version, submitter, "submit sender for health regression")
	if err != nil {
		t.Fatal(err)
	}
	active, err := h.senders.Decide(context.Background(), pending.ID, pending.Version, true, checker, "approve sender for health regression", time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	unavailable, err := h.senders.ObserveHealth(context.Background(), active.ID, active.Version, metacloud.HealthUnavailable, time.Now().UTC(), checker, "provider send health unavailable")
	if err != nil {
		t.Fatal(err)
	}
	response := metaAdminCall(t, h.handler, h.tokens[maker], http.MethodPost, "/api/v1/admin/meta-senders/"+active.ID+"/templates/sync", map[string]any{"expectedVersion": unavailable.Version, "reason": "synchronise templates without health mutation"})
	if response.Code != http.StatusOK {
		t.Fatalf("sync=%d %s", response.Code, response.Body.String())
	}
	stored, err := h.senders.Get(context.Background(), active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Health != metacloud.HealthUnavailable || stored.Version != unavailable.Version {
		t.Fatalf("template sync changed sender health/version: before=%#v after=%#v", unavailable, stored)
	}
}
