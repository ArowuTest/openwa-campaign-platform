package campaignpreparation

import (
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
	"context"
	"errors"
	"reflect"
	"slices"
	"time"
)

type CampaignReader interface {
	Get(context.Context, string) (campaign.Campaign, error)
}
type OrganisationReader interface {
	Get(context.Context, string) (organisation.Organisation, error)
}
type PurposeReader interface {
	Get(context.Context, string) (consent.Purpose, error)
}
type OrganisationPolicyReader interface {
	ValidatePurpose(context.Context, string, string) error
}
type ReviewReader interface {
	Get(context.Context, string) (consent.Review, error)
	ValidateCampaignReview(context.Context, string, string, string, string, time.Time) error
}
type ProviderReader interface {
	Get(context.Context, string) (provider.Definition, error)
	Require(context.Context, string, provider.Channel, string, time.Time, []provider.Capability) (provider.Definition, error)
}
type GatewayPoolReader interface {
	Get(context.Context, string) (sender.GatewayPool, error)
}
type SenderPoolReader interface {
	GetPool(context.Context, string) (sender.Pool, error)
}
type SnapshotReader interface {
	Get(context.Context, string) (segment.Snapshot, error)
}
type MessageReader interface {
	Get(context.Context, string) (message.Version, error)
}
type AssetReader interface {
	ResolveClean(context.Context, string, storage.AssetPurpose, int64) (storage.Asset, error)
}
type CommercialRecordReader interface {
	GetByCampaign(context.Context, string) (commercial.Record, error)
}
type CommercialApprovalReader interface {
	ValidateCampaignApproval(context.Context, string, string, int64) (string, error)
}
type RouteReader interface {
	LatestByCampaign(context.Context, string) (execution.RoutingPlan, error)
	Reservations(context.Context, string) ([]execution.CapacityReservation, error)
	ValidateForExecution(context.Context, execution.RoutingPlan, campaign.Campaign, time.Time) error
}
type TestMessageReader interface {
	ListSends(context.Context, string) ([]testmessage.Send, error)
}
type CapacityReader interface {
	RouteCapacity(context.Context, execution.PoolRoute, time.Time) (execution.PoolCapacity, error)
}
type MaintenanceReader interface {
	Check(context.Context, platformpolicy.Operation, platformpolicy.OperationalScope, time.Time) error
}

type Service struct {
	Campaigns            CampaignReader
	Organisations        OrganisationReader
	Purposes             PurposeReader
	OrganisationPolicies OrganisationPolicyReader
	Reviews              ReviewReader
	Providers            ProviderReader
	GatewayPools         GatewayPoolReader
	SenderPools          SenderPoolReader
	Snapshots            SnapshotReader
	Messages             MessageReader
	Assets               AssetReader
	CommercialRecords    CommercialRecordReader
	CommercialApprovals  CommercialApprovalReader
	Routes               RouteReader
	TestMessages         TestMessageReader
	Capacity             CapacityReader
	Maintenance          MaintenanceReader
	Clock                func() time.Time
	SafetyMarginPercent  int
}

func (s *Service) Get(ctx context.Context, campaignID string) (Readiness, error) {
	if s == nil {
		return Readiness{}, ErrUnavailable
	}
	for _, reader := range []any{s.Campaigns, s.Organisations, s.Purposes, s.OrganisationPolicies, s.Reviews, s.Providers, s.GatewayPools, s.SenderPools, s.Snapshots, s.Messages, s.Assets, s.CommercialRecords, s.CommercialApprovals, s.Routes, s.TestMessages, s.Capacity, s.Maintenance} {
		if reader == nil {
			return Readiness{}, ErrUnavailable
		}
		value := reflect.ValueOf(reader)
		switch value.Kind() {
		case reflect.Ptr, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
			if value.IsNil() {
				return Readiness{}, ErrUnavailable
			}
		}
	}
	c, err := s.Campaigns.Get(ctx, campaignID)
	if err != nil {
		return Readiness{}, err
	}
	switch c.Status {
	case campaign.StatusDraft, campaign.StatusConsentReviewPending, campaign.StatusConsentApproved, campaign.StatusAudienceBuilding, campaign.StatusAudienceValidated, campaign.StatusMessageReviewPending, campaign.StatusMessageApproved, campaign.StatusCommercialApproved, campaign.StatusFinalApprovalPending:
	case campaign.StatusScheduled, campaign.StatusDispatching, campaign.StatusPaused, campaign.StatusCompleted, campaign.StatusCompletedWithExceptions, campaign.StatusCancelled:
		return Readiness{}, ErrStageUnsupported
	default:
		return Readiness{}, ErrInvalidEvidence
	}
	if c.ID != campaignID || c.Version < 1 || c.Version > MaxSafeInteger || c.MaximumUniqueRecipients < 1 || c.MaximumUniqueRecipients > MaxSafeInteger || c.MaximumMessagesPerRecipient != 1 || c.EligibleAudienceCount < 0 || c.EligibleAudienceCount > MaxSafeInteger || c.OrganisationID == "" || c.PurposeID == "" || c.ConsentReviewID == "" {
		return Readiness{}, ErrInvalidEvidence
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	if now.IsZero() || s.SafetyMarginPercent < 0 || s.SafetyMarginPercent > 90 {
		return Readiness{}, ErrInvalidEvidence
	}
	r := Readiness{CampaignID: c.ID, CampaignVersion: c.Version, CampaignStatus: c.Status, AssessedAt: now, Checks: []ReadinessCheck{}, Limitations: []string{AdvisoryLimitation, ReservationLimitation, "Current suppression, policy and source evidence are rechecked by final release guards. Accepted controlled tests are not delivery guarantees."}}
	start := now
	if c.RequestedStartAt != nil {
		if c.RequestedStartAt.After(start) {
			start = c.RequestedStartAt.UTC()
		}
		r.EffectiveStartAt = &start
	}
	end := start
	if c.CompletionDeadlineAt != nil {
		end = c.CompletionDeadlineAt.UTC()
	}

	orgCheck := newCheck("organisation", "ORGANISATION_ACTIVE", "The saved organisation is active.", "Review the saved organisation and purpose policy.")
	orgCheck.Evidence = refs("organisation", c.OrganisationID)
	org, e := s.Organisations.Get(ctx, c.OrganisationID)
	if e != nil {
		orgCheck.fail("UNAVAILABLE", "ORGANISATION_UNAVAILABLE", "The saved organisation could not be verified.")
	} else if org.ID != c.OrganisationID || org.Status != organisation.StatusActive {
		orgCheck.fail("BLOCKED", "ORGANISATION_NOT_ACTIVE", "The saved organisation is not active.")
	} else {
		orgCheck.Evidence[0].Status = string(org.Status)
	}
	policyErr := s.OrganisationPolicies.ValidatePurpose(ctx, c.OrganisationID, c.PurposeID)
	if policyErr != nil {
		if errors.Is(policyErr, organisation.ErrPurposeProhibited) || errors.Is(policyErr, organisation.ErrPurposeNotPermitted) {
			orgCheck.fail("BLOCKED", "PURPOSE_NOT_PERMITTED", "The saved purpose is not permitted by organisation policy.")
		} else {
			orgCheck.fail("UNAVAILABLE", "ORGANISATION_UNAVAILABLE", "Organisation policy could not be verified.")
		}
	}
	r.Checks = append(r.Checks, orgCheck)

	purposeCheck := newCheck("purpose", "PURPOSE_VALID", "The saved purpose matches the review and wording.", "Review the exact purpose, organisation, review and wording bindings.")
	purposeCheck.Evidence = refs("purpose", c.PurposeID)
	p, pe := s.Purposes.Get(ctx, c.PurposeID)
	review, re := s.Reviews.Get(ctx, c.ConsentReviewID)
	switch {
	case pe != nil:
		purposeCheck.fail("UNAVAILABLE", "PURPOSE_UNAVAILABLE", "The saved purpose could not be verified.")
	case p.ID != c.PurposeID || !p.Active || p.Channel != "WHATSAPP":
		purposeCheck.fail("BLOCKED", "PURPOSE_NOT_ACTIVE", "The saved purpose is not an active WhatsApp purpose.")
	case p.OrganisationID != "" && p.OrganisationID != c.OrganisationID:
		purposeCheck.fail("BLOCKED", "PURPOSE_ORGANISATION_MISMATCH", "The purpose does not match the organisation.")
	case policyErr != nil:
		if errors.Is(policyErr, organisation.ErrPurposeProhibited) || errors.Is(policyErr, organisation.ErrPurposeNotPermitted) {
			purposeCheck.fail("BLOCKED", "PURPOSE_NOT_PERMITTED", "Organisation policy does not permit the purpose.")
		} else {
			purposeCheck.fail("UNAVAILABLE", "PURPOSE_POLICY_UNAVAILABLE", "The purpose policy could not be verified.")
		}
	case p.ConsentReviewID != c.ConsentReviewID:
		purposeCheck.fail("BLOCKED", "PURPOSE_REVIEW_MISMATCH", "The purpose references a different consent review.")
	case re != nil:
		purposeCheck.fail("UNAVAILABLE", "PURPOSE_REVIEW_UNAVAILABLE", "The bound review could not be verified.")
	case p.WordingVersion == "" || p.WordingVersion != review.WordingVersion:
		purposeCheck.fail("BLOCKED", "PURPOSE_WORDING_MISMATCH", "Purpose and review wording versions do not match.")
	}
	r.Checks = append(r.Checks, purposeCheck)
	cc := newCheck("consent", "CONSENT_VALID", "The saved consent review covers the assessed window.", "Obtain an independent consent review covering the saved campaign window.")
	cc.Evidence = refs("consentReview", c.ConsentReviewID)
	if re != nil {
		cc.fail("UNAVAILABLE", "CONSENT_UNAVAILABLE", "The saved consent review could not be verified.")
	} else {
		cc.Evidence[0].Version = review.Version
		cc.Evidence[0].Status = string(review.Status)
		switch {
		case review.ID != c.ConsentReviewID || review.Version < 1 || review.OrganisationID != c.OrganisationID || review.Channel != "WHATSAPP" || (review.Scope != consent.ReviewScopeCampaign && review.Scope != consent.ReviewScopeOrganisation && review.Scope != consent.ReviewScopeSource) || (review.Scope == consent.ReviewScopeCampaign && review.CampaignID != c.ID):
			cc.fail("BLOCKED", "CONSENT_REVIEW_INVALID", "The consent review does not authorise the saved campaign scope.")
		case review.Status == consent.StatusDraft || review.Status == consent.StatusPending:
			cc.fail("PENDING_REVIEW", "CONSENT_REVIEW_PENDING", "The consent review awaits an independent decision.")
		case review.Status != consent.StatusApproved || review.ExpiresAt == nil || !review.ExpiresAt.After(now) || !review.ExpiresAt.After(start) || review.RevokedAt != nil || review.SupersededByID != "":
			cc.fail("BLOCKED", "CONSENT_REVIEW_INVALID", "The consent review is not currently valid.")
		case c.CompletionDeadlineAt != nil && !review.ExpiresAt.After(end):
			cc.fail("BLOCKED", "CONSENT_EXPIRES_BEFORE_DEADLINE", "The consent review does not remain valid beyond the saved deadline.")
		default:
			for _, at := range []time.Time{now, start} {
				if s.Reviews.ValidateCampaignReview(ctx, c.ConsentReviewID, c.ID, c.OrganisationID, "WHATSAPP", at) != nil {
					cc.fail("UNAVAILABLE", "CONSENT_UNAVAILABLE", "The consent review could not be revalidated.")
					break
				}
			}
		}
	}
	r.Checks = append(r.Checks, cc)

	ac := newCheck("audience", "AUDIENCE_FROZEN", "The immutable audience matches the saved binding.", "Freeze and validate the campaign audience through the existing audience workflow.")
	ac.Evidence = refs("audienceSnapshot", c.AudienceSnapshotID)
	basis, basisKind := c.MaximumUniqueRecipients, "AUTHORISED_MAXIMUM"
	if c.AudienceSnapshotID == "" {
		ac.fail("BLOCKED", "AUDIENCE_NOT_FROZEN", "No immutable audience snapshot is bound.")
	} else {
		snap, e := s.Snapshots.Get(ctx, c.AudienceSnapshotID)
		if e != nil {
			ac.fail("UNAVAILABLE", "AUDIENCE_UNAVAILABLE", "The saved audience snapshot could not be verified.")
		} else if snap.ID != c.AudienceSnapshotID || snap.CampaignID != c.ID || snap.SnapshotHash == "" || snap.SnapshotHash != c.AudienceSnapshotHash || snap.EligibleCount != c.EligibleAudienceCount || snap.EligibleCount < 1 || snap.EligibleCount > c.MaximumUniqueRecipients || snap.DefinitionVersion < 1 || snap.ConsentPolicyVersion == "" || snap.ConfigurationVersion == "" {
			ac.fail("BLOCKED", "AUDIENCE_EVIDENCE_CHANGED", "The immutable audience does not match the saved binding.")
		} else {
			basis, basisKind = snap.EligibleCount, "SNAPSHOT"
			ac.Evidence = []EvidenceReference{{Kind: "audienceSnapshot", ID: snap.ID, Hash: snap.SnapshotHash, Version: snap.DefinitionVersion}, {Kind: "consentPolicy", ID: snap.ConsentPolicyVersion}, {Kind: "configuration", ID: snap.ConfigurationVersion}}
		}
	}
	r.Checks = append(r.Checks, ac)
	// Read the immutable message once; derive capabilities even when old transport records omitted them.
	mv, me := message.Version{}, message.ErrNotFound
	if c.MessageVersionID != "" {
		mv, me = s.Messages.Get(ctx, c.MessageVersionID)
	}
	pd, pde := provider.Definition{}, provider.ErrNotFound
	if c.Transport.ProviderDefinitionID != "" {
		pd, pde = s.Providers.Get(ctx, c.Transport.ProviderDefinitionID)
	}
	gp, gpe := sender.GatewayPool{}, sender.ErrSenderNotFound
	if c.Transport.GatewayPoolID != "" {
		gp, gpe = s.GatewayPools.Get(ctx, c.Transport.GatewayPoolID)
	}
	mc := s.messageCheck(ctx, c, mv, me, pd, pde, gp, gpe)
	r.Checks = append(r.Checks, mc, s.transportCheck(ctx, c, pd, pde, gp, gpe, now, start, end))
	sc := newCheck("schedule", "SCHEDULE_VALID", "The saved UTC window and timezone are valid.", "Save a valid start, deadline, IANA timezone and optional quiet hours.")
	if c.RequestedStartAt == nil || c.CompletionDeadlineAt == nil || c.Timezone == "" {
		sc.fail("BLOCKED", "SCHEDULE_INCOMPLETE", "The saved schedule is incomplete.")
	} else if _, e := c.EvaluateDispatchWindow(now); e != nil || !c.CompletionDeadlineAt.After(*c.RequestedStartAt) {
		sc.fail("BLOCKED", "SCHEDULE_INVALID", "The saved schedule is invalid.")
	} else if !end.After(start) {
		sc.fail("BLOCKED", "DEADLINE_PASSED", "The effective deadline has passed.")
	}
	r.Checks = append(r.Checks, sc)
	capCheck, preview := s.capacityCheck(ctx, c, basis, basisKind, now, start, end, sc.Status == "PASS")
	r.Capacity = preview
	r.Checks = append(r.Checks, capCheck, s.pilotCheck(ctx, c, start), s.commercialCheck(ctx, c))
	maintenance := newCheck("maintenance", "MAINTENANCE_CLEAR", "No current maintenance policy blocks release at the effective start.", "Review the applicable maintenance window; no override is provided here.")
	if e := s.Maintenance.Check(ctx, platformpolicy.OperationCampaignStart, platformpolicy.OperationalScope{Provider: string(c.Transport.Provider), GatewayPoolID: c.Transport.GatewayPoolID, SenderPoolID: c.Transport.SenderPoolID}, start); e != nil {
		if errors.Is(e, platformpolicy.ErrBlocked) {
			maintenance.fail("BLOCKED", "MAINTENANCE_BLOCKS_RELEASE", "Maintenance blocks release at the effective start.")
		} else {
			maintenance.fail("UNAVAILABLE", "MAINTENANCE_UNAVAILABLE", "Maintenance policy could not be verified.")
		}
	}
	r.Checks = append(r.Checks, maintenance)
	final := newCheck("finalReview", "FINAL_REVIEW_ELIGIBLE", "Evidence may be presented for independent final review; approval remains required.", "Use the existing independent approval workflow and its permission and MFA checks.")
	if c.Status != campaign.StatusCommercialApproved && c.Status != campaign.StatusFinalApprovalPending {
		final.fail("PENDING_REVIEW", "FINAL_REVIEW_STAGE_PENDING", "Complete the preceding governed stages before requesting final review.")
	}
	r.Checks = append(r.Checks, final)
	r.State = "READY_FOR_FINAL_REVIEW"
	for _, check := range r.Checks {
		if check.Status == "BLOCKED" || check.Status == "UNAVAILABLE" {
			r.State = "BLOCKED"
			break
		}
		if check.Status == "PENDING_REVIEW" {
			r.State = "REQUIRES_REVIEW"
		}
	}
	r.ReadyForFinalReview = r.State == "READY_FOR_FINAL_REVIEW"
	latest, e := s.Campaigns.Get(ctx, c.ID)
	if e != nil {
		return Readiness{}, e
	}
	if latest.Version != c.Version || latest.Status != c.Status || latest.ID != c.ID {
		return Readiness{}, campaign.ErrConflict
	}
	return r, nil
}
func newCheck(key, code, message, remediation string) ReadinessCheck {
	return ReadinessCheck{Key: key, Status: "PASS", Code: code, Message: message, Remediation: remediation, Evidence: []EvidenceReference{}}
}
func (c *ReadinessCheck) fail(status, code, message string) {
	c.Status = status
	c.Code = code
	c.Message = message
}
func refs(kind, id string) []EvidenceReference {
	if id == "" {
		return []EvidenceReference{}
	}
	return []EvidenceReference{{Kind: kind, ID: id}}
}
func routeFor(c campaign.Campaign) execution.PoolRoute {
	t := c.Transport
	return execution.PoolRoute{SenderPoolID: t.SenderPoolID, GatewayPoolID: t.GatewayPoolID, Provider: string(t.Provider), Engine: string(t.Engine), ProviderAdapterVersion: t.AdapterVersion, ProviderDefinitionID: t.ProviderDefinitionID, ProviderDefinitionVersion: t.ProviderDefinitionVersion, GatewayPoolVersion: t.GatewayPoolVersion, MaximumRecipients: c.MaximumUniqueRecipients}
}
func (s *Service) messageCheck(ctx context.Context, c campaign.Campaign, m message.Version, err error, p provider.Definition, pe error, g sender.GatewayPool, ge error) ReadinessCheck {
	out := newCheck("message", "MESSAGE_TRUSTED", "The approved immutable message and media match the saved route.", "Approve the exact message version and verify trusted media and route capabilities.")
	out.Evidence = refs("messageVersion", c.MessageVersionID)
	if c.MessageVersionID == "" || errors.Is(err, message.ErrNotFound) || err == nil && m.Status != message.StatusApproved {
		out.fail("BLOCKED", "MESSAGE_NOT_APPROVED", "No approved message version is bound.")
		return out
	}
	if err != nil {
		out.fail("UNAVAILABLE", "MESSAGE_UNAVAILABLE", "The saved message could not be verified.")
		return out
	}
	if m.ID != c.MessageVersionID || m.CampaignID != c.ID || m.ContentHash == "" || m.ContentHash != c.MessageContentHash || m.Version < 1 || m.ApprovedBy == "" || m.ApprovedAt == nil {
		out.fail("BLOCKED", "MESSAGE_EVIDENCE_CHANGED", "The message does not match the saved approval evidence.")
		return out
	}
	out.Evidence[0].Version = int64(m.Version)
	out.Evidence[0].Hash = m.ContentHash
	out.Evidence[0].Status = string(m.Status)
	required := map[message.Type]provider.Capability{message.TypeText: provider.CapabilitySendText, message.TypeImageCaption: provider.CapabilitySendImage, message.TypeVideo: provider.CapabilitySendVideo, message.TypeDocument: provider.CapabilitySendDocument}[m.Type]
	if pe != nil || ge != nil {
		out.fail("UNAVAILABLE", "MESSAGE_CAPABILITY_UNAVAILABLE", "The route capability evidence could not be verified.")
		return out
	}
	if required == "" || !slices.Contains(p.Capabilities, required) || !slices.Contains(g.Capabilities, sender.Capability(required)) {
		out.fail("BLOCKED", "MESSAGE_CAPABILITY_UNSUPPORTED", "The saved route does not support the approved message type.")
		return out
	}
	if m.Type != message.TypeText || m.Media != nil {
		if m.Media == nil || m.Media.AssetID == "" || m.Media.SHA256 == "" || m.Media.MediaType == "" || m.Media.Size <= 0 || m.Media.ScanStatus != "CLEAN" {
			out.fail("BLOCKED", "MEDIA_NOT_TRUSTED", "The message lacks complete trusted media evidence.")
			return out
		}
		a, e := s.Assets.ResolveClean(ctx, m.Media.AssetID, storage.AssetPurposeMessageMedia, p.MaximumAttachmentBytes)
		if e != nil {
			if errors.Is(e, storage.ErrAssetNotFound) || errors.Is(e, storage.ErrAssetNotClean) {
				out.fail("BLOCKED", "MEDIA_NOT_TRUSTED", "The media is not available as a clean trusted asset.")
			} else {
				out.fail("UNAVAILABLE", "MEDIA_UNAVAILABLE", "Trusted media metadata could not be verified.")
			}
			return out
		}
		if a.ID != m.Media.AssetID || a.Purpose != storage.AssetPurposeMessageMedia || a.Status != storage.AssetStatusClean || a.SHA256 != m.Media.SHA256 || a.MediaType != m.Media.MediaType || a.Size != m.Media.Size || p.MaximumAttachmentBytes > 0 && a.Size > p.MaximumAttachmentBytes {
			out.fail("BLOCKED", "MEDIA_NOT_TRUSTED", "Trusted media differs from the approved message.")
			return out
		}
		out.Evidence = append(out.Evidence, EvidenceReference{Kind: "mediaAsset", ID: a.ID, Version: a.Version, Hash: a.SHA256, Status: string(a.Status)})
	}
	return out
}
func (s *Service) transportCheck(ctx context.Context, c campaign.Campaign, p provider.Definition, pe error, g sender.GatewayPool, ge error, now, start, end time.Time) ReadinessCheck {
	out := newCheck("transport", "TRANSPORT_CURRENT", "The exact frozen route sources remain active.", "Review the saved provider, gateway and logical sender pool; replacements require governed changes.")
	t := c.Transport
	out.Evidence = append(refs("providerDefinition", t.ProviderDefinitionID), refs("gatewayPool", t.GatewayPoolID)...)
	out.Evidence = append(out.Evidence, refs("senderPool", t.SenderPoolID)...)
	if t.Provider != campaign.ProviderOpenWA {
		out.fail("BLOCKED", "TRANSPORT_PREPARATION_OUT_OF_SCOPE", "This preparation workflow supports OpenWA routes.")
		return out
	}
	if t.Validate() != nil || t.RoutingMode != campaign.RoutingSenderPool || t.ProviderDefinitionID == "" || t.ProviderDefinitionVersion < 1 || t.GatewayPoolVersion < 1 {
		out.fail("BLOCKED", "TRANSPORT_EVIDENCE_CHANGED", "The frozen route is incomplete or inconsistent.")
		return out
	}
	pool, se := s.SenderPools.GetPool(ctx, t.SenderPoolID)
	if pe != nil || ge != nil || se != nil {
		out.fail("UNAVAILABLE", "TRANSPORT_UNAVAILABLE", "The saved route sources could not be verified.")
		return out
	}
	if p.Status != provider.StatusActive || g.Status != sender.GatewayPoolActive || pool.Status != "ACTIVE" {
		out.fail("BLOCKED", "TRANSPORT_SOURCE_RETIRED", "A saved route source is no longer active.")
		return out
	}
	if p.ID != t.ProviderDefinitionID || p.Version != t.ProviderDefinitionVersion || p.Provider != string(t.Provider) || p.Channel != provider.ChannelWhatsApp || p.Engine != string(t.Engine) || p.AdapterVersion != t.AdapterVersion || g.ID != t.GatewayPoolID || g.Version != t.GatewayPoolVersion || string(g.Provider) != string(t.Provider) || string(g.Engine) != string(t.Engine) || g.AdapterVersion != t.AdapterVersion || pool.ID != t.SenderPoolID {
		out.fail("BLOCKED", "TRANSPORT_EVIDENCE_CHANGED", "Saved route identity or version differs from current evidence.")
		return out
	}
	if p.MinimumGatewayVersion != "" {
		valid, e := provider.VersionAtLeast(g.AdapterVersion, p.MinimumGatewayVersion)
		if e != nil || !valid {
			out.fail("BLOCKED", "TRANSPORT_EVIDENCE_CHANGED", "The gateway adapter does not satisfy the frozen provider minimum.")
			return out
		}
	}
	required := make([]provider.Capability, 0, len(t.RequiredCapabilities))
	for _, cap := range t.RequiredCapabilities {
		required = append(required, provider.Capability(cap))
		if !slices.Contains(g.Capabilities, sender.Capability(cap)) || !slices.Contains(p.Capabilities, provider.Capability(cap)) {
			out.fail("BLOCKED", "MESSAGE_CAPABILITY_UNSUPPORTED", "The saved route lacks a required capability.")
			return out
		}
	}
	for _, at := range []time.Time{now, start, end} {
		if p.EffectiveFrom.After(at) || p.EffectiveTo != nil && !p.EffectiveTo.After(at) || g.EffectiveFrom != nil && g.EffectiveFrom.After(at) || g.EffectiveTo != nil && !g.EffectiveTo.After(at) {
			out.fail("BLOCKED", "TRANSPORT_SOURCE_RETIRED", "A saved route source is not effective throughout the saved window.")
			return out
		}
		current, e := s.Providers.Require(ctx, string(t.Provider), provider.ChannelWhatsApp, string(t.Engine), at, required)
		if e != nil {
			out.fail("UNAVAILABLE", "TRANSPORT_UNAVAILABLE", "Active provider evidence could not be verified.")
			return out
		}
		if current.ID != p.ID || current.Version != p.Version || current.AdapterVersion != p.AdapterVersion {
			out.fail("BLOCKED", "TRANSPORT_EVIDENCE_CHANGED", "An active provider replacement differs from the frozen binding.")
			return out
		}
	}
	out.Evidence = []EvidenceReference{{Kind: "providerDefinition", ID: p.ID, Version: p.Version, Status: string(p.Status)}, {Kind: "gatewayPool", ID: g.ID, Version: g.Version, Status: string(g.Status)}, {Kind: "senderPool", ID: pool.ID, Status: pool.Status}}
	return out
}
func (s *Service) pilotCheck(ctx context.Context, c campaign.Campaign, at time.Time) ReadinessCheck {
	out := newCheck("pilot", "PILOT_EVIDENCE_VALID", "The exact plan, own held reservation and accepted controlled test match.", "Use the existing plan, reservation and controlled-test workflows for the exact saved route.")
	p, e := s.Routes.LatestByCampaign(ctx, c.ID)
	if e != nil {
		if errors.Is(e, execution.ErrRoutingPlanNotFound) {
			out.fail("BLOCKED", "PILOT_EVIDENCE_INCOMPLETE", "The campaign has no approved pilot plan.")
		} else {
			out.fail("UNAVAILABLE", "PILOT_UNAVAILABLE", "The approved pilot plan could not be verified.")
		}
		return out
	}
	holds, e := s.Routes.Reservations(ctx, p.ID)
	if e != nil {
		out.fail("UNAVAILABLE", "PILOT_UNAVAILABLE", "Pilot reservation evidence could not be verified.")
		return out
	}
	sends, e := s.TestMessages.ListSends(ctx, c.ID)
	if e != nil {
		out.fail("UNAVAILABLE", "PILOT_UNAVAILABLE", "Controlled-test evidence could not be verified.")
		return out
	}
	evidence, e := execution.EvaluateDayOnePilotAdmission(c, sends, p, holds)
	if e != nil || c.Transport.Provider != campaign.ProviderOpenWA || p.ApprovedBy == "" || p.ApprovedAt.IsZero() {
		out.fail("BLOCKED", "PILOT_EVIDENCE_INCOMPLETE", "Exact approved plan, held reservation and accepted test evidence is incomplete.")
		return out
	}
	if e = s.Routes.ValidateForExecution(ctx, p, c, at); e != nil {
		if errors.Is(e, execution.ErrRoutingPlanInvalid) {
			out.fail("BLOCKED", "PILOT_EVIDENCE_INCOMPLETE", "The plan is no longer valid for the saved campaign.")
		} else {
			out.fail("UNAVAILABLE", "PILOT_UNAVAILABLE", "Current pilot policy could not be revalidated.")
		}
		return out
	}
	out.Evidence = []EvidenceReference{{Kind: "routingPlan", ID: evidence.RoutingPlanID, Version: evidence.RoutingPlanVersion}, {Kind: "capacityReservation", ID: evidence.ReservationID, Status: evidence.ReservationStatus}, {Kind: "controlledTest", ID: evidence.AcceptedTestMessageID, Status: "ACCEPTED"}}
	return out
}
func (s *Service) commercialCheck(ctx context.Context, c campaign.Campaign) ReadinessCheck {
	out := newCheck("commercial", "COMMERCIAL_APPROVED", "The commercial approval covers the saved organisation and maximum.", "Obtain independent commercial approval and complete the governed commercial transition.")
	v, e := s.CommercialRecords.GetByCampaign(ctx, c.ID)
	if errors.Is(e, commercial.ErrNotFound) || e == nil && (v.Status == commercial.StatusDraft || v.Status == commercial.StatusPending) {
		out.fail("PENDING_REVIEW", "COMMERCIAL_APPROVAL_PENDING", "Commercial approval is pending.")
		if e == nil {
			out.Evidence = []EvidenceReference{{Kind: "commercialRecord", ID: v.ID, Version: v.Version, Status: string(v.Status)}}
		}
		return out
	}
	if e != nil {
		out.fail("UNAVAILABLE", "COMMERCIAL_UNAVAILABLE", "Commercial evidence could not be verified.")
		return out
	}
	out.Evidence = []EvidenceReference{{Kind: "commercialRecord", ID: v.ID, Version: v.Version, Status: string(v.Status)}}
	if v.ID == "" || v.Version < 1 || v.Status != commercial.StatusApproved || v.CampaignID != c.ID || v.OrganisationID != c.OrganisationID || v.ApprovedRecipients < c.MaximumUniqueRecipients || c.CommercialApprovalID != "" && c.CommercialApprovalID != v.ID {
		out.fail("BLOCKED", "COMMERCIAL_APPROVAL_INVALID", "Commercial evidence does not authorise the saved campaign.")
		return out
	}
	approved, e := s.CommercialApprovals.ValidateCampaignApproval(ctx, c.ID, c.OrganisationID, c.MaximumUniqueRecipients)
	if e != nil {
		if errors.Is(e, commercial.ErrNotApproved) || errors.Is(e, commercial.ErrNotFound) {
			out.fail("BLOCKED", "COMMERCIAL_APPROVAL_INVALID", "Commercial approval is not current.")
		} else {
			out.fail("UNAVAILABLE", "COMMERCIAL_UNAVAILABLE", "Commercial approval could not be revalidated.")
		}
		return out
	}
	if approved != v.ID {
		out.fail("BLOCKED", "COMMERCIAL_APPROVAL_INVALID", "Commercial approval identity changed.")
		return out
	}
	if c.Status != campaign.StatusCommercialApproved && c.Status != campaign.StatusFinalApprovalPending {
		out.fail("PENDING_REVIEW", "COMMERCIAL_TRANSITION_REQUIRED", "The approved commercial record still requires the campaign transition.")
	} else if c.CommercialApprovalID != v.ID {
		out.fail("BLOCKED", "COMMERCIAL_APPROVAL_INVALID", "The campaign lacks the exact commercial approval binding.")
	}
	return out
}
