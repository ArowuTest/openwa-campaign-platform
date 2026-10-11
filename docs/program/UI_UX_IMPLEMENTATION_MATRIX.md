# UI UX Implementation Matrix

## Purpose and baseline

**Runtime update — 8 October 2026:** Bounded audience/cohort source `6d445cf0d5095b006622ef06e2ba251157bbbbd1` is accepted, pushed and deployed across the eight Railway application services. Production migrations/grants were applied once and verified. Nine live authenticated operator smoke checks passed, including real server rule validation; governed persistence and provider delivery remain unproven. See [the production release handover](COHORT_PRODUCTION_RELEASE_HANDOVER_2026-10-08.md) for deployment and QA evidence. The four segment rows below now use E for bounded accepted capabilities, while their remaining screen and composed-journey work stays open.

This document maps all 67 screen IDs and eight mandatory journeys in the authoritative UI/UX specification to the current implementation, evidence and remaining work. It is a source-and-evidence baseline, not certification that every screen, state, role, browser journey or accessibility requirement is complete.

Baseline date: 7 October 2026. Worktree: `C:\Users\sanus\OpenWA\campaign-platform-active\repo\.agent\worktrees\large-audience-ingestion-20261006`. Branch: `feat/large-audience-ingestion-20261006`. Accepted base HEAD: `9bae302d4c3c4ea4593ce294440a7cd31b68bb72`.

The resumable import UI/recovery tranche is accepted at that base. The bounded cohort/estimate/segment/snapshot source candidate is now **SOURCE ACCEPTED**, including the upload-finaliser least-privilege repair and review adjudication. Final source execution: 84 frontend tests/typecheck/lint/build; bounded 28/28 desktop/mobile controls and scoped accessible-name checks; actual frontend Docker image/runtime health; materialisation PostgreSQL ordinary/race 75 passing results each; full Linux ordinary/race 1,418 passing results in 49 packages each plus vet/build; 208 governance tests and 306 implemented OpenAPI routes. Broad Go runs each explicitly skip 142 unconfigured integration/load/OS cases. The supplemental finaliser role ordinary/race each pass 97 results with 13 unrelated skips; schema 17 and all new actual-role cases pass without skips. Forced-failure atomic migration rollback/upgrade proof and source review closure pass. Final 63-file implementation/contracts/tests digest is 1d695ccec5548deb1659889c34c10840502c82ce588ddf0b76548c735dd438b7. User authorized subsequent commit/push/deploy; this matrix records source acceptance, while actual later commit/build/runtime/smoke receipts are in Git and the latest FS checkpoint. Production migration 0095/0096 and corrected grants must precede application rollout. Anonymous network admission is verified at direct/proxied 401 AUTHENTICATION_REQUIRED, health 200. Production is empty of business/QA data; positive governed persistence needs lawful QA setup and an independent approver. Local mocked UI evidence does not establish real mutations, complete journeys, whole-application accessibility, provider UAT or production scale.

No live-provider UAT, complete browser-journey acceptance, whole-application WCAG 2.2 AA certification, production-scale certification, push or deployment is implied. No coverage percentage is calculated.

## Authority and provenance

Repository-relative paths in this document resolve from the worktree root above. The full SRS, not just its generated requirement rows, is authoritative: it also contains narrative workflows, business rules, lifecycle tables, exceptional cases and architectural decisions. Root reports reading its full body in bounded ranges before next-tranche planning.

| Source | Exact path and provenance | Use |
|---|---|---|
| Full SRS v1.0 | `docs/reference/OpenWA_Internal_Campaign_Platform_SRS_v1.0.docx`; original `C:\Users\sanus\Downloads\OpenWA_Internal_Campaign_Platform_SRS_v1.0.docx`; both 1,406,060 bytes; identical SHA-256 `0aa632755b9823940c0e3586cede5ed4e4f6a20d5865ccaae53752a82ded9f8d` | Product requirements, acceptance rules, lifecycle and edge cases. |
| Full UI/UX specification v1.0 | `docs/reference/OpenWA_Internal_Campaign_Platform_UI_UX_Design_Specification_v1.0.docx`; original `C:\Users\sanus\Downloads\OpenWA_Internal_Campaign_Platform_UI_UX_Design_Specification_v1.0.docx`; both 853,295 bytes; identical SHA-256 `63dc25cd71901bbe3adaef8e76bfef2fd9a0417cb8048d97ebb6fd66281a03c9` | All 67 exact screen IDs/names, required actions/states, global patterns, eight mandatory prototypes and UX acceptance criteria. |
| Additional designer brief | `C:\Users\sanus\Downloads\Additional COntext for the UI UX design.txt`; 6,916 bytes, 295 lines | Exceptional-state coverage, desktop/laptop/tablet adaptations, accessible semantic HTML, tokens, licensing, screen mapping and HTML-to-React handoff. Its illustrative UX-* labels do not replace the authoritative UI-* screen IDs. |
| Designer HTML package | `C:\Users\sanus\Downloads\design-canvas-export.zip`; 646,158 bytes | Visual/interaction reference only. The 25 supplied screens do not establish complete 67-screen coverage. |
| Integration alignment | `docs/architecture/OpenWA_Integration_Approach_and_Alignment_Findings_v1.0.docx` | Isolated engine-specific gateway pools, immutable route evidence, capability validation and no silent fallback/unsafe resend. |
| Designer intake | `docs/UI-UX/DESIGNER_HTML_PROTOTYPE_INTAKE.md`; `docs/designer-handoff/HTML_REVIEW_CHECKLIST.md` | HTML references remain subordinate to the SRS, UX specification, security, accessibility and backend contracts. |
| Full requirement catalogue/evidence | `docs/requirements/srs_requirements.json`, `docs/requirements/evidence.json`, `docs/requirements/traceability.json`, `docs/requirements/TRACEABILITY.md` | Existing SRS requirement traceability, not direct evidence that all screen states are implemented. |
| Programme and release records | `docs/program/MASTER_IMPLEMENTATION_ROADMAP.md`, `docs/program/PRODUCTION_READINESS_CHECKLIST.md`, `docs/program/RISK_AND_TECHNICAL_DEBT_REGISTER.md` | Workstreams and open screen/exception/RBAC/masking/accessibility/release gates. Historical baseline sections require reconciliation with later accepted source. |
| Engineering audit | `docs/audit/INDEPENDENT_ENGINEERING_AUDIT_PLAN.md` and `docs/audit/INDEPENDENT_ENGINEERING_AUDIT_FINDINGS_0.8.3.md` | Older engineering audit and intended frontend audit scope, not a completed full UX acceptance audit. |
| Historical frontend handover | `C:\Users\sanus\Downloads\OPENWA_ADMIN_FRONTEND_HANDOVER_2026-10-05.md` | Historical context for a different earlier slice. Its capability descriptions and old manual-hold/auth/lint gaps are not current source acceptance authority. |

Some existing traceability frontend notes are stale. For example, UX-002 describes a hardcoded environment badge, UX-009 says no sender page exists, and UX-013 describes missing frontend report workflow, while current source has a session-driven classification banner, sender console and aggregate reports. These differences require evidence reconciliation, not automatic promotion of the requirements to complete.

### Approved later decisions to preserve

The current approved working scope is an internal, authenticated, platform-scoped operator product; external clients do not operate a self-service campaign portal. Railway owns the authoritative Go control/API/PostgreSQL plane; Hostinger owns isolated OpenWA gateway/session/browser runtime; direct Meta remains a sibling transport. Preserve `docs/decisions/ADR-0007-railway-authoritative-postgres.md`.

Day 1 allows multiple READY numbers/sessions inside **one engine-specific OpenWA sender pool per campaign**. This is not a single-number restriction. Mixed-engine or multi-pool execution, automatic fallback and automatic reallocation remain deferred. The selected logical pool must not be presented as an arbitrary alphanumeric WhatsApp sender identity.

UNKNOWN remains sticky and evidence-led; no blind automatic resend or cross-route resend follows ambiguity. `docs/requirements/BACKEND_FREEZE_RELEASE_DECISIONS.md` preserves the approved release-one reconciliation posture and deferred retry/UNKNOWN policy-governance refinements.

The same freeze decision explicitly defers campaign-scoped mutable sender profile identity, retaining `docs/decisions/ADR-0006-campaign-scoped-sender-profile-identity.md` and `docs/requirements/CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md` as future authority. Do not make name/photo/About/username mutation a new Day-1 launch prerequisite.

Offline campaign vetting with minimum attributable consent/governance evidence is the accepted Day-1 operating model. Select and preserve approved organisation/purpose/channel/review evidence; do not reopen an elaborate consent-product expansion merely to complete the campaign wizard. This does not silently waive the original richer consent screens: their remaining work stays visible below and needs an explicit scoped disposition.

These working decisions are also recorded in the current project context. Historical handovers and generated traceability notes must not override later explicit user decisions or inspected current source.

## Classification and evidence rules

| Category | Meaning | What it does not mean |
|---|---|---|
| E | An implemented bounded capability has accepted source and targeted recorded evidence. Here this applies to portions of the import workspace accepted at `9bae302` and segment/cohort/snapshot capabilities accepted at `6d445cf`. | Not every required action/state of the named screen is accepted; not a full mandatory browser journey or production/live-provider certification. |
| C | Implemented candidate source and direct targeted tests exist, but the current candidate is unaccepted. | A passing focused UI test does not accept the backend repair, source freeze or complete screen/journey. |
| P | Meaningful source foundation exists, but required UI workflow/state coverage or direct acceptance evidence is incomplete. | Not zero implementation, and not permission to infer completion from a route/component/API name. |
| M | The specified UI workflow was not found in the inspected current frontend source. Backend foundations may exist. | Not a claim that the domain/backend is absent. Missing UI and missing backend contracts must be investigated separately. |

A category describes inspected capability/evidence, not a verified runtime status or a waiver. Detailed state lists below are **planned acceptance dimensions**, not assertions that those states have already passed. No row is a substitute for a future screen x role x state x API x test evidence record.

### Shared source and evidence keys

| Key | Meaningful source/test paths and boundary |
|---|---|
| AUTH | `apps/admin-web/app/login/page.tsx`, `apps/admin-web/app/mfa/page.tsx`, `apps/admin-web/app/step-up/page.tsx`, `apps/admin-web/app/access-denied/page.tsx`; `apps/admin-web/components/auth-provider.tsx`, `apps/admin-web/components/app-shell.tsx`, `apps/admin-web/lib/session.ts`, `apps/admin-web/lib/navigation.ts`. Backend browser-auth contract tests: `internal/platform/httpserver/auth_browser_contract_test.go`. |
| SAFE | `apps/admin-web/lib/api.ts`, `apps/admin-web/lib/control-api-proxy.ts`; `apps/admin-web/tests/safety.test.ts`, `apps/admin-web/tests/navigation.test.ts`, `apps/admin-web/tests/hosting.test.ts`. These are client/helper/proxy tests, not rendered business-screen or complete browser-journey proof. |
| DASH | `apps/admin-web/components/dashboard-console.tsx` and `apps/admin-web/app/page.tsx`. It displays server operational snapshots; sustained live refresh/interaction acceptance is not established here. |
| ORG | `apps/admin-web/app/organisations/organisation-manager.tsx`. Current list/create/primary-contact surface; backend organisation governance is broader than this UI. |
| CONS | `apps/admin-web/app/consent-reviews/review-manager.tsx`. Current intake form; backend review rules have `internal/consent/review_test.go`, but these do not prove the missing reviewer UX. |
| IMPORT | `apps/admin-web/app/audiences/imports/import-workspace.tsx`, `apps/admin-web/app/audiences/imports/page.tsx`; `apps/admin-web/lib/audience-import-upload-model.ts`; legacy bounded `apps/admin-web/app/audiences/imports/import-preview.tsx` is not the complete resumable source workspace. |
| IMPORT-E | `evidence/release-gates/audience-import-day1-ui-acceptance-2026-10-06.json`, present in accepted `9bae302`. Recorded desktop/mobile render and axe checks used isolated read-only mock inventories, not a complete real browser write journey. Recorded local backend/unit evidence and accepted source establish a bounded UI/recovery tranche, not all UX requirements. The record's historical pending-commit wording does not supersede its later presence in accepted HEAD. |
| IMPORT-T | `apps/admin-web/tests/audience-import-upload.test.ts`; `internal/platform/httpserver/audience_upload_sessions_test.go`, `internal/platform/httpserver/audience_import_list_test.go`, `internal/platform/httpserver/import_conflict_pagination_test.go`; `internal/audience/importer/upload_composed_postgres_integration_test.go`, `internal/audience/importer/upload_session_postgres_integration_test.go`, `internal/audience/importer/import_list_pagination_postgres_integration_test.go`. Backend/helper evidence must not be relabelled complete rendered-UI coverage. |
| SEG | `apps/admin-web/components/filter-builder.tsx`, `apps/admin-web/components/segment-snapshot-manager.tsx`; `apps/admin-web/lib/audience-builder-model.ts`. Accepted cohort/estimate/segment/snapshot source at `6d445cf`; 84 focused frontend tests plus typecheck/lint/build pass at the frozen UI source. Bounded local browser checks pass; candidate acceptance and composed real-backend journey evidence remain separate. |
| SEG-T | `apps/admin-web/tests/filter-builder.component.test.tsx` and `apps/admin-web/tests/segment-snapshot-manager.component.test.tsx` render actual React components, use actual API client/pagination and substitute external HTTP. They inspect controls and late-response/current-context behavior. `apps/admin-web/tests/audience-builder.test.ts` tests models/helpers; `apps/admin-web/tests/audience-component-test-helpers.ts` alone is not test acceptance. |
| SEG-B | `internal/platform/httpserver/cohort_estimates_test.go`, `internal/platform/httpserver/materialisation_estimate_http_test.go`, `internal/platform/httpserver/materialisation_estimate_evidence_test.go`, `internal/platform/httpserver/audience_snapshot_governance_test.go`, `internal/platform/httpserver/audience_snapshot_governance_revalidation_test.go`; `internal/audience/cohort/estimate_job_postgres_integration_test.go`; `internal/audience/materialisation/postgres_commit_snapshot_integration_test.go`. Bounded backend acceptance is closed at `6d445cf`. Sync revalidation, exact-wire equality and same-count materialisation fencing passed the final real-PostgreSQL ordinary/race gate (75 results each, zero skips), with full Linux gates and least-privilege upload-role regression closure. Positive governed live persistence remains unproven. |
| CAMP | `apps/admin-web/app/campaigns/campaign-manager.tsx` and `apps/admin-web/app/campaigns/[id]/campaign-workspace.tsx`. Current ID-driven creation, route/test/reservation evidence and late-stage execution controls, not a full preparation wizard. |
| CAMP-B | `internal/campaign/model.go`, `internal/campaign/service.go`; `internal/campaignworkspace/model.go`, `internal/campaignworkspace/service.go`; `internal/platform/httpserver/server.go`; tests `internal/platform/httpserver/campaign_approval_authorization_test.go`, `internal/platform/httpserver/campaign_pilot_admission_direct_api_test.go`, `internal/platform/httpserver/campaign_execution_atomicity_test.go`. Backend guards do not prove completed campaign UX. |
| CONTENT-B | `internal/message/version.go`, `internal/message/service.go`; `internal/platform/httpserver/assets.go`; `internal/message/version_test.go`, `internal/message/material_versioning_test.go`, `internal/message/service_test.go`. Existing immutable content, typed variables/fallbacks, links, hashes and trusted media foundations; composer/media UI is missing. |
| COMM-B | `internal/commercial/service.go` and `internal/commercial/service_test.go`. Existing independent commercial record/governance, not a complete commercial wizard/approval UX. |
| EXEC-B | `internal/execution/service.go`, `internal/execution/pilot_admission.go`; `internal/execution/pilot_admission_test.go`, `internal/execution/service_test.go`; `internal/testmessage/model.go`. Exact-route pilot and capacity/approval gates exist; the pre-final readiness contract remains a gap. |
| SEND | `apps/admin-web/app/senders/sender-console.tsx`. Masked sessions/pools/nodes and reasoned lifecycle/test-recipient controls. Backend pairing/PII tests include `internal/platform/httpserver/sender_pairing_authorization_test.go` and `internal/platform/httpserver/sender_msisdn_privacy_postgres_integration_test.go`; no complete pairing UI follows from them. |
| OPS | `apps/admin-web/app/operations/operations-console.tsx`; backend tests `internal/platform/httpserver/operations_pagination_test.go`, `internal/platform/httpserver/operations_error_test.go` and `internal/platform/httpserver/task6_authorization_matrix_test.go`. Current readiness/jobs/incidents/exceptions surface, not all operational workbenches. |
| INBOUND-B | `internal/inbound`, `internal/consent/optout_test.go`, `internal/consent/optout_policy_test.go`; `internal/platform/httpserver/inbound_optout_test.go`, `internal/platform/httpserver/inbound_pagination_test.go`, `internal/platform/httpserver/inbound_retention_policy_test.go`. Backend STOP/inbound foundations exist; operator workbench is missing. |
| REP | `apps/admin-web/app/reports/reports-console.tsx`. Server campaign, organisation and financial aggregates with privacy evidence; not full report generation/approval/expiring download. |
| PRIV-B | `internal/privacy`, `internal/retention`, `internal/operations`; `internal/platform/httpserver/export_authorization_test.go`, `internal/platform/httpserver/privacy_export_authorization_test.go`; `internal/operations/export_postgres_integration_test.go`; `internal/privacy/export_postgres_integration_test.go`, `internal/privacy/erasure_suppression_postgres_integration_test.go`. Backend governance exists but does not establish the missing privacy/reveal/export screens. |
| ADMIN-B | `internal/platformpolicy`, `internal/identity`, `internal/audit`, `internal/retention` and protected HTTP contracts. These are existing domain foundations, not implemented administration screens. |

All file references in the keys are repository-relative; directory references denote broader domain foundations, not screen acceptance proof.

## Screen inventory

The IDs and screen names below are copied from the full UI/UX specification, not derived from Next.js route counts.

### Authentication and access

| Screen ID | Exact screen name | Category | Current source/evidence | Remaining UI work and planned acceptance dimensions |
|---|---|---|---|---|
| UI-AUTH-01 | Sign in | P | AUTH, SAFE: email/password login, MFA challenge handoff, permission-safe return paths and errors. | Direct rendered/authenticated browser proof of invalid credentials, lockout, network error, loading, expired challenge and preserved safe destination; keyboard/focus/labels. |
| UI-AUTH-02 | Multi-factor authentication | P | AUTH: MFA verification and step-up page; backend auth/CSRF contract foundations. | Full enrolment/recovery/expired challenge and step-up-return behavior within approved auth scope; direct UI tests and session expiry/accessibility acceptance. |
| UI-AUTH-03 | Password and access recovery | M | No recovery UI found; identity/backend capability must be assessed separately. | Approved controlled recovery/request-access flow; expired/invalid token, restricted access, escalation and non-disclosing errors. |
| UI-AUTH-04 | Session expired / access denied | P | AUTH: shell redirects, access-denied surface and safe return paths. | Explicit expired/revoked/permission-lost states, work preservation and reauthentication return; do not leak denied object detail. |

### Dashboard and work management

| Screen ID | Exact screen name | Category | Current source/evidence | Remaining UI work and planned acceptance dimensions |
|---|---|---|---|---|
| UI-DASH-01 | Operations dashboard | P | DASH: operational counts, release-attention and reconciliation signals with generated timestamp. | Actionable role-specific drill-down, telemetry freshness/degraded states, safe auto-refresh, risk/throughput views and browser/accessibility tests. |
| UI-DASH-02 | My work and approvals | M | No personalised work/approval queue UI found. | Assigned approvals/tasks, age/deadline, filtering, claiming/reassignment where supported, empty/overdue/withdrawn states. |
| UI-DASH-03 | Global search results | M | No global-search UX found. | Permission-scoped server search, controlled exact-MSISDN lookup, masked results and audited access; running/empty/denied states without cross-scope count leakage. |

### Organisations

| Screen ID | Exact screen name | Category | Current source/evidence | Remaining UI work and planned acceptance dimensions |
|---|---|---|---|---|
| UI-ORG-01 | Organisation list | P | ORG: register with status, contact and country; list/create API calls. | Server filtering/paging and complete risk/permission/empty/error states; direct screen tests rather than API/route inference. |
| UI-ORG-02 | Create/edit organisation | P | ORG: create form exists. | Edit/detail/load and optimistic conflict UX, governed field restrictions, save/error summary, authority-aware actions and navigation-with-unsaved-work behavior. |
| UI-ORG-03 | Organisation detail | M | No full detail workspace found; current table is not a detail screen. | Summary/contacts/reviews/sources/campaigns/commercial refs/alerts/audit; suspended/archived/remediation and permission-specific states. |
| UI-ORG-04 | Organisation contacts and commercial references | P | ORG shows primary contact; COMM-B provides broader commercial backend foundations. | Governed contact/reference maintenance, history, visibility and conflict states; current single-contact display is not complete management. |
| UI-ORG-05 | Suspend/archive organisation | M | No organisation lifecycle action UX found; broader backend governance exists. | Impact review, reason, authority, confirmations and immutable evidence; active-campaign consequences and already-suspended/archived states. |

### Consent and evidence

These original richer screens remain visible for scope/evidence reconciliation. Day-1 campaign preparation should consume the approved offline basis and minimum evidence rather than silently expanding the consent product.

| Screen ID | Exact screen name | Category | Current source/evidence | Remaining UI work and planned acceptance dimensions |
|---|---|---|---|---|
| UI-CONS-01 | Consent review queue | M | Current CONS component is intake only, not a queue. | Review/renewal prioritisation, evidence completeness, risk/expiry/assignee and filter/open actions, or explicit scoped disposition. |
| UI-CONS-02 | Create consent review | P | CONS: organisation/source/purpose/wording/checks/restrictions intake form; backend review rules. | Minimum approved Day-1 evidence contract and clear recorded-versus-approved state; required fields, duplicate/conflicting scope, preserved validation errors and tested permission boundaries. |
| UI-CONS-03 | Consent review workspace | M | Backend review/decision rules exist; no current decision workspace found. | Attributable evidence/restriction/expiry decision and history within approved scope; mandatory checks, self-approval/scan/unavailable/conflict states. |
| UI-CONS-04 | Evidence viewer and library | M | Storage/review backend foundations do not supply a viewer screen. | Safe versioned file inspection, checksum/scan/uploader metadata, restricted downloads, missing/superseded/unsafe-format states, or scoped disposition. |
| UI-CONS-05 | Consent purpose and wording versions | M | Import/builder consume controlled purpose inventories; no management UX found. | Version/compare/retire/approve UI, effective-date conflicts and preserved historical campaign basis, or scoped disposition. |
| UI-CONS-06 | Expiry and renewal | M | Existing approved review expiry is consumed by backend/import; no renewal UI found. | Renewal/shortening/expiry, changed evidence and campaign impact without silent scope expansion, or scoped disposition. |
| UI-CONS-07 | Remediation and rejection detail | M | No remediation/rejection workspace found. | Reason codes, owner/due/history, revised evidence and reopen/close semantics within approved operating scope. |

### Audience repository and imports

E marks the accepted **bounded import capability**, not acceptance of every original screen action or exceptional state.

| Screen ID | Exact screen name | Category | Current source/evidence | Remaining UI work and planned acceptance dimensions |
|---|---|---|---|---|
| UI-AUD-01 | Contact repository | M | No scalable contact search/list UI found. Backend canonical audience foundations exist. | Masked repository search, server filters/paging, provenance/freshness/eligibility, permitted export and segment handoff; expensive query/denied/stale states. |
| UI-AUD-02 | Contact detail | M | No complete contact detail UI found. | Profile/provenance/consent/suppression/history, controlled correction/reveal/privacy requests and legal-hold/recycled/conflicting-attribute states. |
| UI-AUD-03 | Import jobs list | E | IMPORT, IMPORT-E/T: organisation-scoped recent-import inventory, cursor pagination, refresh and reopen. | Complete all specified source/import types, owner/retry/failed-stage dimensions and real composed browser mutation/recovery proof; filtered-empty versus no-data and role/accessibility states. |
| UI-AUD-04 | Import wizard - source and upload | E | IMPORT, IMPORT-E/T: approved-context intake, bounded resumable parts, progress/pause/resume/cancel and browser-reload metadata. | Direct-S3 remains rollout-gated; validate real transfer/reconnect/expiry/oversize/unsupported/malware/encrypted-format dimensions and file-identity changes. Mock inventory QA is not a real upload journey. |
| UI-AUD-05 | Import wizard - column mapping | E | IMPORT and audience-import-upload tests: bounded CSV header probe, explicit mapping and XLSX context. | Complete specified transformations/examples/templates, duplicate/required-field/mapping-error UX and real source-to-server/browser acceptance; never infer missing demographics. |
| UI-AUD-06 | Import validation results | E | IMPORT, IMPORT-E/T: server-authoritative status/counts, bounded issue samples/report download and governed approval. | Complete count reconciliation, threshold/partial/engine-error and remapping/rejection paths; test exact authorised actions through a real composed browser/API journey. |
| UI-AUD-07 | Duplicate and merge review | E | IMPORT, IMPORT-E/T: masked profile conflict display, KEEP_EXISTING/USE_INCOMING resolution and post-merge reconciliation closure. | Full specified provenance/scope/selected-merge/defer states as contract permits, optimistic conflicts, late refresh and consent separation; direct UI action acceptance beyond render-only QA. |
| UI-AUD-08 | Data-quality dashboard | M | No dedicated quality dashboard found. | Completeness/invalid geography/stale age/source-quality aggregates, drill-down/remediation, delayed/no-data and accessible reporting. |
| UI-AUD-09 | Suppression management | M | Backend suppression/STOP/privacy foundations exist, but no operator management screen found. | Masked scoped suppression add/import/revoke/history with reasons/authority; duplicate/hold/irreversible do-not-contact states. |
| UI-AUD-10 | Privacy/data-subject request | M | PRIV-B: domain/test foundations; no request workspace found. | Identity verification, scoped access/correction/objection/deletion/restriction workflows, due dates/legal holds, evidence, approval and secure exports. |

### Segments and snapshots

The four E rows have bounded source acceptance and deployed runtime at `6d445cf`, with final real-database/broader Linux gates closed. Composed browser journeys, governed live persistence, masking/sample UX and full screen-state acceptance remain open.

| Screen ID | Exact screen name | Category | Current source/evidence | Remaining UI work and planned acceptance dimensions |
|---|---|---|---|---|
| UI-SEG-01 | Saved segments list | E | SEG, SEG-T: actual manager inventory/version/clone controls, tested current-context and late-response behavior. | Full owner/purpose/last-estimate metadata, retired-definition/stale estimate states and role/browser/keyboard acceptance; positive governed live save/reopen acceptance. |
| UI-SEG-02 | Visual segment builder | E | SEG, SEG-T/B: nested AND/OR rules, authoritative catalogues, validation, summaries and save integration. Rendered tests exercise context changes. | Hydration and exact-wire equality focused RED/GREEN are recorded; remaining acceptance covers the composed Lagos/age-freshness journey, expensive/unsupported rules, all role/failure states, responsive/a11y acceptance and positive governed live composed-workflow acceptance. |
| UI-SEG-03 | Estimate and exclusion analysis | E | SEG, SEG-T/B: durable estimates and staged counts; as-of versus calculation-time evidence and stale-context guards. | Job-level asOf handling focused RED/GREEN is recorded; remaining acceptance covers full exclusion-stage/version/freshness interpretation, aggregate drill-down, outage/recovery and real browser plus database-backed end-to-end evidence; positive governed production fixture acceptance remains open. |
| UI-SEG-04 | Masked sample preview | M | Current estimate/snapshot UI deliberately does not render recipient row payloads; no specified sample workflow found. | Bounded permission/privacy-governed sample, method/limit/provenance/eligibility reasons, empty/denied/refresh states. Do not add a bulk-reveal shortcut. |
| UI-SEG-05 | Create and inspect audience snapshot | E | SEG, SEG-T/B: campaign-bound materialisation jobs, cancel/progress and immutable snapshot hash/version evidence; rendered race/current-context tests. | Implemented governance repairs passed final real-database and broader Linux gates; remaining work includes full expiry/supersession/post-snapshot suppression/approval/invalidation UX and composed browser acceptance. |

### Campaigns, messages and approvals

| Screen ID | Exact screen name | Category | Current source/evidence | Remaining UI work and planned acceptance dimensions |
|---|---|---|---|---|
| UI-CAMP-01 | Campaign list | P | CAMP: register with status/route/audience/window; CAMP-B has campaign persistence/guards. | Source-filtered/paged list, organisation/purpose/owner/risk/next action/blockers, clone/archive behavior and empty/limited/error states. Candidate list filters are not accepted baseline authority. |
| UI-CAMP-02 | Campaign creation wizard | P | CAMP creates one governed DRAFT with manually supplied IDs/versions. | Durable multi-step save/edit/reopen, authoritative selectors and stage prerequisites; incomplete step, lost permission, source retirement, validation summary and unsaved-work states. No browser-only approval rules. |
| UI-CAMP-03 | Campaign overview | P | CAMP displays route/test/hold/metrics and late execution controls. | Direct detail GET, durable preparation sections, exact audience/message/review/config hashes/versions, approvals/schedule/capacity/reports/audit; locked fields, state changes and conflicts. |
| UI-CAMP-04 | Message composer and versioning | M | CONTENT-B: immutable message versions, types, variables/fallbacks, links, hashes and masked preview exist. | Typed composer, trusted-media attachment, masked representative preview, validation, new-version/save/restore, review/test handoff and missing-fallback/URL/media/locked states. |
| UI-CAMP-05 | Media library | M | CONTENT-B and assets API provide trusted object/scan foundations; no library UI found. | Upload/version/archive/attach, checksum/type/size/scan/rights/expiry/usage; pending/failed/quarantined/unsupported/expired states and no table autoplay. |
| UI-CAMP-06 | Test send | P | CAMP, EXEC-B: approved-recipient/session ID form and controlled-test evidence. | Authoritative compatible READY/test-recipient selectors, exact message preview/variables, response/acceptance and unavailable/invalid/partial/route-drift states. ACCEPTED is not delivered. |
| UI-CAMP-07 | Approval queue | M | Backend staged approval/commercial records exist; no full approval queue found. | Approval type/risk/deadline/submitter/age, open/claim/reassign as supported, overdue/prerequisite-change/withdrawn states; no approval without evidence detail. |
| UI-CAMP-08 | Approval detail | M | CAMP-B, COMM-B and EXEC-B supply guarded decisions; current workspace's late buttons are not evidence-rich approval detail. | Exact immutable artefacts/diffs/warnings/capacity/audit, reasoned decision and self-approval/stale/conflicting/route-risk states; MFA and reapproval semantics. |
| UI-CAMP-09 | Schedule and pre-flight | P | CAMP collects dates/timezone/quiet hours and manual exact-route hold values; EXEC-B has final pilot gates and scheduled-or-later plan/forecast. | Pre-final read-only readiness/forecast/blockers, server-issued selectors/evidence, authoritative timezone/UTC/DST behavior, measured capacity/consent-expiry/deadline/sender risk and explicit release confirmation. |
| UI-CAMP-10 | Live campaign dashboard | P | CAMP metrics and DASH/OPS readiness aggregates exist. | Canonical funnel, throughput/forecast, sender health/incidents/breakdowns, safe auto-refresh/freshness, at-risk/stale/provider-degraded/completed and accessible announcements. |
| UI-CAMP-11 | Pause, resume and emergency stop | P | CAMP late pause/resume/cancel controls and SEND lifecycle controls; guarded backend execution foundations. | Impact/reason/confirmation/incident and explicit in-flight semantics; changed-risk resume, already-paused/outage/UNKNOWN and authorised global emergency stop. Never imply message recall. |
| UI-CAMP-12 | Completion and reconciliation | M | OPS has exception resolution and REP has aggregates; no composed campaign completion workspace found. | Final/pending/UNKNOWN/exclusion counts, closure prerequisites/decision, late-event/report-version policy and completion-with-exceptions behavior. |
| UI-CAMP-13 | Campaign report and export | P | REP displays campaign/financial aggregates and privacy evidence; PRIV-B protects exports. | Generate/approve/version/download/share through controlled channels, data cut-off/methodology/caveats, queued/failed/expired/revoked and small-cell states. Aggregate display is not secure export acceptance. |
| UI-CAMP-14 | Clone campaign | M | Campaign backend cloning exists; no clone workflow found. | Copy-selection/new dates/constraints and fresh draft IDs; retired source evidence, no copied-valid snapshot/approval/payment/entitlement and explicit review. |

### Senders and operations

| Screen ID | Exact screen name | Category | Current source/evidence | Remaining UI work and planned acceptance dimensions |
|---|---|---|---|---|
| UI-SEND-01 | Sender/session list | P | SEND: masked sessions, engine/pool/node/status/capacity/heartbeat. | Full health/rate/queue/last-success/filter/detail and permission/readiness states; logical-pool versus actual-number explanation and direct UI tests. |
| UI-SEND-02 | Session detail | P | SEND selection with reasoned start/drain/resume/stop controls. | Auth/ownership/current-campaign/events/limits/reconnect diagnostics, controlled limit/recovery/retirement UX, redacted logs and conflict/unreachable/auth-expired states. |
| UI-SEND-03 | Connect/pair account | M | Sender inventory excludes QR payloads; backend pairing authorization exists. | Controlled registration/pairing/recovery owner, expiring transient QR/pairing state, duplicate/rejected/conflicting session paths without logging or persisting sensitive QR material. |
| UI-SEND-04 | Sender pool management | P | SEND pool inventory/capacity display; single-engine routing/backend governance foundations. | Governed create/membership/change/drain/retire, measured capacity and immutable campaign impact; busy/unhealthy/duplicate/no-capacity states. No Day-1 mixed-engine expansion. |
| UI-SEND-05 | Worker node list/detail | P | SEND node route/status/load inventory. | Restricted detailed heartbeat/resource/version/ownership/maintenance/drain diagnostics, approved runbooks and degraded/stale/mismatch/disk-pressure states. |
| UI-OPS-01 | Active operations centre | P | OPS/DASH readiness, jobs/incidents/exceptions aggregates. | Active campaign/rate/deadline wallboard, scoped drill-down/escalation/acknowledgement and incident/stale/no-active states with accessible freshness. |
| UI-OPS-02 | Queue monitor | P | OPS durable job list/summary and reasoned retry/cancel. | Campaign/sender filters, lag/throughput/delayed/reconstruction visibility, authorised intake controls and inconsistent-count/Redis-unavailable/recovery states. PostgreSQL remains authoritative. |
| UI-OPS-03 | Failed delivery workbench | P | OPS delivery exceptions and job actions; backend error/authorization foundations. | Retryable/permanent reason/remediation, exact approved bulk scope and duplicate-safety evidence, limit/provider-outage and partial-action states; no unsafe resend of UNKNOWN. |
| UI-OPS-04 | Unknown outcome reconciliation | P | OPS evidence/ref/reason resolution form, duplicate-risk acknowledgement where applicable; backend reconciliation foundations. | Complete event/time/confidence history, unresolved/late/conflicting/missing-provider-ID and immutable resolution view; governed permissions, confirmation and UI tests. UNKNOWN never means delivered or safe to retry. |
| UI-OPS-05 | Inbound replies and opt-outs | M | INBOUND-B: STOP/inbound domain and API tests exist; no operator workbench found. | Masked reply/classification/suppression history, reviewed ambiguity/escalation, duplicate/multilingual/exact-keyword states and visible final-dispatch exclusion evidence. |
| UI-OPS-06 | Alert centre | M | DASH/OPS show incident/critical counts; no dedicated alert-centre workflow found. | Persistent severity/source/affected-object/owner history, acknowledge/assign/resolve/runbooks and duplicate/stale/storm states. Counts are not completed alert workflow. |

### Reports and administration

| Screen ID | Exact screen name | Category | Current source/evidence | Remaining UI work and planned acceptance dimensions |
|---|---|---|---|---|
| UI-REP-01 | Reports library | P | REP displays server reports/financial aggregates; PRIV-B contains governed export foundations. | Generated/scheduled/versioned report inventory, cut-off/approver/expiry and generate/download/regenerate; queued/failed/expired/revoked states with actual authorisation/download UI proof. |
| UI-REP-02 | Audience and campaign analytics | P | REP aggregate campaign/organisation metrics, breakdowns and privacy evidence. | Server filters, trends, saved views, governed drill-down/export, accessible chart/table alternatives and stale/small-cell/no-data states. |
| UI-ADMIN-01 | User administration | M | ADMIN-B identity/auth foundations exist; no management screen found. | Individual provision/suspend/MFA reset/session revocation, scope/expiry and effective permissions; privileged/duplicate/locked states, reasons and audit. |
| UI-ADMIN-02 | Roles and permissions | M | AUTH/SAFE consume permissions; backend identity/security foundations exist. | Versioned role matrix, compare/approve/activate/effective-access review, escalation/orphan/session-impact safeguards; consuming permissions is not role administration. |
| UI-ADMIN-03 | Configuration catalogue | M | ADMIN-B platform policy and domain-governed settings exist. | Effective-dated create/validate/approve/schedule, impacted drafts/history and overlaps/invalid thresholds; preserve approved deferred retry/UNKNOWN governance scope. |
| UI-ADMIN-04 | Audit log | M | Backend audit/search/export foundations exist; no full audit UI found. | High-volume masked event search/detail, correlations/before-after references, approved range export and archive/integrity/denied states. |
| UI-ADMIN-05 | System health and backup status | P | DASH/OPS provide platform readiness signals. | Complete service/database/queue/storage/worker/backup/replication/certificate freshness, incident/check/restore-test views and degraded/outage states. Existing counts do not certify backup or recovery. |
| UI-ADMIN-06 | Retention and archive policies | M | PRIV-B/ADMIN-B provide domain foundations; no controlled policy UI found. | Versioned simulation/approval/holds, archive destinations/next runs/impact, legal-hold conflict/archive failure/deletion-blocked and secure destructive confirmations. |

## Mandatory journey inventory

These are the exact Prototype A-H flows in UI/UX specification section 16.3. The category is a composed-journey assessment, not a roll-up of route counts.

| Prototype | Mandatory flow | Current source/evidence baseline | Remaining composed acceptance |
|---|---|---|---|
| A | Create organisation -> consent review -> approve with restrictions | P: ORG/CONS provide intake; backend governance exists. Current consent UI is not a complete reviewer workspace. | Attributable minimal offline basis/expiry/restrictions and independent decision through approved Day-1 scope; denied/self-approval/invalid evidence paths. Richer consent-product expansion requires explicit disposition, not automatic scope reopening. |
| B | Upload 10m-capable audience file -> map -> validate -> reconcile -> approve | E for bounded import capability at `9bae302`; IMPORT-E records local render/axe/mock-inventory QA and backend/unit proof. Complete mandatory journey remains partial. | Actual authenticated browser write/reload/recovery, malware/validation/conflict/approval/reconciliation exceptional paths; authorised large-file and external object-store/performance evidence remain separate. |
| C | Build Lagos 18-35 segment -> inspect staged counts -> create snapshot | P: bounded source accepted and deployed at `6d445cf`; targeted rendered/model/backend and nine bounded live smoke checks pass. Full composed journey and masked sample UX remain open. | Exercise an approved governed fixture through composed browser age method/freshness/policy/count/snapshot and stale/changed-context states; implement missing sample UX with masking and permissions. |
| D | Create campaign -> compose/test message -> select sender -> pre-flight -> approvals -> schedule | P: CAMP/CONTENT-B/COMM-B/EXEC-B exist, but the integrated wizard, durable draft/detail and pre-final readiness contract are unfinished. | Staged resumable preparation with real immutable audience/content/media/review/config evidence, exact-route test, one engine pool, measured capacity, independent approvals and typed/MFA scheduling; failure/retirement/conflict/permission loss. |
| E | Monitor at-risk live campaign -> inspect degraded sender -> pause/drain -> resume | P: CAMP/SEND/OPS provide limited metrics/actions. No complete live composed acceptance established. | Safe refresh/forecast/freshness, precise in-flight/pause/drain semantics, changed-risk resume and outage/ownership/UNKNOWN behavior; real provider/operational UAT separate. |
| F | Process STOP reply -> create suppression -> confirm campaign exclusion | M for operator journey; INBOUND-B and final eligibility backend foundations exist. | Complete masked inbound/suppression/exclusion UX with exact-keyword ambiguity, duplicates, authority and attributable evidence; never infer full flow from backend tests alone. |
| G | Generate and approve campaign report | P: REP displays aggregates; protected export/report foundations exist. | Real generated report job/version/cut-off, independent approval and controlled expiring download; failed/stale/small-cell/revoked and late-event report-version behavior. |
| H | Reveal/export sensitive data with permission, reason and step-up authentication | M for composed UI; PRIV-B protects backend operations. | Authorised reveal/export UI with reason/scope/expiry/audit, denied/cancel/revoke/back-button/no-cache tests and masking throughout. Existing backend authorization tests are not whole journey acceptance. |

## Next bounded campaign-preparation priorities

The next accepted unit after cohort acceptance should be **campaign detail, durable DRAFT and pre-final readiness foundations**, followed by completion of Prototype D. It must not automatically close UI-CAMP-01 through UI-CAMP-09.

### Source-confirmed prerequisites

1. **Direct detail GET.** `internal/campaign/service.go` and PostgreSQL repository provide domain Get, but the inspected registered/OpenAPI routes do not expose `GET /api/v1/campaigns/{id}`. Current workspace searches bounded campaign inventory. Add an authoritative permission/scoping/masking contract rather than silently depending on the first 2,000 inventory rows.
2. **Durable DRAFT save/edit/restore.** Current create persists a governed DRAFT, not arbitrary incomplete wizard state. Organisation/purpose/consent-review/transport governance must already be valid; schedule may be omitted in the domain until later. There is no general draft PATCH/save. `Campaign.AmendMaterial` rejects DRAFT, CONSENT_* and AUDIENCE_BUILDING and cannot edit name/organisation/purpose/consent-review. `campaignworkspace` notes/tags/archive are not resumable wizard persistence. Define permitted incomplete data and early-stage editing deliberately without inventing consent/route evidence.
3. **Pre-final readiness contract.** No general preflight route was found. Existing execution plan/forecast require scheduled-or-later states; GET execution-plan records admission evidence, while forecast is read-only. They cannot simply be reused as a pre-final read-only wizard checklist. Assess exact artefacts, version/freshness, blockers and measured single-engine capacity before final approval, while keeping final server gates authoritative.
4. **Pending-state reconciliation.** The SRS lifecycle specifies COMMERCIAL_PENDING; current campaign model has COMMERCIAL_APPROVED but not COMMERCIAL_PENDING. The independent commercial record has PENDING_APPROVAL; campaign remains MESSAGE_APPROVED until an explicit APPROVE_COMMERCIAL transition. Explain both actual states without inventing a browser-only campaign status. Other catalogue gaps must be verified against source and explicitly dispositioned.
5. **Typed contract closure.** `contracts/openapi/control-api.yaml` has incomplete message-create/idempotency/request detail and approval/test/commercial stubs; material amendment schema omits implemented timezone/quiet-hours fields. Reconcile actual handlers and schemas before relying on generated wizard clients.

### Bounded acceptance plan

- Reconcile the full SRS, UI/UX, approved later decisions and this matrix into explicit contract/scenario scope before implementation.
- Prove direct detail authorization, scoped not-found/denied behavior, masked immutable evidence and no inventory-scan dependency.
- Prove durable draft create/save/reload/edit, permitted incomplete fields, optimistic conflicts, invalid/retired sources, failure recovery and rejection of unauthorised changes. Saving a draft must not approve, reserve, release or dispatch.
- Prove pre-final read-only readiness with authoritative blockers and freshness/version evidence, exact one-engine pool and multiple READY sessions, measured capacity/deadline evaluation and no business mutation from a preview read.
- Connect a bounded preparation workspace to those contracts. Test actual rendered controls/current-context behavior, not only payload/model helpers. Demonstrate preservation after failed saves, reload/reopen, permission loss and optimistic conflict.
- Run relevant unit/HTTP/PostgreSQL clean/upgrade/least-privilege tests plus OpenAPI parity, frontend tests/typecheck/lint/build/audit and bounded authenticated local browser keyboard/axe/responsive scenarios. These are planned gates; no result is asserted by this plan.
- Freeze exact source, complete independent review/adjudication, update direct evidence/scenario mappings and make only an accepted commit. Push/deployment remain separately authorised.

Subsequent Prototype D integration still includes campaign-bound immutable snapshot selection, composer/media versions and masked preview, approved test-recipient and compatible READY-session selection, exact-route controlled test, authoritative hold/capacity evidence, commercial/content/final approval detail, timezone-aware schedule and typed/MFA confirmation. Do not promise deadline feasibility from operator-entered capacity values or mark the journey complete from final-approval buttons alone.

## Planned cross-cutting acceptance dimensions

These dimensions apply to every relevant screen; they are not a list of already verified statuses:

- Role/action/data-scope accuracy, deny-by-default, maker/checker restrictions and recent MFA where required.
- Loading, no-data versus filtered-empty, validation, permission denied/lost, unavailable service, partial failure, stale evidence, optimistic conflict and recovery.
- Durable long-running jobs, leave/reopen/reload behavior, progress stages, idempotent replay and safe cancellation/retry.
- Immutable artefact IDs/hashes/config/review versions, recorded-versus-approved state, material-change reapproval and source retirement/expiry.
- Masked PII, controlled reveal/copy/export, no unsafe browser caching/back-button/page-source leakage, and no secrets/QR material in source or ordinary diagnostics.
- Canonical queued/submitted/gateway-accepted/sent/delivered/read/failed/UNKNOWN distinctions, no fabricated counts and evidence-led uncertainty.
- Explicit impact/reason/confirmations for release, pause, cancel, export, override and emergency actions; precise in-flight/no-recall semantics.
- Server-side high-volume paging/filtering/jobs, privacy thresholds, authoritative timestamps and stale-count controls; no client loading of millions of records.
- Keyboard paths, focus, error summaries, labels, contrast/status alternatives, chart table alternatives, zoom and desktop/laptop/tablet adaptations.
- Source-bound direct tests and actual composed browser evidence, with mock/local/live/production and accepted/candidate boundaries recorded separately.

The matrix must be updated as new evidence is accepted. A source edit, passing helper test, route count, historical handover or nearby domain test cannot by itself promote a whole screen or mandatory journey to complete.
