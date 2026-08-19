package metacloud

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/message"
)

type fakeMessageReader struct{ value message.Version }

func (f fakeMessageReader) Get(context.Context, string) (message.Version, error) { return f.value, nil }

type fakeTemplateClient struct {
	items  []Template
	called int
}

func (f *fakeTemplateClient) ListTemplates(context.Context, TemplateListRequest) ([]Template, error) {
	f.called++
	return append([]Template(nil), f.items...), nil
}

func TestTemplateServiceSyncsFetchedCatalogueThenPersists(t *testing.T) {
	now := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	client := &fakeTemplateClient{items: []Template{{MetaTemplateID: "1", Name: "hello", Language: "en_US", Category: "MARKETING", Status: "APPROVED", Components: []byte(`[]`)}}}
	store := NewMemoryTemplateStore()
	svc := &TemplateService{Store: store, Client: client, Clock: func() time.Time { return now }}
	sender := Sender{OrganisationID: "org", WABAID: "waba", CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", Status: StatusActive}
	if err := svc.SyncSenderTemplates(context.Background(), sender); err != nil {
		t.Fatal(err)
	}
	approved, err := store.ListApproved(context.Background(), "org", "waba")
	if err != nil || client.called != 1 || len(approved) != 1 || approved[0].LastSyncedAt != now {
		t.Fatalf("approved=%#v called=%d err=%v", approved, client.called, err)
	}
}

func TestTemplateServiceCreatesBindingOnlyAgainstApprovedCompatibleTemplate(t *testing.T) {
	now := time.Date(2026, 8, 12, 1, 5, 0, 0, time.UTC)
	version := approvedTextVersion()
	store := NewMemoryTemplateStore()
	components := []byte(`[{"type":"BODY","text":"Hello {{1}}, order {{2}} is ready"}]`)
	hash, err := CanonicalComponentHash(components)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceWABATemplates(context.Background(), "org", "waba", []Template{{MetaTemplateID: "1", Name: "order_ready", Language: "en_US", Category: "UTILITY", Status: "APPROVED", Components: components, ComponentHash: hash}}, now); err != nil {
		t.Fatal(err)
	}
	svc := &TemplateService{Store: store, Messages: fakeMessageReader{value: version}, Clock: func() time.Time { return now }}
	binding := Binding{MessageVersionID: version.ID, TemplateName: "order_ready", Language: "en_US", BodyVariableNames: []string{"first_name", "order_id"}, TemplateComponentHash: hash, CreatedBy: "checker"}
	created, err := svc.CreateBinding(context.Background(), "org", "waba", binding)
	if err != nil || created.CreatedAt != now {
		t.Fatalf("binding=%#v err=%v", created, err)
	}
	bad := binding
	bad.BodyVariableNames = []string{"order_id", "first_name"}
	bad.MessageVersionID = "other"
	if _, err := svc.CreateBinding(context.Background(), "org", "waba", bad); err == nil {
		t.Fatal("incompatible binding accepted")
	}
}
