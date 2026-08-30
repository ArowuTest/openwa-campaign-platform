# I4 Fresh15 Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete and accept the I4 runtime/release-governance remediation as one sizeable, exact-source-reviewed engineering chunk.

**Architecture:** Keep runtime URL and pool authority in the Go control plane, retain authenticated rejection evidence in PostgreSQL without inventing pool authority, and keep release verification deterministic. Finish with one full gate and one Fresh15 council rather than reviewing individual file edits.

**Tech Stack:** Go 1.23, PostgreSQL 17, Python governance verifiers, TypeScript/NestJS gateway, Docker-based Linux verification, Git exact-source fingerprints.

**Spec:** `docs/superpowers/specs/2026-08-24-i4-fresh15-remediation-design.md`

## Global Constraints

- Work directly in `C:\Users\sanus\OpenWA\campaign-platform-active\repo` on `work/backend-production-engineering`.
- Preserve all pre-existing modified and untracked candidate files.
- Do not reset, clean, stash, rebase, amend, force, deploy, send provider traffic, or mutate production state.
- Follow RED -> minimal GREEN -> focused regression -> full gate.
- Do not commit per task; the project rule is `ONLY COMMIT ACCEPTED` at the complete I4 boundary.
- Council findings are evidence inputs, not votes or implementation instructions.

---

### Task 1: Establish reproducible local verification

**Files:**
- Read: `go.mod`
- Read: `Makefile`
- Read: `validation/ai-review/infrastructure-i4-fresh14-20260823/I4-fresh14-gate-summary.txt`
- Modify: none

**Interfaces:**
- Consumes: existing Docker/Go/Python validation environment.
- Produces: exact commands for focused Go, PostgreSQL, governance, and full gates.

- [ ] **Step 1: Locate the established Go 1.23.2/PostgreSQL-capable validation path**

  Search the repository and validation tree for the Fresh14 Go and PostgreSQL commands. Prefer already-cached images/toolchains; do not change project dependencies to fit an incompatible host toolchain.

- [ ] **Step 2: Verify the current runtime URL test is RED**

  Run `TestRuntimeRegistrationRejectsGovernedInternalURLDrift` against the unchanged candidate. Expected: failure because the reported sibling URL is accepted or persisted.

- [ ] **Step 3: Verify the governance baseline**

  Run the governance suite with the bundled Python runtime. Record current pass/fail output without changing source.

### Task 2: Enforce governed runtime URL immutability

**Files:**
- Modify: `internal/sender/runtime_registration.go`
- Modify: `internal/sender/runtime_registration_test.go`

**Interfaces:**
- Consumes: `RuntimeRegistrationService.Register`, governed `Node.InternalURL`, normalized `RuntimeReport.InternalURL`.
- Produces: runtime heartbeats that reaffirm or bootstrap URL authority but cannot rewrite it.

- [ ] **Step 1: Add/confirm behavioral tests**

  Preserve the existing drift test. Add explicit success coverage for an equal normalized governed URL and for first registration when the governed URL is blank. The break each test catches is respectively an accidental rejection of legitimate heartbeat and accidental removal of the bootstrap contract.

- [ ] **Step 2: Run the focused tests RED**

  Expected: drift remains RED; legitimate existing behavior remains GREEN.

- [ ] **Step 3: Implement the minimal authority comparison**

  After report normalization and governed pool/provider/engine/adapter checks, compare a non-empty normalized governed URL to `report.InternalURL`. On mismatch, record the existing authenticated `REJECTED` event and return `ErrRuntimeDrift`. Leave a blank governed URL eligible for first registration.

- [ ] **Step 4: Run focused and sender-package GREEN**

  Run the three URL cases and the full `internal/sender` package tests.

### Task 3: Preserve rejection evidence for unassigned nodes

**Files:**
- Create: `database/migrations/0090_gateway_runtime_rejection_evidence.sql`
- Modify: `internal/sender/postgres_governance.go`
- Modify: `internal/sender/runtime_resource_health_postgres_integration_test.go`
- Modify: `internal/sender/runtime_registration_test.go` if memory parity needs explicit coverage

**Interfaces:**
- Consumes: authenticated node ID, optional governed pool, declared pool ID, request hash, rejection reason.
- Produces: one append-only `gateway_runtime_events` row for every authoritative rejected signed report, with nullable `gateway_pool_id` only when `event_type='REJECTED'` and no governed pool exists.

- [ ] **Step 1: Write the PostgreSQL RED test**

  Create a valid node with `gateway_pool_id=NULL`, submit a correctly signed report declaring a nonexistent pool, and assert: first call rejects; second call returns `ErrRuntimeReplay`; exactly one `REJECTED` event exists; stored pool is empty; declared pool remains in `runtime_identity`.

- [ ] **Step 2: Run the integration test RED**

  Expected: `RecordRuntimeRejection` returns `gateway runtime rejection has no governed pool evidence anchor` and no event is stored.

- [ ] **Step 3: Add the narrow schema contract**

  Drop `NOT NULL` from `gateway_runtime_events.gateway_pool_id` while retaining the foreign key. Add a check equivalent to `event_type='REJECTED' OR gateway_pool_id IS NOT NULL` so accepted/heartbeat evidence cannot lose pool authority.

- [ ] **Step 4: Update persistence**

  Insert `NULLIF($3,'')::uuid` for runtime event pool IDs, coalesce nullable pool IDs on read, and permit empty pool only for the rejection path after the governed node lookup proves the node exists.

- [ ] **Step 5: Run migration histories and PostgreSQL GREEN**

  Run clean and upgrade migration paths plus the focused integration test and the full established PostgreSQL matrix.

### Task 4: Make waiver/test governance deterministic

**Files:**
- Modify: `scripts/verify-release-readiness.py`
- Modify: `scripts/verify-deployment-readiness.py`
- Modify: `tests/governance/test_release_readiness.py`
- Modify: `tests/governance/test_deployment_readiness.py`

**Interfaces:**
- Consumes: waiver owner/approver labels and UTC observed/expiry timestamps.
- Produces: canonical maker/checker separation and non-expiring test fixtures.

- [ ] **Step 1: Add case-variant RED tests**

  Use `owner='release-authority'` and `approver='Release-Authority'` and assert both verifiers reject the waiver.

- [ ] **Step 2: Run the new tests RED**

  Expected: current case-sensitive comparison accepts the waiver.

- [ ] **Step 3: Canonicalize actor comparison**

  Compare stripped identifiers with Unicode `casefold()` while preserving original evidence values.

- [ ] **Step 4: Replace the fixed-date acceptance fixture**

  Generate `observed_at` from current UTC minus a small margin and `expires_at` from current UTC plus seven days. Assert verifier behavior, not source text.

- [ ] **Step 5: Run full governance GREEN**

  Run all governance tests, deployment topology/readiness verification, release governance, and production-candidate honesty check. The production candidate must remain non-zero solely because release gates are open/blocked.

### Task 5: Reconcile documentation and full deterministic gate

**Files:**
- Modify: `PROJECT_STATUS_HANDOVER.md`
- Modify: `docs/BUILD_CHECKPOINT.md`
- Modify: `docs/handover/CURRENT_STATUS.md`
- Modify: `docs/handover/NEXT_ACTIONS.md`
- Modify: `docs/program/MASTER_IMPLEMENTATION_ROADMAP.md`
- Modify: `docs/program/BACKEND_ADVERSARIAL_FINDINGS.md`
- Modify: `docs/program/RISK_AND_TECHNICAL_DEBT_REGISTER.md`

**Interfaces:**
- Consumes: verified Fresh14 adjudication and remediation evidence.
- Produces: repository-resident continuation state through the Fresh15 pre-council boundary.

- [ ] **Step 1: Record normalized Fresh14 adjudication**

  Document confirmed URL drift, partially valid unassigned-node evidence, rejected IPv6/staging/compose claims, deterministic fixed-date failure, canonical maker/checker hardening, and the waiver-horizon policy limitation.

- [ ] **Step 2: Run the complete deterministic gate**

  Re-run Go tests/race/vet/build, PostgreSQL migration histories and matrix, gateway TypeScript/security/durability/authority/lifecycle suites, governance/topology/readiness/release checks, OpenAPI parity, secret/security checks, SBOM, traceability byte check, diff hygiene, and staged-index check.

- [ ] **Step 3: Write the gate summary from actual output**

  Do not copy Fresh14 claims. Record exact Fresh15 candidate results and keep all live/external gates honest.

### Task 6: Freeze, review, adjudicate, and accept Fresh15

**Files:**
- Create: `validation/ai-review/infrastructure-i4-fresh15-20260824/freeze_fresh15.py`
- Create: `validation/ai-review/infrastructure-i4-fresh15-20260824/run-i4-fresh15-council.cjs`
- Create: `validation/ai-review/infrastructure-i4-fresh15-20260824/I4-fresh15-adjudication.md`
- Create: generated packet, fingerprints, raw responses, reviews, and summary files in the same evidence directory

**Interfaces:**
- Consumes: exact unstaged/staged/untracked candidate bytes and deterministic gate summary.
- Produces: double-matching source/config fingerprints, blind review lanes, normalized adjudication, and an accepted or remediated next boundary.

- [ ] **Step 1: Double-freeze exact candidate bytes**

  Build the tracked candidate with `git diff HEAD --binary` so staged and unstaged bytes are both represented. Include every untracked candidate file, hash the packet and source/config set, run twice, and require identical fingerprints.

- [ ] **Step 2: Run one eight-lane blind council**

  Send identical packet bytes to the approved Grok, DeepSeek, Gemini, and GLM specialist lanes. Preserve actual model/fallback identity. Require at least five usable lanes, three actual model families, and no more than three fallback lanes.

- [ ] **Step 3: Independently adjudicate**

  Normalize duplicates and classify every material finding. Reproduce valid defects before authorizing changes. Rechallenge only the originating reviewer for rejected findings.

- [ ] **Step 4: Accept or advance the fresh boundary**

  If source/config changes, re-gate and create Fresh16. If no valid defect remains, update the acceptance record, stage only accepted candidate files, inspect the staged diff, and create one local accepted I4 commit.

- [ ] **Step 5: Begin the next backend chunk immediately**

  Start durable delivery reconstruction plus pause/resume/cancellation planning and RED coverage. Do not start infrastructure or frontend until backend acceptance contracts are stable.
