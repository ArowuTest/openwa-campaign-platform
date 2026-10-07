# OpenWA cohort and snapshot acceptance checkpoint — 7 October 2026

## Current decision

**SOURCE ACCEPTED for the bounded cohort/estimate/segment/snapshot tranche.** All applicable source execution gates and review adjudication are closed, including the upload-finaliser privilege repair. This document records pre-rollout source acceptance; the new implementation commit and later GitHub/production deployment receipts are recorded in Git and the latest FS checkpoint. The user explicitly authorized commit, push and production rollout. This acceptance does not cover the whole 67-screen platform, live provider UAT or production-scale certification.

The import/recovery tranche is accepted at `9bae302d4c3c4ea4593ce294440a7cd31b68bb72`. The subsequent cohort, estimate, saved-segment and snapshot candidate has now passed bounded source acceptance. Source acceptance will cover this bounded tranche, not all 67 screens or the whole platform.

### Final source acceptance closure

- Upload-finaliser defect: real audience-worker role failed with SQLSTATE 42501 before the correction (exec-muym9jw4-04e1dc35, exit 1). Corrected bootstrap reads both upload tables and grants exactly ten session lifecycle/result update columns plus parts state solely for existing FOR UPDATE locks. Source SQL SHA-256: bb9d6b9ef769ec1aac1f27094bfbee20a30b4afbf372cb6ae8e0241c5ab3cab8.
- Supplemental execution: exec-muymb9es-317cd692 and exec-muymhg3k-6aba08d0 exit 0. Importer ordinary/race each have 97 passing test results, zero failures and 13 unrelated unconfigured skips; schema has 17 passing results. All new actual-role tests pass without skips. Five normal/race repetitions each have 25 passing results and zero skips. Importer vet, role convergence, forbidden-write SQLSTATE checks and uploaded-part immutability pass.
- Updated-grant governance, secrets and whitespace: exec-muymcief-ac877eca exits 0; 208 governance tests pass. Existing source build/frontend/PostgreSQL proofs below remain bound to the unchanged files.
- Independent atomic upgrade rehearsal: exec-muymi0b7-ebd76d29 exits 0. Exact 0095 + 0096 + corrected 002 applied from a fresh old-94 baseline; forced pre-COMMIT failure restored objects, ACLs and role flags; replay was refused and estimate-role SQL passed. Production bundle SHA-256: 8f56044d0fd3c2ad3d24bf9c9fa99ad65ff74eaccb456cc5e85cee22e07c8b9f. Production execution is a subsequent guarded release step.
- Blind upload correctness review approved on actual paid Grok 4.6; separate security approved on actual governed Nemotron 3 Ultra fallback; Ling 3.1 Flash adjudication approved with no failed dimensions. Final core correctness and root adjudication: Counting kernel council 32a7fbbaf0aa181f0429fd03960c0adc9385fff0719aaeb0ac7f9d6c76f7c21a approved with no findings or failed dimensions (actual Nemotron 3 Ultra). Fresh independent full-path materialisation review found no Critical/Important issue in governance bracketing, current HTTP permissions, completed-estimate binding or atomic PostgreSQL publication; root verified the atomic row-lock/count/hash/token transaction and binder source. Inherited lease expiry enables reclaim: unchanged-token authority persists until takeover/cancellation wins; no documented materialisation hard-expiry contract or duplicate publication was found, so that finding is rejected with rationale. Large external material reviews timed out; direct work-1791411404355-6f7b9c returned partial source analysis on actual Nemotron fallback but ended before a final verdict at its response limit. These are availability/format limitations, not external approvals. Root accepts the bounded source from the executed ordinary/race/role/schema/build/governance evidence and complete independent peer review; no Critical/Important finding remains.
- Production recovery proof at 21:36:35 UTC: six valid matching-cluster basebackups and matching archived-WAL frontier, zero archive failures. Restore execution remains unproven.
- Production read-only inventory at 21:33:34 UTC has zero organisations, reviews, active purposes, active policies, contacts or AUDIENCE_BUILDING campaigns. Default authenticated UI/auth/CSRF/navigation smoke is prepared for the matched rollout. Positive governed persistence/snapshot tests require vetted QA setup and an independently authorised reviewer/approver; no fixture or approval was fabricated.
- Gateway dependency source tests/build/typecheck/audit remain green. Railway rollout does not update Hostinger: its gateway image/provenance and separate runtime release are being handled explicitly.

Historical pending/runtime statements below are retained as evidence history and are superseded by this final source closure. Later runtime success must be established by the actual deployment and smoke receipts.

### Latest gate closure — 7 October, 21:05 UTC

This update supersedes historical pending/runtime-blocked statements later in the evidence history.

- Final materialisation PostgreSQL ordinary and race: `exec-muykpwgh-82abf777`, exit 0, 75 passing test results in each run, zero skipped test cases. Same-count policy replacement before/during membership, actual audience-worker role and frozen snapshot commit all passed.
- Full Linux root-module ordinary/race/vet/build: `exec-muyksz67-f9c0cb89`, exit 0. Ordinary and race each recorded 1,418 passing test results, 49 passing packages, zero failures and 142 explicitly environment-gated skipped test cases. Cohort/materialisation/pagination PostgreSQL lanes were configured; other unconfigured integration/load/OS lanes remain outside this proof. Separate fresh normal/race DBs were used. Full JSON receipts are `.agent/review/final-go-normal-20261007.jsonl` (SHA-256 `8650a964b385e2a870accbbf2073726f2e90a95c7c95b350bdfada8ba3b8adae`) and `final-go-race-20261007.jsonl` (`b9f71803432403dac7b99caf9365f350669f1ee51a4563109685cc0a7440d3ce`). Checked sequential runner SHA-256: `b978c3ba3aa73d9fee793c7f4b520e5fd0a17093e87204f647689f192187a354`.
- Linux governance/OpenAPI: `exec-muyksznq-74266dcd`, exit 0, 208 tests and 306 implemented route pairs. Fresh tracked/untracked secret scan and diff check passed.
- Actual frontend Docker build: `exec-muyl9vip-14eee8da`, exit 0. Local immutable manifest `sha256:0d18ea063ec4382ad55ae5e0e264b89682c5d5fa2ee81b14fac7165f6db4c8d5`. Read-only, unprivileged container health identified admin-web at runtime port 8080. Task container/listener were closed. This is a local image proof, not a Railway deployment digest.
- External review: initial preferred-provider council failed requests; bounded retries completed some security/architecture roles but correctness/adjudication remained unavailable. The large NVIDIA council expired its worker lease without a source result. Root independently inspected and rejected unsupported recovery/provenance/custom-provider/clock/architecture proposals with source rationale; those are not fixed-code claims. Fresh bounded review of the real upload-grant correction is pending. Do not label unavailable/invalid model output as approval.
- Live rollout preparation: all eight Railway application services remain on the older actual deployment revision `f56b6301bed07372f659093601430fff6955462f`; configured source pins are `daa9f3d`, with equivalent application trees. Production has 0094 but lacks 0095/0096 and the new audience-worker grants. Managed PgBackRest catalog proves six matching-system-ID basebackups, latest differential completed 20:05:54 UTC; fresh `pgbackrest check` (`exec-muyl98vk-48ffc828`, exit 0) successfully archived a new WAL segment. On-demand API backup creation returned OAUTH_INSUFFICIENT_GRANT; actual continuous recovery-point evidence is separate. Restore rehearsal remains unproven.
- Production network correction is verified at 21:10 UTC: only the current operator IPv4 /32 was appended to ALLOWED_NETWORK_CIDRS; trusted proxy prefixes and existing admitted networks were preserved. Control-api redeployed its existing accepted older build as deployment `4363e1f7-b90c-44ed-82e8-94fa0403f0b5`. Both actual direct/proxied `/api/v1/auth/me` routes now return 401 `AUTHENTICATION_REQUIRED`, and both health routes return 200. This is anonymous admission proof, not authenticated workflow acceptance.

This checkpoint is a living source/evidence record. Later timestamped FS checkpoints and exact execution receipts govern runtime results; preserve failed attempts and distinguish source, mocked UI, live API, deployment and certification evidence.

## Repository and continuity

- Active worktree: `C:\Users\sanus\OpenWA\campaign-platform-active\repo\.agent\worktrees\large-audience-ingestion-20261006`.
- Branch: `feat/large-audience-ingestion-20261006`.
- Previously accepted import/recovery base: `9bae302d4c3c4ea4593ce294440a7cd31b68bb72`.
- Canonical checkout: `C:\Users\sanus\OpenWA\campaign-platform-active\repo`; do not overwrite its different backend branch.
- External repository: `https://github.com/ArowuTest/openwa-campaign-platform.git`.
- FS V2B New configured root: `fs`. Relative cwd must be supplied explicitly after a thread/tool-state reset.
- Source acceptance precedes the authorized commit/push/deploy sequence. Exact later revisions, deployment IDs and smoke results belong to the release receipt and latest FS checkpoint. The separate network repair reused the older control-api build.
- Explicit 66-file candidate manifest: `.agent/review/candidate-manifest-20261007.json`. Its 63 non-document code/contract/test files have aggregate SHA-256 `1d695ccec5548deb1659889c34c10840502c82ce588ddf0b76548c735dd438b7`. All 60 prior non-SQL implementation files are byte-identical to the earlier full-gate manifest; only the role bootstrap and two new role regression files extend that source proof. The complete manifest includes this document, so its final aggregate belongs in the FS checkpoint. Older 40/41/64-file digests are historical.

A transient “not a git repository” result came from a missing tool-store cwd after context reset; explicitly restoring the active cwd confirmed the original worktree, Git metadata and HEAD intact. Do not reset, recreate or discard the candidate.

## Product and requirement authority

The product is an authenticated in-house operator platform. External clients contact the team through separate channels; operators vet the order and load recipient MSISDNs, content and media. Do not introduce public signup or a client self-service sending portal.

The full SRS and UI/UX specification are the product authority; third-party HTML is a design reference that must be adapted to real backend contracts. The original UX specification contains **67 screens and eight mandatory journeys**. The provided HTML package has approximately 25 screens and is incomplete.

| Source | SHA-256 / companion record |
|---|---|
| `docs/reference/OpenWA_Internal_Campaign_Platform_SRS_v1.0.docx` | `0aa632755b9823940c0e3586cede5ed4e4f6a20d5865ccaae53752a82ded9f8d` |
| `docs/reference/OpenWA_Internal_Campaign_Platform_UI_UX_Design_Specification_v1.0.docx` | `63dc25cd71901bbe3adaef8e76bfef2fd9a0417cb8048d97ebb6fd66281a03c9` |
| All-screen implementation baseline | `docs/program/UI_UX_IMPLEMENTATION_MATRIX.md`; exact screen IDs, classifications, gaps and evidence limits |
| Next bounded implementation plan | `docs/superpowers/plans/2026-10-07-campaign-preparation-foundations.md`; preparatory only |
| Approved architecture and scope | `docs/decisions/ADR-0007-railway-authoritative-postgres.md`; `docs/requirements/BACKEND_FREEZE_RELEASE_DECISIONS.md` |

Root previously read the complete SRS body, full UX specification and designer brief. Reading the documents does not establish that every requirement has been implemented or accepted. The matrix is an evidence baseline, not yet a complete screen × role × state × API × scenario acceptance record.

Preserve the approved operating decisions:

- Railway owns the authoritative Go/control/data plane. Hostinger owns isolated gateway/session/browser runtime. Meta Cloud is a sibling transport.
- Day 1 permits one campaign to use **one engine-specific pool with multiple READY sender numbers**. It is not limited to one number. Baileys and WWebJS/Chromium pools remain separate.
- Mixed-engine/multi-pool campaigns, automatic fallback and automatic reallocation remain deferred pending atomic reservation and ownership fencing.
- UNKNOWN remains sticky and evidence-led; no blind automatic resend or cross-route resend.
- Offline vetting with minimum attributable consent/governance evidence is the Day-1 model. Do not reopen a richer consent-product expansion as a new launch prerequisite.
- Campaign-scoped mutable sender-profile identity remains an explicitly deferred feature.
- The accepted two-million-row import proof is not ten-million-profile or five-million-ledger performance certification.

## Implemented candidate capabilities

The current tranche adds an operator cohort/filter builder, authoritative filter/geography catalogues, durable asynchronous estimates with exclusion stages, saved-segment reopening, campaign-bound snapshot materialisation, progress/cancellation and immutable evidence display.

Backend hardening includes:

1. Bounded whole-transaction Schedule retries for typed PostgreSQL serialization/deadlock failures only; no ambiguous-result or business-error retries.
2. Frequency-cap window semantics `(AsOf - window, AsOf]`, verified at SQL boundaries.
3. Just-in-time one-job estimate claims, cancellation checks, bounded estimate heartbeat context and joined renewal lifecycle.
4. Locked/fenced estimate-result persistence using live owner/version/status/lease evidence and database wall-clock expiry.
5. Exact five-field consent/policy governance revalidation after estimate calculation.
6. Least-privilege audience-worker bootstrap/result/lifecycle grants and real application-role proof.
7. Current actor/RBAC and refreshed filter-registry validation before estimate consumption and materialisation scheduling.
8. Exact-wire normalization for absent/empty rule Children/Values arrays without changing primitives, order or authorization semantics.
9. Both synchronous snapshot handlers revalidate canonical governance after member selection and before snapshot creation.
10. Asynchronous materialisation checks exact governance before querying, before appending members, and before final publication, including the fallback staged-member read.

The five authority fields represent consent review identity/version/wording and organisation-policy identity/version. Equal audience counts cannot substitute for equal authority or member identities.

The materialisation heartbeat implementation itself was not redesigned in this repair; do not generalize the estimate worker's bounded renewal claim to every worker.

## Review and new bounded repairs

The latest HTTP security/correctness source review found no Critical/Important defect in its eight-file scope. A Minor test-hardening suggestion remains: assert zero estimate repository reads on rejected actor/registry requests, not merely zero created jobs.

The materialisation source review confirmed check placement and production wiring, but identified an Important startup configuration gap. Outer service pointers could be populated while inner repositories were nil/typed-nil; a missing policy DB could later panic. A bounded repair adds configuration-presence validation before readiness/claim. It is not a database health probe or arbitrary recursive introspection. Consent PostgreSQL repositories already return errors for nil DB; do not turn this into unrelated repository refactoring.

Startup regression evidence:
- Genuine RED: `exec-muyhp446-f2d1dc48`, exit 1, 14 invalid dependency cases still passed Validate and claimed a real in-memory job.
- Initial GREEN: `exec-muyhsbpq-0f86a53e`, exit 0, all 14 invalid cases reject before claim; three legitimate providers without optional validators remain supported.
- Pure materialisation governance regression: `exec-muyhu2jn-83db2582`, exit 0, 14 selected top-level tests. The independent four-file startup source review closed the Important finding with no new Critical/Important issue.
- Broader affected native run: `exec-muyio3iu-d8e8cf3b`, exit 0, 345 passing test results across materialisation, consent and HTTP plus worker compile. Seven PostgreSQL cases/subcases explicitly skipped; this is not SQL/race acceptance.

A fresh local browser audit found unnamed governed-value/boolean selects and scalar filter-value inputs. Root added only programmatic names, preserving behavior. Latest `filter-builder.tsx` SHA-256 is `9972f4b4131d4e6b03f6ad047c3a005388d16584769b311026670da9b35ae22f`. The final CSS SHA-256 is `7e19c1699d0f0702cf839a5665ea98eb2b72e81fab7cbf51dc625f168f7e5fc9`. Fresh 84-test/typecheck/lint/build checks passed for the unchanged final TypeScript; a subsequent CSS-only build and final browser packet passed. Mobile overflow was separately reproduced at 617px on a 390px viewport, traced to an implicit grid minimum on a manager card, and fixed without hiding content.

The next campaign plan was independently checked against actual source. Corrections preserve frozen provider/gateway bindings on unrelated draft edits; bind purpose to the selected consent review and wording; distinguish static pool reserve deduction from dynamic campaign reservations; and qualify no-op/replay behavior. These are planning repairs, not implemented campaign features.

## Evidence ledger

| Scope | Recorded result | Boundary |
|---|---|---|
| Frontend exact-wire/model/rendered tests | `exec-muyfbih8-e5fd0174`: 50 Node/model + 34 rendered tests, exit 0, no skips | Historical pre-label receipt; superseded by `exec-muyi15ep-b09f2424` at final TypeScript |
| Frontend typecheck | `exec-muyfcuar-e09845a5`, exit 0 | Historical receipt; final `exec-muyi15ep-b09f2424` rerun also passes |
| Frontend lint | `exec-muyfe6vb-251a9ef7`, exit 0 | Historical receipt; final `exec-muyi15ep-b09f2424` rerun also passes |
| Production Next build | `exec-muyfi0i5-b05524a8`, exit 0; 15 static pages, standalone | Historical receipt; final label build `exec-muyi15ep-b09f2424` and CSS build `exec-muyimogb-3900a707` both pass |
| Sync snapshot/wire regressions | `exec-muyfoidg-b5d0bd92`: 7 top-level + 20 subtests, exit 0, no skips | Broader HTTP gate still pending |
| Same-count policy/member swap | `exec-muyg2a60-19ecdcc0`: genuine real-PG RED with before/during rotation; stable/pagination/role positives passed | Old worker incorrectly published P-labelled Q membership; final Linux GREEN pending |
| Resolver/startup adapter tests | `exec-muygb8ju-5c7c8d8c` genuine RED; `exec-muygge32-e705735b` focused native unit GREEN | Later inner-dependency fix supersedes the startup freeze |
| Cohort race suite before materialisation follow-up | `exec-muycze1r-35a0bd78`: 190 top-level + 135 subtests, exit 0, no selected skips/race reports | Does not accept the later worker/API/UI changes |
| Clean schema and role proof | `exec-muycw0de-b8257252`: pre-bootstrap, 96 migrations, post-bootstrap, role SQL proof, exit 0 | Isolated local DB, not production migration |
| Linux gateway tests | `exec-muycfe23-764cfd5b`: 104 tests, exit 0 | Gateway source unchanged since this receipt |
| Gateway typecheck/build/security | `exec-muybyqic-3ca1b5de`, exit 0, production audit 0 | No provider pairing/live send claim |
| Candidate secret/dependency scan | `exec-muye9o65-419fba65`, exit 0; admin all-dependency audit 0 | Final refresh `exec-muyi6dw1-4b1e7931` passed; refresh again after this checkpoint edit |
| Go formatting/whitespace | Native compatible gofmt inspection and `git diff --check` passed during this continuation | Rerun after startup and label/layout changes also passed; final documentation diff check still required |
| Initial local browser | Read-only mock HTTP isolation passed; desktop/mobile rendered | Old RED retained; final `exec-muyiqzf9-58e429d8` exit 0, 28/28 checks, loaded/final mobile 390px, no unnamed controls |

Retain failed/incomplete receipts rather than replacing them with passing labels:

- Earlier full ordinary Go run passed before the final repairs; the replacement race run failed reused-fixture uniqueness/check constraints and a too-short HTTP package budget. Vet/build did not run after its Docker interruption. Use separate fresh ordinary/race DBs and explicit first-party package roots.
- Native Windows Go SQL execution fails `sql: unknown driver "postgres"` because the intentionally cgo-only driver is absent. Native units are useful; native SQL/race is not a valid acceptance substitute.
- Native governance refresh `exec-muyhia5n-217814a3` ran 208 tests but exited 1 with 100 environment errors because tests spawn unavailable `python3`. It did not run the subsequent OpenAPI step.
- Linux governance refresh `exec-muyhmr5f-493ae960` failed before tests because Docker's Linux pipe had been stopped. No GREEN is implied.
- Windows gateway fsync/EPERM failures are retained; do not weaken Linux durability to hide platform behavior.
- Infrastructure/launcher failure is not genuine regression RED.

## Final bounded browser and native-unit receipts

Final browser source isolation covered 55 frontend files, identical before/after: `7b4f656f6918ebc7615c380b0f0f1894dbfbda11e0394560c5868e47b889b009`. Desktop document width remained 1440px; mobile initial/loaded/final document width remained 390px. Tables retained local horizontal scrolling. All 28 control checks passed. Loaded-main axe-core 4.13.0 label/select-name checks had zero violations/incomplete results at both viewports; governed initial-state audits also had zero violations. This is bounded evidence, not full accessibility certification.

| Final ignored receipt | SHA-256 |
|---|---|
| `.agent/qa/audience-browser-qa-20261007-final-playwright.json` | `721f1927205aa8d62d7583e589fc735ddcf43893f7c1df296aa6b6ebcb3fbf0c` |
| `.agent/qa/audience-browser-qa-20261007-final-governed.json` | `4a2b24a46018629f7565db9191bcf5126f5800daa9b112ebdfbe68ba831761cb` |
| `.agent/qa/audience-browser-qa-20261007-final-cleanup.json` | `a3472ca6b633d71afcec21d97ebe773b6a00a2e4e7cc0c5b403a3137aa7ba4e8` |

The governed console retained one generic resource 404 without a corresponding network entry. Deliberately blocked mutation requests returned 405 and emitted expected console errors. No uncaught page error or outside-origin request was observed. QA browsers, exact task servers and ports 18182/18183 are closed; no cached test Chromium remains.

Native affected suites produced 345 passing test results and zero failures. The seven skipped PostgreSQL cases/subcases were same-count materialisation replacement before/during membership; audience-worker-role stable materialisation; frozen snapshot commit; consent opt-out metric replay; authenticated Meta webhook persistence; and encrypted/masked sender registration. A parent test whose children all skipped is not database proof. Raw JSON receipt: `.agent/review/native-affected-final-20261007.jsonl`.

Final source reviews found no Critical/Important issue in the bounded HTTP authorization/wire, materialisation-governance/startup, or final UI-label/containment scopes. The corrected next campaign plan's three P2 findings and replay clarification are closed. These verdicts do not accept all 64 candidate files or the whole 67-screen product.

## Test runtime and resource rules

The shared machine is also building another project. Its deliberate Docker Desktop stop/WSL shutdown commands repeatedly remove the Linux engine while OpenWA verifiers are starting. Root observed explicit desktop-cli quit entries and active other-build sessions. Do not stop/kill that unrelated work or repeatedly restart Docker while it owns the runtime.

Primary isolated test runtime:
- Existing PostgreSQL container `openwa-upload-pg17`, PostgreSQL 17, host 55495 → container 5432.
- Existing warm image `openwa-step3-go-warm:local`, Go 1.23.2, cgo/libpq enabled; image ID `sha256:8904ba8f437692b61e885ee2e1576d27a25768c56cbf38d6c05f727c8b92b2f4`.
- One heavy Go verifier at a time; two CPUs, 1200 MiB, GOMAXPROCS=2, readonly source mount, `-mod=readonly -p 1`.
- Retrieve only synthetic test-container credentials in process memory. Never print/write a DSN or Docker Config.Env.
- Keep the reserved final normal/race databases pristine until their respective run. Existing test DB/container/image/volumes are not to be deleted.

Databases already bootstrapped with 96 migrations and role grants:
- `openwa_materialisation_governance_review_20261007`: dedicated focused worker lane.
- `openwa_cohort_final_normal_20261007`: reserved final ordinary lane.
- `openwa_cohort_final_race_20261007`: separate reserved final race lane.

Native fallback:
- Cached Go 1.23.11 Windows binary under `C:\Users\sanus\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.23.11.windows-amd64\bin`.
- Process-only GOTOOLCHAIN=local, GOPROXY=off; no global upgrade/go.mod downgrade.
- CGO is unavailable; do not claim native SQL/race acceptance or add a driver bypass.
- Task-owned native PostgreSQL test cluster under `.agent/qa/pg17-native-cohort-20261007`, port 55497, was shut down after proving it cannot solve the cgo limitation. Do not alter user PostgreSQL services.
- Node 22.15.0, Python 3.12 available natively. Do not globally upgrade tools.

The ignored final script `.agent/review/run-final-cohort-gates-20261007.ps1` runs explicit `./cmd/... ./internal/... ./tests/...` ordinary/race/vet/build with separate DBs and a 900-second package budget. Avoid `./...` scans through installed node_modules/nested vendor modules. Additional environment-gated suites may remain unconfigured; disclose their skips rather than claiming all external integrations were exercised.

## Local browser evidence rules

Ignored read-only harness `.agent/qa/openwa_builder_ui_mock.py` fronts loopback 18182 and standalone 18183. API reads use synthetic QA data; unknown APIs return 404 and all mutations 405. No real auth cookie/credential or API request is forwarded; image optimizer requests are blocked. Production upstream guards were not changed.

Local mock controls/audits are separate from real PostgreSQL/HTTP workflow proof. Record exact tool/browser/source and state; a one-state accessibility audit is not full WCAG 2.2 AA, keyboard-flow or mandatory-journey acceptance. A failed QA locator is not a product defect. Close only task-owned browser/server processes.

## Historical verification and remaining sequence before final source closure

Completed: startup RED/GREEN/regression and source-review closure; frontend 84 tests/typecheck/lint/build; exact generated next-env baseline restoration; final CSS build; hash-verified static refresh; final bounded browser label/responsive/control checks. All task-owned QA resources were closed. The explicit candidate manifest is saved; final documentation edits require its aggregate to be refreshed.

The remaining steps are:
1. When the Linux runtime is available without competing shutdowns, run focused materialisation real-PG ordinary/race against its dedicated lane.
2. Run final root-module ordinary and race suites against the two separate pristine DBs; inspect selected skips, failure output and race reports. Then vet/build.
3. Refresh applicable Linux governance, OpenAPI and tracked+untracked secret checks. Do not mistake a skipped or failed launcher for a pass.
4. Preserve scoped review closures at exact hashes; verify no new Critical/Important finding and retain the Minor authorization-read assertion suggestion.
5. Refresh the ordinal-sorted explicit path/SHA manifest covering every changed/untracked candidate file. Hash UTF-8 `path:lowercaseSHA` lines joined by LF without trailing newline; avoid self-referential documentation digests.
6. Update this record/matrix and durable FS checkpoint/project context with all receipts, blockers and exact manifest.
7. Only after acceptance, stage the explicit frozen file set, recheck hashes/secrets and commit locally. No push/deploy is implied.

## Next implementation after this tranche is accepted

Implement the three independently reviewable campaign foundations in the source-reviewed plan:

1. Direct `GET /api/v1/campaigns/{id}`; remove the UI's scan of up to 20 inventory pages.
2. Dedicated guarded persistence for governed DRAFT save/edit/reload, optimistic conflicts, immutable source identity and atomic audit event; existing notes/tags and later material amendment are not this capability.
3. Separate read-only saved-campaign preparation readiness. Do not reuse execution Plan or campaignworkspace.Get because they can write. Show blockers and advisory capacity honestly; preserve exact transport, purpose/review, content/snapshot and commercial evidence.

The plan does not implement arbitrary pre-governance incomplete drafts or all journey-D wizard screens. Follow-on work includes composer/media/versioning, masked personalization preview, controlled test, capacity hold, commercial/content/final review, timezone scheduling and instant governed release.

Live-provider pairing, text/media send/receipt, inbound, reconnect/recovery, full operator UAT, load/scale, backup/restore and production release evidence remain separate outstanding work. The platform is not “all backend/infra done, only testing left.”
