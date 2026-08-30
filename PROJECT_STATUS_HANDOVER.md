# OpenWA Campaign Platform — Project Status & Handover

**Last updated:** 2026-08-10 10:05 (Europe/London)
**Repository:** `C:\Users\sanus\OpenWA\campaign-platform-active\repo`
**Branch:** `work/backend-production-engineering`
**Committed HEAD:** `786451d5616f5e179903fd9b61c5e26f6de76f77`
**HEAD subject:** `docs: record Task 5 checkpoint`

## Purpose of this document

This is the living continuity record for the OpenWA Internal Campaign Platform backend-production-engineering work. Read this file before resuming work if conversation context, agent context, or local session history has been lost.

This document must be updated at meaningful checkpoints, especially before commits, after discovering material defects/gaps, and before handing work to another session/agent.

## Non-negotiable working rules

- Evidence before assertion. Never mark a requirement complete from filenames, nearby functionality, architecture similarity, or assumption.
- `IMPLEMENTED_TESTED` requires implementation plus direct evidence matching the requirement's acceptance criterion.
- Implementation without sufficient acceptance proof is at most `PARTIAL`.
- Missing or approximate evidence does not count as completion.
- Do not delete or narrow requirements to make the implementation appear complete.
- Use TDD for new/changed behavior: prove RED for the intended reason, make the minimal fix, then prove GREEN.
- For unexpected failures, diagnose the actual root cause before editing production code.
- Use the authoritative Windows repo above; do not substitute a stale copy.
- Do not claim Task completion/commit/checkpoint until fresh verification supports it.
## Scope and source-of-truth hierarchy

Authoritative scope precedence currently used:

1. `OpenWA_Internal_Campaign_Platform_SRS_v1.0.docx`
2. `OpenWA_Internal_Campaign_Platform_UI_UX_Design_Specification_v1.0.docx`
3. Explicit integration/runtime findings
4. Later user-approved decisions
5. Later evidence-backed corrections over unsupported earlier claims
6. Release notes alone do not prove live/external closure

Previously fully read source material includes the SRS (~56 pages, 398 granular requirements), UI/UX specification (~46 pages), `Section of chat in thread 8.txt` (1,058 lines), and `hostimger vps set up discussion.txt` (1,051 lines).

Primary backend closure plan:

`docs/superpowers/plans/2026-08-07-backend-closure-after-configuration-governance.md`

Runtime configuration design:

`docs/superpowers/specs/2026-08-07-runtime-configuration-governance-design.md`

Runtime doctrine: code invariants only; operational knobs in governed PostgreSQL configuration; environment endpoints/safety ceilings in deployment config; secrets never in source/defaults; fail closed where safety/governance evidence is missing.

## Core architecture doctrine

- Go + PostgreSQL are the authoritative business/control plane.
- Node/NestJS `openwa-gateway` owns WhatsApp transport.
- Redis is reconstructable/supporting state only.
- PostgreSQL is authoritative for campaigns, recipient ledger, audience snapshots, consent/suppression, sender configuration, routing/reservations, delivery state, UNKNOWN outcomes, audit, governance and operations.
- Durable execution uses PostgreSQL recipient ledger + transactional outbox + durable jobs.
- Ambiguous provider/send outcome becomes `UNKNOWN`; never perform an unsafe automatic resend.
- `third_party/openwa/upstream` is retained reference/provenance only, not the active application gateway.
## Production infrastructure decision (approved direction)

The agreed production direction is **hybrid infrastructure**:

- **Railway:** authoritative control/data plane — Go APIs/workers, PostgreSQL, Redis, private application networking, and Railway S3-compatible object storage.
- **Hostinger VPS:** WhatsApp execution/gateway fleet — Baileys, whatsapp-web.js, Chromium, persistent session state, sender/session proxies and gateway-node capacity.
- Keep provider-specific details out of business logic. Model generic gateway/infrastructure nodes so Hostinger/Railway can be replaced later.

Reasoning: measured control/data-plane memory is modest; persistent WhatsApp/Chromium sessions are the more variable RAM workload. Railway reduces control-plane/DB/object-storage operational burden; Hostinger provides inexpensive fixed RAM/root control for stateful browser-heavy gateways.

Measured capacity evidence from the 2026-08-08 local simulation:

- 13 active OpenWA containers idle: ~790 MiB combined RAM.
- PostgreSQL idle: ~204 MiB; gateway idle without active sender sessions: ~91 MiB.
- 1,000,000 recipient-shaped indexed rows: ~656 MB disk; PostgreSQL ~618 MiB during load.
- 10,000,000 synthetic contact/MSISDN rows: 1.662 GB heap + 2.113 GB representative indexes = 3.776 GB total.
- PostgreSQL peak during 10m index build: ~1.98 GB RAM.
- Whole 13-service stack with warmed 10m dataset: ~2.45 GB RAM.
- Exact HMAC lookup and bounded 1,000-row pages remained millisecond-scale in the synthetic benchmark.
- Synthetic scale tables were dropped after measurement.

Important sizing conclusion: total campaign/audience size primarily increases database/storage/duration. RAM scales more strongly with sender-session concurrency, especially WWebJS/Chromium. Real active-session measurements are still a production gate.

A full hybrid production infrastructure/architecture document was generated outside this repo during the discussion; if it is later added to the repo, this handover should link to its committed path.

**Authoritative delivery sequence (user-confirmed 2026-08-08):**

1. Finish backend build, hardening, Task 5 evidence reconciliation, then Task 6 adversarial code-side review and Task 7 backend checkpoint.
2. Integrate the closed backend with the agreed Railway control/data plane + Hostinger gateway fleet and prove deployment/network/DB/object-storage/live-transport/failure/DR behavior.
3. Only after backend + production-infrastructure integration is proven, build the deferred frontend against the completed APIs, using the designer-produced UI as visual/workflow reference plus the authoritative UI/UX specification.

Do not start the production frontend early merely because frontend requirements remain NOT_STARTED/PARTIAL in the SRS catalogue.
## Task 4 — CLOSED and committed

Task 4 was completed and committed independently at:

`c728cfa8bd8ff6ff0a95068602ef575982eb9a8e` — `feat: close Task 4 pagination and gateway telemetry`

Task 4 major outcomes:

- Cursor/keyset pagination completed for all genuinely growing top-level list handlers.
- Handler audit shows every list handler that accepts a `limit` and can grow unbounded also exposes cursor continuation.
- Remaining non-cursor list handlers are classified bounded/reference/current-state views.
- Migration `0070_top_level_pagination_indexes.sql` added missing keyset access paths without duplicating existing indexes.
- Clean migration replay 0001→0070 succeeded.
- OpenAPI pagination contract reconciled for 19 newly paginated top-level inventories.
- OpenAPI route parity remained 281/281 implemented `/api/v1` method/path pairs.
- Approved test-recipient pagination received HTTP + PostgreSQL continuation proof.
- Gateway runtime resource-health telemetry now persists container disk/inode/process/network evidence.
- Active/development Compose was wired for signed runtime registration/heartbeat.
- Governed gateway runtime version bug fixed: telemetry heartbeats no longer increment governance version.
- Live proof observed `REGISTERED → HEARTBEAT → HEARTBEAT...` while node governance version remained stable.
- Active dev gateway fixture was governed/provisioned rather than auto-created by heartbeat.

Fresh Task 4 verification before commit included full Go tests, `go vet`, `go build`, race verification for affected packages, PostgreSQL integration tests, gateway typecheck/build/resource-health checks, governance tests, npm audits, production Compose validation, rebuilt active stack, 13/13 container health, `/healthz` 200, `/readyz` 200, fresh heartbeat/resource evidence, diff hygiene and secret scan.
## Task 5 — IN PROGRESS, NOT COMMITTED

Task 5 is the 398-requirement evidence reconciliation. Do not call it complete yet.

Authoritative evidence source:

`docs/requirements/evidence.json`

Generated outputs:

- `docs/requirements/TRACEABILITY.md`
- `docs/requirements/traceability.json`

Current generated distribution at this handover point:

- `IMPLEMENTED_TESTED`: 160
- `PARTIAL`: 128
- `NOT_STARTED`: 107
- `BLOCKED_EXTERNAL`: 3
- Total: 398

These counts are not a target. They are only the consequence of evidence decisions.

Reconciliation method:

1. Read exact requirement and acceptance criterion from `srs_requirements.json`/traceability.
2. Inspect active implementation semantics; retained upstream/reference code does not count.
3. Find existing direct automated evidence.
4. If acceptance evidence is missing but behavior should exist, add a narrow characterisation/acceptance test first.
5. Run against real PostgreSQL where the requirement is persistence/transaction/concurrency specific.
6. Fix production defects only when the unchanged test exposes them.
7. Set status conservatively; document the exact missing evidence for PARTIAL/NOT_STARTED.
8. Regenerate traceability from the evidence source and run an ID-level contamination check.
### Task 5 reviewed families so far

**SND — reviewed and evidence source updated.** Key evidence decisions include:

- SND-002: direct HTTP→encryption→PostgreSQL→ordinary-read proof; plaintext E.164 absent from normal JSON/readback.
- SND-012: encrypted governed per-session proxy configuration, audit evidence, step-up/admin route protection, active-session change rejection and gateway propagation proven.
- SND-018: PostgreSQL test proves default `MaxActiveCampaigns=1`; campaign B is rejected on the same sender while campaign A is active.
- SND-019: direct 403 role-denial and MFA step-up pairing-data tests.
- SND-020: gateway log-sanitisation test + committed-secret scanner both green.
- SND-007 and SND-009 were downgraded/kept PARTIAL because lease/fence primitives are correct, but the production sender-lease acquisition/renewal lifecycle is not wired end-to-end.
- SND-008 remains PARTIAL: database allocator rejects stale worker/expired lease, but lease renewal via live heartbeat lifecycle is not fully wired/proven.
- SND-011 remains PARTIAL: hard session ceiling exists, but admission is not resource-aware.
- SND-017 remains unproven for campaign-or-organisation sender reservation semantics.
- SND-022 remains NOT_STARTED: no real warm-standby concept exists.

**DLV — reviewed and evidence source updated.** Key decisions:

- DLV-001: natural-key/replay uniqueness proven in PostgreSQL.
- DLV-003: entitlement-overflow transaction rollback proven to leave zero recipients/outbox rows.
- DLV-004: crash/reclaim outbox publication proof with fencing and one durable job.
- DLV-006: explicit 5,000,000-recipient bounded release test: 1,000 calls of <=5,000 recipients and bounded deterministic shard IDs.
- DLV-007: PostgreSQL `FOR UPDATE SKIP LOCKED` single-owner claim, expiry recovery and stale-fence rejection proven.
- DLV-008: fresh/capable sender allocates; stale heartbeat, exhausted daily capacity and expired lease are excluded.
- DLV-009 remains PARTIAL: final eligibility safely blocks pause/future start/deadline/quiet hours/etc., but temporary holds are currently terminally marked `SUPPRESSED_BEFORE_SEND` instead of held for later.
- DLV-010: final pre-send eligibility query proven against pause, future start, deadline, quiet hours, inactive contact, suppression and consent withdrawal after fixing purpose-ID typing.
- DLV-012: two-attempt retry history persists all ordered delivery events; authoritative recipient ended at `GATEWAY_ACCEPTED` with `AttemptCount=2`.
- DLV-013 remains PARTIAL: failure classification exists, but no governed administratively configurable retry policy by failure category.
- DLV-014 remains PARTIAL: exponential durable-job backoff exists, but no retry jitter/configuration matching acceptance.
- DLV-018 remains NOT_STARTED: no campaign-cancellation workflow applies cancellation to undispatched recipients and reconciles counts.
- DLV-019 remains PARTIAL: expired job claims recover, but no actual worker-process kill/restart proof.
- DLV-020 remains NOT_STARTED: no job reconstruction after an outbox row is already PUBLISHED and its queue job is lost.
- DLV-021 remains PARTIAL: queue depth/oldest pending exist globally, not at every required campaign/shard/sender dimension.
- DLV-022 remains PARTIAL: priority ordering exists but no configurable campaign-priority/fairness/no-starvation proof.
- DLV-023 remains PARTIAL: durable capacity reservations exist, but no reservation-specific audit trail was found.
- DLV-024: forecast refresh responds to changed measured throughput.
- DLV-025: full admission assumptions/decision survive PostgreSQL round-trip.

**CAM — reviewed and evidence source updated.** Key decisions:

- CAM-001: release rejects suspended organisation or rejected consent-review basis with no obligations created.
- CAM-002 remains PARTIAL: name/purpose/window/deadline exist; category and distinct owner requirements are not fully represented.
- CAM-003 remains PARTIAL: lifecycle covers most required states, but campaign `EXPIRED` is absent.
- CAM-005: all four message types are constructible and capability-gated (`SEND_TEXT`, `SEND_IMAGE`, `SEND_VIDEO`, `SEND_DOCUMENT`) at governed dispatch.
- CAM-007: typed variables, fallbacks, masked preview and required-variable failure directly tested.
- CAM-008 remains PARTIAL: preview masks sensitive values but uses caller-supplied values, not representative approved test records.
- CAM-010: immutable version history and differing hashes proven across body/variable/link/media changes.
- CAM-011: release rejects any message version other than the campaign's approved version.
- CAM-015: HTTPS scheme and configured destination-domain allow-list both directly tested.
- CAM-013/014 remain NOT_STARTED: no active STOP-instruction policy or tracked-link implementation found.
- CAM-016/017/018 remain partial/incomplete for timezone/DST closure, organisation/jurisdiction windows and latest-admission semantics.
- CAM-020 has cancellation-impact calculation but not full audience invalidation impact.
- CAM-022 has tags/internal notes but no explicit visibility model/client-report leakage contract for notes.
- CAM-024 has audited archive/restore for terminal campaigns but not retention-policy-driven archive timing.
### MET — REVIEWED and evidence source updated

Important correction: during the live reconciliation discussion, incremental-counter/idempotency findings were briefly associated with the wrong MET IDs. The authoritative requirement source was re-read before any MET evidence write. Do **not** carry that mistaken mapping forward.

Authoritative examples confirmed:

- MET-012 = configurable minimum-cohort privacy threshold.
- MET-017 = organisation-facing campaign report.
- MET-018 = report file outputs (PDF/XLSX/CSV).
- Incremental counters/event replay idempotency belong to other MET rows and must be mapped only after reading their exact `cells` from `srs_requirements.json`.

Fresh MET evidence reconciled into `evidence.json` and verified on a clean disposable database:

- `internal/delivery/metrics_idempotency_postgres_integration_test.go` is GREEN: a QUEUED→SUBMITTING transition moves aggregate counters from queued=1/submitted=0 to queued=0/submitted=1; replaying the identical event leaves metrics unchanged and stores exactly one event.
- `internal/operations/campaign_report_breakdowns_postgres_integration_test.go` is GREEN: three real campaign-recipient/contact rows reconcile to raw state/age/gender breakdowns including UNKNOWN buckets.
- The same report test proves minimum-cohort privacy: threshold 2 leaves Lagos=2 visible and suppresses all one-person cells; raw breakdowns do not escape the privacy boundary.
- The same report test injects campaign internal note, organisation internal note, raw provider-error detail and an MSISDN-like value; none appear in JSON or CSV client report output. This is direct report-leakage evidence relevant to MET-019.
- `internal/operations/report_terminology_test.go` is GREEN and proves submitted and delivered remain distinct in CSV rendering.
- `internal/operations/report_output_totals_test.go` is GREEN and proves the same delivered total survives CSV, valid PDF and valid XLSX rendering.
- Full `internal/operations` + `internal/delivery` package tests are GREEN after recreating `campaign_task4_verify` from migrations 0001→0070.

Material MET gap discovered:

`campaignBreakdownSQL` joins `campaign_recipients` to the **current `contacts` table** for state/LGA/gender/reported_age. Therefore old campaign demographic reports can potentially change if a contact profile is edited later. Do not mark any requirement requiring frozen/as-of campaign demographics fully complete until this is resolved/proven.

Another MET finding: the metric reconciliation worker detects and records aggregate drift and shortens the next reconciliation interval, but its existing unit test explicitly verifies drift is recorded **without overwriting metrics**. Any requirement requiring automatic correction of metric drift is not complete.
### BR — REVIEWED and evidence source updated

BR-001…BR-030 have now been explicitly reconciled from the exact SRS rule text. Current BR result: 15 IMPLEMENTED_TESTED, 11 PARTIAL, 2 NOT_STARTED, 2 BLOCKED_EXTERNAL.

Key direct evidence added during BR reconciliation:

- BR-001: demographic age/gender match with no consent produces zero obligations and NO_ACTIVE_CONSENT in PostgreSQL.
- BR-002: final eligibility independently rejects mismatched organisation, purpose and channel basis and rejects overriding suppression.
- BR-005/006: PostgreSQL rejects audience-snapshot update/delete and approved-message mutation; material message edits create new immutable versions.
- BR-007: an approved commercial entitlement for campaign A cannot be reused by campaign B.
- BR-016: a governed profile update changes profile data while withdrawn consent stays withdrawn with zero active grants.
- BR-019: gateway durability evidence proves raw MSISDN is absent from structured logs and Prometheus output for the static send route.
- BR-014: coordinator start refuses a hard deadline when safe effective capacity is insufficient.
- BR-029: suspended organisations fail closed and quarantined sender sessions contribute zero selectable capacity.

Conservative BR gaps retained:

- BR-004 PARTIAL: duplicate prevention is strong, but no separately approved resend workflow exists.
- BR-005 PARTIAL: snapshot immutability is proven, but the full new-snapshot plus new-approval cycle is not directly proven.
- BR-011 PARTIAL: lease/fence primitives are correct but the production lease acquisition/renewal lifecycle is not wired end-to-end.
- BR-013 PARTIAL: admission uses live healthy safe-capacity values plus headroom, but no evidence proves those safe values are derived from measured historical throughput.
- BR-017 PARTIAL: reported age is retained with source metadata; no derived estimated-age/approximation model exists.
- BR-018 NOT_STARTED: no governed approved-source/policy enforcement model for demographic/interest inference.
- BR-020 PARTIAL: release one is internal-only in the current surface, but no dedicated no-external-portal regression exists.
- BR-021 PARTIAL: governed overrides have reason/effective period/audit/maker-checker, but no explicit high-risk second-approver class.
- BR-022 NOT_STARTED: reports do not state complete metric definitions/formulas/provider limitations.
- BR-024 PARTIAL: obligations are PostgreSQL-backed, but no explicit Redis-loss integration scenario has been executed.
- BR-030 PARTIAL: sender and execution transitions are attributable, but ordinary campaign approval-stage CompareAndSwap changes lack durable actor-attributed status history.
- BR-009 and BR-012 are BLOCKED_EXTERNAL because genuine WhatsApp-account identity and production IP-stability/no-evasion behavior require live deployment evidence.

Fresh BR verification: disposable PostgreSQL was recreated through migrations 0001→0070; orchestration, dispatch, audience/importer, commercial, execution, sender, gateway, delivery, message, platformpolicy and retention packages all passed serially against the corrected environment. The gateway durability suite also passed in an isolated dependency install.

### CON — REVIEWED and evidence source updated

CON-001…CON-022 are **Contact Data Management** requirements. Current CON result: 10 IMPLEMENTED_TESTED, 10 PARTIAL, 2 NOT_STARTED.

Direct evidence added/confirmed:

- Canonical PostgreSQL merge of two approved imports with the same protected E.164 produces one contact, deterministic HMAC identity, encrypted-at-rest MSISDN, two provenance links and two immutable profile-history observations.
- CON-007: real import validation rejects an invalid state/LGA relationship as INVALID_GEOGRAPHY; blank optional state/LGA remains blank rather than being invented.
- CON-009/012: exact self-declared age/date survives merge and a later age update retains the earlier observation and source hash.
- CON-017: lifecycle supports active/inactive/invalid/recycled/deceased and additional terminal/suppressed states; final eligibility rejects any contact whose status is not ACTIVE.
- CON-020: ordinary contact operations use opaque IDs; permission-guarded privacy lookup requires a complete E.164 value and rejects a partial plaintext MSISDN.
- CON-021: PostgreSQL rectification preserves the pre-change profile with privacy-case ID, executor, reason and original source-system/source-record lineage.

Conservative gaps retained:

- CON-001 PARTIAL: canonical dedup is proven, but identity scope is currently one platform-wide HMAC namespace rather than a governed configurable scope model.
- CON-005 PARTIAL: backend/API masking is tested; production frontend/UI acceptance remains deferred.
- CON-006 PARTIAL: decryption is limited to explicit dispatch/test/privacy/export paths in code, but target-deployment least-privilege identities still require environment proof.
- CON-008 PARTIAL: missing state/LGA is supported, but import staging still requires/defaults country ISO2; fully unknown country is not closed.
- CON-010 and CON-013 NOT_STARTED: no estimated-current-age model and no governed configurable age-staleness bands.
- CON-011/016 PARTIAL: source/recorded/verification metadata exists, but there is no general confidence model and segmentation does not expose all confidence/source dimensions.
- CON-014/015 PARTIAL: optional/self-declared demographics and dynamic typed attributes exist, but per-attribute purpose governance and end-to-end governed optional-attribute ingestion remain incomplete.
- CON-019 PARTIAL: approved import dedup/update and rollback exist; there is no explicit canonical-contact-to-contact merge workflow proving preservation of two existing contacts' consent/event lineage.
- CON-022 PARTIAL: high-volume indexes/materialisation exist, but no genuine 10-million-canonical-contact NFR run has been executed as acceptance evidence.

Fresh CON verification: disposable PostgreSQL was recreated through migrations 0001→0070; shared crypto, audience importer, filter/cohort/contact lifecycle, privacy, dispatch and audience materialisation packages all passed with the configured canonical-merge, rectification and final-eligibility PostgreSQL tests.

### AC — REVIEWED and evidence source updated

AC-001…AC-020 are end-to-end acceptance criteria. Current AC result: 4 IMPLEMENTED_TESTED, 15 PARTIAL, 1 BLOCKED_EXTERNAL.

Directly closed: AC-005 final eligibility, AC-006 immutable/versioned audience evidence, AC-008 entitlement atomicity/replay isolation, and AC-017 named security controls (role isolation, MFA/step-up, safe uploads, PII-safe logs/metrics, protected exports).

Key retained gaps: AC-009/011 published-outbox/lost-queue reconstruction; AC-010 production lease acquisition/renewal/handover; AC-012 separate gateway-accepted campaign metric; AC-013 opt-out reporting writer; AC-014/015 full persisted 5m/10m acceptance workloads; AC-018 one composed consent→report audit reconstruction; AC-019 hosted branch protection evidence. AC-016 is BLOCKED_EXTERNAL pending real target deployment plus backup/restore proof.

Fresh AC verification: identity/storage/observability/httpserver/operations security packages passed; a freshly migrated PostgreSQL database passed orchestration entitlement/replay, outbox crash recovery, gateway lease primitives and the composed campaign pipeline.

### CNS — REVIEWED and evidence source updated

CNS-001…CNS-020 are the Consent Management requirements. Current CNS result: 16 IMPLEMENTED_TESTED, 4 PARTIAL.

Directly closed with fresh evidence: separate durable consent grants scoped by organisation/purpose/channel; evidence and effective dates; withdrawal precedence and later re-consent; append-only consent events; scoped/manual suppression; governed STOP handling; minimised HMAC suppression through privacy erasure; consent-aware materialisation/final-send checks; and encrypted privacy export containing consent/suppression evidence.

Retained CNS gaps: CNS-004 lacks first-class governed EXPIRED/REVOKED/SUPERSEDED grant transition workflows; CNS-015 lacks repeated-hard-failure automation into invalid/recycled review/suppression; CNS-018 stores exact release reason codes but lacks an authorised per-contact exclusion-reason display endpoint; CNS-019 supports per-grant expiry and snapshot policy-version evidence but lacks a governed configurable expiry/renewal policy service.

Fresh CNS verification: consent/cohort/materialisation/httpserver packages passed; migrations 0001→0070 replayed cleanly; PostgreSQL consent-ledger acceptance, full privacy package (including erasure and encrypted access export), and both final-eligibility PostgreSQL regressions passed. The privacy legal-hold pagination fixture was made rerun-safe by isolating its fixed synthetic subject; pagination assertions were unchanged.

### APR — REVIEWED and evidence source updated

APR-001…APR-015 are Approval Governance. Current APR result: 8 IMPLEMENTED_TESTED, 6 PARTIAL, 1 NOT_STARTED.

Directly closed: consent approval before audience progression; approved message required before scheduling/dispatch; campaign.approve permission plus four-eyes final approval; material-change invalidation; governed offline quotation/invoice/payment/commercial records; unique-recipient/one-message entitlement; entitlement binding to organisation/campaign/snapshot/message; and atomic entitlement consumption under concurrent PostgreSQL release.

Retained APR gaps: APR-001 has fixed rather than risk/size/category-configurable stages; APR-005 has fixed rather than threshold-configurable maker-checker; APR-006 lacks complete immutable decision evidence for every campaign approval stage; APR-009 has a mandatory rather than effective-dated optional commercial gate; APR-013 lacks a separately approved additional-message/resend entitlement workflow; APR-014 has emergency-stop lifecycle evidence but no alert-generation integration; APR-015 has no dedicated campaign/policy exception workflow with reason, expiry, second approval and report visibility.

Fresh APR verification: focused campaign/commercial/platformpolicy/httpserver acceptance tests passed with -count=1; the disposable PostgreSQL database was rebuilt through migrations 0001→0070; entitlement overflow rollback, approved-message mismatch rejection, concurrent entitlement allocation and organisation/campaign/snapshot/message binding tests all passed.

### SEG — REVIEWED and evidence source updated

SEG-001…SEG-020 are Segmentation requirements. **Authoritative current SEG result: 5 IMPLEMENTED_TESTED, 13 PARTIAL, 2 NOT_STARTED.** This supersedes the earlier live-session handover note that claimed 8/11/1; exact SRS acceptance wording and the current `evidence.json` support 5/13/2.

Directly closed with fresh evidence: SEG-007 consent-scoped demographic matching; SEG-013 saved immutable segment versions; SEG-015 immutable frozen snapshot membership/hash/policy evidence; SEG-017 final live pre-send consent/suppression/cap checks; SEG-018 identity-free snapshot overlap counts.

Retained gaps include: deferred visual UI (SEG-001); no true relative-recency operator (SEG-002); missing first-class interests/source/consent-status dimensions (SEG-003); no estimated-age policy/confidence model (SEG-004/005); no governed relative age-staleness policy (SEG-006); no full acceptance count waterfall (SEG-008/012); complexity ceilings are code constants rather than governed configuration (SEG-009); no human-readable nested-rule summary (SEG-010); no measured estimate NFR (SEG-011); permissions exist but no distinct data-use-policy enforcement layer (SEG-014); snapshot evidence is persisted but no complete dedicated audit-view API (SEG-016); small-cell privacy is report-oriented rather than a general cohort-preview/dashboard boundary (SEG-019); restart-safe materialisation is proven but not at multi-million acceptance scale (SEG-020).

Fresh SEG verification included filter/cohort/segment/materialisation/dispatch/operations/httpserver packages on PostgreSQL. A clean RED→GREEN proof showed migrations through 0070 allowed frozen snapshot-member UPDATE/DELETE/extra INSERT; migration `0071_audience_snapshot_member_immutability.sql` then made the unchanged PostgreSQL mutation test pass.

### IMP — REVIEWED and evidence source updated

IMP-001…IMP-020 are Import Management requirements. Current IMP result: **15 IMPLEMENTED_TESTED, 5 PARTIAL, 0 NOT_STARTED**.

Directly closed/revalidated: CSV/XLSX governed import support; versioned templates; governed mapping definitions; quarantine/signature/malware intake; spreadsheet-injection protection; immutable/replayable import identity; row/geography validation; no country guessing; durable PostgreSQL duplicate staging; governed update/conflict policy; consent non-broadening; masked issue export; safe rollback; approved-consent-to-audience boundary; and retention/deletion evidence.

Retained IMP gaps:
- IMP-006 PARTIAL: processing is streaming/bounded, but no million-row memory-limit acceptance run.
- IMP-011 PARTIAL: insert/update outcomes are known, but no first-class unchanged/no-change count.
- IMP-014 PARTIAL: preview and cancel-before-mutation are proven, but preview does not estimate repository insert/update/no-change effects.
- IMP-015 PARTIAL: bounded PostgreSQL batches, leases and restart/retry architecture exist, but no large-import throughput/failure-recovery acceptance run.
- IMP-017 PARTIAL: persisted reconciliation lacks a distinct unchanged-contact count, so exact row-to-outcome reconciliation is incomplete.

Fresh IMP PostgreSQL evidence on a clean 0001→0071 database proves immutable checksum/uploader/creation evidence, exact request replay, durable duplicate staging, cancel-before-canonical-mutation, protected canonical merge/lineage, profile update without consent resurrection, safe supersession-aware rollback, and rejection of merge when consent review is unapproved. `internal/audience/importer/merge_postgres_integration_test.go` now also proves that rolling back a newer independent import restores the prior profile/source/active-consent state while revoking only the newer import-derived consent.

A new adversarial RED→GREEN regression found and fixed a legacy audience-snapshot governance bypass: `POST /api/v1/campaigns/{id}/audience-snapshots` previously trusted caller-supplied members/evidence hashes and returned 201 even when the governed cohort contained zero eligible contacts. It now derives members server-side through the cohort eligibility service and rejects empty/ineligible/oversize cohorts. The OpenAPI contract was aligned, and route parity now reports **282 implemented `/api/v1` method/path pairs** with no duplicate prefixes. The missing OpenAPI contract for `POST /audience-imports/{id}/cancel` was also added.

Latest generated Task 5 matrix after ADM: **159 IMPLEMENTED_TESTED / 134 PARTIAL / 102 NOT_STARTED / 3 BLOCKED_EXTERNAL = 398**.
### ADM — REVIEWED and evidence source updated

ADM-001…ADM-018 are Administration requirements. Current ADM result: **6 IMPLEMENTED_TESTED, 11 PARTIAL, 1 NOT_STARTED**.

Directly closed with fresh evidence: deterministic PostgreSQL configuration rollback (ADM-004); governed STOP/suppression policy (ADM-008); organisation/purpose/channel frequency caps (ADM-009); versioned sender/session pacing + health settings consumed by worker runtime (ADM-011); append-only PostgreSQL audit persistence (ADM-014); and persisted tamper-evident audit integrity (ADM-017).

Retained ADM gaps:
- ADM-001 PARTIAL: generic catalogue validates canonical JSON/scope/lifecycle but lacks a first-class per-key typed schema/value-type registry.
- ADM-002 PARTIAL: effective-dated versions are real and related campaign evidence freezes several policy/provider versions, but `campaigns.configuration_versions` is not wired into the active campaign model/repository.
- ADM-003 PARTIAL: maker-checker covers common governed policies but retry/reconciliation/UNKNOWN-window policy remains incomplete.
- ADM-005 PARTIAL: consent purposes are data-driven but there is no complete governed purpose/channel/expiry/review-rule admin workflow.
- ADM-006 NOT_STARTED: no governed age-staleness/estimation policy consumed by segmentation.
- ADM-007 PARTIAL: geography validation exists, but reference-data updates/history are not governed end to end.
- ADM-010 PARTIAL: campaign approval has fixed RBAC/four-eyes but no configurable threshold/risk/category-to-role workflow engine.
- ADM-012 PARTIAL: retry/reconciliation/UNKNOWN mechanisms exist but important windows/backoffs remain fixed/deployment settings.
- ADM-013 PARTIAL: privacy thresholds and retention are governed/versioned, but administrator-defined report-template composition is incomplete.
- ADM-015 PARTIAL: full audit fields round-trip when supplied, but all applicable service paths are not comprehensively proven to supply every applicable context field.
- ADM-016 PARTIAL: searchable controlled audit export exists, but no one-test campaign end-to-end audit reconstruction.
- ADM-018 PARTIAL: operational alert framework exists, but no dedicated privileged-config-change or audit-integrity-failure alert signal/test.

Fresh ADM verification on a clean 0001→0071 PostgreSQL database passed `internal/platformpolicy`, `internal/audit`, `internal/sender`, `internal/dispatch`, `internal/consent`, `internal/organisation`, and `internal/operations` sequentially, plus the full HTTP server package. OpenAPI parity remains **282 implemented `/api/v1` method/path pairs** and `git diff --check` is clean.

A new PostgreSQL configuration lifecycle acceptance test proves effective-date resolution, independent approval, supersession and deterministic rollback. A new audit PostgreSQL acceptance test proves complete-field persistence, hash-chain verification and UPDATE/DELETE rejection.
### IAM — REVIEWED and evidence source updated

IAM-001…IAM-016 are Identity & Access requirements. Current IAM result: **7 IMPLEMENTED_TESTED, 8 PARTIAL, 1 NOT_STARTED**.

Directly closed with fresh evidence: provisioned-user-only access (IAM-001); maker-checker campaign release separation (IAM-006); configurable idle/absolute session expiry (IAM-008); immediate account-disable session revocation (IAM-009); last-login + active-session visibility (IAM-012); authorised revoke-all with immediate token invalidation (IAM-013); and configurable network/CIDR admission (IAM-016).

Retained IAM gaps:
- IAM-002 PARTIAL: MFA challenge/verification is real and provisioning/reset default to MFA, but account administration can still set MFARequired=false and such an account can receive a first-factor-only session.
- IAM-003 NOT_STARTED: no real pluggable OIDC/SAML/passwordless identity-provider abstraction/configuration exists.
- IAM-004 PARTIAL: all ordinary application routes use RBAC middleware and targeted tests exist, but there is no exhaustive permission matrix proving the exact required permission for every protected action.
- IAM-005 PARTIAL: PostgreSQL proves multi-role permission union, but /auth/me deliberately omits effective permissions from its JSON response, so the acceptance-required display is incomplete.
- IAM-007 PARTIAL: broad fresh-MFA step-up exists for exports, privacy/legal holds, identity/config administration, final release, execution/destructive actions and import rollback, but there is no dedicated cryptographic-key-change/rotation action with step-up evidence.
- IAM-010 PARTIAL: failed-login detection/lockout is now PostgreSQL-proven, but the threshold/duration remain hard-coded at 5 attempts / 15 minutes rather than governed configurable controls.
- IAM-011 PARTIAL: authentication/MFA/lock/permission-denied events persist with time/context and privileged business actions use the audit ledger, but authentication_events are not exposed through the controlled audit search/export surface.
- IAM-014 PARTIAL: generic human account names are rejected and attributable non-login/service identities exist, but full production evidence that every activity maps to a unique human/service identity remains deployment-dependent.
- IAM-015 PARTIAL: separate least-privilege NOLOGIN database roles exist per service/worker class, but target production credential review proving distinct rotatable LOGIN identities and no shared superuser secret is an infrastructure gate.

Fresh IAM verification: a clean 0001→0071 PostgreSQL database passed internal/identity, internal/persistence/postgres, internal/shared/config and the full HTTP server package. The new persistent identity acceptance proves provisioning, MFA challenge + TOTP, session creation, last-login/session context, multi-role union, unprovisioned-user rejection, revoke-all, failed-login counters/lockout, unlock, re-login and immediate disable/revocation. OpenAPI parity remains **282 implemented /api/v1 method/path pairs**, and git diff --check is clean.

### GWY — REVIEWED and evidence source updated

GWY-001…GWY-018 are Gateway Integration requirements. Current GWY result: **14 IMPLEMENTED_TESTED, 2 PARTIAL, 0 NOT_STARTED, 2 BLOCKED_EXTERNAL**.

Directly closed/revalidated with current-source evidence: the versioned internal provider boundary (GWY-001); durable send idempotency and no-repeat replay (GWY-002); single-owner PostgreSQL lease plus worker authority fencing (GWY-003); complete signed runtime/resource health with governed stale-node handling (GWY-005); cross-node authority rejection plus append-only rejection evidence (GWY-007); bounded per-session in-flight pipeline with direct load proof (GWY-008); graceful drain (GWY-009); signed/replay-protected provider and inbound callbacks (GWY-011); no local campaign-audience persistence (GWY-012); bounded object-reference media handling (GWY-013); sanitised logs/metrics (GWY-014); kill-during-send UNKNOWN recovery with no automatic resend (GWY-015); provider/engine capability/version enforcement (GWY-016); and absence of rapid proxy rotation/evasion behavior (GWY-018).

Retained GWY gaps/external gates:
- GWY-004 BLOCKED_EXTERNAL: persistent auth/session directories exist, but acceptance requires an encrypted production worker volume plus a genuine authenticated WhatsApp restart proving restore without re-pairing where the engine permits.
- GWY-006 PARTIAL: worker session connection/authentication/error state and control-plane heartbeat/last-success evidence exist, but forced gateway disconnect/reconnect state is not yet proven end-to-end in the operations dashboard.
- GWY-010 PARTIAL: provider message identifiers persist into canonical delivery/attempt evidence, but there is no separately persisted redacted raw-provider-status metadata object.
- GWY-017 BLOCKED_EXTERNAL: encrypted/audited per-session proxy governance and worker propagation are proven, but actual production connectivity/egress stability requires deployed Hostinger/network evidence.

Two new RED→GREEN hardening findings were closed during GWY reconciliation. A PostgreSQL authority-conflict test proved stale/conflicting owners were rejected but had no REJECTED event; the dispatch authority path now appends immutable rejection evidence without changing the live owner. A real child-process kill during provider submission proved crash-left idempotency evidence restarted as PENDING; startup recovery now converts prior-process PENDING submissions to UNKNOWN and the same key cannot auto-resubmit.

Fresh GWY verification used a newly recreated campaign_task5_gwy_verify database through migrations 0001→0071 and passed internal/gateway, internal/operations, internal/dispatch, internal/sender and internal/delivery. The current-source gateway production Docker build passed Nest compilation and reported zero npm vulnerabilities. Gateway durability, kill-during-send UNKNOWN, session-authority fencing, per-session pipeline-bound, embedded engine, retained Baileys recovery and container resource-health scripts all passed. OpenAPI remains **282/282 implemented /api/v1 method/path pairs**, git diff --check is clean, affected Go vet passed, and the exact committed-secret scanner passed.

### PRV — REVIEWED and evidence source updated

PRV-001…PRV-010 are Privacy Operations requirements. Current PRV result: **6 IMPLEMENTED_TESTED, 3 PARTIAL, 1 NOT_STARTED, 0 BLOCKED_EXTERNAL**.

Directly closed/revalidated with fresh evidence: protected exact MSISDN lookup resolves a known canonical contact without partial plaintext search (PRV-001); attributed rectification changes current values while preserving source/history (PRV-003); approved objection atomically creates effective global WhatsApp suppression and contact suppression (PRV-004); erasure removes unnecessary PII while preserving minimised suppression and decision evidence (PRV-005); controlled encrypted portability/access export appends an authoritative export-request event and remains permission/MFA gated (PRV-006); and independent review/executor separation protects sensitive disclosure (PRV-008).

Conservative PRV gaps retained:
- PRV-002 PARTIAL: the access package includes current profile/source fields, attribute source evidence, consent, suppression and campaign history, but does not expose the separate contact_sources provenance/history records maintained by imports.
- PRV-007 PARTIAL: case status, due date, assignee/owner and immutable evidence are persisted/indexed, but no backend job or alert policy produces an overdue DSAR alert.
- PRV-009 PARTIAL: legal holds have governed maker-checker lifecycle and fresh PostgreSQL evidence proves scheduled privacy-case deletion excludes an active-held case, but the scheduler silently filters held records and does not append a dedicated skip/hold reason for that deletion decision.
- PRV-010 NOT_STARTED: no first-class privacy-impact/data-inventory report identifies categories, purposes, sources and retention in one governed report.

Fresh PRV acceptance added positive PostgreSQL proof for exact HMAC lookup, immediate objection suppression, controlled portability/export event persistence, and legal-hold exclusion from scheduled retention. A clean campaign_task5_prv_verify database was rebuilt through migrations 0001→0071 and privacy, retention, HTTP, dispatch/final-eligibility and operations packages all passed. Affected Go vet, OpenAPI 282/282, git diff --check and the exact committed-secret scanner also passed.

## Task 5 production defects found and fixed so far

These are real active-code fixes exposed by unchanged PostgreSQL acceptance tests. Do not revert them as “test-only cleanup” without re-running the proving tests.

1. `internal/orchestration/postgres.go`
   - Fixed UUID/text comparison in recent-campaign frequency-cap eligibility: `recent_campaign.purpose_id::text = $3`.
   - Fixed transactional-outbox JSON binding by passing marshalled payload as text instead of `[]byte`/`bytea` to `jsonb`.
2. `internal/jobs/postgres.go`
   - Fixed durable-job JSON payload binding: `string(job.Payload)` instead of `[]byte(job.Payload)` for `jsonb`.
3. `internal/dispatch/postgres_material.go`
   - Fixed text-domain consent/suppression/frequency-cap purpose comparisons against campaign UUID by casting campaign purpose to text.
4. `internal/execution/postgres.go`
   - Fixed `campaign_capacity_assessments.reasons` JSON binding by passing JSON text rather than `[]byte`.
5. `internal/persistence/postgres/campaign_workspace.go`
   - Fixed workspace tag JSON binding by passing JSON text.
   - Fixed archive timestamp parameter inference with explicit `$4::timestamptz`.

6. `internal/audience/importer/merge_postgres.go`
   - Fixed row-returning mutation-capture CTEs that were incorrectly executed with ExecContext under libpq.
   - Fixed contact-source refresh SQL that assumed a non-existent `contact_sources.id`; the table uses the composite key `(contact_id, organisation_id, source_record_hash)`.
   - Updated import-created consent grants to the current governed schema: consent-review/evidence/effective-time/actor/idempotency fields plus a transactional GRANT_CREATED consent event.

7. `internal/segment/postgres.go`
   - Fixed audience-snapshot `segment_definition` JSON binding by passing marshalled JSON as text instead of `[]byte`/`bytea` to `jsonb`.
8. `internal/audience/materialisation/{memory.go,postgres.go}`
   - Fixed materialisation-job definition/eligibility and CommitSnapshot definition JSON bindings by passing JSON text to `jsonb` under libpq.
   - Fixed page checkpoints to return RUNNING jobs to PENDING before clearing lease owner/expiry, matching the worker protocol and reserving RUNNING for actively leased work.
9. `database/migrations/0071_audience_snapshot_member_immutability.sql`
   - Added database-level UPDATE/DELETE rejection for frozen audience-snapshot members and a serialized capacity guard for INSERT, after PostgreSQL acceptance proved 0001→0070 allowed all three post-freeze mutations.

10. `internal/platform/httpserver/server.go` + `internal/platform/httpserver/audience_snapshot_governance_test.go`
   - Legacy audience-snapshot POST previously trusted caller-supplied members and eligibility hashes, allowing a forged snapshot to bypass server-side cohort eligibility.
   - RED proved a forged member produced 201 while the governed cohort contained zero eligible contacts; GREEN now derives membership server-side through `Cohorts.Materialise` and fails closed for empty/ineligible/oversize cohorts.
11. `internal/audit/postgres.go` + `internal/audit/event.go`
   - PostgreSQL audit writes with populated before/after evidence were binding `json.RawMessage` as `[]byte`/`bytea`, causing `jsonb` insertion failure under libpq; fixed by binding JSON text.
   - Persisted hash verification was unstable because PostgreSQL normalizes `jsonb`, `inet` and `timestamptz` representations after the pre-insert hash was calculated. Audit hashing/scanning now canonicalizes JSON semantically, host IP representation and microsecond timestamp precision so legitimate round-trips verify while tampering still fails.
   - `internal/audit/postgres_acceptance_integration_test.go` provides the clean RED→GREEN PostgreSQL evidence and also proves UPDATE/DELETE rejection.
12. `internal/persistence/postgres/identity_admin.go`
   - PostgreSQL account administration used ExecContext for SELECT pg_advisory_xact_lock(...), which libpq rejects as a row-returning statement. Fixed with QueryRowContext(...).Scan(...).
13. `internal/persistence/postgres/identity_admin.go`
   - Optimistic account administration appended FOR UPDATE to the grouped aggregate account query; PostgreSQL rejects FOR UPDATE with GROUP BY. Fixed by locking the base internal_users row first, then reading the aggregate account snapshot in the same transaction.
14. `internal/identity/service.go`
   - Session IDs were base64-derived strings while internal_user_sessions.id is uuid, causing real MFA login to fail at persistent session creation. Session IDs are now deterministic RFC4122-form UUIDs derived from the random token digest without exposing token material.
15. `internal/persistence/postgres/identity.go`
   - Failed-login lockout used an untyped timestamp + interval expression that PostgreSQL resolved incorrectly; RecordFailedLogin failed and Service.recordFailure swallowed the repository error, so counters stayed at zero. The timestamp is now explicitly cast to timestamptz and the persistent IAM acceptance checks the stored count after every bad login plus the threshold lock.

16. `internal/dispatch/postgres_material.go`
   - Stale/conflicting gateway authority attempts were correctly rejected but the authoritative event ledger recorded only successful ISSUED authority writes. A PostgreSQL RED test proved the live owner stayed unchanged while REJECTED count remained zero.
   - The rejection path now appends a safe immutable REJECTED event. Its first GREEN attempt exposed a libpq type-inference error for the rejection timestamp inside `jsonb_build_object`; explicit `timestamptz` casts fixed the real PostgreSQL path.
   - `internal/dispatch/gateway_authority_rejection_postgres_integration_test.go` proves one ISSUED + one REJECTED event and unchanged live ownership.
17. `services/openwa-gateway/src/idempotency.service.ts`
   - A real child-process kill after the durable PENDING write but during provider submission proved restart left the attempt PENDING, which did not satisfy the required uncertain-state classification.
   - Startup recovery now converts PENDING records surviving from a prior process episode to UNKNOWN immediately; ordinary in-process cleanup keeps the existing age threshold. UNKNOWN remains durable and cannot auto-resubmit.
   - `scripts/test-gateway-crash-unknown.js` is the process-kill RED→GREEN proof; `scripts/test-gateway-durability.js` remains green.

These bugs demonstrate why Task 5 must use real PostgreSQL/libpq evidence rather than only memory repositories or code inspection.

## Requirement evidence rows changed from committed HEAD so far

Exactly **278** evidence records differ from committed HEAD at this checkpoint. The diff is confined to reviewed families: **AC=20, ADM=18, APR=15, BR=30, CAM=18, CNS=20, CON=21, DLV=21, GWY=18, IAM=16, IMP=15, MET=24, PRV=10, SEG=20, SND=12**.

Current reviewed-family summaries include:
- MET: 5 IMPLEMENTED_TESTED / 15 PARTIAL / 4 NOT_STARTED
- BR: 15 IMPLEMENTED_TESTED / 11 PARTIAL / 2 NOT_STARTED / 2 BLOCKED_EXTERNAL
- SEG: 5 IMPLEMENTED_TESTED / 13 PARTIAL / 2 NOT_STARTED
- IMP: 15 IMPLEMENTED_TESTED / 5 PARTIAL
- ADM: 6 IMPLEMENTED_TESTED / 11 PARTIAL / 1 NOT_STARTED
- IAM: 7 IMPLEMENTED_TESTED / 8 PARTIAL / 1 NOT_STARTED
- GWY: 14 IMPLEMENTED_TESTED / 2 PARTIAL / 2 BLOCKED_EXTERNAL
- PRV: 6 IMPLEMENTED_TESTED / 3 PARTIAL / 1 NOT_STARTED

Current whole-catalogue totals: **163 IMPLEMENTED_TESTED / 140 PARTIAL / 90 NOT_STARTED / 5 BLOCKED_EXTERNAL = 398**.

## New Task 5 acceptance/regression evidence files currently uncommitted

The following new tests/scripts were added during evidence reconciliation and are intentionally uncommitted until Task 5 verification/commit:

- `internal/audience/importer/geography_validation_test.go`
- `internal/consent/eligibility_acceptance_test.go`
- `internal/persistence/postgres/consent_ledger_acceptance_integration_test.go`
- `internal/privacy/erasure_suppression_postgres_integration_test.go`
- `internal/platform/httpserver/privacy_export_authorization_test.go`
- `internal/campaign/approval_acceptance_test.go`
- `internal/platform/httpserver/campaign_approval_authorization_test.go`
- `internal/dispatch/unapproved_message_postgres_integration_test.go`
- `internal/audience/cohort/postgres_age_consent_integration_test.go`
- `internal/audience/filter/segmentation_complexity_test.go`
- `internal/segment/snapshot_member_immutability_postgres_integration_test.go`
- `internal/audience/materialisation/postgres_commit_snapshot_integration_test.go`
- `internal/audience/materialisation/append_members_state_test.go`


- `internal/audience/importer/merge_postgres_integration_test.go`
- `internal/audience/importer/profile_update_consent_postgres_integration_test.go`
- `internal/privacy/msisdn_search_boundary_test.go`
- `internal/privacy/rectification_postgres_integration_test.go`
- `internal/delivery/attempt_history_postgres_integration_test.go`
- `internal/delivery/metrics_idempotency_postgres_integration_test.go`
- `internal/dispatch/final_eligibility_postgres_integration_test.go`
- `internal/dispatch/governed_route_message_types_test.go`
- `internal/dispatch/pacing_controller_postgres_integration_test.go`
- `internal/execution/admission_postgres_integration_test.go`
- `internal/execution/forecast_refresh_test.go`
- `internal/gateway/postgres_lease_integration_test.go`
- `internal/jobs/postgres_claim_integration_test.go`
- `internal/message/link_policy_test.go`
- `internal/message/material_versioning_test.go`
- `internal/message/supported_types_test.go`
- `internal/operations/campaign_report_breakdowns_postgres_integration_test.go`
- `internal/operations/report_terminology_test.go`
- `internal/operations/report_output_totals_test.go`
- `internal/operations/runtime_health_policy_postgres_integration_test.go`
- `internal/orchestration/large_campaign_bounded_test.go`
- `internal/orchestration/postgres_release_integration_test.go`
- `internal/orchestration/campaign_evidence_immutability_postgres_integration_test.go`
- `internal/outbox/postgres_crash_recovery_integration_test.go`
- `internal/persistence/postgres/campaign_workspace_integration_test.go`
- `internal/platform/httpserver/export_authorization_test.go`
- `internal/platform/httpserver/sender_msisdn_privacy_postgres_integration_test.go`
- `internal/platform/httpserver/sender_pairing_authorization_test.go`
- `internal/sender/allocator_postgres_integration_test.go`
- `scripts/test-session-authority-fencing.js`
- `internal/dispatch/gateway_authority_rejection_postgres_integration_test.go`
- `scripts/test-gateway-crash-unknown.js`
- `scripts/test-session-pipeline-bound.js`
- `internal/privacy/exact_lookup_postgres_integration_test.go`
- `internal/privacy/objection_postgres_integration_test.go`
- `internal/privacy/export_postgres_integration_test.go`
- `internal/retention/privacy_hold_postgres_integration_test.go`

Do not remove these simply because some are characterisation tests; they are the acceptance evidence supporting Task 5 traceability decisions and several production bug fixes.
## Security cleanup / credential rotation

**Current security state supersedes the earlier rotation note:** during the 2026-08-08 IMP verification, a malformed temporary multi-DSN environment file caused libpq to echo the then-current active development PostgreSQL credential into tool-visible output. Treat the **current `openwa0828active` campaign-role credential as compromised**.

The earlier rotation completed on 2026-08-08 is now historical only; a **new manual re-rotation is required**. Attempts to rotate again through the remote command channel were blocked by the remote safety layer before execution, so do not assume a second rotation occurred.

Required action before security-sensitive/production-like validation:
- manually rotate the `campaign` role password in `openwa0828active-postgres-1` to a fresh random value;
- update only the external override `C:\Users\sanus\OpenWA\campaign-platform-active\validation\openwa0828active-db.env` (never print/commit the value);
- force-recreate the active Compose services using env order: shared validation env, active DB override, active gateway identity override;
- re-prove 13/13 container health and a PostgreSQL integration connection after rotation.

The separate `openwa0828smoke` credential also remains potentially exposed from the older incident and should be rotated/recreated before security-sensitive use.

For continued local code evidence until rotation, test DSNs are derived in memory from the running control-api configuration; temporary env files are deleted after each run, and captured failure output is credential-redacted before display. Do not copy any exposed credential into this document or source control.
## Local verification/runtime environment

Authoritative development stack:

- Compose project: `openwa0828active`
- Active service count: 13 containers when fully healthy
- PostgreSQL container: `openwa0828active-postgres-1`
- Docker network: `openwa0828active_private`
- Shared validation env file: `C:\Users\sanus\OpenWA\campaign-platform-active\validation\openwa0828smoke.env`
- Active DB credential override: `C:\Users\sanus\OpenWA\campaign-platform-active\validation\openwa0828active-db.env` (secret; never print/commit)
- Active gateway identity overlay was separated from smoke identity during Task 4; do not collapse the active/smoke gateway IDs back to shared deterministic defaults.

Disposable PostgreSQL verification database:

`campaign_task4_verify`

It is safe to recreate for integration evidence. Do not use the active application database for synthetic append-only governance fixtures.

Task 4 database recreation helper:

`C:\Users\sanus\OpenWA\campaign-platform-active\validation\task4-recreate-db.sh`

Known useful test image/caches:

- Go builder: `campaign-task2-go-builder:latest`
- Alternate Go test image: `openwa0828-go-test`
- Go build cache volume: `openwa-task4-gocache`
- Go module cache volume: `openwa-task4-gomod`

Security rule: never print secret values from env files. For PostgreSQL tests, construct temporary DSN/env material without logging the password and remove temporary env files after the command.
## Exact resume point after this handover update

Resume **Task 5 / API Integration & Data-Minimisation family (API-001…API-008)**.

SEG, IMP, ADM, IAM, GWY and PRV are explicitly reconciled in docs/requirements/evidence.json. Current generated matrix is **163 IMPLEMENTED_TESTED / 140 PARTIAL / 90 NOT_STARTED / 5 BLOCKED_EXTERNAL = 398**. Evidence contamination is exactly **278 records**, confined to reviewed families only.

Immediate next steps:
1. Re-read exact API-001…API-008 requirement and acceptance text plus current evidence.
2. Inspect the delivery-command DTO/signing/decryption boundary, gateway trust/authentication, idempotency and media-reference handling against those exact criteria.
3. Use current GWY/dispatch PostgreSQL and gateway acceptance evidence where it directly satisfies API acceptance; add characterization only where proof is missing.
4. Keep live/deployment evidence explicitly external instead of promoting mocks or code shape.
5. Update only API evidence, regenerate all 398 rows, run contamination/path checks and fresh API verification.
6. Update this handover again at the API checkpoint.

Backend closure order remains: complete Task 5 all families → Task 6 adversarial backend review/fixes → Task 7 backend checkpoint → Railway+Hostinger integration/proof → deferred frontend programme using the designer UI as reference.

## Task 5 completion gate — do not skip

Before any Task 5 completion/commit claim, run fresh verification from the current source and record failures honestly. At minimum:

- regenerate and validate all 398 traceability rows;
- ensure evidence-status changes match exact requirement IDs and acceptance criteria;
- run all newly added acceptance/regression tests;
- run affected PostgreSQL/domain packages against a freshly recreated disposable database;
- run the full Go suite;
- run `go vet ./...` and `go build ./...`;
- run race detection for changed concurrency/lease/delivery paths and broader packages if practical;
- run gateway TypeScript/typecheck/build/tests affected by sender/session authority;
- rerun governance/security tests and committed-secret scan;
- run `git diff --check`;
- inspect the complete diff, including every untracked file, before staging;
- confirm no temporary validation scripts/env files remain;
- confirm worktree scope is Task 5 only before committing;
- commit Task 5 independently only after the above is green or remaining external gates are explicitly separated.

## External/live production gates still open

These are not closed by embedded/unit/PostgreSQL tests and should stay separately identified:

- genuine WhatsApp authentication for WWebJS and Baileys;
- QR/pairing against real accounts;
- genuine text/media sends;
- inbound message/opt-out handling with real provider traffic;
- delivery/ack evidence against real WhatsApp behavior;
- reconnect/watchdog/endurance behavior;
- genuine ambiguous/UNKNOWN outcome evidence;
- target-volume and sustained-load/endurance testing;
- real sender-session RAM/CPU measurements, especially Chromium memory growth;
- independent security/penetration review;
- production hybrid Railway + Hostinger deployment/network validation;
- backup/restore and DR/RPO/RTO exercises;
- monitoring/on-call runbooks and operational-owner approval.
## Current dirty worktree snapshot

The branch remains intentionally dirty because Task 5 is in progress. Do **not** reset/clean/stash without first understanding the candidate changes.

PRV checkpoint candidate changes add PostgreSQL acceptance files internal/privacy/exact_lookup_postgres_integration_test.go, internal/privacy/objection_postgres_integration_test.go, internal/privacy/export_postgres_integration_test.go and internal/retention/privacy_hold_postgres_integration_test.go on top of the existing SEG/IMP/ADM/IAM/GWY Task 5 changes.

Latest evidence contamination check: **278** requirement records differ from HEAD, confined to reviewed families only. Latest OpenAPI parity: **282 implemented /api/v1 method/path pairs**. git diff --check is clean at the PRV checkpoint; the full Task 5 completion gate remains pending.

The latest disposable verification DB is campaign_task5_prv_verify; it is disposable and may be recreated. Never use the active application DB for synthetic append-only fixtures.
## Continuity / memory protocol

This file is the repository-backed memory source for the project. It is deliberately more trustworthy than conversational recollection because it can be versioned with the code and reviewed against Git history.

On every substantial continuation:

1. Read this file first.
2. Run `git status --short`, `git rev-parse HEAD`, and inspect the latest commits.
3. Check whether this document's HEAD/worktree statements still match reality; update them if not.
4. Add new material findings immediately when they change the safe resume point, requirement status, architecture decision, or production defect list.
5. Before committing a task, update this handover with the new commit hash and exact next task.
6. Never store passwords, API keys, TOTP seeds, encryption keys or other secret values here.

There is no tool in the current ChatGPT session that can force-write the product's separate persistent user-memory store. Therefore this repo document is the authoritative durable handover mechanism. If persistent memory is available in a future environment, it should point back to this file rather than duplicate volatile implementation details.


## Latest checkpoint supersession — API complete, resume ORG (2026-08-09 03:33 BST)

This tail section supersedes any older resume/count summary above it until the handover can be compacted safely. Task 5 API reconciliation is complete at the code/evidence layer.

API-001…API-008 result: **7 IMPLEMENTED_TESTED / 1 PARTIAL / 0 NOT_STARTED / 0 BLOCKED_EXTERNAL**. API-008 remains PARTIAL because current `/api/v1` contract/route checks do not compare a candidate v1 schema against the previously supported v1 schema to reject breaking changes automatically.

Fresh API verification used disposable PostgreSQL database `campaign_task5_api_verify` migrated 0001→0071. `internal/dispatch`, `internal/delivery`, `internal/gateway`, full `internal/platform/httpserver`, `internal/shared/config`, and `internal/shared/envfile` passed. Affected Go vet passed. `scripts/test-gateway-durability.js` passed in an isolated current-source Node environment. Production Compose validation passed with 8 services / 25 mounted secrets. OpenAPI remains **282/282** implemented `/api/v1` method/path pairs. The exact committed-secret scanner passed. `git diff --check` passed.

API added direct payload-minimisation evidence in `internal/dispatch/api_data_minimisation_test.go`: the signed gateway delivery command contains only the approved transport/correlation field set, with no consent or demographic payload. Production Compose proves late MSISDN decryption remains in the campaign worker and the OpenWA gateway receives neither the MSISDN encryption key nor a database credential.

Current generated 398-row matrix: **168 IMPLEMENTED_TESTED / 140 PARTIAL / 85 NOT_STARTED / 5 BLOCKED_EXTERNAL = 398**. Evidence contamination from committed HEAD is exactly **286 records**, adding all 8 API rows to the prior 278-record boundary and no unrelated family.

**Resume next at Task 5 / ORG-001…ORG-010 Organisation Management.** Re-read exact SRS text/evidence, inspect persistent organisation lifecycle/approval/status/purpose/consent/import/commercial/authorization behavior, add PostgreSQL characterization only where proof is missing, update only ORG evidence, regenerate 398 rows, run contamination and fresh ORG verification, then append/update this handover again.

Backend sequencing remains authoritative: finish all Task 5 backend families → Task 6 adversarial backend review/fixes → Task 7 backend checkpoint → Railway + Hostinger production integration/proof → frontend programme using the designer UI as reference and the formal UI/UX specification as contract.


## Latest checkpoint supersession — ORG complete, resume CRV (2026-08-09)

This tail section supersedes the earlier API resume marker above it. Task 5 ORG-001…ORG-010 is reconciled as **4 IMPLEMENTED_TESTED / 5 PARTIAL / 1 NOT_STARTED / 0 BLOCKED_EXTERNAL**.

Closed/revalidated: ORG-001 internal organisation records are usable by campaigns without external portal credentials; ORG-005 versioned maker-checker organisation policy blocks prohibited campaign purposes; ORG-006 persists ACTIVE/SUSPENDED/UNDER_REVIEW/CLOSED lifecycle and suspension blocks campaign creation/progression; ORG-007 commercial quote/invoice/payment/entitlement governance remains campaign-specific and maker-checker controlled.

Conservative ORG gaps: ORG-002 is PARTIAL because registration reference and industry exist only as dormant schema columns, not active model/API/repository fields; ORG-003 is NOT_STARTED because there is no multiple/effective-dated internal/external organisation-contact aggregate; ORG-004 is PARTIAL because controller role is captured in consent-review evidence but not governed on the organisation aggregate; ORG-008 is PARTIAL because event history stores actor/reason/version/status transitions but not historical changed profile values; ORG-009 is PARTIAL because normalized legal-name+country duplicates are hard-rejected but there is no configurable-identifier potential-duplicate warning workflow; ORG-010 is PARTIAL because approved retention/branding policy exists but report generation does not consume/report the exact policy version.

New direct PostgreSQL evidence: `internal/persistence/postgres/organisation_lifecycle_postgres_integration_test.go` proves persistent lifecycle/status event attribution, suspended-organisation campaign rejection and normalized duplicate enforcement. `internal/persistence/postgres/organisation_policy_acceptance_postgres_integration_test.go` proves maker-checker policy activation and campaign rejection of a prohibited purpose while an allowed purpose proceeds.

Fresh ORG verification used disposable database `campaign_task5_org_verify` migrated 0001→0071. `internal/organisation`, `internal/campaign`, `internal/commercial`, targeted PostgreSQL organisation-policy/commercial/lifecycle tests and `TestOrganisationLifecycleAPI` passed. A named vet container exited 0 for organisation/campaign/commercial/persistence/http packages. OpenAPI remains **282/282** `/api/v1` method/path pairs; committed-secret scan and `git diff --check` passed.

Current generated matrix: **166 IMPLEMENTED_TESTED / 145 PARTIAL / 82 NOT_STARTED / 5 BLOCKED_EXTERNAL = 398**. Evidence contamination from committed HEAD is exactly **296 records**, equal to the prior 286 plus all 10 ORG rows and no unrelated family.

**Resume next at Task 5 / CRV-001…CRV-014 Consent Review.** Re-read exact SRS criteria, reconcile provenance/wording/controller/evidence/sample/review lifecycle/expiry/re-review against current persistent consent-review implementation, add characterization tests only where existing behavior lacks direct proof, update only CRV evidence, regenerate all 398 rows, verify the contamination boundary and append another handover checkpoint.

## Latest checkpoint supersession — CRV complete, resume EVT (2026-08-09)

This tail section supersedes the earlier ORG resume marker. Task 5 CRV-001…CRV-014 is reconciled as **8 IMPLEMENTED_TESTED / 5 PARTIAL / 1 NOT_STARTED / 0 BLOCKED_EXTERNAL**.

Closed/revalidated: organisation/source/campaign review scope; immutable wording/version; checksum-backed trusted evidence; malware-clean evidence availability; reviewer/decision/date/expiry/restrictions; mandatory rejection/revocation/restriction reasons; sample-review notes with direct-PII rejection; and material-change re-review through a replacement review that supersedes the prior approval and requires fresh independent approval.

Conservative CRV gaps: CRV-002 PARTIAL because there is no distinct URL/form-identifier field and sourceSystem is not mandatory at every scope; CRV-004 PARTIAL because purposeCode/permitted partners are recorded but not fully enforced for organisation/source-scoped campaign reuse; CRV-008 PARTIAL because expiry is enforced dynamically but there is no runtime transition/event to EXPIRED; CRV-012 PARTIAL because no separately approved non-consent lawful-basis alternative exists; CRV-013 PARTIAL because expired approval is blocked but advance-expiry warning/alert is absent; CRV-014 NOT_STARTED because no reason+expiry+second-approval compliance override exists.

CRV candidate hardening includes `database/migrations/0072_consent_review_revision_deferred_fk.sql`, which makes the consent-review supersession self-reference deferrable so the atomic revision transaction can establish both sides without weakening referential integrity, plus `internal/persistence/postgres/consent_review_lifecycle_postgres_integration_test.go` proving persisted provenance, approved-with-restrictions evidence, expiry ineligibility, append-only events, revision lineage/supersession, fresh reapproval and revocation.

Fresh CRV verification used disposable database `campaign_task5_crv_verify` migrated **0001→0072 (72 migrations)**. `TestPostgreSQLConsentReviewLifecyclePersistsGovernanceEvidence` and `internal/consent` passed. `internal/campaign` passed and the PostgreSQL release suite passed, including `TestPostgreSQLReleaseBlocksInvalidOrganisationAndConsentReviewBasis`. Affected vet for consent/campaign/orchestration/persistence/http exited 0. OpenAPI remains **282/282** implemented `/api/v1` method/path pairs; the exact committed-secret scan passed; `git diff --check` passed.

Current generated matrix: **170 IMPLEMENTED_TESTED / 149 PARTIAL / 74 NOT_STARTED / 5 BLOCKED_EXTERNAL = 398**. Evidence contamination from committed HEAD is exactly **310 records**, equal to the prior 296 plus all 14 CRV rows and no unrelated family.

**Resume next at Task 5 / EVT-001…EVT-015 Event Processing & Reconciliation.** Re-read exact SRS criteria and current evidence; reconcile canonical gateway events, deduplication, ordering/out-of-order handling, UNKNOWN/reconciliation, inbound correlation and event retention against the current PostgreSQL/gateway implementation. Add characterization/regression tests only where existing behavior lacks direct proof, update only EVT evidence, regenerate all 398 rows, verify the contamination boundary and append another handover checkpoint.

Backend sequencing remains authoritative: finish remaining Task 5 backend families → Task 6 adversarial backend review/fixes → Task 7 backend checkpoint → Railway + Hostinger production integration/proof → frontend programme using the designer UI as reference and the formal UI/UX specification as contract.

## Latest checkpoint supersession — EVT complete, resume REP (2026-08-09)

This tail section supersedes the earlier CRV resume marker. Task 5 EVT-001…EVT-015 is reconciled as **5 IMPLEMENTED_TESTED / 8 PARTIAL / 2 NOT_STARTED / 0 BLOCKED_EXTERNAL**.

Closed/revalidated: durable provider-event deduplication without double-counting metrics; monotonic out-of-order delivery-state precedence with event history; prevention of late lower-confidence state downgrade; synchronous/idempotent governed inbound opt-out suppression; and encrypted, minimised, permission-separated inbound reply storage/retention.

Conservative EVT gaps: EVT-001 PARTIAL because invalid signatures are rejected but no dedicated rejection alert/audit is emitted; EVT-002 PARTIAL because malformed events are rejected rather than durably quarantined; EVT-005 PARTIAL because there is no separately retained redacted raw provider envelope with a dedicated diagnostic/retention surface; EVT-006 PARTIAL because OpenWA mapping exists but provider-specific reason taxonomy/contract coverage is incomplete; EVT-007 PARTIAL because safe clientReference/provider-ID correlation exists but truly unmatched events return 404 instead of entering a durable unmatched-event queue; EVT-011 PARTIAL because reply classification/escalation persists actor/time/version but has no append-only audit event; EVT-012 PARTIAL because UNKNOWN/reconciliation exceptions are surfaced for manual resolution while the scheduled reconciliation worker is metric-count reconciliation, not provider-status reconciliation; EVT-013 NOT_STARTED because no governed provisional-to-final UNKNOWN reconciliation window exists; EVT-014 PARTIAL because contradiction/highest-ack evidence exists but GATEWAY_ACCEPTED is folded into submitted metrics; EVT-015 NOT_STARTED because no asynchronous bulk provider-status adapter interface exists.

Fresh EVT verification used disposable PostgreSQL database `campaign_task5_evt_verify_0834`, migrated **0001→0072 (72 migrations)**. `internal/delivery`, `internal/inbound`, `internal/gateway`, full `internal/platform/httpserver`, and `internal/operations` passed. Explicit PostgreSQL acceptance passed for delivery-event metric idempotency/replay and complete retry event history; state-machine tests passed for delivered-before-sent and contradictory late failure; signed inbound STOP/non-STOP and reply review tests passed. Affected `go vet` exited 0. The current-source OpenWA gateway production build stage passed. OpenAPI remains **282/282** implemented `/api/v1` method/path pairs; committed-secret scan passed; `git diff --check` passed.

Current generated matrix: **171 IMPLEMENTED_TESTED / 157 PARTIAL / 65 NOT_STARTED / 5 BLOCKED_EXTERNAL = 398**. Evidence contamination from committed HEAD is exactly **325 records**, equal to the prior 310 plus all 15 EVT rows and no unrelated family.

**Resume next at Task 5 / REP-001…REP-012 Repository & Supply-Chain Governance.** Re-read exact SRS criteria; reconcile private-repo provenance, upstream reference controls, branch/fork/code-owner governance, dependency/container pinning, SBOM and security scanning against actual repository/CI state. Keep hosted Git-provider settings external where they cannot be proven from the checkout; add code-side release/security automation only through focused tests. Update only REP evidence, regenerate all 398 rows, verify contamination, run fresh repository/security checks, then update this handover again.

Backend sequencing remains authoritative: complete remaining Task 5 backend/repository families → Task 6 adversarial backend review/fixes → Task 7 backend checkpoint → Railway + Hostinger production integration/proof → frontend programme using the designer UI as reference and the formal UI/UX specification as contract.

## Latest checkpoint supersession — REP complete, reconcile HST/BKP/UX gates next (2026-08-09)

Task 5 REP-001…REP-012 is reconciled conservatively as **0 IMPLEMENTED_TESTED / 8 PARTIAL / 4 NOT_STARTED / 0 BLOCKED_EXTERNAL**. The checkout has meaningful supply-chain controls, but hosted repository enforcement and strict release closure are not yet proven.

REP code-side evidence: root/vendor licence and provenance files are present; UPSTREAM.md records source URL, supplied archive/version/hash/import date and explicitly marks exact upstream commit UNVERIFIED; public OpenWA is documented as read-only/untrusted with manual isolated-review updates and no automatic merge/deployment path. Deployed Node apps have package lockfiles. Production Compose requires digest-pinned image variables. CI runs pull-request/main checks and invokes repository checks including release-security validation.

Fresh normal release-security validation passed: production Compose structure **8 services / 25 secrets**, committed-secret scan, npm dependency audits with no production exceptions, and deterministic CycloneDX generation of **30 components**. Repository governance tests (`tests.governance.test_release_readiness`) passed 3/3 and `git diff --check` passed. The strict SBOM path deliberately failed because the root `package-lock.json` is absent, confirming REP-009/010 cannot be promoted to full closure.

Hosted-control gaps remain explicit: the connected GitHub installation exposes no matching campaign-platform/OpenWA repository, so private visibility/fork restriction/branch protection cannot be evidenced; no CODEOWNERS file exists; no hosted test PR demonstrates code-owner enforcement. CI actions use floating major tags rather than SHA pins. The normal CI path does not enable the strict gitleaks/Trivy/Syft release gate and does not retain/upload its generated SBOM as a release artefact.

Current generated matrix after REP: **170 IMPLEMENTED_TESTED / 163 PARTIAL / 60 NOT_STARTED / 5 BLOCKED_EXTERNAL = 398**. Evidence contamination from committed HEAD is exactly **337 records**, equal to the prior 325 plus all 12 REP rows and no unrelated family.

**Next Task 5 action:** explicitly reconcile the remaining infrastructure/frontend-dependent catalogue families HST-001…010, BKP-001…010 and UX-001…015 against current deployment/config/runbook/UI source. This is evidence classification only: do not perform Railway+Hostinger production integration or start frontend implementation during Task 5. Keep genuine deployment/DR/UI acceptance as external/deferred gates. After those families, run the full Task 5 completion gate and move into Task 6 adversarial backend review/fixes.

Backend sequencing remains authoritative: finish Task 5 evidence reconciliation → Task 6 adversarial backend review/fixes → Task 7 backend checkpoint → Railway + Hostinger production integration/proof → frontend programme.

## Latest checkpoint supersession — all Task 5 families reconciled; run full completion gate (2026-08-09)

The final catalogue sweep reconciled HST-001…010, BKP-001…010 and UX-001…015 without starting the deferred production-infrastructure or frontend programmes.

HST is **0 IMPLEMENTED_TESTED / 6 PARTIAL / 0 NOT_STARTED / 4 BLOCKED_EXTERNAL**. Production Compose is structurally hardened/private, cross-service identities are signed and runtime telemetry now includes CPU, memory, disk, inode, PID/uptime, file-descriptor and network evidence. Real Railway/Hostinger port/firewall/SSH/time-sync/patch/credential/failure-simulation proof remains an infrastructure gate. Fresh production-compose validation passed at 8 services / 25 secrets and sender/operations/HTTP packages passed against PostgreSQL.

BKP is **0 IMPLEMENTED_TESTED / 1 PARTIAL / 0 NOT_STARTED / 9 BLOCKED_EXTERNAL**. Application/infrastructure manifests are version-controlled, but there is intentionally no claim of production PostgreSQL PITR/WAL, independent encrypted backup target, backup identity, object-backup restore, monthly restore drill, replica promotion or immutable/ransomware-resistant backup evidence before the Railway+Hostinger infrastructure phase.

UX is **0 IMPLEMENTED_TESTED / 12 PARTIAL / 3 NOT_STARTED / 0 BLOCKED_EXTERNAL**. The existing Next.js admin shell has responsive structure, skip navigation, focus styles, semantic controls/tables and early organisation/consent/import/audience/campaign pages; a fresh Next.js production build succeeded for 8 generated routes. It is still an early implementation, not the formal UI/UX programme: live audience waterfall, age-staleness warnings, sender page, saved views, comprehensive confirmations/version evidence/operational remediation, accessibility audit and browser-cache acceptance remain open. Former UX-013/015 greens were deliberately downgraded because backend export/no-cache controls do not by themselves prove the production frontend workflow/browser behaviour.

Current generated matrix after all 23 families are explicitly reconciled: **168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL = 398**. Evidence contamination from committed HEAD is exactly **372 records**, equal to the prior 337 plus all 35 HST/BKP/UX rows and no unrelated family. `git diff --check` is clean at this checkpoint.

**Immediate next action is the Task 5 full completion gate, not Task 6 yet:** regenerate/validate 398 rows; run all new acceptance/regression tests; fresh full PostgreSQL/domain suite; full `go test ./...`, `go vet ./...`, `go build ./...`, and race coverage where practical; current-source gateway build and acceptance scripts; governance/security/secret/OpenAPI checks; inspect the complete tracked + untracked diff and remove only genuinely temporary artifacts. Do not commit or claim Task 5 complete until this gate is fresh and green or a remaining failure is explicitly diagnosed/externalised.

After a green Task 5 gate: update this handover again, create the independent Task 5 commit/checkpoint, then begin Task 6 adversarial backend release-readiness review/fixes. Production Railway+Hostinger integration remains after Task 7; frontend remains after infrastructure proof.

## Final Task 5 completion checkpoint — code/evidence reconciliation complete (2026-08-09)

This section supersedes all earlier Task 5 resume markers. **Task 5 — reconcile all 398 SRS requirements — is complete at the code/evidence layer and is ready for its independent checkpoint commit.** This does not mean production release readiness; the external/live/infrastructure/frontend gates below remain open.

Final authoritative requirement distribution: **168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL = 398**.

Evidence-source integrity was checked immediately before commit: `srs_requirements.json` contains 398 IDs and `evidence.json` now contains exactly the same 398 explicit IDs, with **0 missing and 0 extra**. Seven reviewed NOT_STARTED requirements that had previously relied on generator defaults (`CAM-013`, `CAM-014`, `SND-005`, `SND-017`, `SND-022`, `DLV-018`, `DLV-020`) were made explicit without changing totals. Four obsolete evidence keys not present in the authoritative SRS (`NFR-D-005`, `NFR-D-007`, `NFR-R-004`, `NFR-R-008`) were removed. Relative to pre-Task-5 HEAD, 379 current-SRS evidence rows differ plus four obsolete-key deletions = 383 evidence-key changes.

Final PostgreSQL verification used fresh disposable database `campaign_task5_final_1227`, migrated cleanly **0001→0072 (72 migrations)**. All 31 PostgreSQL opt-in test DSNs were pointed at that database and `go test -p 1 -count=1 ./...` exited 0, including all internal packages, `tests/integration`, and `tests/schema`.

Fresh static/concurrency verification from the same current source: `go vet ./...` + `go build ./...` exited 0; full `go test -race -p 1 -count=1 ./...` exited 0. The adversarial PostgreSQL suite now reaches and passes all six intended concurrency scenarios after fixing its test setup to execute multi-statement DDL/DML one statement at a time under libpq.

Fresh gateway verification: explicit `npm run typecheck` exited 0 after syncing 56 pinned retained OpenWA source files; production `openwa-gateway` Docker build exited 0; durability, process-kill→UNKNOWN recovery, session-authority fencing, per-session pipeline bound (max observed in-flight 3), embedded OpenWA engine, and compiled container resource-health collector tests all passed.

Fresh repository/security verification: traceability generator produced 398 rows with the final counts above; OpenAPI covers **282/282** implemented `/api/v1` method/path pairs; production Compose validation reports 8 services / 25 secrets; committed-secret scan passed; Node security audit passed with no production exceptions; CycloneDX generation produced 30 components; normal release-security gate passed; governance structure remains valid with its eight production hard gates correctly open/external rather than falsely closed; staged `git diff --check` is clean.

Complete candidate diff review included all formerly untracked migrations/tests/scripts. No temp env files, binaries, build outputs, or scratch files remain in the Git candidate. New PostgreSQL tests are opt-in only when their named DSN is absent, but the final full run explicitly supplied all 31 DSNs so those tests were not accepted on skip-only evidence.

Production/live gates remain deliberately separate: genuine WWebJS/Baileys authentication/pairing/sends/inbound/acks/reconnect/ambiguous outcomes; sustained target-volume/endurance and real sender-session resource measurements; independent penetration/security assurance; Railway+Hostinger deployment/network proof; backup/restore/DR/RPO/RTO; monitoring/on-call/operational approval; and the deferred production frontend/UAT/accessibility programme.

Security reminder: the active development PostgreSQL credential previously exposed in tool-visible output must still be treated as compromised until a fresh manual re-rotation and post-rotation health/integration proof are observed. Do not infer that rotation from these Task 5 tests.

**Next engineering task after the Task 5 checkpoint commit: Task 6 — adversarial backend release-readiness review and fixes.** Do not start Railway+Hostinger integration or frontend implementation until Task 6 and Task 7 backend closure are complete.

## Post-commit Task 5 record

Task 5 checkpoint commit: `1ad6376d9850f7c603ce2a0269fe6452bfb41fcc` — `feat: complete Task 5 requirements reconciliation`.

The Task 5 commit completed with a clean worktree. The next engineering activity is **Task 6 — adversarial backend release-readiness review and fixes**. Any Task 6 findings/fixes must be kept separate from the Task 5 checkpoint and must follow the same evidence-before-assertion/TDD/debugging rules.

## Task 6 adversarial backend review — in progress (2026-08-09)

Task 6 began from clean post-Task-5 HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77`. The review is intentionally defect-seeking and uses focused PostgreSQL RED→GREEN acceptance tests before production edits.

Confirmed/fixed PostgreSQL defects so far:
- reporting-privacy activation used `ExecContext` for row-returning `pg_advisory_xact_lock`; fixed with `QueryRowContext(...).Scan(...)` and direct activation regression evidence;
- provider-capability activation had the same advisory-lock/libpq defect; fixed and directly proven;
- saved segment definition create/update/version-history failed on raw `[]byte` JSONB bindings; fixed by text binding;
- the segment update also reused one prepared parameter for integer `definition_version` and bigint optimistic `version`; parameters are now separate and create→read→update→immutable-history is green;
- audience-import reconciliation evidence JSONB used raw bytes; fixed with durable readback, exact replay and conflicting-evidence rejection proven;
- campaign metric-reconciliation canonical/stored JSONB used raw bytes; fixed with lease release and persisted evidence proven;
- governed test-message `variable_values` JSONB used raw bytes; fixed with durable readback and idempotent replay proven;
- message-version destination links/template variables used raw bytes; direct PostgreSQL draft/read/idempotent-replay test reproduced RED and is now green after text binding.

Current Task 6 worktree is intentionally dirty only with these production fixes and their focused PostgreSQL regression tests. No Task 6 commit has been created yet.

Immediate next action: continue the adversarial persistence audit across campaign/consent/retention/release-critical JSONB and transaction boundaries, then expand into security/RBAC/idempotency/fencing/cancellation/recovery review. Update this handover again at the next material Task 6 checkpoint before any commit.

## Task 6 checkpoint — Docker resource audit and campaign completion flood fix (2026-08-09)

OpenWA-only Docker audit was performed read-only before any cleanup. No non-OpenWA Docker resource was modified, and no pre-existing OpenWA container/image/volume/database was deleted.

Resource evidence at audit time:
- `openwa0828active`: authoritative development stack; 13 services when fully healthy; about 1.84 GiB resident RAM in the sampled run.
- `openwa0828smoke`: older validation/smoke stack from the 0.8.28 validation tree; about 1.42 GiB resident RAM. It is not the authoritative Task 6 runtime and does not need to remain running outside smoke validation.
- Active ClamAV is required: control-api points `CLAMAV_ADDRESS` to `clamav:3310` for import malware scanning. Do not remove the active ClamAV merely because the smoke stack has another instance.
- Exited OpenWA Task 5 verification containers hold about 4.86 GiB of writable layers and are runtime-disposable after evidence/log requirements are satisfied.
- Docker build cache reported 25.1 GiB total / 388 entries / 0 active at the initial sample, but only 13.96 GiB was marked reclaimable. Global builder pruning is not safe under the OpenWA-only cleanup constraint because the Docker host is shared with other projects.
- OpenWA named volumes are comparatively small except active PostgreSQL (~1.20 GiB) and reusable Go caches. Task 4/5/6 disposable databases are mostly ~16–19 MiB each and are not the source of the large footprint.
- The active `campaign` database was ~703 MiB. `campaign_execution_events` alone was ~684 MiB (437 MiB heap + 247 MiB indexes).
- The historical 1M-MSISDN test data is not resident in contacts/recipient tables; those are tiny. The large table contained 2,288,187 duplicate `COMPLETION_ASSESSMENT_FAILED` events plus six terminal `READY` completion events.

A live scheduler defect was discovered during the Docker audit. The active campaign worker was appending ~12.6 duplicate completion-failure events/second across six synthetic dispatching campaigns.

T6-011 root cause/fix:
- `internal/execution/postgres.go::Metrics` was anchored on `campaign_metrics`, so an existing campaign without a metrics row returned `sql.ErrNoRows`.
- Focused PostgreSQL regression `internal/execution/postgres_metrics_integration_test.go` reproduced RED, then GREEN after anchoring the query on `campaigns` with an optional metrics left join.
- Full `internal/execution` package passed against fresh 72-migration DB `campaign_task6_metrics_2214a`.
T6-012 root cause/fix:
- After T6-011, the same legacy fixtures exposed a second defect: campaign reads coalesced nullable transport fields to empty Go strings, while CAS/material-update SQL wrote those strings back without `NULLIF`, violating `campaigns_transport_engine_check` on status-only transitions.
- `TestPostgreSQLCampaignCompareAndSwapPreservesNullTransportFields` reproduced RED with SQLSTATE 23514, then passed unchanged after nullable update bindings were corrected.
- Existing Task 6 campaign transport/material-evidence regression also remained GREEN.

Live proof:
- campaign worker was temporarily stopped as reversible containment while fixes were built; no data was removed.
- corrected `openwa0828active-campaign-worker` image was rebuilt/recreated with documented env order: shared validation env -> active DB override -> active identity override.
- worker returned healthy.
- six previously looping synthetic campaigns transitioned from `DISPATCHING` to `COMPLETED`, version 2.
- `campaign_execution_events` count remained exactly flat over a 20-second post-fix sample (`2,288,193 -> 2,288,193`).

The duplicate event rows have NOT been deleted or vacuumed. Any cleanup of those 2.288m duplicate test/failure events should be a separate deliberate operation after preserving the evidence needed for Task 6/Task 7.

## OpenWA Docker cleanup checkpoint — 2026-08-10

User-authorised cleanup was restricted to OpenWA resources only. No other Docker project was touched.

- Stopped all 13 running `openwa0828smoke-*` services; the smoke containers were preserved rather than removed.
- Preserved all six `openwa0828smoke_*` named volumes, including PostgreSQL, Redis, ClamAV, gateway/session and object-data state.
- Removed 20 exited OpenWA verification containers only: Task 5 verification artifacts, three temporary Task 6 regression containers, and `openwa-crv-vet`.
- Docker container disk usage fell from about 5.48 GB to 260.8 MB, recovering about 5.2 GB of container writable-layer disk.
- No global Docker image/build-cache/volume prune was run. Build cache remains intentionally available for Task 6/7 work.
- `openwa0828active` remains 13/13 healthy after cleanup.
- Post-cleanup active service memory sample total is about 1.57 GiB; active ClamAV and PostgreSQL remain required/retained.
- Campaign execution-event flood remains closed: count stayed 2,288,193 -> 2,288,193 over a fresh 15-second sample.
- The 2.288M historical duplicate execution events were not deleted or vacuumed; preserve until evidence/cleanup decision is explicitly made.

## Task 6 checkpoint — sender heartbeat authority + signed machine-auth work (2026-08-10)

Current branch/HEAD remain `work/backend-production-engineering` / `786451d5616f5e179903fd9b61c5e26f6de76f77`. Task 6 remains intentionally dirty/uncommitted; do not reset or clean the worktree.

T6-013 sender heartbeat trust/authority remains OPEN, but the store-level authority portion is now proven/fixed:
- `internal/sender/heartbeat_integrity_test.go` first reproduced heartbeat overwriting governed capacity (`25/500/2` -> `999/9999/99`).
- `internal/sender/memory_governance.go` now preserves configured `SafeMessagesPerMinute`, `SafeDailyCapacity` and `InFlightLimit`.
- same-day `SentToday` can only advance; a focused RED proved naive monotonic logic would never reset, then UTC-day rollover semantics were added and GREEN.
- memory heartbeat now uses canonical `AllowedSessionTransition`, closing a RED where `READY` could incorrectly jump back to `PAIRING`.
- `internal/sender/heartbeat_postgres_integration_test.go` reproduced the same governed-capacity overwrite on fresh PostgreSQL DB `campaign_task6_heartbeat_0151` and is now GREEN with equivalent same-day/day-rollover semantics.
- focused sender regressions remain GREEN for governed capacity/conflict, quarantine cannot be bypassed by heartbeat, memory heartbeat integrity, and PostgreSQL heartbeat integrity.

Signed machine-auth portion is in progress:
- legacy design issue originated in this repository baseline; do NOT attribute it to ECC. This is our own implementation using the MIT OpenWA project as reference/transport foundation.
- intended node direction remains to retire duplicate human-authenticated `POST /api/v1/internal/sender-nodes/{id}/heartbeat`; signed `/api/v1/internal/gateway-nodes/{id}/runtime` is the authoritative node runtime path.
- `internal/sender/session_heartbeat_test.go` was written first and RED-proven because no signed session-heartbeat service existed.
- `internal/sender/session_heartbeat.go` is now implemented using the existing runtime HMAC current/previous-secret verification, timestamp skew, durable runtime nonce/replay store, strict JSON, path/body session binding, node/session ownership binding and stale/out-of-order telemetry rejection.
- a test-file syntax typo introduced during a PowerShell replacement was corrected. The signed-service GREEN rerun has NOT yet been observed; Desktop Commander disconnected immediately before that rerun, so do not claim this service verified yet.

Exact next action: rerun `TestSessionHeartbeatRequiresSignedNodeBoundReplaySafeTelemetry` unchanged. Only after GREEN should HTTP routing be changed: remove the legacy node heartbeat route, switch session heartbeat from human `sender.operate` auth to the signed machine handler, add HTTP regressions, update OpenAPI, then continue Task 6 review.

## Task 6 checkpoint — T6-013 heartbeat trust boundary closed (2026-08-10)

Task 6 remains in progress on branch `work/backend-production-engineering` at uncommitted HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77`. Do not reset, clean or split the intentionally dirty Task 6 worktree.

T6-013 is now FIXED at the code boundary:
- memory and PostgreSQL heartbeat mutations preserve governed `SafeMessagesPerMinute`, `SafeDailyCapacity` and `InFlightLimit`;
- same-UTC-day `SentToday` is monotonic and a new UTC day permits the new observed value;
- memory heartbeat uses the canonical session transition guard, matching PostgreSQL behavior;
- the obsolete human-authenticated `POST /api/v1/internal/sender-nodes/{id}/heartbeat` route and OpenAPI operation are removed;
- signed `POST /api/v1/internal/sender-sessions/{id}/heartbeat` is outside human bearer middleware and requires the existing gateway runtime HMAC timestamp, nonce and signature headers;
- the signed service enforces exact-body verification, strict JSON, path/body session identity, governed node/session ownership, durable nonce replay protection and stale/out-of-order rejection;
- the control-api memory and PostgreSQL runtime constructors both wire `SessionHeartbeatService` using the existing runtime current/previous secrets, skew and nonce store; no second service secret was introduced.

Fresh RED/GREEN evidence:
- the legacy node-route HTTP regression failed with the route still exposed (503 through identity middleware), then passed with 404 after removal;
- the signed service regression initially exposed a time-dependent test fixture: its fixed timestamp had fallen behind the memory nonce store's wall clock, so the nonce expired before replay. The fixture now uses current UTC time without changing any security expectation;
- `TestSessionHeartbeatRequiresSignedNodeBoundReplaySafeTelemetry` is GREEN for signed acceptance, governed-capacity preservation, nonce replay rejection, node/session mismatch, stale telemetry, stale signatures, unsigned rejection and path/body mismatch;
- HTTP regression is GREEN for ordinary bearer-without-machine-signature rejection, valid signed acceptance and nonce replay conflict;
- PostgreSQL governed-capacity/usage/day-rollover regression is GREEN against `campaign_task6_heartbeat_0151`.

Fresh verification:
- full `internal/sender`, `internal/platform/httpserver` and `cmd/control-api` tests passed;
- affected `go vet` and control-api build exited 0;
- race tests passed for sender and HTTP server (`internal/platform/httpserver` completed in 145.853s);
- OpenAPI YAML parsed and route parity now reports 281/281 implemented `/api/v1` pairs, reduced from 282 solely because the obsolete node heartbeat was removed;
- candidate-worktree high-confidence secret scan passed;
- `git diff --check` passed before this documentation update.

The active gateway source currently emits the signed aggregate gateway-node runtime report but no per-session heartbeat emitter was found. This does not reopen the removed authentication bypass; it remains an explicit runtime-integration point to assess during the remaining gateway/control-plane failure review and later Railway/Hostinger proof.

Exact next action: continue Task 6 adversarial review after T6-013. Re-run `git diff --check` and inspect the complete tracked/untracked Task 6 candidate, then audit the next release-critical authorization/idempotency/lease-recovery boundary. Do not start Task 7, Railway/Hostinger deployment or frontend work yet.

## Task 6 checkpoint — gateway fencing and outbox integrity (2026-08-10)

Task 6 remains in progress on branch `work/backend-production-engineering` at uncommitted HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77`. The worktree remains intentionally dirty with the cumulative Task 6 fixes and direct regressions; do not reset, clean, stash or split it casually.

T6-014 is FIXED at the gateway session-authority boundary:
- `SessionAuthorityService.validate()` performed an unsynchronised read→validate→write sequence. Two concurrently accepted authorities for the same session could replace the durable record out of order, leaving lease fence 2 after lease fence 3 had already been accepted.
- `scripts/test-gateway-session-authority.js` deterministically delayed the lease-2 replace until after lease 3. RED retained fence 2 instead of 3.
- A per-session promise queue now serialises validation and durable replacement while preserving concurrency between different sessions. The unchanged regression is GREEN and retains the highest accepted fence.

T6-015 is FIXED at both durable gateway event outboxes:
- provider-event and inbound-message outboxes wrote a temporary record then used POSIX `rename(temp, final)`, expecting `EEXIST` to protect an existing event identifier. On Linux, rename replaces the destination, so conflicting evidence could silently overwrite the original durable record.
- `scripts/test-gateway-outbox-integrity.js` reproduced RED for both outboxes: same event ID plus different evidence did not reject.
- Both outboxes now create the final record exclusively with `open(..., 'wx', 0o600)`, sync file and directory durability, accept byte-identical idempotent replay, reject conflicting evidence, and retain the original record. Both regressions are GREEN.

Fresh verification after T6-014/T6-015:
- combined focused Node run passed all three new subtests;
- complete first-party Go `go test -count=1 ./...` passed across all packages;
- isolated current-source gateway verification synced all 56 retained OpenWA files, passed strict `tsc --noEmit`, passed emitted build with `tsc --incremental false`, passed the embedded engine test, and passed all three new gateway regressions;
- the production gateway image intentionally prunes developer type packages, so the verifier supplied only the two exact declared type packages (`@types/express@5.0.3` and `@types/qrcode@1.5.6`) in its disposable filesystem. The earlier type errors without them were an environment artefact, not a source fix;
- `git diff --check` was clean before this documentation update.

Exploratory hypotheses that were disproved were not converted into production changes: alerting and retention JSONB already use text bindings; the privacy raw-message path passed its existing PostgreSQL rectification test; and the proposed campaign lease race is prevented by `FOR UPDATE SKIP LOCKED`. Its temporary exploratory test/database were removed.

Exact next action: remove only the exited disposable `openwa-task6-gateway-verify` container after its successful logs have been preserved, rerun `git diff --check`, then continue the remaining Task 6 transaction/RBAC/idempotency/fencing/cancellation/retention/audit/configuration/gateway failure-mode review. Task 7, infrastructure deployment and frontend work have not started.

## Task 6 checkpoint — campaign execution trust boundary (2026-08-10)

T6-016 is FIXED. The authenticated generic `POST /api/v1/campaigns/{id}/transition` route accepted `START_DISPATCH`, `PAUSE`, `RESUME`, `CANCEL` and `COMPLETE` and called `Campaigns.Transition` directly. That bypassed the dedicated execution coordinator responsible for admission/capacity assessment, maintenance and dispatch-window enforcement, routing-reservation activation/release, completion reconciliation and execution-event recording.

Focused HTTP RED proved a generic `CANCEL` returned 200, changed a DRAFT campaign to CANCELLED and incremented its version without passing through `Execution.Cancel`. The minimal route fix rejects all five execution lifecycle actions with `CAMPAIGN_EXECUTION_ROUTE_REQUIRED` before campaign mutation; the unchanged regression is GREEN and proves status/version remain unchanged.

The OpenAPI `CampaignTransition` schema was independently RED-proven to advertise the same bypass and an invalid request shape: it required client-supplied `actorId` even though strict JSON decoding treats actor identity as server-derived, omitted required `expectedVersion`, and exposed server-derived/execution-only fields. The contract now lists approval/build actions only, requires `expectedVersion`, exposes only accepted client fields, documents the governed execution endpoint and declares the 409 boundary response.

Fresh verification: both focused trust-boundary tests passed, the complete `internal/platform/httpserver` package passed in 18.283s, the disposable verifier exited 0 and was removed, and `git diff --check` passed before this documentation update. Task 6 remains in progress and uncommitted.

Exact next action: continue the separate campaign reservation/state atomicity and recovery audit, then remaining RBAC/idempotency/fencing/UNKNOWN/cancellation/retention/audit/configuration/gateway failure modes. Do not begin Task 7, deployment or frontend work.

## Task 6 checkpoint — safe gateway session teardown (2026-08-10)

T6-017 is FIXED. `GatewayMessagingService.stopSession`, `logoutSession` and `deleteSession` previously called synchronous `SessionPipelineService.drain()` and immediately invoked provider teardown. Drain rejected queued work but did not wait for active provider sends, so a lifecycle command could interrupt a submission already admitted to the per-session pipeline and create an avoidable ambiguous/UNKNOWN outcome.

A deterministic Node regression held one admitted send open and exercised stop, logout and delete independently. RED proved all three provider teardown calls occurred while the send was still active. The pipeline now has a teardown-specific drain barrier: it marks the session draining, rejects queued work, prevents resume/concurrent teardown, waits for active count to reach zero, then permits the provider lifecycle call. Teardown state is cleared in `finally`; the session remains drained until explicitly resumed. All three unchanged regressions are GREEN.

Fresh gateway verification copied current source into an isolated disposable production-image filesystem, supplied only the two exact declared type packages absent from the pruned runtime image, and passed strict `tsc --noEmit`. The consolidated gateway safety run passed six subtests covering both outbox evidence boundaries, concurrent lease-fence preservation and all three teardown operations; the embedded OpenWA engine test also passed. The verifier exited 0, was removed, and `git diff --check` passed before this documentation update.

Task 6 remains in progress and uncommitted. Exact next action: continue campaign reservation/state recovery plus remaining RBAC/idempotency/fencing/replay/UNKNOWN/retention/audit/configuration/gateway review, then run the final full verification matrix.

## Task 6 checkpoint — failed-start capacity recovery (2026-08-10)

T6-018 is FIXED. `Coordinator.Start` activates governed multi-pool reservations before attempting the optimistic campaign transition to DISPATCHING. If that transition failed while the campaign remained non-dispatching, the baseline returned immediately and left all reservations ACTIVE indefinitely, reducing available capacity and potentially blocking unrelated campaigns.

A focused regression used the real memory routing administration with governed provider/gateway evidence and two reservations. RED proved a simulated failed campaign transition left the first reservation ACTIVE. Start now records the activated plan, re-reads durable campaign state after a transition error, and releases reservations only when the campaign is not DISPATCHING. If the durable state cannot be read, it fails closed without an unsafe release and joins the recovery error; a previously/redundantly released plan is treated idempotently.

A second concurrency guard simulates another start winning the campaign CAS. It proves the losing request observes DISPATCHING and retains the winner's ACTIVE reservations rather than compensating them away. Both focused tests and the complete `internal/execution` package passed; the disposable verifier exited 0 and was removed. `git diff --check` passed before this documentation update.

Task 6 remains in progress and uncommitted. Exact next action: continue event-recording/recovery atomicity, RBAC/idempotency/fencing/replay/UNKNOWN/retention/audit/configuration and gateway/control-plane failure review before the final full verification matrix.

## Task 6 checkpoint — active reservation release guard + open atomicity finding (2026-08-10)

T6-019 is FIXED. The step-up-protected operator routing-plan release endpoint still allowed ACTIVE reservations to be released while the owning campaign was DISPATCHING or PAUSED/resumable. That could make committed capacity appear available for a second campaign while the first continued or resumed sending.

Service-level RED proved a DISPATCHING campaign's ACTIVE reservation was released without error. `RoutingAdministration.Release` now resolves the plan/campaign and rejects DISPATCHING or PAUSED with `ErrRoutingPlanConflict`; HTTP maps this to 409 `ROUTING_RESERVATIONS_IN_USE`. PostgreSQL repeats the same status predicate inside the reservation `UPDATE`, closing the service-check/update race, and treats already terminally released reservations as idempotent success.

Fresh PostgreSQL proof used disposable database `campaign_task6_release_0958`, migrated cleanly through all 72 migrations. The regression proved: DISPATCHING release returns the conflict and preserves ACTIVE/fence 2; after the campaign becomes CANCELLED release succeeds; exact retry succeeds without moving fence beyond 3. The focused test, complete execution package and complete HTTP-server package passed. The database and both verifier containers were removed; `git diff --check` passed before this documentation update.

T6-020 is OPEN and must not be hidden: campaign status CAS, routing release and execution-event insertion are separate transactions. A status transition may commit while event evidence or terminal release fails, after which ordinary action retry is rejected by the new state/version. No existing transactional execution outbox or recovery reconciler closes this. This requires a deliberate transactional orchestration design rather than superficial retry loops.

Task 6 remains in progress and uncommitted. Exact next action: design/implement T6-020 safely or explicitly carry it as a release blocker, while continuing RBAC/idempotency/fencing/replay/UNKNOWN/retention/audit/configuration and gateway/control-plane review before the final verification matrix.

## Task 6 checkpoint - T6-020 campaign lifecycle atomicity (2026-08-10)

Task 6 remains in progress on branch `work/backend-production-engineering` at uncommitted HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77`. The worktree remains intentionally dirty with cumulative Task 6 changes. Do not reset, clean, stash, split, commit or push it without deliberate review and separate authorization.

T6-020 is FIXED. Start, pause, resume, cancel and complete no longer persist campaign state, mutate routing reservations and write lifecycle evidence through separate transactions.

Implemented boundary:
- `campaign.Service.PrepareTransition` reuses all existing validation/domain transition logic and returns the candidate without persistence; the original `Transition` path remains for non-execution approval/build actions.
- `execution.Coordinator` now builds one validated `LifecycleCommit` and fails closed when no atomic committer is configured.
- `execution.PostgreSQLLifecycleCommitter` applies campaign optimistic CAS, required reservation activation/release and version-linked lifecycle event insertion in one serializable PostgreSQL transaction.
- Retries are bounded and restricted to PostgreSQL serialization failure `40001` and deadlock `40P01`; domain conflicts and constraints are not retried.
- External gateway/network calls remain outside the database transaction.
Migration `0073_campaign_execution_lifecycle_atomicity.sql` adds nullable `campaign_execution_events.campaign_version`, a positive-version constraint and a partial unique index on `(campaign_id,campaign_version)`. Lifecycle evidence records the resulting campaign version. Existing runner diagnostic events retain NULL version compatibility.

Reservation transaction helpers lock and verify plan ownership/state. HELD activation and ACTIVE/HELD terminal release advance the fence only when state changes. All-ACTIVE activation and all-RELEASED release are idempotent without another fence increment. Mixed, expired, missing or wrong-campaign reservation sets fail closed. The T6-019 DISPATCHING/PAUSED operator-release guard remains intact.

Both production processes now inject the same PostgreSQL atomic committer:
- `cmd/control-api/runtime.go`
- `cmd/campaign-worker/main.go`

The control API schema readiness query now requires the migration-0073 column and unique index. Development/test memory runtime remains intentionally non-atomic and therefore execution actions fail closed unless a committer is explicitly supplied.

A planned committer placement under `internal/persistence/postgres` created a real Go import cycle because execution lease code already imports that package. The implementation was moved to `internal/execution/lifecycle_postgres.go`, which preserves the design while using the exported transaction-aware campaign CAS seam. The design plan was corrected to reflect this package boundary.

Adversarial PostgreSQL proof:
- injected lifecycle-event insertion failure rolls back campaign CAS, reservation release and event;
- expired reservation mutation failure after campaign CAS rolls back campaign and event;
- start, pause, resume, cancel and complete all persist the expected state/version, reservation state/fence and version-linked event;
- pre-activated and pre-released reservation sets are idempotent;
- two concurrent starts yield exactly one success and one `campaign.ErrConflict`, one version increment, one fence increment and one event;
- legacy diagnostic event insertion remains valid with NULL campaign version, while a duplicate non-NULL campaign/version lifecycle event is rejected;
- route-level regression returns controlled 409 with no campaign preparation/mutation when the committer is missing, then calls a configured committer exactly once.

Fresh database verification used exact disposable databases only. The repository migration validator passed migrations 0001-0073: 73 files, 128 tables, 453 indexes and 2007 constraints. Schema evidence for the new column/index returned `true|true`. Focused PostgreSQL execution and T6-019 release tests passed.

Complete verification after implementation:
- `go test ./... -count=1` passed across all first-party Go packages;
- `go vet ./...` passed;
- `go build ./...` passed;
- focused `go test -race` passed for `internal/campaign`, `internal/execution` and `internal/persistence/postgres`;
- `git diff --check` was clean before this documentation update;
- static audit found no lifecycle `Campaigns.Transition`, standalone reservation activation/release or lifecycle `Store.RecordEvent` calls in `internal/execution/service.go`;
- all 13 active `openwa0828active` services remained healthy.

Cleanup removed the two disposable databases and cached-library volume created for this verification. Six older idle Task 6 fixture databases referenced by earlier checkpoints were also removed after confirming zero active connections; the final `campaign_task6_%` database count is zero. These deleted fixtures are not recoverable, but their evidence remains in this handover and no application database was touched.

Task 6 is still not the final release gate. Exact next action: continue the remaining RBAC/maker-checker, idempotency/replay, lease/fencing, UNKNOWN/no-resend, cancellation/crash recovery, retention/legal-hold, audit/configuration and gateway/control-plane adversarial review. Do not begin Task 7, deployment or frontend work yet.

## Task 6 checkpoint - Neon parity restored for T6-020 (2026-08-10)

The missing Neon validation gate for T6-020 is now complete. A disposable branch `openwa-task6-remaining-20260810` with opaque ID `br-misty-sun-ayo0fzqu` was created under project `lucky-credit-36128290` from the default branch. All SQL and tests targeted that branch explicitly; the production branch was not mutated.

The disposable branch schema was reset, then the repository validator applied migrations 0001-0073 on Neon PostgreSQL 18.4. Evidence matched the Docker PostgreSQL gate: 73 migration files, 128 public tables, 453 indexes and 2,007 constraints. The `campaign_execution_events.campaign_version` column and `uq_campaign_execution_event_lifecycle_version` index were both present.
The affected PostgreSQL integration suite passed against Neon:
- injected lifecycle-event rollback;
- mixed/expired reservation rollback;
- all lifecycle action and idempotent reservation cases;
- concurrent single-winner start;
- duplicate version-linked evidence rejection and NULL-version diagnostic compatibility;
- T6-019 active reservation release guard.

The temporary Neon branch was deleted after verification. No connection string or credential was printed or persisted. T6-020 now has both Docker PostgreSQL 17 and disposable Neon PostgreSQL 18 evidence.

Task 6 remains in progress. The active execution plan is `docs/superpowers/plans/2026-08-10-remaining-task6-backend-adversarial-review.md`; next is the authorization, recent-MFA and maker-checker closure pass.

## Task 6 checkpoint - T6-021 authorization and maker-checker closure (2026-08-10)

Task 2 of the remaining Task 6 plan is complete. A mechanical inventory found 161 authenticated unsafe-method route registrations in `Server.Handler`; the high-risk executable matrix covers 40 campaign, routing, sender, export, privacy, retention, configuration, provider, pacing and commercial routes. Each denies no principal and the wrong permission. Twenty-nine privileged actions also prove stale MFA is rejected before domain mutation.

T6-021 is FIXED. Six governed decision services blocked the submitter but not the original creator: organisation policy, commercial record, provider capability, consent opt-out policy, inbound-retention policy and sender-pacing policy. A second actor could submit the creator's draft and the creator could then approve it.

The retained RED regressions use three actors and proved the creator could activate all six records. The minimal domain fix rejects both `CreatedBy` and `SubmittedBy`; the same regressions are GREEN and assert the record remains PENDING after denial. Domain placement protects HTTP, worker and future callers.

Existing independent-actor protection was re-exercised for consent review, message-version approval, reporting-privacy policy and platform configuration. A static scan of production approval guards now shows every `SubmittedBy` comparison paired with `CreatedBy`.

Verification ran in `campaign-task2-go-builder:latest`:
- focused six-package three-actor regressions passed;
- `TestTask6HighRiskRoutesRejectMissingAndWrongAuthority` passed;
- `TestTask6PrivilegedMutationsRequireRecentMFABeforeDomainWork` passed;
- full affected commercial, organisation, provider, consent, inbound, sender, message, operations, platformpolicy and HTTP-server package suites passed.

No migration, SQL or persistence query changed in T6-021, so no database branch was needed for this domain-only guard. The complete disposable-Docker and disposable-Neon gates remain mandatory in Task 7. The active stack was not rebuilt or modified.

Exact next action: Task 3 idempotency, replay, durable lease and fencing closure. Task 6 remains uncommitted and in progress.

## Task 6 checkpoint — replay/fencing plus UNKNOWN/cancellation/crash recovery closed (2026-08-11)

Task 6 remains intentionally dirty/uncommitted on `work/backend-production-engineering` at HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77`. Do not reset, clean, stash, split, commit or push the cumulative Task 6 worktree.

T6-022 is FIXED and its database parity gap is closed. Campaign execution claims exclude every unexpired lease, return a monotonic fence, release requires owner+fence, and lifecycle commits validate the same unexpired fence inside the serializable campaign transaction. Docker PostgreSQL/race tests are GREEN. Focused changed-SQL parity was also GREEN on disposable Neon PostgreSQL 18.4 branch `br-withered-snow-ayoowi5z`; the branch was deleted after proof.

Task 4 of `docs/superpowers/plans/2026-08-10-remaining-task6-backend-adversarial-review.md` is now complete and produced three additional High findings:

- T6-023 FIXED: a recipient already persisted as `UNKNOWN` could still reach the gateway on replay because the reducer ignored lower-stage progress but the handler continued. `UNKNOWN` is now a handler-level no-send state; direct replay calls the gateway zero times.
- T6-024 FIXED: cancellation could commit after final eligibility but before `SUBMITTING`, while the delivery transaction locked only the recipient. `EventSubmitting` now locks/validates the campaign in the same PostgreSQL transaction; explicit `CAMPAIGN_CANCELLED` classification records canonical recipient cancellation and prevents the gateway call when cancellation wins.
- T6-025 FIXED: a reclaimed job whose recipient was already `SUBMITTING` could auto-resubmit after worker death. Recovery now converts stale `SUBMITTING` to durable `UNKNOWN` without a gateway call, and newly established UNKNOWN outcomes set `reconciliation_required=true`.

Additional no-resend/cancellation evidence is GREEN: worker cancellation before SUBMITTING remains retryable with zero gateway calls; queued/claimed cancellation suppresses new sends; `GATEWAY_ACCEPTED` and `UNKNOWN` evidence are preserved; queue repair reconstructs zero jobs for UNKNOWN and one when the same authoritative row is explicitly `FAILED_RETRYABLE`; expired durable-job reclaim advances lease version, rejects stale completion and increments attempt count monotonically.
Docker evidence for Task 4 used `campaign-task2-go-builder:latest` and a fresh PostgreSQL 17 container migrated through all 73 repository migrations. Focused PostgreSQL tests, complete affected dispatch/delivery/jobs/execution suites, and race-enabled verification are GREEN. The HTTP gateway characterization proves a timeout after the request reaches the gateway path is `OUTCOME_UNKNOWN`, never safe-to-retry.

Current-source gateway durability verification used a disposable TypeScript 5.9.2 verifier only; `scripts/test-gateway-durability.js` passed and a real SIGKILL during provider execution passed `scripts/test-gateway-crash-unknown.js`, recovering as UNKNOWN after restart with zero resubmissions.

Task-4 Neon parity used disposable branch `openwa-task6-task4-neon-20260811`, opaque ID `br-jolly-glitter-aydmzuxs`, PostgreSQL 18.4. Focused database semantics proved cancellation is blocked at the submission boundary, explicit cancellation classification returns `CAMPAIGN_CANCELLED`, UNKNOWN queue repair produces 0 jobs, and FAILED_RETRYABLE produces 1. The branch was deleted. The complete 73-migration Neon rerun remains deliberately reserved for the final Task-6 gate.

Disposable Task-6 PostgreSQL containers/networks `openwa-task6-cancel-*` and completed prior-slice `openwa-task6-lease-*` were removed after verification. `git diff --check` was clean before this documentation update.

Exact next action: **Task 5 of the remaining Task-6 plan — retention, legal holds, audit and configuration closure.** Begin with held/unheld retention at the same cutoff and legal-hold creation/release/expiry races, then audit atomicity/attribution and concurrent configuration activation. Do not begin Task 7, Railway+Hostinger deployment or frontend work yet.

## Task 6 checkpoint — Task 5 retention/audit/configuration closure (2026-08-11)

Task 5 of `docs/superpowers/plans/2026-08-10-remaining-task6-backend-adversarial-review.md` is complete. Task 6 remains intentionally dirty/uncommitted on `work/backend-production-engineering` at HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77`.

Five additional High findings are FIXED:
- T6-026: legal-hold activation/release and erasure now share a subject-scoped PostgreSQL transaction advisory lock; the reproduced in-flight hold/erasure race is GREEN.
- T6-027: sensitive incident, export, download and delivery-reconciliation mutations now persist exact prepared audit evidence in the same PostgreSQL transaction; outbox replay enters the append-only audit hash chain idempotently, including JSONB canonicalization.
- T6-028: retention query arguments now bind six parameters for inbound/import/export families and append cutoff only for families that reference `$7`.
- T6-029: migration `0074_inbound_reply_encrypted_and_redacted_content.sql` permits empty legacy plaintext only for encrypted or retention-redacted inbound content and rejects unsafe empty content.
- T6-030: export REVOKED state, unused download-grant revocation and audit-outbox evidence now commit/roll back atomically.

Task-5 characterization is GREEN for held/unheld retention, active/released/expired legal holds, post-claim hold recheck, audit contention/idempotent replay/conflicting replay/tamper/attribution, concurrent configuration activation, download authorization/consumption audit failure, delivery-reconciliation audit failure and export-revocation failure injection.

Fresh Docker PostgreSQL 17 verification replayed all 74 migrations. The focused privacy/retention/audit/platformpolicy/outbox/delivery/operations suites and campaign-worker compile passed. A fresh second 74-migration database then passed `go test -race -p=1` across all seven changed packages. Focused `go vet`, campaign-worker build to `/tmp`, and `git diff --check` passed.

Neon parity was run only on newly named disposable branches and both were deleted. `br-dark-meadow-ay05lxut` (PostgreSQL 18.4) proved migration-0074 constraints, six/seven-parameter retention scheduling, hold recheck and transaction/audit rollback. After T6-027/T6-030 was extended, `br-wispy-king-ayjyrenp` (PostgreSQL 18.4) proved download authorization, download consumption, delivery-reconciliation closure and export revocation commit all effects together on success and roll all effects back on injected failure.

Exact next action: **Task 6 — gateway, Compose and control-plane security closure.** Run current-source strict TypeScript/build and gateway scripts, validate production Compose hardening/interpolation, scan tracked plus untracked candidate files for secrets/PII logging, and audit gateway body limits/replay windows/nonce durability/authority expiry/teardown/outbox durability/graceful shutdown. Real WhatsApp pairing/send/ack/reconnect/endurance remains an external live gate. Do not begin Task 7, deployment or frontend work yet.

## Task 6 checkpoint — gateway, Compose and security closure (2026-08-11)

Task 6 remains intentionally dirty/uncommitted on `work/backend-production-engineering` at HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77`. Do not reset, clean, stash, split, commit or push the cumulative Task 6 worktree.

Task 6 plan item 6 is complete. Seven additional gateway/security findings were reproduced and fixed:
- T6-031 High: signed provider callbacks and runtime heartbeats could follow redirects while carrying HMAC trust headers; both now use `redirect: manual`.
- T6-032 Medium: callback failures buffered the complete error body with `response.text()` before slicing; a shared reader now consumes at most 300 bytes and cancels the remainder.
- T6-033 High: accepted session-authority fences were renamed without file/directory fsync; durable replacement now fsyncs the file and containing directory before success.
- T6-034 Medium: gateway `/tmp` was writable/executable; packaged Chromium was proven compatible with `/tmp:size=512m,noexec,nosuid`, and production Compose now uses that mount.
- T6-035 Medium: the previous callback-signing secret was unnecessarily mounted into the gateway; it remains only on the control API where previous-key verification occurs.
- T6-036 Medium: `PROFILING_TOKEN` and `MEDIA_DOWNLOAD_SECRET` had no gateway runtime consumer and were removed from gateway bootstrap/mounts while legitimate control-api/worker consumers remain.
- T6-037 Medium: structured `email` fields bypassed the shared Go logger redaction policy; shared logging now redacts email keys.

RED→GREEN evidence includes `scripts/test-gateway-control-plane-security.js`, `scripts/test-gateway-session-authority.js`, `scripts/test-gateway-outbox-integrity.js` and `internal/observability/observability_test.go`. Characterization also proves stale/future gateway commands are rejected before nonce persistence, expired/excessive authority writes nothing, and unsuccessful shutdown flushes preserve both provider and inbound outbox evidence for restart.

Final current-source gateway verification used the disposable Node 22 verifier and passed strict `tsc --noEmit`, emitted Nest build, retained-source sync of 56 pinned OpenWA files, embedded engine tests, three session-authority tests, four outbox-integrity/shutdown tests, three drain/teardown tests, durability/idempotency/observability tests, real SIGKILL→UNKNOWN recovery, six control-plane security tests and secret-file tests.

The actual production Dockerfile rebuilt successfully as image `openwa-task6-gateway-final:verify`, image ID `sha256:bb649a829063962d1391723c2c4ea96042a60c3e72958f5cd4d376cf4cd2e19e`. Runtime proof on that image returned `uid=1000(node)`, confirmed a read-only root filesystem with writable `noexec` `/tmp`, Chrome headless exit 0, `/healthz` 200, an ~80 KB JSON request reaching controller validation with 400, and an ~150 KB request rejected by the parser with 413.

Fresh production Compose interpolation used synthetic secrets only. All 8 resolved services are read-only, drop `ALL` capabilities, use `no-new-privileges`, have healthchecks, publish no host ports and use digest-pinned images. The private network is internal; only `openwa-gateway` joins egress. The gateway now mounts exactly four secrets: active/previous command signing, active callback signing and active runtime signing.

Security/contract evidence is GREEN: tracked+untracked high-confidence secret scan passed; additional URI classification found zero high-entropy or unclassified connection strings; strict Node security passed with no production exceptions; OpenAPI parity remains 281/281; and `git diff --check` was clean before this documentation update. The shared PII-log scan found no MSISDN value logging and the only structured email path is now redacted by the shared logger.

Real WhatsApp pairing/authentication, genuine text/media sends, provider ACKs, reconnect/endurance and genuine ambiguous provider outcomes remain external live gates. Railway+Hostinger deployment/network proof, independent security assurance, backup/restore/DR and operational-owner approval also remain external/release gates.

Exact next action: Task 7 of `docs/superpowers/plans/2026-08-10-remaining-task6-backend-adversarial-review.md` — run the final full Docker Go/PostgreSQL regression, fresh migration validator through 0074, focused race suites, disposable Neon PostgreSQL 18 parity, final gateway/OpenAPI rerun, cleanup and handover reconciliation. Do not call Task 6 complete until every Task-7 local gate is green.

## Task 6 final checkpoint — internal Task 7 Docker + Neon closure (2026-08-11)

Overall Backend Task 6 is locally complete after the final verification gate. The cumulative worktree remains intentionally dirty/uncommitted on `work/backend-production-engineering` at committed HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77`; no reset, clean, stash, commit, push, deployment or frontend work was performed.

T6-038 High is FIXED. Under sufficient audit-chain contention, `PostgreSQLRepository.Append` could exhaust its inner SQLSTATE 40001/40P01 transaction retries and return the raw PostgreSQL error. `Recorder.RecordPrepared` refreshes the chain head only on `ErrChainConflict`, so a valid governed audit append could fail rather than retry against the new head. Exhausted retryable PostgreSQL transaction errors are now normalized to `ErrChainConflict`. The deterministic RED regression `internal/audit/postgres_retry_test.go` failed with the raw forced 40001 before the fix and is GREEN afterward; the real 12-writer PostgreSQL contention regression also passed 20 consecutive runs.

Final Docker Go evidence is GREEN:
- `/usr/local/go/bin/go test ./... -count=1` passed all first-party packages, exit 0;
- `/usr/local/go/bin/go vet ./...` and `/usr/local/go/bin/go build ./...` passed in the named Docker verifier;
- the complete PostgreSQL opt-in inventory discovered 36 `POSTGRES_*_DATABASE_URL` variables;
- a fresh PostgreSQL 17 database passed repository migrations 0001-0074 with 74 files, 128 public tables, 453 indexes and 2,007 constraints;
- the complete `go test -p=1 -count=1 ./...` run with all 36 PostgreSQL opt-ins enabled passed after three shared-fixture isolation defects were corrected in tests only;
- focused `go test -race -p=1 -count=1` passed across 23 affected packages, including `internal/platform/httpserver` in 394.559s, with marker `TASK7_FOCUSED_RACE_GREEN`.

The three initial full-suite PostgreSQL failures were proven to be test isolation rather than production defects: outbox claim could see unrelated pending rows, test-message persistence used fixed provider/gateway fixture identities, and pagination assumed global recipient-table emptiness. Each failed test passed alone on a clean 74-migration database before fixture-only isolation changes. The same tests then passed against the already-contaminated database and in the final fresh full suite.
Final Neon PostgreSQL 18 evidence is GREEN. A first disposable branch `openwa-task6-final-neon-20260811` (`br-crimson-brook-ay838ipc`) re-proved migration-0074 semantics, execution lease fencing, audit transaction/CAS behavior and PostgreSQL 18 serialization SQLSTATE behavior, then was deleted.

The literal final migration-validator gate then used a second disposable branch `openwa-task6-final-fullchain-20260811` (`br-flat-moon-aypoqh2v`). A branch-only `task7_validator` login was created with a synthetic one-time credential and least-privilege database/schema creation grants. The remote verifier received only a temporary bundle containing `scripts/validate-postgres-migrations.py` plus the 74 migration SQL files; no repository source, Git metadata, env files or real Neon credential was exposed. The repository migration validator passed 0001-0074 on Neon PostgreSQL 18.4 in 10.237s with 128 public tables, 453 indexes and 2,007 constraints.

Actual compiled Go test binaries were then run against that fully migrated Neon branch without mounting the source tree. GREEN evidence:
- `TestTask6AuditContentionReplayTamperAndAttribution` — 17.66s;
- `TestPostgreSQLAppendNormalisesRetryExhaustionToChainConflict` — 0.16s;
- `TestPostgreSQLExecutionLeaseHasOneWinnerAndCannotBeReclaimedBeforeExpiry` — 4.87s;
- `TestTask6RetentionExecutesOnlyUnheldContentAndRechecksHoldAfterClaim` — 5.27s.
The connector independently confirmed PostgreSQL `180004`, 128 tables, 453 indexes, 2,007 constraints, required `campaign_recipients`, `gateway_runtime_nonces` and `retention_jobs` relations, and exactly one `inbound_replies_message_text_check` constraint. `br-flat-moon-aypoqh2v` was deleted and no longer appears in Neon search.

Final gateway/control-plane evidence is GREEN on a current-source isolated Node verifier: strict `tsc --noEmit`, emitted Nest build, embedded retained OpenWA engine, session-authority fencing/fsync/expiry, provider and inbound outbox integrity/shutdown, drain/teardown, gateway durability, real SIGKILL-to-UNKNOWN recovery, six control-plane security tests and secret-file tests all passed with exit 0 and marker `TASK7_GATEWAY_NODE_GREEN`. Final static security also passed committed-secret scanning, production Compose security validation, strict Node security and OpenAPI parity at 281/281 implemented `/api/v1` method/path pairs.

Cleanup is complete: 10 Task-6/Task-7 disposable Docker containers and one Task-7 network were removed; zero Task-6/Task-7 containers, networks, named volumes or temp files remain. Both final Neon branches were deleted. The untouched active stack has exactly 13 `openwa0828active-*` containers and all 13 report healthy.
Local Task 6 completion does not close external/live release evidence. Still external: genuine WWebJS/Baileys authentication and pairing; real text/media sends; inbound/STOP provider traffic; delivery/read ACKs; reconnect/watchdog/endurance; genuine ambiguous provider outcomes; sustained target-volume endurance; real sender-session RAM/CPU; independent security/penetration review; Railway+Hostinger deployment/network proof; backup/restore/DR/RPO/RTO; monitoring/on-call/runbooks and operational-owner approval; and production frontend/UAT/accessibility.

Task 6 findings T6-001 through T6-038 are recorded FIXED in `docs/program/BACKEND_ADVERSARIAL_FINDINGS.md`. The internal Task-7 checklist in `docs/superpowers/plans/2026-08-10-remaining-task6-backend-adversarial-review.md` is complete.

Exact next authorized action: preserve the current cumulative worktree and obtain separate authorization before creating a checkpoint commit/push or beginning Railway+Hostinger production-infrastructure integration. Do not start the deferred production frontend merely because Backend Task 6 is locally complete.

Final Neon inventory also found an older disposable `openwa-task6-fencing-20260810` branch (`br-nameless-night-ayunric6`). It was confirmed `Default=false`, unprotected, with 0 bytes written and no application schema, then deleted. Final Neon searches return zero `openwa-task6` and zero `openwa-task7` branches.


## 17 August 2026 checkpoint — R18 routing-capacity adjudication

R18 blind review passed A/C/D and split on B. Adjudication confirmed sequential same-day daily-capacity double-booking and terminal-campaign HELD-capacity creation; the adjacent-window hourly claim was rejected as over-broad. Both confirmed defects were reproduced RED, minimally fixed, and are GREEN on PostgreSQL 17/18 plus execution unit/race gates. Source changed, so R18 cannot freeze the backend; fresh blind R19 is mandatory.


## 17 August 2026 checkpoint — I2 node-addressed OpenWA and split-production remediation

R19 is preserved as historical blind-review evidence, but its source freeze was intentionally reopened after infrastructure I1 review exposed a reachable architecture/runtime mismatch. Frozen routing authority selected a concrete OpenWA gateway node, while campaign/test-message HTTP submission still used one static `OPENWA_GATEWAY_URL`. That was incompatible with the approved Hostinger multi-node gateway fleet and could send to a different node from the one whose ID/version/session lease was authorised.

The coherent remediation now captures `sender_nodes.internal_url` during governed route validation before the SUBMITTING ambiguity boundary, propagates it through campaign dispatch and controlled test sends, and lets `HTTPGateway` submit to that request-specific destination. Static URL remains compatibility fallback only. Campaign-worker transport construction accepts OpenWA signing-secret + governed node addressing without requiring a static URL. No post-SUBMITTING database/routing lookup was added.

Real PostgreSQL validation uncovered three additional pre-existing test-route query defects and closed them: explicit timestamptz typing for the freshness parameter; UUID→text casting for legacy campaign gateway-pool comparison; and optional minimum gateway version semantics. Clean PostgreSQL 17 and 18 databases replayed 0001→0089 and both passed `TestPostgreSQLGovernedRouteCarriesExactNodeURL`. Controlled test-route PostgreSQL proof also passes and returns the exact stored node URL.

Infrastructure I1 was then remediated into the I2 candidate. `compose.production.yaml` is now a seven-service Railway control-plane reference only; OpenWA lives solely in the Hostinger gateway manifest and Meta remains direct. The control-plane reference contains no static OpenWA gateway URL and requires network/trusted-proxy boundaries. The production verifier is tied to `config/deployment-topology.json` and a tamper regression proves it rejects a reintroduced OpenWA service. Hostinger requires explicit private `GATEWAY_INTERNAL_URL`, Railway HTTPS callback/inbound/media URLs and an exact SSRF hostname.

Fresh local gates: 50/50 governance tests; production Compose 7 services/25 secrets; split topology verifier; Docker rendering of the Railway control plane; BAILEYS + WHATSAPP_WEB_JS Hostinger rendering and resolved preflight; affected Go unit and race suites; clean PG17/PG18 node-addressing regressions; OpenAPI 294/294; committed-secret scan; Node-security/release-governance checks; TypeScript syntax + gateway durability; 8/8 gateway control-plane security; 9/9 outbox integrity; 3/3 session authority; 18/18 drain/lifecycle plus serialization; whole-worktree diff check; and full exact-source Go test/vet/build marker `I2_EXACT_GO_GREEN`.

The eight formal release gates remain open or externally blocked exactly as before; local infrastructure configuration does not promote them. No live Railway/Hostinger provisioning, provider pairing/send, credential mutation, DNS/firewall change, commit/push/deployment or frontend work occurred.

I2 pre-review source/config fingerprint (documentation excluded) is `bdaebba1e8d92ac858e642ca4ea9b07410d01d39a20f3bfe9bdaeb8879d63fa1` tracked / `3cfdd21cb240c82a0eff0b663977731d0513495b9cdda82af98b7bf71caa8d8f` untracked (189 files). Exact next action: regenerate/validate conservative traceability, prove this fingerprint is unchanged by documentation-only reconciliation, freeze the complete I2 review packet, run fresh genuinely blind council, adjudicate/reproduce any findings, remediate only confirmed defects, re-gate after any source/config change, then establish the new freeze and immediately continue the next infrastructure slice.


## 18 August 2026 checkpoint — I3 pre-review

I2 blind review found four independently adjudicated production-boundary defects, all now remediated in the current I3 candidate: cross-provider media URL reachability/HTTPS, production runtime advertised gateway URL fail-close, removal of static-router fallback after governed node selection, and driver-aware platform-governance S3 startup validation. The approved sibling transports, Railway/Hostinger split, pre-SUBMITTING node-address resolution, sticky UNKNOWN/no-resend, Meta governance and security/fencing invariants remain unchanged.

Fresh I3 local evidence: governance 52/52; production security 7 services/25 secrets; split topology; production Compose render; resolved BAILEYS + WHATSAPP_WEB_JS Hostinger preflight; OpenAPI 294/294; candidate secret scan; strict Node security with zero production exceptions/vulnerabilities; real isolated gateway npm-ci + tsc; Linux gateway control security 9/9, outbox 9/9, session authority 3/3, drain/lifecycle 18/18 plus serialization; clean diff hygiene; and full exact-source Go test/vet/build marker I3_EXACT_GO_GREEN. Windows directory-fsync EPERM was independently reproduced as a host limitation and the unchanged durability suite passed on Linux.

Traceability remains 398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL. All eight release hard gates remain OPEN/BLOCKED_EXTERNAL. Current non-document source/config fingerprint is tracked `3a8f5ab26faccb6ac6b21e0b2e1daea88e536849400a497607f81bad74bdf14b` / untracked `8e7af7541650f946aae0d1a4165e77786596541672a61ee0967c250ca2f18773` across 189 non-document untracked files. I3 is not freeze authority until the fresh blind Grok 4.6 / Qwen 3.8 Max / Gemini 3.7 Flash / GLM 5.2 council on identical current bytes is independently adjudicated. No commit, push, deployment, live provider action or frontend work occurred.


## 18 August 2026 — I3 final infrastructure freeze (I3-FINAL-FREEZE-20260818)

I3 is frozen locally after the complete remediated2 gate and fresh genuinely blind council. Exact full Go gate: `go test -count=1 ./... && go vet ./... && go build ./...` exited 0 with `I3_REMEDIATED2_EXACT_GO_GREEN`. Fresh supporting evidence is also GREEN: governance 56/56, gateway control security 15/15, Linux outbox 9/9, session authority 3/3, lifecycle 18/18 plus serialization, affected race, gateway TypeScript, secret scan, OpenAPI 294/294, split topology/Compose, and zero-vulnerability Node audits.

Final source/config freeze: tracked non-doc diff SHA-256 `0fe4c15839ec7207ed6f6931446c4377fc94c82a0bee6856cfec832e46a3f53e`; untracked non-doc content SHA-256 `529a278c0e3eb86deb87676439f66bf46159b0851b5fea265ab8e0f06aac3a72` across 189 files. Fresh blind packet: 860176 bytes, SHA-256 `d97a1a54d39509312ccf03191a9d62730f6a7bb97ad9b65fd83d77bae7f8b48f`.

Final blind council: Grok 4.6 PASS (attempt 1), Qwen 3.8 Max PASS (attempt 1), Gemini 3.7 Flash PASS (attempt 1), GLM 5.2 PASS (attempt 2; first attempt was an evidence limitation only). Normalization found no material finding and therefore no remediation was authorized.

Release honesty is unchanged: traceability is 398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL; all eight hard release gates remain OPEN or BLOCKED_EXTERNAL. This freeze does not claim live Railway/Hostinger deployment, real-provider validation, production-scale evidence, DR proof, independent penetration testing, operations exercise, or frontend completion.

Continuation authority: proceed immediately into I4 locally using the continuous substantial-chunk/TDD/full-gate/blind-council cadence.

## I4 large production-hardening boundary — pre-council evidence (2026-08-18)

<!-- I4_LARGE_PRODUCTION_HARDENING_PRECOUNCIL -->

The former narrow I4 deployment-readiness slice has been deliberately widened into one substantial local backend production-hardening boundary. It now covers deployment-readiness evidence contracts plus the release-governance trust boundary: exact hard-gate membership and authoritative definitions, exact governance-document membership, safe repo-relative paths, structured CLOSED evidence, formal time-bounded WAIVED evidence, future-timestamp rejection, malformed-evidence fail-closed handling, Must-traceability coupling, and RG-006/RG-007 coupling to accepted recovery/monitoring evidence.

Pre-council gate evidence on the exact local candidate:
- governance: **97/97 GREEN**
- exact Go: `go test -count=1 ./... && go vet ./... && go build ./...` → `I4_LARGE_EXACT_GO_GREEN`
- full race: `go test -race -count=1 ./...` → `I4_LARGE_FULL_RACE_GREEN`
- gateway TypeScript: `I4_LARGE_GATEWAY_TSC_GREEN`
- Linux gateway authority/lifecycle/durability: `I4_LARGE_LINUX_GATEWAY_GREEN`
- Node audits: admin **0 vulnerabilities**, gateway **0 vulnerabilities**
- CycloneDX SBOM: **29 components**
- production Compose structure: **7 Railway services / 25 secrets**
- OpenAPI: **294** implemented `/api/v1` method/path pairs
- traceability: **398 total = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**, regenerated byte-for-byte unchanged
- production-candidate honesty check: actual **exit 2**, with all eight hard gates still outstanding
- source/config fingerprint: tracked diff `61309317112221e3dcd9131e0793e377dd6176055d37dfeb79e548af12995ce5`; untracked content `d75d5ba7722cea667d5d1927c8667db8fc40154dcc68613d3b4e64577c521f47`; untracked non-doc files **192**

No live Railway/Hostinger deployment, real provider traffic, live monitoring proof, DR exercise, external penetration test, or other external release evidence is claimed. All hard gates remain OPEN or BLOCKED_EXTERNAL exactly as declared. The next boundary is an exact-source blind council using Grok 4.6, DeepSeek V4 Pro, Gemini 3.7 Flash, and GLM 5.2; adjudication precedes any remediation.

## I4 council adjudication, remediation, and full re-gate (2026-08-19)

<!-- I4_POST_COUNCIL_REMEDIATION_REGATE -->

The exact-source I4 blind council produced two independently adjudicated valid defects. Grok 4.6 and Gemini 3.7 Flash identified a metrics-worker health-port contract drift (`:8093` runtime default versus authoritative Railway port `8096`); Grok 4.6 identified a control-api readiness-probe drift (`/healthz` in production Compose versus authoritative `/readyz`). DeepSeek V4 Pro returned PASS. GLM 5.2 returned no usable review because its response hit the model length limit; that is recorded as an evidence limitation, not as a pass or a programme blocker.

Both valid findings were reproduced RED before remediation, then fixed as one coherent I4 batch. Metrics-worker now defaults to `:8096`. Control-api production Compose now probes `/readyz`, and `verify-production-compose.py` fail-closes service health port/path drift against `infrastructure/railway/service-contracts.json` for every Railway service.

Post-remediation gate evidence on the changed local candidate:
- focused regressions: **GREEN** for metrics-worker default-port contract and production healthcheck contract drift
- governance: **98/98 GREEN**
- exact Go: `go test -count=1 ./... && go vet ./... && go build ./...` -> `I4_NATIVE_EXACT_GO_GREEN`
- full race: `go test -race -count=1 ./...` -> `I4_NATIVE_FULL_RACE_GREEN`
- gateway TypeScript: **GREEN**
- Linux gateway control-plane/outbox/session-authority/lifecycle/durability/recovery suite: **GREEN**
- Node production audits: admin **0 vulnerabilities**, gateway **0 vulnerabilities**
- CycloneDX SBOM: **29 components**
- static/security/topology/release/OpenAPI bundle: `I4_POST_COUNCIL_STATIC_SECURITY_GREEN`; OpenAPI remains **294/294** implemented `/api/v1` method/path pairs
- production-candidate honesty check: actual **exit 2** as required, with all eight hard release gates still outstanding

No live Railway/Hostinger deployment, real provider traffic, live monitoring proof, DR exercise, external penetration test, or other external release evidence is claimed. RG-001 through RG-008 remain OPEN or BLOCKED_EXTERNAL exactly as declared. Because reviewed source changed during remediation, the next mandatory boundary is a new exact-source/config fingerprint and a fresh genuinely blind council before I4 can be accepted; any fresh valid findings must again be adjudicated before remediation.
## 20 August 2026 — I4 Fresh7 adjudication/remediation and Fresh8 pre-review boundary

Fresh7 reached seven usable genuinely blind lanes before findings were opened. Independent adjudication confirmed an arbitrary/unapproved HTTPS gateway-DNS authority gap, production HTTP gateway advertisement despite the cross-provider TLS contract, a queued-heartbeat shutdown race that could publish READY after DRAINING, incomplete Railway secret-class evidence, Hostinger gateway omission from the release SBOM, and missing explicit TLS observation in ACCEPTED public-ingress evidence. The claimed requirement that the external gateway URL must always use port 2785 was rejected because 2785 is the container listen/health port and the architecture preserves reverse-proxy/DNS/certificate portability. Indefinite retry of a failed DRAINING report was also rejected; the reachable queued-READY race was reproduced separately and fixed.

The coherent remediation now makes production gateway destinations HTTPS-only and binds DNS destinations to explicit governed `GATEWAY_RUNTIME_ALLOWED_HOSTS` authority across Hostinger preflight, gateway startup and control-plane runtime registration. A shutdown fence prevents non-DRAINING heartbeats once teardown begins. Railway secret contracts now account for sender-proxy, media-download and profiling material; production Compose verification checks mounted sensitive files against authoritative classes. The strict CycloneDX generator includes the Hostinger OpenWA image and is functionally GREEN with 30 components / 8 container components; lockfile enforcement now follows dependency-bearing manifests rather than the dependency-free workspace aggregator. Public Railway control-api ingress declares TLS required and ACCEPTED network evidence must explicitly prove HTTPS/TLS observation.

Fresh8 pre-review evidence is GREEN: governance **115/115**; production Compose **7 Railway services / 25 secrets**; topology/readiness/release governance and committed-secret scan GREEN; Node security GREEN; OpenAPI **294/294**; full Linux gateway boundary GREEN with control-plane security **22/22**, outbox **9/9**, session authority **3/3**, drain/lifecycle **18/18**, crash-to-UNKNOWN recovery, durability, fencing, lifecycle serialization, pipeline bound, retained Baileys recovery, secret-file handling and resource-health collection; fresh PostgreSQL 17 Final10 applied all three migration histories and the complete **40-DSN** repository suite exited 0; full Go `test`, `race`, `vet`, and `build` are GREEN on the exact candidate.

Council evidence recovery is fail-honest. A primary reviewer that repeatedly times out or returns unusable length-limited evidence remains recorded as unavailable; `z-ai/glm-5.3` may then receive the identical frozen packet and specialist lane as a separately identified fallback. Fallback output is not relabelled as the failed model, and acceptance still requires reviewer-family diversity.

All eight hard release gates remain OPEN or BLOCKED_EXTERNAL. This local candidate does not claim live Railway/Hostinger deployment, real-provider traffic, production-scale performance, penetration testing, backup/DR proof, operational exercise or frontend completion. The mandatory next boundary is Fresh8 exact-source/config freeze and genuinely blind council; any valid finding must again be independently adjudicated, reproduced RED where practical, remediated as one batch and re-gated before I4 acceptance.

## 20 August 2026 — OpenWA I4 Fresh9 pre-council authority

Authoritative repo remains `C:\Users\sanus\OpenWA\campaign-platform-active\repo`, branch `work/backend-production-engineering`, accepted base HEAD `887e577565de22642cf4041a223835d122e6ff15`; the active I4 candidate is the intentional dirty worktree and the staged index remains empty.

Fresh8 adjudication/remediation is complete. Confirmed defects were RED-proven and fixed for production network-CIDR startup, Hostinger ACCEPTED TLS evidence, waiver maker-checker independence, accepted service-evidence ownership, explicit target/measured RPO/RTO enforcement, and bounded failed-DRAINING reporting. Rejected claims were not implemented; usable originating-reviewer recalibration retracted/refined them.

Fresh9 local gate evidence is GREEN: governance **120/120**; Railway **7 services / 25 secrets**; OpenAPI **294/294**; full Linux gateway including control-plane security **23/23**, outbox **9/9**, authority **3/3**, drain/lifecycle **18/18** and durability/recovery/fencing/serialization/pipeline/Baileys/secret/resource-health checks; full Go `test`, `race`, `vet`, `build`; fresh PostgreSQL 17 with all three migration histories and **40-DSN** suite exit 0; CycloneDX **30 components / 8 container images** including Hostinger. Traceability is **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**.

Immediate continuation: exact Fresh9 freeze -> blind council -> normalize/adjudicate -> RED/batch fix/re-gate only if a valid defect survives -> otherwise accept and locally commit I4 -> immediately begin controlled Neon PostgreSQL parity/backend freeze. No push, deployment, live-provider traffic or production credential/network mutation is authorized by this local evidence.

## 20 August 2026 — I4 Fresh9 adjudication/remediation and Fresh10 pre-council boundary

Fresh9 blind review completed with seven usable lanes across three model families. Independent adjudication rejected the claimed Python IPv4-mapped control-host bypass, partially validated the residual deployed `OPENWA_GATEWAY_URL` configuration surface, and confirmed rejected-runtime nonce replay plus the gateway/control-plane capacity-bound mismatch.

The valid findings were reproduced RED before remediation. Staging/production campaign-worker now rejects a non-empty static `OPENWA_GATEWAY_URL` while governed node-addressed OpenWA remains enabled through the command-signing secret. Runtime registration consumes the nonce after governed node/pool resolution but before drift validation so the first invalid signed report retains REJECTED evidence and replay cannot append duplicate evidence. Gateway runtime capacity now fails closed outside the control-plane-supported 1..1000 range.

Fresh9 reviewer recalibration closed the disputed claims: Grok retracted the mapped-address finding; GLM 5.3 governance, recovered-security and topology reviewers refined the static-URL claim to the configuration-surface issue actually remediated. No reviewer maintained the disproven plaintext fallback-send sequence.


## 20 August 2026 — I4 Fresh10 final local gate boundary

<!-- FRESH10_FINAL_ACCEPTANCE -->
Fresh10 remediation is fully re-gated locally. Valid Fresh9 findings were fixed by rejecting residual static `OPENWA_GATEWAY_URL` in staging/production, consuming signed runtime nonces before drift rejection evidence can be replay-flooded, and aligning gateway runtime capacity with the control-plane maximum of 1000.

Final local evidence is GREEN: governance 120/120; OpenAPI 294/294; production Compose 7 Railway services / 25 secrets; resolved Hostinger governed HTTPS boundary; gateway TypeScript and complete Linux gateway suite including control-plane security 24/24; full Go ordinary tests, full race, vet and build; fresh PostgreSQL 17 with all three migration histories and full 40-DSN repository suite; committed-secret and Node security checks; CycloneDX 30 components / 8 container images including Hostinger. Production-candidate validation correctly remains non-zero because all eight hard release gates are still OPEN/BLOCKED_EXTERNAL.

No live Railway/Hostinger deployment, provider traffic, production credential mutation, scale proof, DR exercise, penetration test, operational exercise or frontend completion is claimed. The mandatory next acceptance boundary is the exact Fresh10 source/config freeze and genuinely blind council; only an accepted council boundary may be locally committed.

## 21 August 2026 — I4 Fresh10 adjudication and Fresh11 pre-council

Fresh10 blind review produced five confirmed material gaps after independent reproduction: pool-unavailable runtime-report nonce replay, governed gateway-host parsing incorrectly reusing a 64-character keyword parser, an in-flight READY registration surviving shutdown start, RG-001 gate-level waiver laundering around Must traceability, and an unhandled malformed Railway-contract KeyError. All five were reproduced RED and remediated as one batch. Rejected claims were recalibrated privately: mapped Hostinger bind bypass RETRACTED; RG-006/RG-007 waiver relaxation RETRACTED; zero RPO/RTO refined to remain valid when provenance/targets are satisfied.

Fresh11 pre-council evidence is GREEN: governance 122/122; Railway 7 services / 25 secrets; gateway TypeScript, security 25/25, outbox 9/9, authority 3/3, drain/lifecycle 18/18, UNKNOWN crash recovery and durability; Go test/race/vet/build all exit 0; genuinely fresh PostgreSQL 17 with three migration histories and the full 40-DSN repository suite exit 0; OpenAPI 294/294; committed-secret and Node-security checks GREEN; CycloneDX 30 components / 8 container images including Hostinger.

All eight hard release gates remain OPEN or BLOCKED_EXTERNAL. No live deployment/provider traffic, production credential mutation, scale proof, DR exercise, external penetration test, operational exercise, or frontend completion is claimed. The mandatory next boundary is exact Fresh11 freeze plus genuinely blind council before I4 acceptance/local commit.

## 24 August 2026 — I4 Fresh14 adjudication and Fresh15 remediation boundary

Fresh14 produced eight usable blind lanes across three actual model families; two DeepSeek lanes were unusable at the response-length boundary and are correctly labelled as GLM 5.3 fallbacks. Independent adjudication is preserved at `validation/ai-review/infrastructure-i4-fresh14-20260823/I4-fresh14-adjudication.md`. Confirmed defects were governed-node URL drift, case-variant waiver self-approval, a fixed-date test time bomb, and the reachable unassigned-node portion of rejection-evidence loss. The claimed staging allowlist bypass and native IPv6 control-host bypass were rejected. A maximum waiver horizon remains policy authority not supplied by engineering; no arbitrary duration was invented.

The coherent Fresh15 remediation now binds a non-empty governed node URL while preserving first-registration bootstrap; compares waiver actors with canonical Unicode case-folding; uses a time-relative acceptance fixture; and adds migration 0090 so only authenticated `REJECTED` runtime events may lack a governed pool anchor while the declared pool remains in immutable runtime identity. The original deleted-pool sequence is prevented by the existing sender-node foreign key.

Current post-change host evidence is governance **128/128 GREEN**, all Go packages except the Linux-only secret-file mode test GREEN on Windows, `go vet ./...` GREEN, `go build ./...` GREEN, focused URL/schema regressions GREEN, and diff hygiene GREEN. The excluded secret-file test correctly cannot prove Unix group/world bits on Windows and will not be weakened. Exact Go 1.23.2 Linux test/race, fresh PostgreSQL 17 migration histories through 0090, the 40-DSN matrix, gateway/Node/static/security/SBOM gates and Fresh15 exact-source review remain mandatory before acceptance.

The sender-name discussion is now durable authority in ADR-0006 and `docs/requirements/CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`: reusable underlying account, exclusive campaign reservation, requested-versus-observed profile identity, verification before READY, versioned engine capabilities, and disabled-by-default username operations until genuine validation.

All eight release hard gates remain OPEN or BLOCKED_EXTERNAL. No live deployment/provider traffic, production credential mutation, scale proof, DR exercise, penetration test, operational exercise or frontend completion is claimed. Fresh14 is historical after source changes; Fresh15 has not yet been frozen or reviewed.

## 24 August 2026 — I4 Fresh18 sizeable remediation and pre-council boundary

Fresh16 and Fresh17 were sizeable exact-source council boundaries, not file-by-file reviews. Fresh16 confirmed four defects that Fresh17 remediated: atomic nonce/rejection persistence, durable governed-pool rejection anchoring, gateway/control hostname separation, healthcheck-scoped Compose verification, and observed private-bind evidence. Fresh17 then confirmed three remaining defects: wall-clock runtime causality allowed a retired boot to reclaim service and fenced healthy clock corrections; invalid production capacity could be swallowed by deferred registration; and production gate-closure metadata was not bound to the actual candidate evidence artifact.

Fresh18 remediates those findings as one engineering chunk. Signed runtime reports now carry a positive per-boot `runtimeSequence`; memory and PostgreSQL accept only a newer same-boot sequence, retain accepted boot history, reject every state from a retired boot, keep same-boot DRAINING terminal, and treat `observedAt` as freshness evidence rather than a causal clock. Production gateway startup validates capacity before deferred publication. Production-candidate release verification now requires safe local evidence paths, SHA-256 byte binding, schema-versioned JSON manifests bound to the exact gate and candidate fingerprint, non-empty observed evidence, and independent owner/approver identity. Development-mode structural checks and honestly open gates remain unaffected.

Fresh18 exact-source evidence is GREEN: **2,384 files / 0 byte mismatches**; governance **137/137**; Go ordinary, full race, vet and build; fresh PostgreSQL 17 with three histories through migration 0090 and the full **40-DSN** suite; gateway TypeScript/build, control-plane security **30/30**, and **38** retained durability/lifecycle tests; OpenAPI **294/294**; production Compose **7 Railway services / 25 secrets**; resolved Hostinger boundary for both BAILEYS and WHATSAPP_WEB_JS; committed-secret scan; Node audits **0/0 vulnerabilities**; strict CycloneDX **30 components / 8 images** with SHA-256 `84257faddf0d708bf0a9e0b0b95d897fdc5ef40a922b1ff4225aee8fcf1be7c3`. Production-candidate validation correctly exits 2 because all eight release gates remain OPEN or BLOCKED_EXTERNAL.

The campaign-scoped sender-name authority remains ADR-0006 plus `CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`: reusable account identity is distinct from exclusive campaign reservation and requested-versus-observed sender profile identity. No live deployment, provider traffic, production credential/network mutation, capacity proof, DR exercise, penetration test, operational exercise, or frontend completion is claimed. Fresh18 must now be frozen twice and reviewed once as this complete 54-path chunk; no staging or local acceptance commit is permitted before council adjudication.

## 24 August 2026 — I4 Fresh19 council-remediation boundary

Fresh18 completed one valid 8-lane council after the full sizeable chunk; two primary Grok lanes timed out twice and used explicitly labelled GLM 5.3 fallbacks. The council rejected Fresh18. Adjudication confirmed six material issues and rejected the unsupported or overbroad claims: gateway authority URLs admitted credentials or non-authority components and lacked canonical host/port equality; production capacity was not explicitly coupled to the engine session limit; release manifests did not bind each evidence item to governed gate-local bytes; a rejected authenticated DRAINING declaration did not make that boot terminal; governed runtime authority was checked before but not inside the serialized store transaction; and a configured production boot ID could be reused while the runtime sequence restarted.

Fresh19 remediates those findings as one coherent backend/infrastructure chunk. Go and TypeScript now accept only HTTPS authority-only gateway URLs, canonicalize hostname/trailing dot/port (including default 443), and reject invalid ports. Memory and PostgreSQL recheck governed pool, URL, provider, engine and adapter under their write lock/transaction. Any authenticated DRAINING declaration makes its boot terminal even when the replacement drain is rejected. Production gateway startup rejects configured boot IDs and requires explicit 1..1000 `GATEWAY_CAPACITY` equal to `OPENWA_MAX_CONCURRENT_SESSIONS`; Hostinger Compose and resolved topology enforce the same contract. Production closure/waiver manifests now restrict evidence kinds per gate and bind each item to a non-empty, non-self-referential gate-local artifact with its own SHA-256 fingerprint.

Fresh19 must receive a new exact-source ordinary/race/vet/build, fresh-PG/40-DSN, gateway, governance, topology, release, security, SBOM, traceability and byte-comparison matrix after documentation synchronization. Only then may it be frozen twice and sent to one council as the complete 54-path engineering boundary. The staged index remains empty; no acceptance commit, push, deployment, real-provider traffic, production credential/network mutation, capacity claim, DR claim, penetration-test claim, operational-readiness claim or frontend-completion claim is permitted before adjudication.

The campaign-scoped sender-name authority remains ADR-0006 plus `CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`: reusable account identity is separate from exclusive campaign reservation and requested-versus-provider-observed sender identity. This approved later backend tranche is not silently folded into Fresh19 or claimed implemented.

Fresh19 final executable and governance evidence is GREEN: exact candidate snapshot **2,288 files / 0 byte mismatches**; governance **141/141**; Go ordinary, full race (including the 441.393-second HTTP-server suite), vet and all seven native-libpq command builds; fresh PostgreSQL 17 current/pre-0085/pre-Meta histories through 0090 and the full **40-DSN** repository suite at exit 0; gateway TypeScript/build, control-plane security **32/32**, and **38** retained durability/lifecycle tests; OpenAPI **294/294**; production Compose **7 Railway services / 25 secrets**; resolved Hostinger boundary for BAILEYS and WHATSAPP_WEB_JS; committed-secret scan; Node audits **0/0 vulnerabilities**; strict CycloneDX **30 components / 8 images** with SHA-256 `4cebeb30bcec7a19faad0bc2852b67eb4d7bb9c57582f6827d68f378f1fb3a7f`; and traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL** with byte-identical Markdown and semantically identical JSON regeneration. Production-candidate validation correctly exits 2 because all eight hard gates remain OPEN or BLOCKED_EXTERNAL.

The candidate remains **54 paths**, HEAD `887e577565de22642cf4041a223835d122e6ff15`, branch `work/backend-production-engineering`, with **0 staged paths**. Fresh19 is eligible for deterministic double freeze and one council at this complete boundary; it is not accepted or committed unless adjudication finds no remaining material defect.

## 24 August 2026 — I4 Fresh20 council-remediation boundary

Fresh19 completed one valid blind council at the full 54-path boundary: **8/8 usable lanes**, three actual reviewer families, and three transparently labelled GLM 5.3 fallbacks after unusable primary output. Fresh19 is rejected and remains unstaged. Independent adjudication confirmed nine material or governance defects while rejecting or narrowing overbroad claims: the Railway control origin was not authority-only; TypeScript admitted gateway port zero; Go mishandled IPv6 with explicit default 443; a sibling manifest could be laundered as leaf release evidence; Compose health validation trusted a first-match decoy; invalid URL rejection erased the declared URL; deployed node registration admitted unusable authorities; stale-version DRAINING bypassed nonce/evidence/boot terminality; and README checkpoint authority was stale. The full claim-by-claim record is in the Fresh19 validation evidence directory.

Fresh20 remediates the confirmed set as one coherent backend/infrastructure chunk. Gateway, Go config and resolved topology now share a canonical authority-only HTTPS control origin; full callback/inbound/media endpoints reject credentials, fragments and invalid ports. Gateway advertisement rejects port zero; Go correctly brackets IPv6 after default-port elision. Staging/production node registration enforces the governed authority policy without removing local-development HTTP support. Rejection identity preserves `declaredInternalUrl`. Memory and PostgreSQL route stale-version reports through atomic nonce plus rejection evidence, making an authenticated DRAINING identity terminal. Release verification rejects manifest-shaped leaf evidence, and Compose requires exactly one contract health target. A dedicated `docs/handover/AGENT_PICKUP.md` now provides replacement-agent authority, including sender-name decisions and exact next actions.

Focused Fresh20 evidence is GREEN: gateway control security **34/34**; Linux Go sender/config packages; and deployment-topology plus release-readiness **73/73**. The PostgreSQL conflict regression is authored. Full exact-source ordinary Go, race, vet/build, fresh PostgreSQL histories and 40-DSN suite, gateway TypeScript/build/retained suites, governance/OpenAPI/topology/readiness/security/SBOM/traceability, byte comparison, documentation sync, double freeze, council and adjudication remain mandatory. The candidate is currently **56 dirty paths / 0 staged paths** on HEAD `887e577565de22642cf4041a223835d122e6ff15` and branch `work/backend-production-engineering`; it is not accepted, committed, merged, pushed or deployed.

The campaign-scoped sender identity authority remains ADR-0006 plus the Campaign-Scoped Sender Identity Addendum and is not falsely claimed implemented. All hard release gates remain OPEN or BLOCKED_EXTERNAL; no live deployment, provider traffic, production credential/network mutation, scale proof, DR exercise, penetration test, operational exercise or frontend completion is claimed.

## 25 August 2026 — Fresh20 full local deterministic gate complete; pre-freeze

<!-- FRESH20_FULL_LOCAL_GATE_20260825 -->

Fresh20 remains the active uncommitted I4 remediation candidate on `work/backend-production-engineering` at HEAD `887e577565de22642cf4041a223835d122e6ff15`. The working tree remains intentionally dirty and unstaged. Do not reset, clean, stash, rebase, amend, merge, push or deploy it.

The complete current-source local deterministic matrix is now GREEN: governance **145/145**; Go 1.23.2 Linux/amd64 ordinary tests; full `-race` repository suite; `go vet`; all seven native-libpq command builds; PostgreSQL 17.10 clean, pre-0085 and pre-Meta histories through migration 0090 plus the full **40-DSN** repository suite; gateway TypeScript/build; control-plane security **34/34**; retained OpenWA crash-to-UNKNOWN, durability, outbox **9/9**, session authority **3/3**, drain/lifecycle **18/18**, fencing, serialization, pipeline-bound, retained Baileys and embedded-OpenWA checks; OpenAPI **294/294**; deployment topology/readiness; production Compose **7 Railway services / 25 secret classes**; committed-secret scan; admin and gateway production audits **0 vulnerabilities**; strict CycloneDX **30 components / 8 container images**; traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**, regenerated reproducibly. Production-candidate verification correctly exits **2** because all eight hard release gates remain OPEN or BLOCKED_EXTERNAL.

Fresh20 is **not accepted yet**. Documentation synchronization is now part of the candidate. The mandatory next boundary is: rerun affected post-document static/governance/hygiene checks, build the exact candidate-only snapshot, prove zero byte mismatch, freeze the unchanged candidate twice with identical fingerprints/packet bytes, run one genuinely blind eight-lane council, then independently adjudicate every material finding. Any confirmed defect rejects this freeze and requires RED reproduction, coherent batch remediation, full re-gate, new freeze and fresh review before acceptance.

ADR-0006 plus `docs/requirements/CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md` remain approved later-backend authority for CSI-001..CSI-014. They are deliberately **not** claimed implemented by Fresh20. The core product remains the governed campaign/audience/consent/operations platform; release-one sender-node runtime authority remains OpenWA session-backed infrastructure. Later direct Meta functionality present in source does not redefine that release-one OpenWA node model.

## 26 August 2026 — Fresh20 council remediation and mandatory re-gate

<!-- FRESH20_REMEDIATION_REGATE_20260826 -->

The first Fresh20 freeze was **REJECTED** after blind-council adjudication. Two findings were confirmed and remediated in the same candidate; two material claims were rejected from current source/test evidence. No reviewed freeze authorizes a commit after source changes.

Confirmed remediation 1: non-production governance could admit a governed HTTP OpenWA runtime authority while runtime registration canonicalization was HTTPS-only. This was reachable in the supported local Compose topology, whose gateway authority is `http://openwa-gateway:2785`. The failure was reproduced RED. Runtime registration/governance canonicalization is now mode-aware: staging/production remain canonical HTTPS + allowlist governed, while development/test may use governed HTTP/Docker-local authority. Regressions cover both a private HTTP authority and the actual Compose service authority.

Confirmed remediation 2: a boot with only authenticated `REJECTED` runtime evidence could be followed by an accepted replacement boot, then retry and reclaim node authority because retired-boot fencing counted only accepted REGISTERED/HEARTBEAT history. The reclaim was reproduced RED. Memory and PostgreSQL stores now treat prior authenticated evidence at or before the active boot's registration boundary as evidence that a differing incoming boot is older/retired, while allowing a genuinely newer replacement boot to recover after rejection. Memory and PostgreSQL regressions are GREEN.

Rejected council claims: PostgreSQL runtime events do not insert an empty UUID because `insertRuntimeEvent` assigns `id.New()` when needed; staging is already in the production-strength config validation branch and therefore requires `GATEWAY_RUNTIME_ALLOWED_HOSTS` and governed HTTPS `CONTROL_API_INTERNAL_URL`.

The full remediated acceptance matrix must be rerun before any new freeze. The first Fresh20 freeze (`574fd0822239431e97f4f6e21b02791d0d31f0841c34b949b1e1d04495c49088`) and council are historical evidence only. The next valid boundary is: full deterministic re-gate -> post-doc verification -> new exact double freeze -> fresh blind council on changed bytes using GPT-5.5, Grok 4.6 and GLM as genuine model families -> independent adjudication -> exact accepted staging/commit only if no material finding survives.

ADR-0006 and CSI-001..CSI-014 remain approved later-backend sender-identity authority and are not claimed implemented by Fresh20. The core product remains the governed campaign/audience/consent/operations platform with release-one OpenWA session-backed sender-node runtime authority.

## 27 August 2026 - Fresh20 R3 late-arrival boot remediation

<!-- FRESH20_R3_LATE_ARRIVAL_BOOT_20260827 -->

The Fresh20 R2 freeze was **REJECTED**. Its deterministic gate and double freeze were valid evidence for the reviewed bytes, but GPT-5.5 identified a reachable cross-boot ordering defect and the council did not meet the required usable-lane/model-family floor. No R2 freeze can authorize a commit after source changes.

Confirmed R2 finding: the previous retired-boot remediation fenced already-persisted authenticated evidence using control-plane `OccurredAt`, but a boot could produce a signed runtime report with `ObservedAt=T0`, have that first report delayed, allow replacement boot B to register at `T1`, then deliver boot A's still-valid first report at `T2`. Because A had no prior persisted history, the old R2 fence could accept A and reclaim node authority from B. The sequence was reproduced RED in `TestRuntimeRegistrationRejectsDelayedOlderBootFirstReportAfterReplacement`.

R3 remediation: when a different boot is active and has a registration boundary, both memory and PostgreSQL runtime stores now also treat an incoming signed report whose authenticated `ObservedAt` is at or before the active boot's `RegisteredAt` as older/retired evidence. The existing persisted-history and DRAINING fences remain. A genuinely newer boot may still recover after an initial rejection once its authenticated observation time is after the active registration boundary. Memory and PostgreSQL regressions are GREEN, including `TestPostgreSQLRejectsDelayedOlderBootFirstReportAfterReplacement`, the prior rejection-only-boot regression, and the development HTTP/Compose authority regressions.

R2 council evidence remains historical. The fresh R3 acceptance boundary is: complete full deterministic re-gate on the changed source -> post-document verification -> new exact double freeze -> new blind council using only GPT-5.5, Grok 4.6 and GLM 5.3 genuine model families -> independent adjudication -> exact accepted local commit only if no material finding survives and the reviewer evidence floor is met. Qwen and Gemini are excluded from the active reviewer fleet.

External/live release gates RG-001..RG-008 remain OPEN or BLOCKED_EXTERNAL as previously recorded; Fresh20 does not claim production deployment readiness. ADR-0006 and CSI-001..CSI-014 remain approved later-backend sender-identity authority and are not claimed implemented by this infrastructure remediation.


## 27 August 2026 - Fresh20 R4 post-R3 council remediation full re-gate

<!-- FRESH20_R4_POST_R3_COUNCIL_REGATE_20260827 -->

The Fresh20 R3 freeze is **REJECTED historical evidence only**. Independent adjudication confirmed four reachable defects in the reviewed bytes: pool-unavailable rejection conflated raw declared URL with canonical authority; malformed `gatewayPoolId` could reach the PostgreSQL UUID cast before authenticated rejection/nonce consumption; deployed Go control authority accepted reserved/local hostname classes; and retry-time `ObservedAt` was not a valid cross-boot causal fence. The Hostinger `NODE_ENV=production` finding was rejected as unreachable in the approved manifest, but the topology verifier was hardened to assert that literal explicitly.

R4 preserves the same 56-path candidate and remediates those findings in place. Runtime URL canonicalization now occurs before pool-miss rejection while raw signed declaration evidence remains separate; PostgreSQL pool lookup rejects non-canonical UUID text into the governed authenticated rejection path; staging/production control origins reject local/reserved authority classes; and gateway runtime reports now carry stable signed `bootStartedAt`, validated against process uptime and used by memory/PostgreSQL boot-origin fencing so an older process cannot reclaim authority by retrying with a newer observation timestamp. Focused memory/PostgreSQL/config regressions are GREEN, including malformed pool-ID replay/audit, raw-vs-canonical rejection evidence, reserved deployed control origins, and older-boot retry with newer `ObservedAt`. Gateway control security is **35/35 GREEN**, including stable `bootStartedAt` across heartbeats; governance discovery is **147/147 GREEN**.

Fresh R4 executable evidence on the current non-document source/config bytes is GREEN: ordinary `go test -count=1 ./...` exit 0; full `go test -race -count=1 ./...` exit 0 with `internal/platform/httpserver` 337.532s; `go vet ./...` exit 0; seven native-libpq production command builds GREEN; fresh PostgreSQL 17.10 histories are 141/139/136 public tables with migration 0090 invariant `gateway_pool_id nullable=YES` and exactly one pool-anchor check in each history; the clean 40-DSN repository matrix exits 0; changed Go files are gofmt-clean; and `git diff --check` is GREEN. Gateway authoritative lockfile verification is GREEN after fresh `npm ci` (517 packages audited, 0 vulnerabilities), retained-source sync of 56 pinned files, TypeScript typecheck, and Nest build. A separate cached-symlink dependency fixture produced broad framework type mismatches and was discarded as harness evidence after the fresh-lock reproduction passed.

Supporting R4 gates remain GREEN on the same source bytes: retained OpenWA UNKNOWN/durability/outbox/session/drain/Baileys/fencing/serialization/pipeline/embedded boundary; OpenAPI **294/294**; split deployment topology/readiness; production Compose **7 services / 25 secret classes**; committed-secret scan; admin and gateway production audits with **0 vulnerabilities**; strict local CycloneDX generation **30 components / 8 synthetically pinned container-image references** (generator proof only, not deployed-image evidence); and reproducible traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**. The production-candidate verifier still intentionally exits **2** because RG-001..RG-008 remain OPEN/BLOCKED_EXTERNAL.

No R4 freeze or council is accepted yet. The mandatory next boundary is: post-document governance/static/traceability/secret verification -> exact 2,289-file double freeze with zero byte drift -> fresh blind council on the new packet using GPT-5.5, Grok 4.6 and GLM 5.3 genuine model families with no cross-family fallback -> independent adjudication -> remediate and fully re-gate if any material finding survives; otherwise stage exact accepted paths and create one local commit. No push, deployment, live-provider action or production-readiness claim is authorized by this local gate. ADR-0006 and CSI-001..CSI-014 remain approved later-backend sender-identity authority and are not claimed implemented by Fresh20.


## 28 August 2026 - Fresh20 R5 private-authority remediation and full executable gate

<!-- FRESH20_R5_PRIVATE_AUTHORITY_REGATE_20260828 -->

Fresh20 R4 is **rejected historical evidence only**. Two independent GPT-5.5 blind-review lanes reproduced the same reachable High-severity defect in the R4 freeze: staging/production advertised control and gateway authority accepted private literal IPs (RFC1918 IPv4, IPv6 ULA and IPv4-mapped private IPv6) even though deployed cross-provider authority is intended to be DNS-only. R5 remediates that boundary consistently in Go control configuration, Go runtime registration, the OpenWA gateway TypeScript validator and resolved deployment-topology verification. Development/local network plumbing remains separate; `GATEWAY_BIND_ADDRESS` and governed development HTTP behavior are unchanged.

R5 RED-to-GREEN evidence is explicit. Regression coverage now exercises `10/8`, `172.16/12`, `192.168/16`, `fd00::/8` and IPv4-mapped private IPv6 across Go config/runtime, topology governance and gateway control-plane security. Focused Go config/sender tests are GREEN; gateway control-plane security is **35/35 GREEN**; focused topology is **52/52 GREEN**; full governance discovery is **148/148 GREEN**.

The complete R5 executable acceptance gate is GREEN on an authoritative Linux source volume copied from exactly `git ls-files --cached --others --exclude-standard` and independently verified against its 2,289-file SHA-256 manifest with zero missing, mismatched or extra files. Ordinary `go test -count=1 ./...` exits 0 (`internal/platform/httpserver` 72.703s); full `go test -race -count=1 ./...` exits 0 (`internal/platform/httpserver` 302.243s); `go vet ./...` exits 0; all seven production commands build successfully. The first repeated R5 40-DSN database run was discarded as contaminated fixture evidence because prior interrupted matrices had left fixed-ID integration state. A brand-new PostgreSQL 17.10 R5B fixture was created from scratch and the full 40-DSN repository matrix exits 0 (`internal/platform/httpserver` 31.129s). Fresh migration histories remain 141 / 139 / 136 public tables with `gateway_runtime_events.gateway_pool_id` nullable and exactly one `gateway_runtime_events_pool_anchor_check` in each.

Supporting R5 gates remain GREEN: retained OpenWA UNKNOWN/durability/outbox/session/drain/Baileys/fencing/serialization/pipeline/embedded boundary; OpenAPI **294/294**; deployment topology/readiness; production Compose **7 services / 25 secret classes**; committed-secret scan; admin and gateway production dependency audits **0 vulnerabilities**; gateway fresh-lock `npm ci` **517 packages / 0 vulnerabilities**, retained sync **56** pinned files, TypeScript typecheck and Nest build; strict local CycloneDX generation **30 components**; and reproducible traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**. The production-candidate verifier still intentionally exits **2** because RG-001..RG-008 remain OPEN/BLOCKED_EXTERNAL.

The repository remains intentionally dirty and unstaged on `work/backend-production-engineering` at HEAD `887e577565de22642cf4041a223835d122e6ff15`; the boundary is **56 dirty paths / 0 staged** and `git diff --check` is GREEN. R5 is **not yet accepted**. Mandatory next boundary: post-document governance/static/release/traceability/secret verification -> exact double-freeze -> fresh blind council using genuine GPT-5.5, Grok 4.6 and GLM 5.3 families with the same packet and no cross-family fallback -> independent adjudication -> full remediation/re-gate/re-review if any material finding survives. No push, deployment, live-provider action or production-readiness claim is authorized. ADR-0006 / CSI-001..CSI-014 remain approved later-backend sender-identity authority and are not claimed implemented by Fresh20.


## 30 August 2026 - Fresh20 R6 DNS-only authority remediation full executable acceptance

<!-- FRESH20_R6_DNS_ONLY_AUTHORITY_ACCEPTANCE_20260830 -->

Fresh20 R5 is **rejected historical evidence only**. The R5 blind council reproduced a reachable DNS-authority bypass: legacy numeric IPv4 spellings such as octal, hexadecimal, single-integer and shortened forms could be interpreted as IP addresses by runtime resolvers, and production gateway allowlists could still carry IP-like authorities beside valid DNS names. R6 closes that boundary consistently across Go configuration/runtime registration, OpenWA gateway TypeScript validation, deployment topology verification, and the associated governance/security regressions. Staging/production advertised control and gateway authorities are DNS-only; development/local bind plumbing remains separate and `GATEWAY_BIND_ADDRESS` remains listener-only.

The pre-document executable R6 candidate is 2,289 Git-visible files from `git ls-files --cached --others --exclude-standard`, aggregate SHA-256 `e7a3410cd8a7ece7355f0415eb19495ac2cbc8ad5450adc0ad1e9edfb2ced7d2`, independently verified in the Linux source volume with zero missing, mismatched or extra files. The final post-document freeze hash is recorded in external validation evidence rather than self-embedded in repository text. Fresh executable evidence is GREEN: ordinary `go test -count=1 ./...`; full `go test -race -count=1 ./...`; `go vet ./...`; all seven production commands build; a brand-new PostgreSQL 17.10 R6D three-history fixture plus full 40-DSN repository matrix; governance discovery **149/149**; OpenAPI **294/294**; deployment topology/readiness; production Compose **7 services / 25 secret classes**; committed-secret scan; gateway fresh-lock `npm ci` **517 packages / 0 vulnerabilities**, TypeScript typecheck and Nest build; gateway control security **36/36**; retained OpenWA UNKNOWN/durability/outbox/session/drain/Baileys/fencing/serialization/pipeline/embedded boundary; admin production audit **0 vulnerabilities**; strict CycloneDX generation **30 components**; and traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**. The previously suspicious dispatch race test passed 20/20 focused under `-race` and the full race suite is GREEN.

The production-candidate verifier still intentionally exits **2** because RG-001..RG-008 remain OPEN/BLOCKED_EXTERNAL; this local acceptance does **not** claim production deployment readiness. The mandatory next boundary is exact double-freeze -> fresh blind council across GPT-5.5, Grok 4.6 and GLM 5.3 families -> independent adjudication -> remediate and fully re-gate if any material finding survives; otherwise stage the exact accepted tranche and create one local commit. No push, deployment, live-provider action or production-readiness claim is authorized by this local gate. ADR-0006 / CSI-001..CSI-014 remain approved later-backend sender-identity authority and are not claimed implemented by Fresh20.


## 30 August 2026 - Fresh20 R7 post-council remediation acceptance

<!-- FRESH20_R7_POST_COUNCIL_REMEDIATION_20260830 -->

Fresh20 R6 is rejected historical evidence and must not be committed as accepted. The paid blind council found a reachable Go panic in runtime authority canonicalization: malformed URL input could make `url.Parse` return an error with a nil URL while `parsed.Scheme` was dereferenced first. A focused regression reproduced the nil-pointer panic, the implementation now rejects `err != nil || parsed == nil` before dereference, and the regression is GREEN. The same council identified a case-sensitive `MEDIA_DOWNLOAD_SECRET` placeholder check; focused production-config tests reproduced acceptance of `Development-...` / `DEVELOPMENT-...`, and R7 now lowercases once before checking both `development-` and `change-me`.

Two other council claims were independently adjudicated and rejected. Node 22 WHATWG URL parsing canonicalizes the cited legacy numeric host forms (`0177.0.0.1`, `0x7f.0.0.1`, `010.020.030.050`, `0x0a.0x14.0x1e.0x28`, `167772161`, `10.1`) to canonical IP literals, so the existing TypeScript fallback classifies them as IP-like; the alleged TS bypass did not reproduce. Authenticated rejected DRAINING declarations intentionally make that boot ID terminal: dedicated memory and PostgreSQL regression tests require a new boot ID before READY can re-enter, preserving fail-closed drain/fencing semantics; the Grok alternative policy was therefore not accepted as a defect.

The pre-document executable R7 candidate is 2,289 Git-visible files, aggregate SHA-256 `f1db641058bbb233261a936e1221a35a672f9ca20a4b32cd647995bee24e8643`, independently mirrored into a read-only verifier volume with zero missing, mismatched or extra files. Fresh R7 executable evidence is GREEN: ordinary `go test -count=1 ./...`; full `go test -race -count=1 ./...`; `go vet ./...`; all seven production command builds; fresh PostgreSQL 17.10 three-history fixture plus full 40-DSN matrix; governance 149/149; OpenAPI 294/294; deployment topology/readiness; production Compose; committed-secret scan; gateway fresh-lock `npm ci` / audit / typecheck / build with 0 vulnerabilities; gateway security 36/36; retained OpenWA UNKNOWN/durability/outbox/session/drain/Baileys/fencing/serialization/pipeline/embedded boundary; admin audit 0 vulnerabilities; strict CycloneDX SBOM 30 components; traceability 398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL.

The production-candidate verifier still intentionally exits 2 because RG-001..RG-008 remain OPEN/BLOCKED_EXTERNAL. R7 is a local acceptance candidate only; it does not claim production deployment readiness. The next mandatory boundary is final post-document fingerprint/double-freeze, fresh blind re-review of the remediated bytes, independent adjudication of any material finding, then exact staging and one local commit if the review floor clears. No push, deployment or live-provider mutation is authorized.
