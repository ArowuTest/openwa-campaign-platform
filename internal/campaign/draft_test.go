package campaign

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/sender"
)

const draftOrg = "20000000-0000-4000-8000-000000000001"
const draftPurpose = "30000000-0000-4000-8000-000000000001"
const draftReview = "40000000-0000-4000-8000-000000000001"
const draftGateway = "50000000-0000-4000-8000-000000000001"
const draftPool = "60000000-0000-4000-8000-000000000001"
const draftDefinition = "70000000-0000-4000-8000-000000000001"

func draftEntity(t *testing.T) Campaign {
	t.Helper()
	c, err := New(CreateInput{OrganisationID: draftOrg, PurposeID: draftPurpose, ConsentReviewID: draftReview, Name: "Saved draft", Timezone: "UTC", MaximumUniqueRecipients: 50, MaximumMessagesPerRecipient: 1, CreatedBy: "maker",
		Transport: TransportSelection{Channel: "WHATSAPP", Provider: ProviderOpenWA, Engine: EngineBaileys, RoutingMode: RoutingSenderPool, GatewayPoolID: draftGateway, SenderPoolID: draftPool, AdapterVersion: "v1",
			ProviderDefinitionID: draftDefinition, ProviderDefinitionVersion: 1, GatewayPoolVersion: 1, RequiredCapabilities: []string{"SEND_TEXT"}, FallbackMode: FallbackNone, RoutingPolicyVersion: "r1", CapacityEvidenceVersion: "c1"}}, time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	c.SenderPool = draftPool
	return c
}
func draftInput(c Campaign) DraftSaveInput {
	tr := c.Transport
	tr.ProviderDefinitionID = ""
	tr.ProviderDefinitionVersion = 0
	tr.GatewayPoolVersion = 0
	return DraftSaveInput{ExpectedVersion: c.Version, ActorID: "operator", Reason: "Correct draft fields", Name: c.Name, OrganisationID: c.OrganisationID, PurposeID: c.PurposeID, ConsentReviewID: c.ConsentReviewID,
		RequestedStartAt: c.RequestedStartAt, CompletionDeadlineAt: c.CompletionDeadlineAt, Timezone: c.Timezone, QuietHoursStart: c.QuietHoursStart, QuietHoursEnd: c.QuietHoursEnd,
		MaximumUniqueRecipients: c.MaximumUniqueRecipients, MaximumMessagesPerRecipient: 1, Transport: tr}
}
func TestDraftSaveAllowedFieldsAndUTC(t *testing.T) {
	c := draftEntity(t)
	input := draftInput(c)
	input.Name = "  Edited draft  "
	input.MaximumUniqueRecipients = 123
	start := time.Date(2026, 10, 9, 12, 0, 0, 0, time.FixedZone("offset", 3600))
	end := start.Add(time.Hour)
	input.RequestedStartAt = &start
	input.CompletionDeadlineAt = &end
	input.Timezone = "Africa/Lagos"
	input.QuietHoursStart = "22:00"
	input.QuietHoursEnd = "06:00"
	input.Transport.CapacityEvidenceVersion = "c2"
	now := time.Date(2026, 10, 8, 2, 0, 0, 0, time.FixedZone("offset", 3600))
	got, event, err := c.SaveDraft(input, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Edited draft" || got.MaximumUniqueRecipients != 123 || got.Transport.CapacityEvidenceVersion != "c2" || got.RequestedStartAt.Format(time.RFC3339) != "2026-10-09T11:00:00Z" || got.UpdatedAt.Format(time.RFC3339) != "2026-10-08T01:00:00Z" {
		t.Fatalf("save=%+v", got)
	}
	if got.ID != c.ID || got.CreatedBy != "maker" || !got.CreatedAt.Equal(c.CreatedAt) || got.Status != StatusDraft || got.Version != 2 {
		t.Fatal("save altered identity or status")
	}
	if !reflect.DeepEqual(event.ChangedFields, []string{"NAME", "SCHEDULE", "TRANSPORT", "ENTITLEMENT"}) || event.ActorID != "operator" || event.PreviousStatus != StatusDraft || event.NewStatus != StatusDraft || event.PreviousVersion != 1 || event.NewVersion != 2 {
		t.Fatalf("event=%+v", event)
	}
}
func TestDraftSaveIncompleteScheduleAndClear(t *testing.T) {
	c := draftEntity(t)
	input := draftInput(c)
	start := time.Now().UTC()
	input.RequestedStartAt = &start
	saved, _, err := c.SaveDraft(input, time.Now())
	if err != nil || saved.RequestedStartAt == nil || saved.CompletionDeadlineAt != nil {
		t.Fatalf("incomplete schedule=%+v %v", saved, err)
	}
	input = draftInput(saved)
	input.RequestedStartAt = nil
	input.QuietHoursStart = ""
	input.QuietHoursEnd = ""
	saved, event, err := saved.SaveDraft(input, time.Now())
	if err != nil || saved.RequestedStartAt != nil || len(event.ChangedFields) != 1 || event.ChangedFields[0] != "SCHEDULE" {
		t.Fatalf("clear=%+v %+v %v", saved, event, err)
	}
}
func TestDraftSaveNoOp(t *testing.T) {
	c := draftEntity(t)
	saved, event, err := c.SaveDraft(draftInput(c), time.Now())
	if err != nil || !reflect.DeepEqual(saved, c) || event.ID != "" || len(event.ChangedFields) != 0 {
		t.Fatalf("no-op must preserve row/version/time and emit no event: %+v %+v %v", saved, event, err)
	}
	input := draftInput(c)
	input.ExpectedVersion = 2
	if _, _, err := c.SaveDraft(input, time.Now()); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale no-op=%v", err)
	}
}
func TestDraftSaveRejectsNonDraftAndFrozenEvidence(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Campaign)
	}{
		{"status", func(c *Campaign) { c.Status = StatusConsentReviewPending }},
		{"audience", func(c *Campaign) { c.AudienceSnapshotID = "snapshot" }}, {"audience hash", func(c *Campaign) { c.AudienceSnapshotHash = "hash" }},
		{"message", func(c *Campaign) { c.MessageVersionID = "message" }}, {"message hash", func(c *Campaign) { c.MessageContentHash = "hash" }},
		{"final", func(c *Campaign) { c.FinalApprovedBy = "checker" }}, {"commercial", func(c *Campaign) { c.CommercialApprovalID = "approval" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := draftEntity(t)
			tc.mutate(&c)
			if _, _, err := c.SaveDraft(draftInput(c), time.Now()); !errors.Is(err, ErrDraftNotEditable) {
				t.Fatalf("frozen=%v", err)
			}
		})
	}
	for _, field := range []string{"organisation", "purpose", "review"} {
		t.Run(field, func(t *testing.T) {
			c := draftEntity(t)
			input := draftInput(c)
			switch field {
			case "organisation":
				input.OrganisationID = draftPool
			case "purpose":
				input.PurposeID = draftPool
			case "review":
				input.ConsentReviewID = draftPool
			}
			if _, _, err := c.SaveDraft(input, time.Now()); !errors.Is(err, ErrDraftReferencesLocked) {
				t.Fatalf("tuple=%v", err)
			}
		})
	}
}
func TestDraftSaveInvalidFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*DraftSaveInput)
	}{
		{"actor", func(i *DraftSaveInput) { i.ActorID = "" }}, {"reason short", func(i *DraftSaveInput) { i.Reason = "short" }}, {"reason long", func(i *DraftSaveInput) { i.Reason = strings.Repeat("x", 1001) }},
		{"name", func(i *DraftSaveInput) { i.Name = " " }}, {"name long", func(i *DraftSaveInput) { i.Name = strings.Repeat("x", 251) }},
		{"count zero", func(i *DraftSaveInput) { i.MaximumUniqueRecipients = 0 }}, {"count unsafe", func(i *DraftSaveInput) { i.MaximumUniqueRecipients = 9007199254740992 }}, {"message count", func(i *DraftSaveInput) { i.MaximumMessagesPerRecipient = 2 }},
		{"timezone omitted", func(i *DraftSaveInput) { i.Timezone = "" }}, {"timezone invalid", func(i *DraftSaveInput) { i.Timezone = "Moon/Sea" }},
		{"quiet half", func(i *DraftSaveInput) { i.QuietHoursStart = "22:00" }}, {"quiet format", func(i *DraftSaveInput) { i.QuietHoursStart = "2:00"; i.QuietHoursEnd = "06:00" }}, {"quiet equal", func(i *DraftSaveInput) { i.QuietHoursStart = "22:00"; i.QuietHoursEnd = "22:00" }},
		{"schedule inverted", func(i *DraftSaveInput) {
			a := time.Now()
			b := a.Add(-time.Hour)
			i.RequestedStartAt = &a
			i.CompletionDeadlineAt = &b
		}},
		{"meta", func(i *DraftSaveInput) { i.Transport.Provider = ProviderMeta }}, {"fallback", func(i *DraftSaveInput) { i.Transport.FallbackMode = "ENGINE_FALLBACK" }},
		{"session", func(i *DraftSaveInput) { i.Transport.SessionID = draftPool }}, {"pool UUID", func(i *DraftSaveInput) { i.Transport.SenderPoolID = "pool" }},
		{"version long", func(i *DraftSaveInput) { i.Transport.AdapterVersion = strings.Repeat("x", 101) }},
		{"capability unknown", func(i *DraftSaveInput) { i.Transport.RequiredCapabilities = []string{"MADE_UP"} }}, {"capability duplicate", func(i *DraftSaveInput) { i.Transport.RequiredCapabilities = []string{"SEND_TEXT", "SEND_TEXT"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := draftEntity(t)
			input := draftInput(c)
			tc.mutate(&input)
			if _, _, err := c.SaveDraft(input, time.Now()); !errors.Is(err, ErrDraftInvalid) {
				t.Fatalf("invalid field accepted: %v", err)
			}
		})
	}
}

type draftPurposeReader struct {
	value consent.Purpose
	err   error
}

func (r *draftPurposeReader) Get(_ context.Context, id string) (consent.Purpose, error) {
	if r.err != nil {
		return consent.Purpose{}, r.err
	}
	if id != r.value.ID {
		return consent.Purpose{}, consent.ErrPurposeNotFound
	}
	return r.value, nil
}

type draftReviewReader struct {
	value consent.Review
	err   error
}

func (r *draftReviewReader) Get(_ context.Context, id string) (consent.Review, error) {
	if r.err != nil {
		return consent.Review{}, r.err
	}
	return r.value, nil
}

type draftPoolReader struct {
	values map[string]sender.Pool
	wrong  bool
}

func (r *draftPoolReader) GetPool(_ context.Context, id string) (sender.Pool, error) {
	p, ok := r.values[id]
	if !ok {
		return sender.Pool{}, sender.ErrSenderNotFound
	}
	if r.wrong {
		p.ID = draftGateway
	}
	return p, nil
}

type draftPolicies struct{ err error }

func (p draftPolicies) ValidatePurpose(context.Context, string, string) error { return p.err }
func draftService(t *testing.T) (*Service, *MemoryRepository, *draftPurposeReader, *draftReviewReader, *draftPoolReader, *provider.MemoryStore, Campaign) {
	t.Helper()
	c := draftEntity(t)
	repo := NewMemoryRepository()
	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	orgs := organisation.NewMemoryRepository()
	if err := orgs.Create(context.Background(), organisation.Organisation{ID: draftOrg, Status: organisation.StatusActive}); err != nil {
		t.Fatal(err)
	}
	purposes := &draftPurposeReader{value: consent.Purpose{ID: draftPurpose, OrganisationID: draftOrg, ConsentReviewID: draftReview, Channel: "WHATSAPP", WordingVersion: "w1", Active: true}}
	reviews := &draftReviewReader{value: consent.Review{ID: draftReview, OrganisationID: draftOrg, Channel: "WHATSAPP", WordingVersion: "w1", Status: consent.StatusPending, Scope: consent.ReviewScopeOrganisation}}
	pools := &draftPoolReader{values: map[string]sender.Pool{draftPool: {ID: draftPool, Status: "ACTIVE"}}}
	store := provider.NewMemoryStore()
	now := time.Now().UTC()
	_, err := store.Create(context.Background(), provider.Definition{ID: draftDefinition, Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "v1", Capabilities: []provider.Capability{provider.CapabilitySendText}, Status: provider.StatusActive, EffectiveFrom: now.Add(-24 * time.Hour), Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	gateway := campaignGatewayPools{pool: sender.GatewayPool{ID: draftGateway, Provider: sender.GatewayProviderOpenWA, Engine: sender.GatewayEngineBaileys, AdapterVersion: "v1", Status: sender.GatewayPoolActive, Version: 1, Capabilities: []sender.Capability{sender.CapabilitySendText}}}
	svc := NewService(repo).WithOrganisationReader(organisation.NewService(orgs)).WithOrganisationPolicies(draftPolicies{}).WithConsentPurposeReader(purposes).WithConsentReviewReader(reviews).WithSenderPoolReader(pools).WithProviderCapabilities(&provider.Service{Store: store}).WithGatewayPools(gateway)
	return svc, repo, purposes, reviews, pools, store, c
}
func assertDraftUnchanged(t *testing.T, repo *MemoryRepository, c Campaign) {
	t.Helper()
	got, _ := repo.Get(context.Background(), c.ID)
	events, _ := repo.ListMaterialChanges(context.Background(), c.ID)
	if !reflect.DeepEqual(got, c) || len(events) != 0 {
		t.Fatal("rejection mutated campaign/audit")
	}
}
func TestDraftSaveGovernanceAndRetirement(t *testing.T) {
	cases := []string{"nil purpose", "nil review", "nil pools", "nil provider", "nil gateway", "nil policies", "retired purpose", "missing pool", "paused pool", "retired pool", "wrong pool", "revoked", "expired", "superseded"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			svc, repo, purposes, reviews, pools, _, c := draftService(t)
			want := ErrDraftInvalid
			switch name {
			case "nil purpose":
				svc.WithConsentPurposeReader(nil)
				want = ErrDraftGovernanceUnavailable
			case "nil review":
				svc.WithConsentReviewReader(nil)
				want = ErrDraftGovernanceUnavailable
			case "nil pools":
				svc.WithSenderPoolReader(nil)
				want = ErrDraftGovernanceUnavailable
			case "nil provider":
				svc.WithProviderCapabilities(nil)
				want = ErrDraftGovernanceUnavailable
			case "nil gateway":
				svc.WithGatewayPools(nil)
				want = ErrDraftGovernanceUnavailable
			case "nil policies":
				svc.WithOrganisationPolicies(nil)
				want = ErrDraftGovernanceUnavailable
			case "retired purpose":
				purposes.value.Active = false
			case "missing pool":
				delete(pools.values, draftPool)
			case "paused pool":
				pools.values[draftPool] = sender.Pool{ID: draftPool, Status: "PAUSED"}
			case "retired pool":
				pools.values[draftPool] = sender.Pool{ID: draftPool, Status: "RETIRED"}
			case "wrong pool":
				pools.wrong = true
			case "revoked":
				reviews.value.Status = consent.StatusRevoked
			case "expired":
				past := time.Now().Add(-time.Hour)
				reviews.value.ExpiresAt = &past
			case "superseded":
				reviews.value.SupersededByID = draftPool
			}
			input := draftInput(c)
			input.Name = "Edited"
			if _, err := svc.SaveDraft(context.Background(), c.ID, input); !errors.Is(err, want) {
				t.Fatalf("governance=%v want=%v", err, want)
			}
			assertDraftUnchanged(t, repo, c)
		})
	}
}
func TestDraftSavePurposeReviewBinding(t *testing.T) {
	for _, name := range []string{"purpose review", "wording", "review ID", "organisation", "channel", "campaign scope", "policy"} {
		t.Run(name, func(t *testing.T) {
			svc, repo, purposes, reviews, _, _, c := draftService(t)
			switch name {
			case "purpose review":
				purposes.value.ConsentReviewID = draftPool
			case "wording":
				reviews.value.WordingVersion = "w2"
			case "review ID":
				reviews.value.ID = draftPool
			case "organisation":
				reviews.value.OrganisationID = draftPool
			case "channel":
				reviews.value.Channel = "SMS"
			case "campaign scope":
				reviews.value.Scope = consent.ReviewScopeCampaign
				reviews.value.CampaignID = draftPool
			case "policy":
				svc.WithOrganisationPolicies(draftPolicies{err: errors.New("not permitted")})
			}
			if _, err := svc.SaveDraft(context.Background(), c.ID, draftInput(c)); !errors.Is(err, ErrDraftInvalid) {
				t.Fatalf("binding=%v", err)
			}
			assertDraftUnchanged(t, repo, c)
		})
	}
}
func TestDraftSavePreservesFrozenTransportOnUnrelatedEdit(t *testing.T) {
	svc, repo, _, _, _, store, c := draftService(t)
	input := draftInput(c)
	input.Name = "Edited"
	saved, err := svc.SaveDraft(context.Background(), c.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Transport.ProviderDefinitionID != draftDefinition || saved.Transport.ProviderDefinitionVersion != 1 || saved.Transport.GatewayPoolVersion != 1 {
		t.Fatal("unrelated edit rebound transport")
	}
	// Active definition replacement must invalidate the saved binding, even on no-op.
	def, err := store.Get(context.Background(), draftDefinition)
	if err != nil {
		t.Fatal(err)
	}
	def.Status = provider.StatusRetired
	def.Version = 2
	if _, err := store.CompareAndSwap(context.Background(), def, 1, "checker", "RETIRED"); err != nil {
		t.Fatal(err)
	}
	replacement := def
	replacement.ID = "70000000-0000-4000-8000-000000000002"
	replacement.Status = provider.StatusActive
	replacement.Version = 1
	if _, err := store.Create(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveDraft(context.Background(), saved.ID, draftInput(saved)); !errors.Is(err, ErrDraftInvalid) {
		t.Fatalf("provider replacement accepted=%v", err)
	}
	got, _ := repo.Get(context.Background(), c.ID)
	if got.Version != 2 || got.Transport.ProviderDefinitionID != draftDefinition {
		t.Fatal("invalid binding changed")
	}
}
func TestDraftSaveNeverApprovesOrDispatches(t *testing.T) {
	svc, repo, _, reviews, _, _, c := draftService(t)
	input := draftInput(c)
	input.Name = "Edited"
	saved, err := svc.SaveDraft(context.Background(), c.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != StatusDraft || saved.FinalApprovedBy != "" || saved.CommercialApprovalID != "" || saved.AudienceSnapshotID != "" || saved.MessageVersionID != "" || reviews.value.Status != consent.StatusPending {
		t.Fatal("save approved or bound evidence")
	}
	events, _ := repo.ListMaterialChanges(context.Background(), c.ID)
	if len(events) != 1 || events[0].Sequence != 1 {
		t.Fatal("save audit missing")
	}
}

type draftRacingRepository struct {
	*MemoryRepository
	race func()
}

func (r *draftRacingRepository) SaveDraft(ctx context.Context, c Campaign, e MaterialChangeEvent, v int64) (Campaign, error) {
	r.race()
	return r.MemoryRepository.SaveDraft(ctx, c, e, v)
}
func TestDraftSaveNoOpAuthoritativeRace(t *testing.T) {
	svc, repo, _, _, _, _, c := draftService(t)
	svc.repository = &draftRacingRepository{MemoryRepository: repo, race: func() {
		next := c
		next.Status = StatusConsentReviewPending
		next.Version++
		if err := repo.CompareAndSwap(context.Background(), next, 1); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := svc.SaveDraft(context.Background(), c.ID, draftInput(c)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale no-op=%v", err)
	}
	got, _ := repo.Get(context.Background(), c.ID)
	events, _ := repo.ListMaterialChanges(context.Background(), c.ID)
	if got.Status != StatusConsentReviewPending || got.Version != 2 || len(events) != 0 {
		t.Fatal("no-op overwrote transition")
	}
}

func TestDraftSaveRepositoryFrozenAuthority(t *testing.T) {
	c := draftEntity(t)
	repo := NewMemoryRepository()
	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	input := draftInput(c)
	input.Name = "Edited"
	next, event, err := c.SaveDraft(input, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	next.Transport.ProviderDefinitionID = draftPool
	next.Transport.ProviderDefinitionVersion = 9
	if _, err := repo.SaveDraft(context.Background(), next, event, 1); !errors.Is(err, ErrDraftInvalid) {
		t.Fatalf("repository silently rebound frozen authority: %v", err)
	}
	assertDraftUnchanged(t, repo, c)
}

func TestDraftSaveIANAZoneValidation(t *testing.T) {
	for _, zone := range []string{"Africa/Lagos", "Europe/London", "UTC", "Moon/Sea", "Local"} {
		t.Run(zone, func(t *testing.T) {
			c := draftEntity(t)
			input := draftInput(c)
			input.Timezone = zone
			_, _, err := c.SaveDraft(input, time.Now())
			if zone == "Moon/Sea" || zone == "Local" {
				if !errors.Is(err, ErrDraftInvalid) {
					t.Fatalf("non-IANA zone accepted: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestDraftSaveChecksSavedAndSelectedSenderPool(t *testing.T) {
	for _, name := range []string{"saved paused", "selected paused", "selected missing", "both active"} {
		t.Run(name, func(t *testing.T) {
			svc, repo, _, _, pools, _, c := draftService(t)
			selected := "60000000-0000-4000-8000-000000000002"
			pools.values[selected] = sender.Pool{ID: selected, Status: "ACTIVE"}
			switch name {
			case "saved paused":
				pools.values[draftPool] = sender.Pool{ID: draftPool, Status: "PAUSED"}
			case "selected paused":
				pools.values[selected] = sender.Pool{ID: selected, Status: "PAUSED"}
			case "selected missing":
				delete(pools.values, selected)
			}
			input := draftInput(c)
			input.Transport.SenderPoolID = selected
			got, err := svc.SaveDraft(context.Background(), c.ID, input)
			if name == "both active" {
				if err != nil || got.Transport.SenderPoolID != selected || got.SenderPool != selected || got.Version != 2 {
					t.Fatalf("deliberate pool change=%+v %v", got, err)
				}
				events, _ := repo.ListMaterialChanges(context.Background(), c.ID)
				if len(events) != 1 || !reflect.DeepEqual(events[0].ChangedFields, []string{"TRANSPORT"}) {
					t.Fatalf("pool event=%+v", events)
				}
			} else {
				if !errors.Is(err, ErrDraftInvalid) {
					t.Fatalf("invalid pool accepted=%v", err)
				}
				assertDraftUnchanged(t, repo, c)
			}
		})
	}
}

func TestDraftSaveRepositoryRejectInvalidCandidate(t *testing.T) {
	c := draftEntity(t)
	repo := NewMemoryRepository()
	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	input := draftInput(c)
	input.MaximumUniqueRecipients = 51
	next, event, err := c.SaveDraft(input, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	next.MaximumUniqueRecipients = 9007199254740992
	if _, err := repo.SaveDraft(context.Background(), next, event, 1); !errors.Is(err, ErrDraftInvalid) {
		t.Fatalf("repository accepted unsafe count: %v", err)
	}
	assertDraftUnchanged(t, repo, c)
}

type authoritativeDraftRepository struct {
	*MemoryRepository
	savedTime time.Time
	reads     int
}

func (r *authoritativeDraftRepository) Get(ctx context.Context, identifier string) (Campaign, error) {
	r.reads++
	c, err := r.MemoryRepository.Get(ctx, identifier)
	if r.reads > 1 {
		c.Name = "Later concurrent editor"
		c.Version += 10
	}
	return c, err
}
func (r *authoritativeDraftRepository) SaveDraft(ctx context.Context, c Campaign, event MaterialChangeEvent, expected int64) (Campaign, error) {
	if _, err := r.MemoryRepository.SaveDraft(ctx, c, event, expected); err != nil {
		return Campaign{}, err
	}
	r.MemoryRepository.mu.Lock()
	defer r.MemoryRepository.mu.Unlock()
	stored := r.MemoryRepository.items[c.ID]
	stored.UpdatedAt = r.savedTime
	r.MemoryRepository.items[c.ID] = stored
	return cloneDraftCampaign(stored), nil
}
func TestDraftSaveReturnsAtomicPersistedTimestampWithoutLaterRead(t *testing.T) {
	svc, base, _, _, _, _, c := draftService(t)
	repository := &authoritativeDraftRepository{MemoryRepository: base, savedTime: time.Date(2026, 10, 8, 4, 30, 0, 123456000, time.UTC)}
	svc.repository = repository
	input := draftInput(c)
	input.Name = "Authoritative saved draft"
	saved, err := svc.SaveDraft(context.Background(), c.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.UpdatedAt.Equal(repository.savedTime) {
		t.Fatalf("service returned predicted timestamp instead of authoritative persisted timestamp")
	}
	if saved.Name != input.Name || saved.Version != c.Version+1 || repository.reads != 1 {
		t.Fatal("service performed a later post-commit read instead of returning the atomic saved snapshot")
	}
}
