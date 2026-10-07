# Campaign Preparation Foundations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give internal operators direct campaign detail, durable editing/reopening of governed DRAFT campaigns, and an authoritative read-only preparation checklist before final approval.

**Architecture:** Extend the existing campaign aggregate with a dedicated DRAFT-only save operation; keep later material amendments and execution transitions unchanged. Add a separate preparation query service whose dependencies expose reads/validation only, and connect a small preparation panel to the existing campaign workspace. Deliver and review the three units independently in order.

**Tech Stack:** Existing Go 1.23 module, database/sql and PostgreSQL, OpenAPI 3.1.0, Next.js 16.3.8, React 19.1.1, TypeScript 5.9.2, Vitest/Testing Library, and isolated Linux Go containers. No new framework or dependency is planned.

**Spec:** `docs/reference/OpenWA_Internal_Campaign_Platform_SRS_v1.0.docx`; `docs/reference/OpenWA_Internal_Campaign_Platform_UI_UX_Design_Specification_v1.0.docx`; approved later decisions below; `docs/program/UI_UX_IMPLEMENTATION_MATRIX.md`.

## Status and source provenance

Preparatory plan only, dated 2026-10-07. The cohort/estimate/segment/snapshot candidate remains **UNACCEPTED**. Do not begin these code units until root records its acceptance, freezes the exact accepted source and establishes the next isolated worktree/base. Accepted import base inspected here: `9bae302d4c3c4ea4593ce294440a7cd31b68bb72`. Current inspection worktree: `C:\Users\sanus\OpenWA\campaign-platform-active\repo\.agent\worktrees\large-audience-ingestion-20261006`. No test or runtime result is asserted by this plan.

| Authority | Exact source / relevance |
|---|---|
| Full SRS v1.0 | `docs/reference/OpenWA_Internal_Campaign_Platform_SRS_v1.0.docx`, identical Downloads original, SHA-256 `0aa632755b9823940c0e3586cede5ed4e4f6a20d5865ccaae53752a82ded9f8d`. Full body extraction available at `.agent/review/20261007-full-srs-body.txt` (3445 source paragraphs). Read original/extraction before execution; extraction is a review aid, not versioned authority. |
| Full UX specification v1.0 | `docs/reference/OpenWA_Internal_Campaign_Platform_UI_UX_Design_Specification_v1.0.docx`, SHA-256 `63dc25cd71901bbe3adaef8e76bfef2fd9a0417cb8048d97ebb6fd66281a03c9`. Primary slice: UI-CAMP-02 Campaign creation wizard, UI-CAMP-03 Campaign overview, UI-CAMP-09 Schedule and pre-flight, and mandatory journey D. |
| Sponsor/designer context | `C:\Users\sanus\Downloads\Additional COntext for the UI UX design.txt`; `C:\Users\sanus\Downloads\design-canvas-export.zip`. Visual references supplement the full requirements; 25 exported references do not replace the 67-screen inventory. |
| Approved architecture | `docs/decisions/ADR-0007-railway-authoritative-postgres.md`: Railway control/data authority, Hostinger gateways, Meta direct sibling transport. |
| Approved release decisions | `docs/requirements/BACKEND_FREEZE_RELEASE_DECISIONS.md`: sticky UNKNOWN/evidence-led reconciliation; remaining retry policy governance and campaign profile identity deferred. `docs/decisions/ADR-0006-campaign-scoped-sender-profile-identity.md` and `docs/requirements/CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md` remain future scope. |
| Current implementation baseline | `docs/program/UI_UX_IMPLEMENTATION_MATRIX.md`: E/C/P/M source/evidence categories, all 67 screens/eight journeys; no complete screen-state/browser certification. Later session-approved internal-operator, minimal offline-vetting and one-engine-pool decisions are recorded there. |

SRS anchors for this slice: CAM-001..004, CAM-012, CAM-016..018; APR-002..007, APR-008..013; BR-023..029; API standards P2003..2010; UX-001..004, UX-008..009, UX-011 and UX-014. The full wizard's composer/media/test-send/reviewer/scheduling flow remains later work; this plan does not close all of CAM/APR/UX or journey D.

## Global Constraints

The following quoted requirement values are copied from the SRS; the later approved scope beneath them resolves original architecture/scope assumptions.

- "Internally managed service; no external client portal in initial release" (P0016).
- "Schedule campaigns in a declared timezone and persist UTC." (CAM-016, P1097).
- "Require reapproval after material message, media, link, audience, sender or schedule change." (CAM-012, P1081).
- "Use masked PII by default with explicit authorised reveal." (UX-003, P2115).
- "Show status, owner, next required action and blocking reasons on every campaign." (UX-004, P2119).
- "PII in API responses shall be minimised and masked by permission." (P2010).
- "A suspended organisation or sender cannot be selected for a new release." (BR-029, P1997).

Approved boundaries: internal platform-scoped operators; no fabricated organisation tenancy or external self-service; Railway PostgreSQL authority and Hostinger isolated gateways; minimal attributable offline consent vetting; one engine-specific OpenWA sender pool with several READY sessions permitted; `SENDER_POOL`, `fallbackMode=NONE`; no mixed-engine fallback/reallocation expansion; sticky UNKNOWN/no blind resend; campaign-specific profile mutation deferred.

Saving a DRAFT and reading preparation readiness must not approve, schedule, create a routing plan/hold, release recipients, create an entitlement, mutate delivery obligations, send a controlled test, pair a sender or dispatch. Existing maker/checker, MFA and final release gates remain authoritative. Keep existing Meta detail readable; do not implement a new Meta preparation workflow in this OpenWA tranche.

## Review Focus

Each failure mode has a named test below; none is assumed verified.

1. Direct-link detail for an old campaign outside the first 2000 inventory rows must load by ID without a list scan (Unit 1).
2. Permission/session loss and a slow response from a previously opened campaign must not display or save stale context (Units 1..3).
3. Two operators saving the same DRAFT version, or a DRAFT transitioning while a save is in flight, must cause one atomic winner and an explicit conflict (Unit 2).
4. A selected source/review/provider/pool becoming inactive, expired, replaced or retired must remain identifiable and block unsafe progression; an equal-count replacement is not identical evidence (Units 2..3).
5. Missing measurements, an unmodeled quiet-hour window, database/object-store failure or preview GET replay must never create a hard deadline promise or a business mutation (Unit 3).

---

## Inspected foundations and contract decisions

| Existing source | What the plan relies on / preserves |
|---|---|
| `internal/campaign/model.go`, `service.go`, `transport.go` | Campaign.Get and governed Create exist; Version is int64; DRAFT exists. Create requires organisation/name/purpose/review, positive maximum, exactly one message per recipient and valid transport. Schedule may be absent. Material amendment only accepts AUDIENCE_VALIDATED through SCHEDULED, excluding DRAFT/consent/audience-building states. |
| `internal/persistence/postgres/campaign.go` | Get is a direct UUID query; CAS does not update name/organisation/purpose/review. Material amendment persists an append-only MaterialChangeEvent atomically. Preserve general CAS and lifecycle committer behavior; use a dedicated DRAFT transaction. |
| `internal/campaignworkspace/service.go` | Tags/notes/archive are not wizard persistence. Get calls Ensure and can write a workspace row; do not call it from readiness. Archive restore is not DRAFT restore/reopen. |
| `internal/platform/httpserver/server.go` | List/Create/transition/material-amendment/workspace/test/routing/execution routes exist; direct campaign detail and generic preparation readiness do not. `require` and session-derived actors protect mutations; transitions resolve authoritative snapshot/message evidence. |
| `internal/consent/review.go`, `purpose.go`; `internal/organisation/policy.go` | Review Get, ValidateCampaignReview and Purpose Get exist; policy ValidatePurpose controls allowed/prohibited purpose IDs but is not itself purpose-record existence validation. Keep approval workflow; DRAFT storage is not consent approval. |
| `internal/provider/registry.go`, `internal/sender/gateway_pool.go` | Require/Get validate governed definition/pool identity and capabilities. Provider/gateway versions are server evidence; browser must not author them. |
| `internal/message/{version.go,service.go}`, `internal/storage/asset.go` | Immutable content hash, independent approval and trusted asset checks exist. ResolveClean reads metadata/object Stat; it does not rescan or approve media. Never return media object keys/URLs in readiness. |
| `internal/commercial/service.go` | Separate PENDING_APPROVAL record; campaign remains MESSAGE_APPROVED until explicit APPROVE_COMMERCIAL. Do not invent a browser-only COMMERCIAL_PENDING campaign state. Expose a safe commercial check/status, not invoice/payment/amount data to campaign.read. |
| `internal/execution/{service.go,pilot_admission.go,routing_admin.go,postgres.go}` | Plan writes RecordAdmission; Forecast is read-only but accepts only SCHEDULED/DISPATCHING/PAUSED. ValidateDayOnePilot reads exact controlled-test/plan/reservation evidence. RouteCapacity is read-only and aggregates healthy READY/BUSY sessions from selected sender/gateway pools with fresh node/session heartbeats. Its daily figure deducts the configured sender_pools.reserved_capacity; it does not account for windowed HELD/ACTIVE campaign_pool_capacity_reservations. |
| `apps/admin-web/app/campaigns/[id]/campaign-workspace.tsx` | Current fetchSnapshot scans up to 20 inventory pages; some supplemental errors become empty values. Keep late execution UI, but make core detail and readiness failures explicit rather than a zero/pass result. |
| `apps/admin-web/lib/api.ts`, `lib/session.ts`, same-origin proxy | apiRequest<T>(path, init?, options?) supplies CSRF, request ID, no-store and optional idempotency header. Safe network reads retry once; unsafe writes do not auto-retry. Preserve this policy. |
| `contracts/openapi/control-api.yaml` | OpenAPI 3.1.0; server URL /api/v1; paths omit that prefix. Existing CreateCampaign/TransportSelectionRequest are request schemas, not detail/readiness schemas. Add explicit schemas for these units without sweeping unrelated message/Meta/material-amendment stubs into scope. |

**Chosen bounded DRAFT behavior:** edit/reopen an existing, governed DRAFT. Keep required creation prerequisites; name, saved organisation/purpose/review, entitlement maximum and OpenWA route must remain valid. Schedule and its optional fields may remain incomplete. This is not an arbitrary incomplete wizard-document store, browser localStorage store or a new status. Names are bounded at 250 characters; reasons 8..1000 characters. The existing organisation/purpose/review identity tuple stays read-only in this first save unit; show the locked-reference reason and preserve those records. A new source identity is a later workflow decision, not a hidden reparenting operation. Other preparation edits never reuse or silently rewrite approved evidence.

**Concurrency/replay choice:** known-resource PUT with required expectedVersion; stale or repeated writes cannot produce another change/event. Exact replay after a successful save that changed the aggregate returns 409, not another mutation; a valid no-op with the still-current version remains a no-op. The client does not retry uncertain writes: GET detail, compare persisted values/version, and let the operator reconcile. This deliberately avoids a new generic idempotency ledger. Existing create remains one submitted POST with no automatic retry; creating incomplete drafts or making create replay-safe is an explicitly separate product/API unit.

**Readiness meaning:** a timestamped, saved-campaign query; not validation of unsaved browser inputs and not a launch token. Its optional positive preview classification is READY_FOR_FINAL_REVIEW, never READY_TO_DISPATCH. A change after the query invalidates its value; final gates reload current evidence. Review/approval state is reported honestly at early stages.

## File map

New file names below are planned, not claims that files already exist. No database migration is needed for the chosen bounded design; the existing campaign and append-only material-change tables suffice. If accepted source/schema inspection disproves that, stop that unit's implementation and revise this plan before adding a migration.

| Unit | Create | Modify | Direct test files to create |
|---|---|---|---|
| 1: detail | `internal/platform/httpserver/campaign_detail.go`; `apps/admin-web/lib/campaign-preparation-model.ts` | `internal/platform/httpserver/server.go` routes; `contracts/openapi/control-api.yaml`; `apps/admin-web/app/campaigns/[id]/campaign-workspace.tsx` | `internal/platform/httpserver/campaign_detail_test.go`; `apps/admin-web/tests/campaign-detail.component.test.tsx` |
| 2: DRAFT | `internal/campaign/draft.go`; `internal/persistence/postgres/campaign_draft.go`; `internal/platform/httpserver/campaign_draft.go`; `apps/admin-web/components/campaign-draft-editor.tsx` | `internal/campaign/service.go` repository/readers; `cmd/control-api/runtime.go` both constructors; Unit 1 client model, workspace and route/OpenAPI files; `apps/admin-web/app/campaigns/campaign-manager.tsx` schedule optional and post-create link | `internal/campaign/draft_test.go`; `internal/persistence/postgres/campaign_draft_postgres_integration_test.go`; `internal/platform/httpserver/campaign_draft_test.go`; `apps/admin-web/tests/campaign-draft-editor.component.test.tsx` |
| 3: readiness | `internal/campaignpreparation/model.go`, `service.go`, `service_test.go`, `capacity_postgres.go`; `internal/platform/httpserver/campaign_readiness.go`; `apps/admin-web/components/campaign-readiness-panel.tsx` | `cmd/control-api/runtime.go` both constructors; route/Dependencies/OpenAPI files; client model and workspace | `internal/platform/httpserver/campaign_readiness_test.go`; `internal/platform/httpserver/campaign_readiness_postgres_integration_test.go`; `apps/admin-web/tests/campaign-readiness-panel.component.test.tsx`; `tests/governance/test_campaign_preparation_openapi.py` |

Reuse current auth/test fixture patterns; create a focused `apps/admin-web/tests/campaign-preparation-test-helpers.ts` only if all three actual rendered suites need it. A fixture/helper does not count as acceptance by itself.

## Unit 1: Direct campaign detail GET

**Interfaces**

- Existing consumed signature: `(*campaign.Service).Get(ctx context.Context, identifier string) (campaign.Campaign, error)`.
- Produce handler `(*httpserver.Server).getCampaign(w http.ResponseWriter, r *http.Request)` in campaign_detail.go.
- Register `GET /api/v1/campaigns/{id}` through `s.require("campaign.read", s.getCampaign)`. OpenAPI path `/campaigns/{id}`, operationId `getCampaign`.
- Browser client `getCampaignDetail(campaignId: string, signal?: AbortSignal): Promise<CampaignDetail>` in campaign-preparation-model.ts. Use `apiRequest<CampaignDetail>('/v1/campaigns/' + encodeURIComponent(campaignId), { signal })`.
- `CampaignDetail` matches existing aggregate wire fields, with full `CampaignTransport`: id, organisationId, name, purposeId, consentReviewId, status, requestedStartAt?, completionDeadlineAt?, timezone, quietHoursStart?, quietHoursEnd?, maximumUniqueRecipients, maximumMessagesPerRecipient, audienceSnapshotId?, audienceSnapshotHash?, eligibleAudienceCount, messageVersionId?, messageContentHash?, senderPool?, transport, createdBy, finalApprovedBy?, commercialApprovalId?, pauseReason?, createdAt, updatedAt, version.
- Response transport fields: channel, provider, engine, routingMode, gatewayPoolId?, gatewayPoolVersion?, metaSenderId?, sessionId?, senderPoolId?, adapterVersion, providerDefinitionId?, providerDefinitionVersion?, requiredCapabilities?, fallbackMode, routingPolicyVersion, capacityEvidenceVersion. Preserve existing META/CLOUD_API values in detail; no new provider selection work.
- JSON response is the Campaign object, not a new workspace/list wrapper. Define OpenAPI `CampaignDetail` and `CampaignTransport`; declare server-issued IDs/versions/approval fields readOnly. UTC ISO 8601 times, existing optional omission behavior and numeric int64 version.

**Failure contract**

| Condition | Response/assertion |
|---|---|
| No session / invalid session | Existing 401 AUTHENTICATION_REQUIRED / SESSION_INVALID before repository lookup. |
| No campaign.read | Existing 403 PERMISSION_DENIED; no campaign body. Operators are platform-scoped, not inferred organisation-tenanted. |
| Invalid canonical UUID path | 400 INVALID_CAMPAIGN_ID with safe message; no PostgreSQL cast failure surfaced. |
| Valid missing ID | 404 CAMPAIGN_NOT_FOUND. |
| Nil Campaigns | 503 CAMPAIGNS_UNAVAILABLE. |
| Other repository failure | Existing canonical 500 internal error; no SQL/DSN/stack in response. |
| Suspended organisation/retired route | Historical campaign remains readable with its saved IDs; GET neither rebinds sources nor authorizes release. |
| Sensitive payload | No contacts/MSISDN/QR/credential/recovery reference/private object key, and no extra notes/financial data. Set Cache-Control: no-store. |

- [ ] **Step 1: Add actual failing route/component tests.** Test `TestCampaignDetailDirectLookup` with a repository whose ListPage would fail; direct Get succeeds for a campaign that would lie beyond 2000 records. Table-test auth/path/not-found/unavailable/failure/masking cases as `TestCampaignDetailContract`. Render CampaignWorkspace in `campaign-detail.component.test.tsx`; assert one detail GET, zero campaign-list GETs, explicit core failure, and rejection of a late campaign-A response after rerender to campaign B.
- [ ] **Step 2: Run the Unit 1 focused commands below and retain the real RED receipt.** Expected new GET route 404 and current UI inventory-scan/stale-context assertions fail; a fixture/setup failure is not a useful RED.
- [ ] **Step 3: Implement only the handler, schemas and typed client signatures above.** Validate path before lookup; preserve require ordering and platform scope. Core detail cannot depend on supplemental workspace/metrics availability.
- [ ] **Step 4: Replace only the inventory-scan part of fetchSnapshot with direct detail.** Use request cancellation/current campaign context guard. Separate core loading/error/not-found from optional evidence errors; do not show stale prior campaign after permission loss. Existing supplemental behavior may remain outside this unit if visibly labeled unavailable.
- [ ] **Step 5: Run focused GREEN, schema/route parity, frontend typecheck/lint, and independent unit review.** Save receipts at exact source; do not call this full overview acceptance.
- [ ] **Step 6: Commit only the reviewed Unit 1 files after root acceptance of this unit.** Suggested message: `feat: expose direct campaign detail for preparation`. No push/deploy in this step.

## Unit 2: Durable governed DRAFT edit/save/reload

**Interfaces**

- New domain input `campaign.DraftSaveInput` contains editable fields from the request table below plus `ActorID string json:"-"`; `Transport campaign.TransportSelection`; date fields `*time.Time`; counts int64/int; expectedVersion int64.
- Pure method `(c Campaign).SaveDraft(input DraftSaveInput, now time.Time) (Campaign, MaterialChangeEvent, error)` in draft.go. Preserve ID/CreatedBy/CreatedAt; status stays DRAFT; increment version once, UTC UpdatedAt and immutable event with previous/new status DRAFT and version n/n+1.
- Service method `(*campaign.Service).SaveDraft(ctx context.Context, identifier string, input DraftSaveInput) (Campaign, error)`.
- Extend Repository with `SaveDraft(context.Context, Campaign, MaterialChangeEvent, int64) error`; implement in MemoryRepository and PostgreSQL CampaignRepository. Existing CompareAndSwap/AmendMaterial contracts unchanged.
- Add `WithConsentPurposeReader(reader interface { Get(context.Context, string) (consent.Purpose, error) }) *Service` and `WithConsentReviewReader(reader interface { Get(context.Context, string) (consent.Review, error) }) *Service` to Service; wire consentPurposes/reviews in both control-api constructors. Existing WithConsentReviews transition validator stays unchanged.
- New sentinels in draft.go: `ErrDraftNotEditable`, `ErrDraftReferencesLocked`, `ErrDraftGovernanceUnavailable`, `ErrDraftInvalid`; preserve existing `ErrConflict` and `ErrNotFound`.
- HTTP `(*Server).saveCampaignDraft(w http.ResponseWriter, r *http.Request)`; `PUT /api/v1/campaigns/{id}/draft`, require campaign.write, OpenAPI operationId `saveCampaignDraft`. Return 200 CampaignDetail.
- Browser `saveCampaignDraft(campaignId: string, input: DraftSaveRequest): Promise<CampaignDetail>`; `CampaignDraftEditor({ campaign, onSaved }: { campaign: CampaignDetail; onSaved: (saved: CampaignDetail) => void })`.

**Exact request schema: `CampaignDraftSaveRequest`**

additionalProperties:false at both request and transport; maximum JSON body 256 KiB; canonical UUID IDs. actor/status/approval/hash/server version claims are rejected at the boundary. Define an HTTP request DTO restricted to this table before translating to domain input; do not decode a permissive whole Campaign.

| Field | Wire type / rule |
|---|---|
| expectedVersion | Required integer int64, >=1. |
| reason | Required string, trimmed length 8..1000. Operator reason, not hidden default approval text. |
| name | Required nonblank trimmed string, <=250 characters. |
| organisationId, purposeId, consentReviewId | Required canonical UUID strings. Readable saved refs remain visible if retired; this unit requires equality with the saved tuple and validates its current governance. |
| maximumUniqueRecipients | Required integer int64 1..9007199254740991. Reject fractional/overflow/unsafe JS counts; client cannot authorise a larger entitlement merely by editing this draft maximum. |
| maximumMessagesPerRecipient | Required integer exactly 1. |
| requestedStartAt, completionDeadlineAt | Optional string date-time or null; omission/null clears the field in this full replacement request. Each may be incomplete; if both supplied deadline must be later. UTC is persisted; declare IANA timezone separately. |
| timezone | Required valid IANA string; UI initial value UTC. Do not reinterpret an already stored UTC instant merely when timezone display changes. |
| quietHoursStart, quietHoursEnd | Optional strings; omission/empty clears both; otherwise both HH:MM and unequal. Never half-populate quietly. |
| transport | Required `DraftTransportRequest`: channel=WHATSAPP; provider=OPENWA; engine=WHATSAPP_WEB_JS or BAILEYS; routingMode=SENDER_POOL; gatewayPoolId/senderPoolId UUID; adapterVersion/routingPolicyVersion/capacityEvidenceVersion nonblank <=100; fallbackMode=NONE; requiredCapabilities array of unique existing capability enum values (empty allowed). No sessionId/metaSenderId or client-authored providerDefinitionId/providerDefinitionVersion/gatewayPoolVersion. |

At the service, compare editable transport fields against the loaded aggregate. When they are unchanged, preserve its authoritative provider/gateway IDs and versions and validate that exact frozen binding using existing capability helpers; an active definition replacement must reject the name-only save rather than silently rebind it. Resolve fresh authoritative IDs/versions only for a deliberate editable transport change, recording TRANSPORT in ChangedFields. The restricted request never accepts client-authored authority fields. Require an active organisation; an existing active purpose with compatible organisation/channel and organisation policy; require its ConsentReviewID to equal the campaign consentReviewId and its WordingVersion to equal that review's wording version, following current cohort governance. Require that exact review to belong to the organisation/channel and, if campaign-scoped, this campaign. DRAFT saving does not independently approve that review: pending approval is represented by readiness and existing transition gates. Reject saves against expired/revoked/superseded saved evidence; retain its ID and remediation so the operator can request review/rebinding through a later governed workflow. Do not create or substitute evidence automatically. Review Get validation must be explicit, not merely "non-empty UUID".

**Persistence and failure rules**

- Acquire/guard the campaign row inside the save transaction. Assert expected version, current status DRAFT and no frozen audience/message/final-approval binding before any update. Return conflict if status/version changed while service validation ran.
- Persist all editable columns, including name, route bindings and optional window values. Do not widen general execution CAS. Insert the existing append-only MaterialChangeEvent in the same transaction; roll back campaign changes if event insertion fails.
- Require organisationId/purposeId/consentReviewId to equal the loaded saved tuple; reject any change as ErrDraftReferencesLocked in domain, memory and PostgreSQL implementations. Recheck equality under the campaign-row transaction. No cascade/deletion, hidden reparenting or approval reuse. Readiness always rechecks after a source changes between validation and commit; a saved DRAFT conveys no future source-validity guarantee.
- ChangedFields uses existing event structure with deterministic codes NAME, SCHEDULE, TRANSPORT, ENTITLEMENT, containing only changed categories, no raw field values. A valid no-change save returns the current entity without a new version/event; expectedVersion must still match. Explain this explicit no-op in tests.
- Require authenticated actor and CSRF for cookie-auth PUT. Draft save needs campaign.write, not campaign.approve; it creates no approval and does not require a new MFA action. Existing MFA material amendment/final gates remain.
- Map stale version to 409 CAMPAIGN_VERSION_CONFLICT; departed DRAFT status/frozen bindings to 409 CAMPAIGN_DRAFT_NOT_EDITABLE; tuple changes to 409 CAMPAIGN_DRAFT_REFERENCES_LOCKED; nil governance to 503 CAMPAIGN_DRAFT_GOVERNANCE_UNAVAILABLE; malformed JSON/path to 400 INVALID_JSON/INVALID_CAMPAIGN_ID; invalid fields/governance to 422 CAMPAIGN_DRAFT_INVALID with safe field codes; missing campaign 404; unexpected persistence failure canonical 500. Use no raw DB/object errors.
- After 409, keep unsaved form values, show server version/state and require explicit reload/reconcile. After network uncertainty, do not repeat PUT; GET by ID and show whether the submitted fields persisted. No localStorage/URL persistence of campaign evidence. Leaving/reloading after a successful save restores server values.

- [ ] **Step 1: Add domain/service RED tests in draft_test.go.** `TestDraftSaveAllowedFieldsAndUTC`, `TestDraftSaveIncompleteScheduleAndClear`, `TestDraftSaveNoOp`, `TestDraftSaveRejectsNonDraftAndFrozenEvidence`, `TestDraftSaveGovernanceAndRetirement`, `TestDraftSavePreservesFrozenTransportOnUnrelatedEdit`, `TestDraftSavePurposeReviewBinding`, `TestDraftSaveNeverApprovesOrDispatches`: assert the exact request rules, unchanged active-route fields with a replaced provider definition reject an unrelated edit, same-organisation/channel but different-purpose-review and wording mismatches reject without mutation, source bindings, unchanged creator, one event/version for changes, no event for no-op, and rejection without mutation.
- [ ] **Step 2: Add real PostgreSQL RED tests in campaign_draft_postgres_integration_test.go.** `TestPostgreSQLDraftSaveReload` round-trips every editable column using a new repository instance; `TestPostgreSQLDraftSaveConcurrentVersion` gives exactly one winner/two requests, one event; `TestPostgreSQLDraftSaveTransitionRace` cannot overwrite a concurrent submit-consent transition; `TestPostgreSQLDraftSaveAuditRollback` uses a test-only insert-failure trigger and proves old row/event counts; `TestPostgreSQLDraftSaveLockedReferences` protects downstream records. Guard fixture cleanup to its disposable DB, never a persistent environment.
- [ ] **Step 3: Add HTTP and actual rendered RED tests.** `TestCampaignDraftContract` asserts auth, CSRF, server-derived actor, strict unknown-field rejection, malformed/numeric/date inputs and exact error mapping. Render editor with fresh detail; save/reload values, failed-save preservation, conflict/uncertain reconciliation, permission loss and late A/B responses; assert no transition/routing/test/release/execution request.
- [ ] **Step 4: Run focused RED before implementation.** Record each failing behavior rather than counting compile failure as evidence for every semantic test.
- [ ] **Step 5: Implement the domain/service/repository interfaces and request translation above.** Reuse dispatch-window validation and provider resolution; use the existing material-change event table/privilege model. Wire both runtime constructors. Run database tests using the application role as well as fixture owner; do not add owner privileges to the runtime.
- [ ] **Step 6: Build the DRAFT editor and creation/reopen connection.** Show immutable IDs/current status/version and labels for allowed fields. Save/cancel/reset are explicit; Save only sends this unit's DTO. Update CampaignManager so schedule can be left incomplete and successful create offers the returned campaign detail link; keep current required governance prerequisites. Non-DRAFT views show locked reason and existing later-stage tools, with no DRAFT PUT button.
- [ ] **Step 7: Run focused GREEN, existing campaign/HTTP/persistence regressions, rendered tests, OpenAPI checks, typecheck/lint/build and a bounded authenticated local browser save/reload/conflict/keyboard exercise.** Local mock UI evidence alone does not prove PostgreSQL durability.
- [ ] **Step 8: Review and commit the accepted Unit 2 file set.** Suggested message: `feat: persist governed campaign draft edits`. Commit is conditional on acceptance; no push/deployment.

## Unit 3: Pre-final read-only preparation readiness

**Interfaces and read boundary**

Create package campaignpreparation with `func (s *Service) Get(ctx context.Context, campaignID string) (Readiness, error)`; Clock func() time.Time, SafetyMarginPercent int. Runtime passes the same existing 15 percent margin used by the execution coordinator; do not expose it as browser input or present it as a newly approved universal policy.

Service dependencies are small read-only interfaces, declared in service.go:

| Dependency | Exact consumed signature |
|---|---|
| Campaigns | Get(context.Context, string) (campaign.Campaign, error) |
| Organisations | Get(context.Context, string) (organisation.Organisation, error) |
| Purposes | Get(context.Context, string) (consent.Purpose, error) |
| OrganisationPolicies | ValidatePurpose(context.Context, string, string) error |
| Reviews | Get(context.Context, string) (consent.Review, error); ValidateCampaignReview(context.Context, string, string, string, string, time.Time) error |
| Providers | Get(context.Context, string) (provider.Definition, error); Require(context.Context, string, provider.Channel, string, time.Time, []provider.Capability) (provider.Definition, error) |
| GatewayPools | Get(context.Context, string) (sender.GatewayPool, error) |
| Snapshots | Get(context.Context, string) (segment.Snapshot, error) |
| Messages | Get(context.Context, string) (message.Version, error) |
| Assets | ResolveClean(context.Context, string, storage.AssetPurpose, int64) (storage.Asset, error) |
| CommercialRecords | GetByCampaign(context.Context, string) (commercial.Record, error), supplied by existing Commercial.Store |
| CommercialApprovals | ValidateCampaignApproval(context.Context, string, string, int64) (string, error) |
| Routes | LatestByCampaign(context.Context, string) (execution.RoutingPlan, error); Reservations(context.Context, string) ([]execution.CapacityReservation, error); ValidateForExecution(context.Context, execution.RoutingPlan, campaign.Campaign, time.Time) error |
| TestMessages | ListSends(context.Context, string) ([]testmessage.Send, error) |
| Capacity | RouteCapacity(context.Context, execution.PoolRoute, time.Time) (execution.PoolCapacity, error) |
| Maintenance | Check(context.Context, platformpolicy.Operation, platformpolicy.OperationalScope, time.Time) error |

Assign existing services/stores in both runtime constructors. Existing RouteCapacity does not itself filter session/node engine identity, so provide PostgreSQLCapacityReader in capacity_postgres.go with public RouteCapacity(ctx context.Context, route execution.PoolRoute, at time.Time) (execution.PoolCapacity, error), implemented as a read-only exact-route aggregate. Use a small test/memory Capacity reader with the same semantics in the memory constructor; do not label its synthetic capacity as measured production evidence. Dependencies contains no RecordAdmission/RecordEvent/Create/Ensure/Transition/Schedule/Reserve method. Define sentinel errors ErrUnavailable and ErrStageUnsupported in model.go for missing required query wiring and unsupported lifecycle stage, respectively; preserve campaign.ErrNotFound/ErrConflict for missing ID or aggregate change. New httpserver.Dependencies field `CampaignPreparation *campaignpreparation.Service`; handler `(*Server).getCampaignReadiness(w http.ResponseWriter, r *http.Request)`; register GET /api/v1/campaigns/{id}/readiness under campaign.read; OpenAPI operationId getCampaignReadiness.

Browser `getCampaignReadiness(campaignId: string, signal?: AbortSignal): Promise<CampaignReadiness>` and `CampaignReadinessPanel({ campaign }: { campaign: CampaignDetail })`. API reads only the persisted ID; it accepts no author-authored forecast, counts, approval claims or mutations. DRAFT through FINAL_APPROVAL_PENDING are supported. SCHEDULED/later detail remains readable but readiness returns 409 CAMPAIGN_READINESS_STAGE_UNSUPPORTED, directing the UI to existing execution evidence.

**Exact public schema: `CampaignReadiness`**

| Field / DTO | Type and meaning |
|---|---|
| campaignId, campaignVersion, campaignStatus | string UUID, integer int64, actual campaign.Status. Bind the result to saved aggregate version. |
| assessedAt, effectiveStartAt? | UTC ISO 8601; assessment captured once; effective start=max(requestedStartAt, assessedAt). |
| state | BLOCKED, REQUIRES_REVIEW or READY_FOR_FINAL_REVIEW. Missing/invalid/unavailable check => BLOCKED; pending approval => REQUIRES_REVIEW; positive state only when every defined required check passes at COMMERCIAL_APPROVED/FINAL_APPROVAL_PENDING. |
| readyForFinalReview | boolean derived from state; never permission to final approve/dispatch. |
| checks | ordered array of ReadinessCheck; stable key, status PASS/BLOCKED/PENDING_REVIEW/UNAVAILABLE/NOT_APPLICABLE, code, safe message, remediation, evidence?: EvidenceReference[]. No skipped error becomes PASS. |
| EvidenceReference | kind, id, version? (integer int64), hash?, status?, observedAt?; only authoritative IDs/hashes/status/freshness, not free-form review notes/content/contact samples/amounts/object keys. |
| capacity? | CapacityPreview: senderPoolId, gatewayPoolId, healthySessions, healthyNodes, minimumHealthyNodes, availableMessagesPerMinute, availableHourlyUnits, availableDailyUnits, safetyMarginPercent, basisRecipientCount, basis (SNAPSHOT or AUTHORISED_MAXIMUM), requiredMessagesPerMinute?, projectedCompletionAt?, measurementAsOf, classification (ADVISORY_ONLY or BLOCKED), reasons:string[]. |
| limitations | nonempty safe string array including "Readiness is advisory; final approval revalidates current evidence." No launch/admission token or expiry guarantee. |

All schema objects additionalProperties:false. Numeric counts nonnegative safe integer in the browser; reject malformed responses rather than fabricate zero. checks/evidence/reasons arrays are always arrays. Cache-Control:no-store; masking applies to error details as well as success.

**Checks and deterministic outcomes**

| Key | Source-bound rule / failure code families |
|---|---|
| organisation | Active record and matching purpose policy; suspended/archive/missing => ORGANISATION_NOT_ACTIVE / ORGANISATION_UNAVAILABLE. Preserve saved ID. |
| purpose | Existing active WHATSAPP purpose; organisation compatible, policy permitted; its ConsentReviewID and WordingVersion must match the campaign's exact saved review and wording => PURPOSE_NOT_ACTIVE / PURPOSE_ORGANISATION_MISMATCH / PURPOSE_NOT_PERMITTED / PURPOSE_REVIEW_MISMATCH / PURPOSE_WORDING_MISMATCH. |
| consent | ValidateCampaignReview at assessment and effective start; review stays valid through supplied deadline (expiry equality fails). PENDING_REVIEW/DRAFT => CONSENT_REVIEW_PENDING; revoked/superseded/expired/channel/scope mismatch => CONSENT_REVIEW_INVALID/CONSENT_EXPIRES_BEFORE_DEADLINE. Do not create a review or expose its unstructured evidence. |
| audience | Bound snapshot belongs to campaign; positive count <=maximum; current ID/hash/count and definition/config/consent-policy identifiers retained. No snapshot => AUDIENCE_NOT_FROZEN. No rescan/materialisation of millions of contacts. Equal count but different ID/hash/version never passes equality; no invented expiry field because Snapshot has none. Current suppression is still rechecked by existing final dispatch guards. |
| message/media | Bound campaign version APPROVED, exact content hash; unsupported capabilities => MESSAGE_CAPABILITY_UNSUPPORTED. Media ResolveClean plus checksum/type/size matching => MEDIA_NOT_TRUSTED; object Stat outage => UNAVAILABLE. Missing/unapproved message => MESSAGE_NOT_APPROVED. No preview render with live audience records. |
| transport | OpenWA, one engine-specific pool, SENDER_POOL, NONE; complete frozen provider/pool identities and matching versions; active/effective/capabilities/adapter versions valid through window where governed fields support it. Source replacement => TRANSPORT_EVIDENCE_CHANGED, retirement => TRANSPORT_SOURCE_RETIRED; no automatic replacement/fallback. Unsupported Meta preparation => TRANSPORT_PREPARATION_OUT_OF_SCOPE. |
| schedule | Missing fields => SCHEDULE_INCOMPLETE; invalid timezone/quiet-hours => SCHEDULE_INVALID; effective deadline already passed => DEADLINE_PASSED. Stored ISO/UTC plus IANA display; no browser local-time guess. |
| capacity | Derive one PoolRoute from exact saved transport; query RouteCapacity with assessment time. Healthy node threshold and measured rate/daily units must be positive. Use immutable eligible snapshot count if bound, else maximum as conservative basis; absence of snapshot still blocks audience. Pure EvaluateAdmission may calculate a gross-window advisory projection using 15 percent runtime margin, but never RecordAdmission. Missing/stale/insufficient route measurements => CAPACITY_UNAVAILABLE / CAPACITY_INSUFFICIENT. |
| pilot | Read latest plan/reservations/tests; use ValidateForExecution and pure EvaluateDayOnePilotAdmission; require one route, one HELD reservation for exact window, no reallocation, and ACCEPTED test on exact current message/provider/engine/gateway/version/sender pool. Missing plan/test/hold => PILOT_EVIDENCE_INCOMPLETE. Every exact binding matters, not count alone. |
| commercial | Read safe record status and ValidateCampaignApproval with campaign/org/max; no record/draft/pending => COMMERCIAL_APPROVAL_PENDING. Revoked/wrong org/undercoverage => COMMERCIAL_APPROVAL_INVALID. If commercial record is approved but campaign still MESSAGE_APPROVED, check stays PENDING_REVIEW with COMMERCIAL_TRANSITION_REQUIRED. No invented COMMERCIAL_PENDING campaign status. |
| maintenance | Check CAMPAIGN_START against exact operational scope using existing policy; blocked => MAINTENANCE_BLOCKS_RELEASE. No new override. |
| final review | Creator cannot self-approve under existing rules, but readiness itself is campaign.read and reports evidence rather than granting action permissions. Final approval is not performed/credited by this query. Existing role/MFA checks remain on their mutation endpoints. |

The new PostgreSQLCapacityReader preserves current pool limits, fresh heartbeat thresholds, READY/BUSY and non-draining healthy node rules and the existing static reserved_capacity deduction, and additionally requires matching sender/node gateway pool, canonical provider/engine and adapter binding. Unknown/noncanonical engine evidence contributes no capacity; no speculative engine alias is invented. Test actual existing registration values during execution; if they are not canonical, return CAPACITY_ROUTE_IDENTITY_UNAVAILABLE and record a separately reviewed normalization decision rather than claiming compatibility. Keep execution.PostgreSQLStore.RouteCapacity unchanged in this bounded unit; share a read-only helper only if both consumers' existing contracts remain identical and direct regression tests justify the refactor. Capacity preview is intentionally conservative. Existing RouteCapacity reports healthy READY/BUSY sessions, so label it "healthy sessions", not READY count; test two compatible READY sessions contribute, and wrong-engine/stale/draining sessions do not. Do not add configured quotas or operator-entered rate to measured available capacity. CapacityPreview reports a gross live aggregate with that static deduction. Dynamic campaign HELD/ACTIVE usage is not deducted by this reader and must not be described as accounted for. Do not add back reserved_capacity or add the campaign's HELD units to measured figures; no preview creates a reservation. Keep capacity.classification=ADVISORY_ONLY for valid gross projections, and include RESERVATION_WINDOW_FEASIBILITY_NOT_ASSESSED in capacity.reasons plus the limitation: "These live capacity figures do not assess competing campaign reservations over the saved window. They do not establish reserved-window or deadline feasibility." The required capacity check assesses only exact route identity, current health/freshness, measured available units and the scoped gross-window projection. Its PASS does not mean reservation feasibility passed. The separate required pilot check validates the campaign's exact plan and own HELD reservation without adding it to or subtracting it twice from gross figures. This reservation-feasibility limitation does not independently block READY_FOR_FINAL_REVIEW when the defined required checks pass; that state means presentation for independent review, not established dispatch/deadline feasibility. Never label the unassessed reservation feasibility PASS or NOT_APPLICABLE. A quiet-hours or other unmodeled restricted sending window yields BLOCKED CAPACITY_WINDOW_NOT_MODELED with advisory gross figures and no positive deadline claim. This is a visible limitation pending a separate sending-calendar feasibility unit, not an invented completed calendar model. No hard deadline guarantee even when gross numbers fit.

Expected business blockers return 200 with checks, not a misleading transport error or zero. Missing core service wiring returns 503 CAMPAIGN_READINESS_UNAVAILABLE. Campaign missing/path/auth behave as Unit 1. Unexpected individual read failure returns its UNAVAILABLE check and readyForFinalReview=false with a safe code; malformed/inconsistent core campaign evidence returns a canonical 500, not PASS. Re-read campaign at the end; a different version/status yields 409 CAMPAIGN_VERSION_CONFLICT, so the response never mixes aggregate versions. Other concurrent policy/provider changes can still occur after response; advisory semantics and final revalidation are mandatory.

- [ ] **Step 1: Write real RED query tests in service_test.go.** `TestPreparationReadinessDraftBlockers`; `TestPreparationReadinessExactEvidence`; `TestPreparationReadinessCommercialPendingIsSeparate`; `TestPreparationReadinessRetiredSource`; `TestPreparationReadinessConsentWindowExpiry`; `TestPreparationReadinessConcurrentCampaignChange`; `TestPreparationReadinessCapacityIsAdvisory`; `TestPreparationReadinessNeverMutates`. Use fixed UTC clock and safe IDs; assert every check/status/code, exact ID/hash/version mismatch, same-organisation/channel purpose-review and wording mismatches, source expiry equality, failures never PASS, missing measurements, two READY sessions, unmodeled quiet-hour blocker, the positive review classification retaining the reservation-feasibility limitation without asserting that feasibility passed, exact own HELD pilot binding, and zero write calls. Do not substitute mocked count equality for evidence identity.
- [ ] **Step 2: Add route/real-database RED tests.** `TestCampaignReadinessContract` tests RBAC/no-store/safe field-only DTO and unsupported stages. `TestPostgreSQLCampaignReadinessReadOnly` snapshots campaign/version, event/admission/plan/reservation/recipient/outbox/test-send counts before/after repeated readiness GETs; assert unchanged. `TestPostgreSQLCampaignReadinessMeasuredPool` proves live same-engine READY sessions count, stale/wrong-route/draining sessions do not, and no new reservation is inserted. Assert static reserve deduction is preserved, inserting windowed HELD/ACTIVE rows does not silently alter the explicitly gross figures, exact own HELD evidence is assessed separately, and repeated reads leave reservation rows unchanged. Supply only disposable test services/assets; never call live gateway/object storage.
- [ ] **Step 3: Run RED and implement only the query/schema/interfaces above.** Keep preparation package separate from execution coordinator state transitions. Missing optional-stage evidence is a blocker, not permission to bypass it. Pure validators may be shared when an existing function exactly fits; do not invoke state-changing helpers to reuse code.
- [ ] **Step 4: Render the readiness panel and test real controls/current context.** Tests in campaign-readiness-panel.component.test.tsx exercise per-check remediation, separate pending commercial status, measured/advisory capacity labels, explicit outage/missing evidence, version mismatch after draft save, stale A/B response, permission loss, keyboard/focus and retained immutable IDs. Refresh is GET only; assert zero unsafe HTTP methods. Clear the old result immediately after a save/current campaign version change.
- [ ] **Step 5: Add OpenAPI consumer assertions in test_campaign_preparation_openapi.py.** Pin exact paths, methods, permissions documentation, request-required/additionalProperties/nullable date rules, response fields/enums and failure codes; verify the actual registered routes with existing verify_openapi_routes.py. Do not claim this validates business behavior alone.
- [ ] **Step 6: Run focused GREEN and bounded local browser readiness scenarios.** Evidence records database-backed read-only behavior independently from mocked rendered UI and browser/a11y evidence. Screen source and helper counts are not full UI-CAMP-09 or journey-D acceptance.
- [ ] **Step 7: Review and commit accepted Unit 3 files.** Suggested message: `feat: show read-only campaign preparation readiness`. No next wizard/composer/approval implementation is implied.

## Isolated verification commands for later execution

These are proposed commands, **not executed receipts**. Use the current root-approved local test runtime after cohort acceptance; do not start or modify production compose services. Source is mounted read-only. Existing review script `.agent/review/run-final-cohort-gates-20261007.ps1` demonstrates `openwa-step3-go-warm:local`, `openwa-upload-pg17`, two CPUs, 1200 MiB and separate ordinary/race databases. Confirm the image/toolchain/network exist at execution time.

Use a new disposable PostgreSQL database for each independently reviewed unit and a separate race database, e.g. `openwa_campaign_prep_u2_normal_20261007` / `openwa_campaign_prep_u2_race_20261007`. Supply credentials through task-specific process environment; never write/print a DSN or copy production credentials. Fixture setup may create/drop only these named disposable databases, then apply repository migrations through the approved local bootstrap/validator. Existing integration tests use POSTGRES_PAGINATION_DATABASE_URL and POSTGRES_EXECUTION_ADMISSION_DATABASE_URL; new preparation integration tests use POSTGRES_CAMPAIGN_PREPARATION_DATABASE_URL and fail in gate mode when absent rather than silently treating a skip as pass. Isolate fixture setup and per-test rows so ordinary/race suites never share a database concurrently.

The following PowerShell commands run Linux Go from the Windows worktree. Set the three POSTGRES_* variables to the same per-run disposable DSN inside the isolated test PostgreSQL network before the database commands. Command labels make skips/failures visible; do not infer passing integration evidence from exit zero if tests skipped.

```powershell
docker run --rm --memory 1200m --cpus 2 -v "${PWD}:/workspace:ro" -w /workspace -e GOMAXPROCS=2 openwa-step3-go-warm:local go test -count=1 -p 1 ./internal/platform/httpserver -run '^TestCampaignDetail'
docker run --rm --memory 1200m --cpus 2 -v "${PWD}:/workspace:ro" -w /workspace -e GOMAXPROCS=2 openwa-step3-go-warm:local go test -count=1 -p 1 ./internal/campaign ./internal/platform/httpserver -run 'TestDraftSave|TestCampaignDraft'
docker run --rm --memory 1200m --cpus 2 --network container:openwa-upload-pg17 -v "${PWD}:/workspace:ro" -w /workspace -e GOMAXPROCS=2 -e POSTGRES_CAMPAIGN_PREPARATION_DATABASE_URL -e POSTGRES_PAGINATION_DATABASE_URL openwa-step3-go-warm:local go test -count=1 -p 1 -v ./internal/persistence/postgres -run '^TestPostgreSQLDraftSave'
docker run --rm --memory 1200m --cpus 2 -v "${PWD}:/workspace:ro" -w /workspace -e GOMAXPROCS=2 openwa-step3-go-warm:local go test -count=1 -p 1 ./internal/campaignpreparation ./internal/platform/httpserver -run 'TestPreparationReadiness|TestCampaignReadiness'
docker run --rm --memory 1200m --cpus 2 --network container:openwa-upload-pg17 -v "${PWD}:/workspace:ro" -w /workspace -e GOMAXPROCS=2 -e POSTGRES_CAMPAIGN_PREPARATION_DATABASE_URL -e POSTGRES_PAGINATION_DATABASE_URL -e POSTGRES_EXECUTION_ADMISSION_DATABASE_URL openwa-step3-go-warm:local go test -count=1 -p 1 -v ./internal/platform/httpserver -run '^TestPostgreSQLCampaignReadiness'
```

After each unit's direct GREEN, run its touched package regressions. At the composed tranche boundary, in a **fresh ordinary database** and again a **different fresh race database**, use explicit first-party roots (not nested third_party modules):

```powershell
docker run --rm --memory 1200m --cpus 2 --network container:openwa-upload-pg17 -v "${PWD}:/workspace:ro" -w /workspace -e GOMAXPROCS=2 -e POSTGRES_CAMPAIGN_PREPARATION_DATABASE_URL -e POSTGRES_PAGINATION_DATABASE_URL -e POSTGRES_EXECUTION_ADMISSION_DATABASE_URL openwa-step3-go-warm:local go test -count=1 -p 1 -timeout 900s ./cmd/... ./internal/... ./tests/...
docker run --rm --memory 1200m --cpus 2 --network container:openwa-upload-pg17 -v "${PWD}:/workspace:ro" -w /workspace -e GOMAXPROCS=2 -e POSTGRES_CAMPAIGN_PREPARATION_DATABASE_URL -e POSTGRES_PAGINATION_DATABASE_URL -e POSTGRES_EXECUTION_ADMISSION_DATABASE_URL openwa-step3-go-warm:local go test -race -count=1 -p 1 -timeout 900s ./cmd/... ./internal/... ./tests/...
docker run --rm --memory 1200m --cpus 2 -v "${PWD}:/workspace:ro" -w /workspace -e GOMAXPROCS=2 openwa-step3-go-warm:local go vet -p 1 ./cmd/... ./internal/... ./tests/...
docker run --rm --memory 1200m --cpus 2 -v "${PWD}:/workspace:ro" -w /workspace -e GOMAXPROCS=2 openwa-step3-go-warm:local go build -p 1 -trimpath ./cmd/...
```

Do not silently label other unconfigured environment-gated suites as executed. Record selected environment gates, skips, commands, output, database/image identity and exact source hashes. If broader gates require additional disposable fixtures, populate their actual required variables from accepted test guidance; never point them at Railway production.

Frontend/governance commands, run from next isolated worktree after implementation:

```text
npm --prefix apps/admin-web run test:components -- tests/campaign-detail.component.test.tsx
npm --prefix apps/admin-web run test:components -- tests/campaign-draft-editor.component.test.tsx
npm --prefix apps/admin-web run test:components -- tests/campaign-readiness-panel.component.test.tsx
npm --prefix apps/admin-web test
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run build
python scripts/verify_openapi_routes.py
python -m unittest discover -s tests/governance -p test_campaign_preparation_openapi.py
git diff --check
```

Expected evidence is genuine RED before each implementation, GREEN at exact source, ordinary/race database tests with no selected test skips, route/schema consistency, typecheck/lint/build success, plus bounded authenticated local browser scenarios. Add clean-schema and upgrade-from-accepted-base validation and control-api least-privilege verification even though no migration is planned; no schema change is not proof of persistence/privilege compatibility. Existing scripts/fixtures must be read before command adaptation.

## Review and bounded acceptance

- [ ] Reconfirm accepted cohort source/base and original SRS/UX/approved decisions; revise planned file offsets/interfaces if accepted repairs changed them.
- [ ] Unit 1 reviewer can reject detail independently of DRAFT/readiness. Unit 2 consumes its client/GET; Unit 3 consumes saved aggregate fields and invalidates after its edits. No unit acceptance promotes the entire wizard.
- [ ] Freeze and hash exact touched source after focused/broader gates; independently review security, correctness, architecture and direct testing evidence. Close concrete findings and rerun only affected gates plus required final regressions.
- [ ] Record scenario evidence separately: HTTP/unit, real PostgreSQL ordinary/race, rendered mocked-component, authenticated local browser/keyboard/a11y, and external/provider/performance/production evidence. Do not merge these categories into a coverage percentage.
- [ ] Readiness evidence contains no business mutations, raw sensitive fields, hard deadline promise or source-rebinding. Draft edits never change an approved release. Existing execution lifecycle tests remain green.
- [ ] Commit the reviewed, accepted unit file set; root controls persistence/push. Deployment, production DB provisioning, source imports, real recipient sends and gateway pairing remain separate authorization/work.

## Product limits needing explicit later disposition

These are honest scope limits, not missing implementation bodies or implicit approvals.

1. **Earlier incomplete drafts:** this plan persists governed campaign DRAFTs with valid core references/transport, and incomplete schedule/content/audience steps. Saving before organisation/purpose/review/transport is known requires a separate wizard-draft aggregate/schema/lifecycle decision. Existing SRS/UX says save draft but does not specify permissive persistence semantics; this plan does not invent them.
2. **Governance reference identity changes:** this first save unit locks organisation/purpose/review even before downstream artefacts; an inactive saved identity remains visible and prevents progression rather than being silently replaced. A workflow to replace those sources while preserving/reapproving linked evidence needs its own impact/invalidation decision.
3. **COMMERCIAL_PENDING:** retain actual independent PENDING_APPROVAL versus campaign MESSAGE_APPROVED behavior. A new campaign lifecycle status is a substantive model/API/migration change, not routine UI copy.
4. **Measured deadline feasibility:** this unit shows conservative advisory gross-window capacity and blocks positive readiness when quiet-hour/restricted calendars are not modeled. Dynamic HELD/ACTIVE reservation-window feasibility is explicitly unassessed by this gross advisory preview, including when ready for final review; it is not credited as PASS. A complete future-calendar model, reservation-aware feasibility and any deadline commitment require a separately reviewed capacity/schedule unit and measured evidence.
5. **Complete journey D:** immutable snapshot selection, composer/media/version UI, masked personalization preview, approved recipient/session catalogues, controlled test, capacity hold, commercial/content/final review, typed/MFA confirmation and governed schedule remain follow-on work. No full-UX or production readiness claim follows these three foundations.

## Plan self-review

Spec slice is mapped to the three units and the explicitly deferred journey-D requirements above; full SRS/67-screen delivery is not claimed. All planned public names/types are defined before consumption; request/response prefix rules match current /api/v1 and browser /api proxy. Five Review Focus conditions have direct tests in their owning units. Code bodies are intentionally absent; the plan fixes interfaces, scope, failure assertions, source boundaries and test cycles for the implementer. This self-review is a planning check, not implementation acceptance.

Root reviews this preparatory document before any next implementation. No user confirmation is required to write it or resolve these routine interface choices; the existing cohort acceptance prerequisite remains in force.
