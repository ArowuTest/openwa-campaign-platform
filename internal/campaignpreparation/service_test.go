package campaignpreparation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/commercial"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/message"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/platformpolicy"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/segment"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/storage"
	"campaign-platform/internal/testmessage"
)

type readFunc[T any] func(context.Context, string) (T, error)

func (f readFunc[T]) Get(c context.Context, id string) (T, error) { return f(c, id) }

type policyFunc func(context.Context, string, string) error

func (f policyFunc) ValidatePurpose(c context.Context, o, p string) error { return f(c, o, p) }

type reviewRead struct {
	readFunc[consent.Review]
	validate func(context.Context, string, string, string, string, time.Time) error
}

func (f reviewRead) ValidateCampaignReview(c context.Context, id, ca, o, ch string, at time.Time) error {
	return f.validate(c, id, ca, o, ch, at)
}

type providerRead struct {
	readFunc[provider.Definition]
	require func(context.Context, string, provider.Channel, string, time.Time, []provider.Capability) (provider.Definition, error)
}

func (f providerRead) Require(c context.Context, p string, ch provider.Channel, e string, at time.Time, caps []provider.Capability) (provider.Definition, error) {
	return f.require(c, p, ch, e, at, caps)
}

type poolRead struct {
	value *sender.Pool
	err   error
}

func (f poolRead) GetPool(context.Context, string) (sender.Pool, error) { return *f.value, f.err }

type assetFunc func(context.Context, string, storage.AssetPurpose, int64) (storage.Asset, error)

func (f assetFunc) ResolveClean(c context.Context, id string, p storage.AssetPurpose, n int64) (storage.Asset, error) {
	return f(c, id, p, n)
}

type commercialRead struct {
	value *commercial.Record
	err   error
}

func (f commercialRead) GetByCampaign(context.Context, string) (commercial.Record, error) {
	return *f.value, f.err
}
func (f commercialRead) ValidateCampaignApproval(_ context.Context, c, o string, n int64) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	v := f.value
	if v.Status != commercial.StatusApproved || v.CampaignID != c || v.OrganisationID != o || v.ApprovedRecipients < n {
		return "", commercial.ErrNotApproved
	}
	return v.ID, nil
}

type routeRead struct {
	plan         *execution.RoutingPlan
	reservations *[]execution.CapacityReservation
	err          error
}

func (f routeRead) LatestByCampaign(context.Context, string) (execution.RoutingPlan, error) {
	return *f.plan, f.err
}
func (f routeRead) Reservations(_ context.Context, id string) ([]execution.CapacityReservation, error) {
	if id != f.plan.ID {
		return nil, errors.New("wrong plan lookup")
	}
	return *f.reservations, f.err
}
func (f routeRead) ValidateForExecution(_ context.Context, p execution.RoutingPlan, c campaign.Campaign, at time.Time) error {
	if p.ApprovedBy == "" || p.ApprovedAt.After(at) {
		return execution.ErrRoutingPlanInvalid
	}
	return f.err
}

type sendsRead struct {
	values *[]testmessage.Send
	err    error
}

func (f sendsRead) ListSends(context.Context, string) ([]testmessage.Send, error) {
	return *f.values, f.err
}

type capacityFunc func(context.Context, execution.PoolRoute, time.Time) (execution.PoolCapacity, error)

func (f capacityFunc) RouteCapacity(c context.Context, r execution.PoolRoute, at time.Time) (execution.PoolCapacity, error) {
	return f(c, r, at)
}

type maintenanceFunc func(context.Context, platformpolicy.Operation, platformpolicy.OperationalScope, time.Time) error

func (f maintenanceFunc) Check(c context.Context, o platformpolicy.Operation, s platformpolicy.OperationalScope, at time.Time) error {
	return f(c, o, s, at)
}

type fixture struct {
	service      *Service
	campaign     campaign.Campaign
	purpose      consent.Purpose
	review       consent.Review
	provider     provider.Definition
	gateway      sender.GatewayPool
	pool         sender.Pool
	snapshot     segment.Snapshot
	message      message.Version
	commercial   commercial.Record
	plan         execution.RoutingPlan
	reservations []execution.CapacityReservation
	sends        []testmessage.Send
	capacity     execution.PoolCapacity
	now          time.Time
}

func fixedRead[T any](p *T) readFunc[T] {
	return func(context.Context, string) (T, error) { return *p, nil }
}
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	start, end, expiry := f.now.Add(time.Hour), f.now.Add(3*time.Hour), f.now.Add(24*time.Hour)
	tr := campaign.TransportSelection{Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys, RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "pool", GatewayPoolID: "gateway", GatewayPoolVersion: 2, ProviderDefinitionID: "provider", ProviderDefinitionVersion: 3, AdapterVersion: "1.2.3", FallbackMode: campaign.FallbackNone, RoutingPolicyVersion: "routing-v1", CapacityEvidenceVersion: "capacity-v1"}
	f.campaign = campaign.Campaign{ID: "10000000-0000-4000-8000-000000000001", OrganisationID: "org", PurposeID: "purpose", ConsentReviewID: "review", Name: "Readiness fixture", Status: campaign.StatusCommercialApproved, Version: 9, RequestedStartAt: &start, CompletionDeadlineAt: &end, Timezone: "UTC", MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 1, AudienceSnapshotID: "snapshot", AudienceSnapshotHash: strings.Repeat("a", 64), EligibleAudienceCount: 85, MessageVersionID: "message", MessageContentHash: strings.Repeat("b", 64), CommercialApprovalID: "commercial", Transport: tr}
	f.purpose = consent.Purpose{ID: "purpose", OrganisationID: "org", ConsentReviewID: "review", WordingVersion: "wording-v1", Channel: "WHATSAPP", Active: true}
	f.review = consent.Review{ID: "review", OrganisationID: "org", Scope: consent.ReviewScopeCampaign, CampaignID: f.campaign.ID, Channel: "WHATSAPP", WordingVersion: "wording-v1", Status: consent.StatusApproved, Version: 2, ExpiresAt: &expiry, ReviewedAt: &f.now}
	f.provider = provider.Definition{ID: "provider", Version: 3, Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "1.2.3", Status: provider.StatusActive, EffectiveFrom: f.now.Add(-time.Hour), Capabilities: []provider.Capability{provider.CapabilitySendText}}
	f.gateway = sender.GatewayPool{ID: "gateway", Version: 2, Provider: sender.GatewayProviderOpenWA, Engine: sender.GatewayEngineBaileys, AdapterVersion: "1.2.3", Status: sender.GatewayPoolActive, MinimumHealthyNodes: 1, Capabilities: []sender.Capability{sender.CapabilitySendText}}
	f.pool = sender.Pool{ID: "pool", Status: "ACTIVE", Version: 1}
	f.snapshot = segment.Snapshot{ID: "snapshot", CampaignID: f.campaign.ID, EligibleCount: 85, SnapshotHash: f.campaign.AudienceSnapshotHash, DefinitionVersion: 4, ConfigurationVersion: "config-v1", ConsentPolicyVersion: "consent-v1", CreatedAt: f.now}
	f.message = message.Version{ID: "message", CampaignID: f.campaign.ID, Version: 4, Status: message.StatusApproved, Type: message.TypeText, ContentHash: f.campaign.MessageContentHash, ApprovedBy: "checker", ApprovedAt: &f.now}
	f.commercial = commercial.Record{ID: "commercial", CampaignID: f.campaign.ID, OrganisationID: "org", Status: commercial.StatusApproved, Version: 2, ApprovedRecipients: 100}
	route := execution.PoolRoute{SenderPoolID: "pool", GatewayPoolID: "gateway", Provider: "OPENWA", Engine: "BAILEYS", ProviderAdapterVersion: "1.2.3", ProviderDefinitionID: "provider", ProviderDefinitionVersion: 3, GatewayPoolVersion: 2, MaximumRecipients: 100, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 600, ReservedDailyUnits: 1000}
	f.plan = execution.RoutingPlan{ID: "plan", CampaignID: f.campaign.ID, Version: 1, Routes: []execution.PoolRoute{route}, FallbackMode: "NONE", RoutingPolicyVersion: "routing-v1", CapacityEvidenceVersion: "capacity-v1", ApprovedBy: "checker", ApprovedAt: f.now}
	f.reservations = []execution.CapacityReservation{{ID: "hold", CampaignID: f.campaign.ID, RoutingPlanID: "plan", SenderPoolID: "pool", Status: "HELD", FencingVersion: 1, ReservationStart: start, ReservationEnd: end, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 600, ReservedDailyUnits: 1000}}
	f.sends = []testmessage.Send{{ID: "test", CampaignID: f.campaign.ID, MessageVersionID: "message", MessageContentHash: f.campaign.MessageContentHash, Status: testmessage.SendAccepted, Provider: "OPENWA", Engine: "BAILEYS", GatewayPoolID: "gateway", GatewayPoolVersion: 2, SenderPoolID: "pool", ProviderAdapterVersion: "1.2.3", ProviderDefinitionID: "provider", ProviderDefinitionVersion: 3, SenderSessionID: "session", RouteReference: "route"}}
	f.capacity = execution.PoolCapacity{SenderPoolID: "pool", GatewayPoolID: "gateway", HealthySessions: 2, HealthyNodes: 2, MinimumHealthyNodes: 1, AvailableMessagesPerMinute: 10, AvailableHourlyUnits: 600, AvailableDailyUnits: 1000}
	f.service = &Service{Campaigns: fixedRead(&f.campaign), Organisations: readFunc[organisation.Organisation](func(context.Context, string) (organisation.Organisation, error) {
		return organisation.Organisation{ID: "org", Status: organisation.StatusActive}, nil
	}), Purposes: fixedRead(&f.purpose), OrganisationPolicies: policyFunc(func(context.Context, string, string) error { return nil }), GatewayPools: fixedRead(&f.gateway), SenderPools: poolRead{value: &f.pool}, Snapshots: fixedRead(&f.snapshot), Messages: fixedRead(&f.message), CommercialRecords: commercialRead{value: &f.commercial}, CommercialApprovals: commercialRead{value: &f.commercial}, Routes: routeRead{plan: &f.plan, reservations: &f.reservations}, TestMessages: sendsRead{values: &f.sends}, Capacity: capacityFunc(func(_ context.Context, r execution.PoolRoute, at time.Time) (execution.PoolCapacity, error) {
		if r.SenderPoolID != "pool" || r.Engine != "BAILEYS" || !at.Equal(f.now) {
			return execution.PoolCapacity{}, ErrInvalidEvidence
		}
		return f.capacity, nil
	}), Maintenance: maintenanceFunc(func(context.Context, platformpolicy.Operation, platformpolicy.OperationalScope, time.Time) error {
		return nil
	}), Clock: func() time.Time { return f.now }, SafetyMarginPercent: 15, Assets: assetFunc(func(context.Context, string, storage.AssetPurpose, int64) (storage.Asset, error) {
		return storage.Asset{}, storage.ErrAssetNotFound
	})}
	f.service.Reviews = reviewRead{readFunc: fixedRead(&f.review), validate: func(_ context.Context, id, ca, o, ch string, at time.Time) error {
		r := f.review
		if r.ID != id || r.OrganisationID != o || r.Channel != ch || r.CampaignID != ca || r.Status != consent.StatusApproved || r.ExpiresAt == nil || !r.ExpiresAt.After(at) {
			return errors.New("invalid review")
		}
		return nil
	}}
	f.service.Providers = providerRead{readFunc: fixedRead(&f.provider), require: func(_ context.Context, p string, ch provider.Channel, e string, at time.Time, caps []provider.Capability) (provider.Definition, error) {
		return f.provider, nil
	}}
	return f
}
func assess(t *testing.T, f *fixture) Readiness {
	t.Helper()
	r, e := f.service.Get(context.Background(), f.campaign.ID)
	if e != nil {
		t.Fatalf("expected assessment, got %v", e)
	}
	return r
}
func check(t *testing.T, r Readiness, key, status, code string) {
	t.Helper()
	for _, c := range r.Checks {
		if c.Evidence == nil {
			t.Fatalf("%s lacks evidence array", c.Key)
		}
		if c.Key == key {
			if c.Status != status || c.Code != code {
				t.Fatalf("%s: got %s/%s want %s/%s", key, c.Status, c.Code, status, code)
			}
			return
		}
	}
	t.Fatalf("missing check %s", key)
}
func TestPreparationReadinessDraftBlockers(t *testing.T) {
	f := newFixture(t)
	f.campaign.Status = campaign.StatusDraft
	f.campaign.AudienceSnapshotID = ""
	f.campaign.MessageVersionID = ""
	f.commercial.Status = commercial.StatusDraft
	f.review.Status = consent.StatusPending
	f.sends = nil
	r := assess(t, f)
	if r.State != "BLOCKED" || r.ReadyForFinalReview {
		t.Fatal(r)
	}
	check(t, r, "audience", "BLOCKED", "AUDIENCE_NOT_FROZEN")
	check(t, r, "message", "BLOCKED", "MESSAGE_NOT_APPROVED")
	check(t, r, "consent", "PENDING_REVIEW", "CONSENT_REVIEW_PENDING")
	check(t, r, "commercial", "PENDING_REVIEW", "COMMERCIAL_APPROVAL_PENDING")
	check(t, r, "pilot", "BLOCKED", "PILOT_EVIDENCE_INCOMPLETE")
}
func TestPreparationReadinessExactEvidence(t *testing.T) {
	cases := []struct {
		name, key, code string
		change          func(*fixture)
	}{
		{"snapshot ID", "audience", "AUDIENCE_EVIDENCE_CHANGED", func(f *fixture) { f.snapshot.ID = "other" }},
		{"snapshot hash", "audience", "AUDIENCE_EVIDENCE_CHANGED", func(f *fixture) { f.snapshot.SnapshotHash = strings.Repeat("c", 64) }},
		{"snapshot campaign", "audience", "AUDIENCE_EVIDENCE_CHANGED", func(f *fixture) { f.snapshot.CampaignID = "other" }},
		{"snapshot definition", "audience", "AUDIENCE_EVIDENCE_CHANGED", func(f *fixture) { f.snapshot.DefinitionVersion = 0 }},
		{"message hash", "message", "MESSAGE_EVIDENCE_CHANGED", func(f *fixture) { f.message.ContentHash = strings.Repeat("c", 64) }},
		{"review binding", "purpose", "PURPOSE_REVIEW_MISMATCH", func(f *fixture) { f.purpose.ConsentReviewID = "other" }},
		{"wording binding", "purpose", "PURPOSE_WORDING_MISMATCH", func(f *fixture) { f.purpose.WordingVersion = "other" }},
		{"purpose organisation", "purpose", "PURPOSE_ORGANISATION_MISMATCH", func(f *fixture) { f.purpose.OrganisationID = "other" }},
		{"purpose channel", "purpose", "PURPOSE_NOT_ACTIVE", func(f *fixture) { f.purpose.Channel = "SMS" }},
		{"provider version", "transport", "TRANSPORT_EVIDENCE_CHANGED", func(f *fixture) { f.provider.Version++ }},
		{"gateway ID", "transport", "TRANSPORT_EVIDENCE_CHANGED", func(f *fixture) { f.gateway.ID = "other" }},
		{"pool ID", "transport", "TRANSPORT_EVIDENCE_CHANGED", func(f *fixture) { f.pool.ID = "other" }},
		{"test hash", "pilot", "PILOT_EVIDENCE_INCOMPLETE", func(f *fixture) { f.sends[0].MessageContentHash = "other" }},
		{"hold end", "pilot", "PILOT_EVIDENCE_INCOMPLETE", func(f *fixture) { f.reservations[0].ReservationEnd = f.now }},
		{"hold active", "pilot", "PILOT_EVIDENCE_INCOMPLETE", func(f *fixture) { f.reservations[0].Status = "ACTIVE" }},
		{"reallocation", "pilot", "PILOT_EVIDENCE_INCOMPLETE", func(f *fixture) { f.plan.Routes[0].AllowReallocationIn = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.change(f)
			r := assess(t, f)
			check(t, r, tc.key, "BLOCKED", tc.code)
			if r.ReadyForFinalReview {
				t.Fatal("mismatch passed")
			}
		})
	}
}
func TestPreparationReadinessCommercialPendingIsSeparate(t *testing.T) {
	f := newFixture(t)
	f.commercial.Status = commercial.StatusPending
	r := assess(t, f)
	check(t, r, "commercial", "PENDING_REVIEW", "COMMERCIAL_APPROVAL_PENDING")
	if r.State != "REQUIRES_REVIEW" {
		t.Fatal(r.State)
	}
	f.commercial.Status = commercial.StatusApproved
	f.campaign.Status = campaign.StatusMessageApproved
	r = assess(t, f)
	check(t, r, "commercial", "PENDING_REVIEW", "COMMERCIAL_TRANSITION_REQUIRED")
}
func TestPreparationReadinessRetiredSource(t *testing.T) {
	for _, source := range []string{"provider", "gateway", "pool"} {
		t.Run(source, func(t *testing.T) {
			f := newFixture(t)
			switch source {
			case "provider":
				f.provider.Status = provider.StatusRetired
			case "gateway":
				f.gateway.Status = sender.GatewayPoolRetired
			case "pool":
				f.pool.Status = "RETIRED"
			}
			check(t, assess(t, f), "transport", "BLOCKED", "TRANSPORT_SOURCE_RETIRED")
		})
	}
	f := newFixture(t)
	f.service.SenderPools = poolRead{value: &f.pool, err: errors.New("private database secret")}
	check(t, assess(t, f), "transport", "UNAVAILABLE", "TRANSPORT_UNAVAILABLE")
}
func TestPreparationReadinessConsentWindowExpiry(t *testing.T) {
	f := newFixture(t)
	f.review.ExpiresAt = f.campaign.CompletionDeadlineAt
	check(t, assess(t, f), "consent", "BLOCKED", "CONSENT_EXPIRES_BEFORE_DEADLINE")
	f = newFixture(t)
	f.provider.EffectiveTo = f.campaign.CompletionDeadlineAt
	check(t, assess(t, f), "transport", "BLOCKED", "TRANSPORT_SOURCE_RETIRED")
	f = newFixture(t)
	f.gateway.EffectiveTo = f.campaign.CompletionDeadlineAt
	check(t, assess(t, f), "transport", "BLOCKED", "TRANSPORT_SOURCE_RETIRED")
}
func TestPreparationReadinessConcurrentCampaignChange(t *testing.T) {
	f := newFixture(t)
	n := 0
	f.service.Campaigns = readFunc[campaign.Campaign](func(context.Context, string) (campaign.Campaign, error) {
		n++
		c := f.campaign
		if n == 2 {
			c.Version++
		}
		return c, nil
	})
	_, err := f.service.Get(context.Background(), f.campaign.ID)
	if !errors.Is(err, campaign.ErrConflict) {
		t.Fatalf("got %v", err)
	}
}
func TestPreparationReadinessCapacityIsAdvisory(t *testing.T) {
	f := newFixture(t)
	r := assess(t, f)
	if !r.ReadyForFinalReview || r.State != "READY_FOR_FINAL_REVIEW" {
		t.Fatal(r)
	}
	if len(r.Checks) != 12 {
		t.Fatalf("checks=%d", len(r.Checks))
	}
	for _, c := range r.Checks {
		if c.Status != "PASS" && c.Status != "NOT_APPLICABLE" {
			t.Fatalf("%+v", c)
		}
		if c.Evidence == nil {
			t.Fatal(c.Key)
		}
	}
	if r.Capacity == nil || r.Capacity.Classification != "ADVISORY_ONLY" || r.Capacity.HealthySessions != 2 || r.Capacity.BasisRecipientCount != 85 || r.Capacity.Basis != "SNAPSHOT" || !reflect.DeepEqual(r.Capacity.Reasons, []string{ReservationReason}) {
		t.Fatalf("%+v", r.Capacity)
	}
	if r.Capacity.ProjectedCompletionAt == nil || !r.Capacity.ProjectedCompletionAt.Equal(f.campaign.RequestedStartAt.Add(10*time.Minute)) {
		t.Fatalf("%+v", r.Capacity)
	}
	if !strings.Contains(strings.Join(r.Limitations, " "), ReservationLimitation) {
		t.Fatal(r.Limitations)
	}
	f.campaign.QuietHoursStart = "22:00"
	f.campaign.QuietHoursEnd = "06:00"
	r = assess(t, f)
	check(t, r, "capacity", "BLOCKED", "CAPACITY_WINDOW_NOT_MODELED")
	if r.Capacity.ProjectedCompletionAt != nil {
		t.Fatal("unmodeled window projected")
	}
}
func TestPreparationReadinessProjectionOverflow(t *testing.T) {
	for _, n := range []int64{130664438, MaxSafeInteger} {
		f := newFixture(t)
		f.campaign.MaximumUniqueRecipients = n
		f.campaign.AudienceSnapshotID = ""
		f.capacity.AvailableMessagesPerMinute = 1
		f.capacity.AvailableHourlyUnits = 60
		f.capacity.AvailableDailyUnits = MaxSafeInteger
		r := assess(t, f)
		check(t, r, "capacity", "BLOCKED", "CAPACITY_PROJECTION_UNREPRESENTABLE")
		if r.Capacity.ProjectedCompletionAt != nil {
			t.Fatal("overflow projection")
		}
	}
	f := newFixture(t)
	f.capacity.AvailableMessagesPerMinute = 0
	check(t, assess(t, f), "capacity", "UNAVAILABLE", "CAPACITY_UNAVAILABLE")
}
func TestPreparationReadinessNeverMutates(t *testing.T) {
	f := newFixture(t)
	before, _ := json.Marshal(f.campaign)
	for i := 0; i < 3; i++ {
		r := assess(t, f)
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), "private") {
			t.Fatal(string(raw))
		}
	}
	after, _ := json.Marshal(f.campaign)
	if string(before) != string(after) {
		t.Fatal("campaign mutated")
	}
	// The real service is intentionally incapable of invoking a write via its interfaces.
	for _, field := range []string{"Campaigns", "Routes", "Capacity", "TestMessages", "SenderPools"} {
		typ, _ := reflect.TypeOf(Service{}).FieldByName(field)
		for i := 0; i < typ.Type.NumMethod(); i++ {
			name := typ.Type.Method(i).Name
			for _, unsafe := range []string{"Create", "Ensure", "Record", "Reserve", "Transition", "Schedule", "Send"} {
				if strings.HasPrefix(name, unsafe) && name != "SenderPools" {
					t.Fatalf("write capability %s.%s", field, name)
				}
			}
		}
	}
}

func TestPreparationReadinessTypedNilWiring(t *testing.T) {
	f := newFixture(t)
	var assets *storage.TrustedAssetService
	f.service.Assets = assets
	if _, err := f.service.Get(context.Background(), f.campaign.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("typed nil wiring error=%v want unavailable", err)
	}
}
func TestPreparationReadinessMemoryCapacityUnavailable(t *testing.T) {
	f := newFixture(t)
	f.service.Capacity = MemoryCapacityReader{}
	r := assess(t, f)
	check(t, r, "capacity", "UNAVAILABLE", "CAPACITY_UNAVAILABLE")
	if r.ReadyForFinalReview || r.Capacity != nil {
		t.Fatal("memory invented measured capacity")
	}
}
func TestPreparationReadinessMessageDerivedCapabilities(t *testing.T) {
	for _, typ := range []message.Type{message.TypeText, message.TypeImageCaption, message.TypeVideo, message.TypeDocument} {
		t.Run(string(typ), func(t *testing.T) {
			f := newFixture(t)
			f.campaign.Transport.RequiredCapabilities = nil
			f.message.Type = typ
			f.provider.Capabilities = nil
			check(t, assess(t, f), "message", "BLOCKED", "MESSAGE_CAPABILITY_UNSUPPORTED")
		})
	}
}
func TestPreparationReadinessAssessmentAndStart(t *testing.T) {
	f := newFixture(t)
	past := f.now.Add(-time.Minute)
	f.campaign.RequestedStartAt = &past
	var seen []time.Time
	f.service.Reviews = reviewRead{readFunc: fixedRead(&f.review), validate: func(_ context.Context, _, _, _, _ string, at time.Time) error { seen = append(seen, at); return nil }}
	r := assess(t, f)
	if r.EffectiveStartAt == nil || !r.EffectiveStartAt.Equal(f.now) || len(seen) != 2 || !seen[0].Equal(f.now) || !seen[1].Equal(f.now) {
		t.Fatal("assessment/effective start was not captured consistently")
	}
}

func TestPreparationReadinessProjectionPublicDateBoundary(t *testing.T) {
	f := newFixture(t)
	start := time.Date(9999, 12, 31, 23, 59, 0, 0, time.UTC)
	end := start.Add(59 * time.Second)
	f.campaign.RequestedStartAt = &start
	f.campaign.CompletionDeadlineAt = &end
	f.campaign.AudienceSnapshotID = ""
	f.campaign.MaximumUniqueRecipients = 1
	f.capacity.AvailableMessagesPerMinute = 1
	r := assess(t, f)
	check(t, r, "capacity", "BLOCKED", "CAPACITY_PROJECTION_UNREPRESENTABLE")
	if r.Capacity.ProjectedCompletionAt != nil {
		t.Fatal("unserializable public completion")
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatal("public DTO must remain serializable", err)
	}
}
