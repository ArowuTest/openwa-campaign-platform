# Campaign Execution Lifecycle Atomicity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make each execution lifecycle action atomically commit campaign state, its required routing-reservation mutation, and version-linked execution evidence.

**Architecture:** Existing Go domain validation prepares the next campaign state without persisting it. The execution coordinator builds one lifecycle commit request, and a PostgreSQL committer applies campaign CAS, reservation mutation, and event insertion in one serializable transaction with bounded concurrency retries.

**Tech Stack:** Go, `database/sql`, the repository's internal PostgreSQL driver, PostgreSQL migrations, Docker, Node/TypeScript gateway verification.

## Global Constraints

- Preserve the intentionally dirty cumulative Task 6 worktree; do not reset, clean, stash, or discard unrelated changes.
- Do not commit or push unless the user separately authorizes it.
- Use migration `0073`; never rewrite an applied migration.
- Keep external gateway/network actions outside this transaction.
- Keep approval/build campaign transitions on the existing repository path.
- Require production execution actions to fail closed when the atomic committer is absent.
- New lifecycle events carry the resulting campaign version; historical and diagnostic events may retain a null version.
- Use RED tests before each behavior change.
- Run Go through `campaign-task2-go-builder:latest` using `/usr/local/go/bin/go` and `sh -c`.

---

## File map

- Create `internal/execution/lifecycle.go`: execution-layer commit contract and invariants.
- Create `internal/campaign/service_prepare_transition_test.go`: non-persisting transition preparation tests.
- Modify `internal/campaign/service.go`: share validation between prepare and persist paths.
- Modify `internal/execution/service.go`: route all five lifecycle actions through one committer.
- Modify `internal/execution/service_test.go`: lifecycle mapping and fail-closed unit tests.
- Modify `internal/execution/start_reservation_recovery_test.go`: replace compensation expectations with atomic commit expectations.
- Create `database/migrations/0073_campaign_execution_lifecycle_atomicity.sql`: version-linked event schema.
- Modify `internal/persistence/postgres/campaign.go`: executor-aware campaign CAS helper.
- Modify `internal/execution/routing_postgres.go`: transaction-aware reservation helpers with ownership/state checks.
- Modify `internal/execution/postgres.go`: transaction-aware lifecycle-event insertion.
- Create `internal/execution/lifecycle_postgres.go`: serializable atomic committer and bounded retry.
- Create `internal/execution/lifecycle_postgres_integration_test.go`: success, rollback, and concurrency proof.
- Modify `cmd/control-api/runtime.go`: wire the committer.
- Modify `cmd/campaign-worker/main.go`: wire the same committer.
- Create `internal/platform/httpserver/campaign_execution_atomicity_test.go`: route-level fail-closed regression.
- Modify `docs/program/BACKEND_ADVERSARIAL_FINDINGS.md` and `PROJECT_STATUS_HANDOVER.md`: implementation evidence and checkpoint.

---

### Task 1: Non-persisting campaign transition preparation

**Files:**
- Create: `internal/campaign/service_prepare_transition_test.go`
- Modify: `internal/campaign/service.go`

**Interfaces:**
- Produces: `func (s *Service) PrepareTransition(context.Context, string, TransitionInput) (Campaign, error)`
- Preserves: `func (s *Service) Transition(context.Context, string, TransitionInput) (Campaign, error)`

- [ ] **Step 1: Write the failing preparation test**

Create a repository spy containing a scheduled campaign. Call `PrepareTransition` with `ActionStartDispatch` and assert the returned campaign is `DISPATCHING`, version is incremented once, and `CompareAndSwap` was never called.

```go
prepared, err := service.PrepareTransition(ctx, entity.ID, TransitionInput{
    Action: ActionStartDispatch, ActorID: actorID,
    ExpectedVersion: entity.Version,
})
if err != nil { t.Fatal(err) }
if prepared.Status != StatusDispatching || prepared.Version != entity.Version+1 {
    t.Fatalf("unexpected prepared transition: %+v", prepared)
}
if repo.compareAndSwapCalls != 0 {
    t.Fatal("PrepareTransition persisted campaign state")
}
```

- [ ] **Step 2: Prove RED in the builder**

```powershell
docker run --rm -v "C:\Users\sanus\OpenWA\campaign-platform-active\repo:/repo" -w /repo campaign-task2-go-builder:latest sh -c "/usr/local/go/bin/go test ./internal/campaign -run TestServicePrepareTransition -count=1"
```

Expected: compile failure because `PrepareTransition` does not exist.

- [ ] **Step 3: Extract the validation path**

Move the body of `Transition` through domain transition into `PrepareTransition`. Make `Transition` call `PrepareTransition` and then `repository.CompareAndSwap(ctx, prepared, input.ExpectedVersion)`. Do not duplicate organisation, consent, provider, gateway, commercial, or domain validation.

- [ ] **Step 4: Run focused and package tests**

Run the focused command, then replace `-run ...` with the full `./internal/campaign` package. Expected: PASS.

- [ ] **Step 5: Checkpoint without committing**

Run `git diff --check` and inspect only the Task 1 diff.

---

### Task 2: Lifecycle contract and coordinator cutover

**Files:**
- Create: `internal/execution/lifecycle.go`
- Modify: `internal/execution/service.go`
- Modify: `internal/execution/service_test.go`
- Modify: `internal/execution/start_reservation_recovery_test.go`

**Interfaces:**
- Consumes: `CampaignService.PrepareTransition(...)` from Task 1.
- Produces:

```go
type ReservationOperation string
const (
    ReservationNone ReservationOperation = "NONE"
    ReservationActivate ReservationOperation = "ACTIVATE"
    ReservationRelease ReservationOperation = "RELEASE"
)
type LifecycleCommit struct {
    Campaign campaign.Campaign
    ExpectedVersion int64
    Action campaign.Action
    EventType string
    ActorID string
    Reason string
    Details map[string]any
    RoutingPlanID string
    ReservationOperation ReservationOperation
    OccurredAt time.Time
}
type LifecycleCommitter interface {
    Commit(context.Context, LifecycleCommit) (campaign.Campaign, error)
}
```

- [ ] **Step 1: Write RED coordinator tests**

Add a recording committer and table-driven tests asserting:

- start -> `ACTIVATE` plus `DISPATCH_STARTED` when a plan exists;
- start -> `NONE` without a routing plan;
- pause -> `NONE` plus `CAMPAIGN_PAUSED`;
- resume -> `NONE` plus `CAMPAIGN_RESUMED`;
- cancel -> `RELEASE` plus `CAMPAIGN_CANCELLED` when a plan exists;
- complete -> `RELEASE` plus the assessed terminal event;
- nil committer fails before campaign persistence; and
- admission/precheck rejection never calls the committer.

Assert `fakeStore.events == 0`; lifecycle evidence must no longer use standalone `RuntimeStore.RecordEvent`.

- [ ] **Step 2: Prove RED**

```powershell
docker run --rm -v "C:\Users\sanus\OpenWA\campaign-platform-active\repo:/repo" -w /repo campaign-task2-go-builder:latest sh -c "/usr/local/go/bin/go test ./internal/execution -run 'TestCoordinator.*Atomic|TestCoordinator.*Commit' -count=1"
```

Expected: compile/test failure because the contract and cutover do not exist.

- [ ] **Step 3: Implement the minimal contract and validation**

`LifecycleCommit.Validate` must reject empty campaign ID, non-positive expected version, a candidate version other than `expected+1`, empty event type/actor, unknown reservation operation, and `ACTIVATE`/`RELEASE` without a routing plan ID.

- [ ] **Step 4: Replace legacy lifecycle sequences**

For each action, complete prechecks first, call `PrepareTransition`, resolve any routing plan before persistence, build one `LifecycleCommit`, and call `Committer.Commit`. Remove start activation/compensation and cancel/complete standalone release/event calls. Keep runner diagnostic `RecordEvent` calls unchanged.

- [ ] **Step 5: Replace compensation tests**

Rewrite `start_reservation_recovery_test.go` to prove a committer failure leaves the fake campaign and reservations unchanged and a successful commit records one `ACTIVATE` operation. Remove tests whose only purpose was repairing the old split-commit path.

- [ ] **Step 6: Run execution tests**

Run `go test ./internal/execution -count=1` in the builder. Expected: PASS.

- [ ] **Step 7: Checkpoint without committing**

Run `git diff --check` and inspect the coordinator diff for any remaining lifecycle `RecordEvent`, `Activate`, or `Release` calls.

---

### Task 3: Version-linked event migration and transaction-aware SQL helpers

**Files:**
- Create: `database/migrations/0073_campaign_execution_lifecycle_atomicity.sql`
- Modify: `internal/persistence/postgres/campaign.go`
- Modify: `internal/execution/routing_postgres.go`
- Modify: `internal/execution/postgres.go`

**Interfaces:**
- Produces an internal campaign CAS helper accepting an `ExecContext` executor.
- Produces:

```go
func (s *PostgreSQLRoutingPlanStore) ActivateReservationsInTx(
    context.Context, *sql.Tx, string, string, time.Time,
) error
func (s *PostgreSQLRoutingPlanStore) ReleaseReservationsInTx(
    context.Context, *sql.Tx, string, string, string, time.Time,
) error
func (s *PostgreSQLStore) RecordLifecycleEventInTx(
    context.Context, *sql.Tx, LifecycleCommit,
) error
```

- [ ] **Step 1: Write the migration**

```sql
BEGIN;
ALTER TABLE campaign_execution_events
    ADD COLUMN campaign_version bigint NULL,
    ADD CONSTRAINT chk_campaign_execution_event_version
        CHECK (campaign_version IS NULL OR campaign_version > 0);
CREATE UNIQUE INDEX uq_campaign_execution_event_lifecycle_version
    ON campaign_execution_events(campaign_id, campaign_version)
    WHERE campaign_version IS NOT NULL;
COMMIT;
```

- [ ] **Step 2: Add RED helper/integrity tests**

Add tests proving a lifecycle event requires and persists `Campaign.Version`, duplicate version-linked evidence is rejected, historical null-version diagnostic events remain insertable, and transaction helpers reject a routing plan belonging to another campaign.

- [ ] **Step 3: Prove RED**

Run focused `./internal/execution` and `./internal/persistence/postgres` tests in the builder. Expected: missing method/schema failures.

- [ ] **Step 4: Refactor campaign CAS without changing SQL semantics**

Define a small internal `campaignExecutor` interface with `ExecContext`. Move the existing CAS body to `compareAndSwap(ctx, executor, candidate, expected)`; keep public `CompareAndSwap` delegating to it with `r.DB`. The lifecycle committer in the same package will pass `*sql.Tx`. Preserve the nullable transport behavior already covered by `campaign_repository_postgres_integration_test.go`.

- [ ] **Step 5: Add reservation transaction helpers**

Share each standalone and in-transaction path through executor/query helpers. Inside the transaction: lock the plan and reservation rows; verify plan ownership; accept only the exact state sets approved in the design; advance fencing only when status changes; and return `ErrRoutingPlanConflict` for mixed, unsafe, missing, or wrong-campaign data.

`ReleaseReservationsInTx` receives both actor and campaign ID. It must retain the T6-019 campaign-status guard while observing the terminal candidate status written earlier in the same transaction.

- [ ] **Step 6: Add lifecycle event insertion**

Marshal event details before transaction work and insert `campaign_version=commit.Campaign.Version`. Keep `RecordEvent` unchanged for runner diagnostics so those rows use null campaign version.

- [ ] **Step 7: Run focused regressions**

Run:

```text
go test ./internal/execution -count=1
go test ./internal/persistence/postgres -count=1
```

Expected: PASS, including routing-release and nullable-transport regressions.

- [ ] **Step 8: Validate migration syntax and checkpoint**

Run `python3 scripts/validate-postgres-migrations.py` in the Go verification container with `DATABASE_URL` set to the disposable PostgreSQL DSN, then run `git diff --check`. Do not apply migration 0073 to the long-lived database before the fresh-database test in Task 6.

---

### Task 4: PostgreSQL atomic lifecycle committer

**Files:**
- Create: `internal/execution/lifecycle_postgres.go`
- Create: `internal/execution/lifecycle_postgres_integration_test.go`

**Interfaces:**
- Consumes: `execution.LifecycleCommit`, campaign CAS helper, routing transaction helpers, event transaction helper.
- Produces:

```go
type PostgreSQLLifecycleCommitter struct {
    DB *sql.DB
    Retry RetryPolicy
}
func (c *PostgreSQLLifecycleCommitter) Commit(
    context.Context, execution.LifecycleCommit,
) (campaign.Campaign, error)
```

- [ ] **Step 1: Write the first rollback RED test**

Create a real PostgreSQL fixture with a `DISPATCHING` campaign and `ACTIVE` reservation. Install a test-only trigger that raises when a version-linked `CAMPAIGN_CANCELLED` event is inserted. Invoke cancel commit and assert: campaign remains `DISPATCHING` at the old version, reservation remains `ACTIVE` at the old fence, and no lifecycle event exists.

```go
_, err := committer.Commit(ctx, cancelCommit)
if err == nil { t.Fatal("expected injected event failure") }
assertCampaign(t, db, campaignID, "DISPATCHING", expectedVersion)
assertReservation(t, db, reservationID, "ACTIVE", expectedFence)
assertLifecycleEventCount(t, db, campaignID, 0)
```

- [ ] **Step 2: Prove RED**

Run the exact integration test with `POSTGRES_EXECUTION_DATABASE_URL` set. Expected: compile failure because `PostgreSQLLifecycleCommitter` does not exist.

- [ ] **Step 3: Implement one serializable attempt**

Validate and JSON-serialize before `BeginTx`. In the transaction call campaign CAS first, reservation helper second, and version-linked event insert third. Roll back on every error and return the candidate only after `tx.Commit` succeeds.

- [ ] **Step 4: Add bounded retry**

Wrap the complete transaction attempt with existing `RetryValue(ctx, policy, operation)`. Retry only SQLSTATE `40001` and `40P01`; business conflicts and constraints return immediately. Each retry creates a new transaction.

- [ ] **Step 5: Make the rollback test GREEN**

Rerun the exact integration test. Expected: PASS and all three assertions retain pre-request state.

- [ ] **Step 6: Add the reservation-failure rollback test**

Use a wrong-campaign or mixed-state plan so reservation verification fails after campaign CAS. Assert campaign rollback and zero version-linked events.

- [ ] **Step 7: Add the lifecycle success matrix**

Table-drive start, pause, resume, cancel, and complete commits. For each case assert the resulting campaign status/version, required reservation status/fence behavior, event type, event campaign version, actor, reason, and details.

- [ ] **Step 8: Add concurrency proof**

Prepare two identical start commits with the same expected campaign version and run them concurrently. Assert exactly one success, one `campaign.ErrConflict`, one campaign version increment, reservations `ACTIVE` with one fence increment, and one version-linked event.

- [ ] **Step 9: Add compatibility cases**

Prove an all-`ACTIVE` activation and all-`RELEASED` release are accepted without another fence increment; prove mixed/expired sets fail closed; prove a historical null-version diagnostic event survives.

- [ ] **Step 10: Run packages and checkpoint**

Run `go test ./internal/persistence/postgres ./internal/execution -count=1`, then `git diff --check`. Expected: PASS.

---

### Task 5: Runtime wiring and route-level fail-closed behavior

**Files:**
- Modify: `cmd/control-api/runtime.go`
- Modify: `cmd/campaign-worker/main.go`
- Create: `internal/platform/httpserver/campaign_execution_atomicity_test.go`

**Interfaces:**
- Consumes: `execution.PostgreSQLLifecycleCommitter` from Task 4.
- Produces: both production coordinators configured with the same atomic persistence behavior.

- [ ] **Step 1: Write the RED HTTP regression**

Construct an execution coordinator with no committer and invoke `/api/v1/campaigns/{id}/execution/start` through the server. Assert it returns a controlled service/conflict error and leaves the campaign unchanged. Then configure a recording committer and assert one successful route call invokes it exactly once.

- [ ] **Step 2: Prove RED**

```text
go test ./internal/platform/httpserver -run TestCampaignExecutionRequiresAtomicCommitter -count=1
```

Expected: failure because the legacy coordinator can still transition without the committer.

- [ ] **Step 3: Wire both binaries**

Instantiate one `&execution.PostgreSQLLifecycleCommitter{DB: db, Retry: postgresrepo.DefaultRetryPolicy()}` per process and assign it to `execution.Coordinator.Committer` in control API and campaign worker. Do not create a second database connection or alter gateway wiring.

- [ ] **Step 4: Run focused packages**

Run `go test ./cmd/control-api ./cmd/campaign-worker ./internal/platform/httpserver -count=1`. Expected: PASS.

- [ ] **Step 5: Static production-path audit**

Search `internal/execution/service.go` for lifecycle calls to `Campaigns.Transition`, `RoutingPlans.Activate`, `RoutingPlans.Release`, and lifecycle `Store.RecordEvent`. Expected: none. Search runtime constructors for `execution.Coordinator`; expected: every production constructor supplies `Committer`.

- [ ] **Step 6: Checkpoint without committing**

Run `git diff --check` and inspect both runtime diffs.

---

### Task 6: Fresh PostgreSQL migration and adversarial integration gate

**Files:**
- Test: `database/migrations/0073_campaign_execution_lifecycle_atomicity.sql`
- Test: `internal/execution/lifecycle_postgres_integration_test.go`

**Interfaces:**
- Validates the complete persisted behavior on a disposable database migrated from zero through 0073.

- [ ] **Step 1: Create a disposable database safely**

Resolve the active PostgreSQL container with `docker compose -p openwa0828active -f infrastructure/compose/compose.production.yaml ps -q postgres`. Create a uniquely named database such as `campaign_task6_atomicity_YYYYMMDD_HHMM`; never target the long-lived campaign database.

- [ ] **Step 2: Apply all 73 migrations**

Use the repository's established migration procedure against only the disposable database. Verify the migration ledger contains 73 applied migrations and `campaign_execution_events.campaign_version` plus `uq_campaign_execution_event_lifecycle_version` exist.

- [ ] **Step 3: Run the integration package against it**

Set `POSTGRES_EXECUTION_DATABASE_URL` to the disposable database DSN and run:

```text
/usr/local/go/bin/go test ./internal/persistence/postgres ./internal/execution -run 'TestPostgreSQLExecutionLifecycle|TestPostgreSQLReleaseReservations' -count=1
```

Expected: all success, rollback, concurrency, compatibility, and T6-019 release-guard tests PASS.

- [ ] **Step 4: Run migration validator**

Run `python3 scripts/validate-postgres-migrations.py` against the disposable database. Expected: schema and migration validation PASS.

- [ ] **Step 5: Remove disposable resources**

Terminate test connections, drop only the uniquely named disposable database, and remove any verifier container. Confirm no `campaign_task6_atomicity_*` database and no `openwa-task6-*` container remains.

- [ ] **Step 6: Check active stack health**

Run `docker compose -p openwa0828active -f infrastructure/compose/compose.production.yaml ps`. Expected: all 13 active services healthy.

---

### Task 7: Full regression, evidence, and handover

**Files:**
- Modify: `docs/program/BACKEND_ADVERSARIAL_FINDINGS.md`
- Modify: `PROJECT_STATUS_HANDOVER.md`

**Interfaces:**
- Produces the final T6-020 evidence record and next Task 6 audit checkpoint.

- [ ] **Step 1: Full first-party Go gate**

Run in `campaign-task2-go-builder:latest`:

```text
/usr/local/go/bin/go test ./... -count=1
/usr/local/go/bin/go vet ./...
/usr/local/go/bin/go build ./...
```

Expected: PASS. If an unrelated package fails, diagnose it; do not suppress or misreport it.

- [ ] **Step 2: Focused race coverage**

Run `go test -race` for `./internal/campaign`, `./internal/execution`, and `./internal/persistence/postgres` where the builder/runtime supports it. Record any environmental limitation explicitly.

- [ ] **Step 3: Gateway regression gate**

Using an isolated copy of `services` and `scripts` from current source plus the production image's `/app/node_modules`, run strict `tsc --noEmit`, `scripts/test-gateway-outbox-integrity.js`, `scripts/test-gateway-session-authority.js`, `scripts/test-gateway-session-drain.js`, and the embedded-engine test. Expected: all existing tests PASS.

- [ ] **Step 4: Contract and migration checks**

Run `python3 scripts/verify_openapi_routes.py`, migration validation, and any repository `scripts/check.sh` portions available in the verification images. No OpenAPI execution request/response change is expected.

- [ ] **Step 5: Worktree and resource hygiene**

Run `git diff --check`, `git status --short --branch`, active Compose health, temporary-container listing, and disposable-database listing. Preserve all intentional Task 6 files; remove only resources created by this plan.

- [ ] **Step 6: Update findings**

Change T6-020 from `OPEN` to `FIXED` only if the fresh atomicity, rollback, concurrency, and full regression gates pass. Cite exact test names, migration 0073, production wiring, and verification results.

- [ ] **Step 7: Update handover**

Append a timestamped checkpoint to `PROJECT_STATUS_HANDOVER.md` containing branch/HEAD, dirty-worktree warning, T6-020 guarantee, files changed, exact RED/GREEN evidence, fresh database name and cleanup confirmation, 13-service health, remaining Task 6 review scope, and the next exact action.

- [ ] **Step 8: Final evidence review**

Confirm every acceptance criterion in the approved design maps to observed evidence. Do not claim Task 6 complete merely because T6-020 is fixed; continue the remaining adversarial audit and final release-readiness matrix.

- [ ] **Step 9: Leave changes uncommitted**

Do not commit or push. Report the completed changes, verification evidence, any blocker, and the exact remaining Task 6 state to the user.

---

## Plan self-review checklist

- Spec coverage: preparation seam, atomic commit, reservation invariants, version-linked evidence, retry, rollback, concurrency, runtime wiring, migration compatibility, full verification, findings, and handover are all assigned.
- Completeness scan: the plan contains no incomplete markers or unspecified implementation steps.
- Type consistency: `LifecycleCommit`, `LifecycleCommitter`, `ReservationOperation`, transaction helper signatures, and `PostgreSQLLifecycleCommitter` are defined once and consumed consistently.
- Scope: no gateway redesign, distributed transaction, endpoint idempotency-key change, generic audit rebuild, or frontend work is included.
- Worktree safety: all checkpoints preserve the cumulative uncommitted Task 6 changes and prohibit commit/push without separate authorization.
