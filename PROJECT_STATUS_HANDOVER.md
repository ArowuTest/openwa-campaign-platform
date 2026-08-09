# OpenWA Campaign Platform — Project Status & Handover

**Last updated:** 2026-08-08 22:42 (Europe/London)
**Repository:** `C:\Users\sanus\OpenWA\campaign-platform-active\repo`
**Branch:** `work/backend-production-engineering`
**Committed HEAD:** `c728cfa8bd8ff6ff0a95068602ef575982eb9a8e`
**HEAD subject:** `feat: close Task 4 pagination and gateway telemetry`

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
