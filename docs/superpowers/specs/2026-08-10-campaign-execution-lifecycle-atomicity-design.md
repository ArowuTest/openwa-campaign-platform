# Campaign Execution Lifecycle Atomicity Design

**Date:** 2026-08-10
**Status:** Approved architecture; written specification awaiting review
**Finding:** T6-020

## Purpose

Eliminate partial campaign lifecycle commits. A lifecycle action must never change campaign state without also applying its required routing-reservation mutation and recording its execution evidence.

## Scope

This design covers `START_DISPATCH`, `PAUSE`, `RESUME`, `CANCEL`, and `COMPLETE`. It covers PostgreSQL campaign state, campaign routing reservations, and `campaign_execution_events`.

Approval/build transitions remain on the existing campaign repository path. Capacity calculation, maintenance checks, dispatch-window checks, consent checks, and completion assessment remain pre-commit validation.

## Required guarantee

For each lifecycle action, all required authoritative PostgreSQL writes commit together or all roll back. No normal failure, cancellation, deadlock, serialization conflict, or optimistic-version conflict may leave a partially applied lifecycle action.

External gateway/network actions are outside the transaction boundary. T6-020 concerns records held in the same PostgreSQL database.

## Current defect

The coordinator currently invokes campaign CAS, routing activation/release, and execution-event insertion through separate repository calls. A later call can fail after campaign status has committed, stranding reservations or evidence. The original action cannot then be safely repeated because the campaign version and state have advanced.

## Chosen approach

Introduce a transaction-aware lifecycle committer. Existing Go domain validation prepares the next campaign state; a PostgreSQL persistence component performs the final CAS, reservation mutation, and event insert in one serializable transaction.

Rejected alternatives:

- A transactional outbox gives eventual rather than immediate consistency.
- A stored procedure would duplicate evolving campaign-domain rules in SQL.
- Compensating updates cannot reliably reconstruct missing governance evidence.

## Component boundaries

### Campaign service

Refactor lifecycle validation into `PrepareTransition`. It loads the campaign, verifies `expectedVersion`, applies organisation, consent, provider, gateway, and domain-transition rules, and returns the proposed next state without saving it.

The existing `Transition` method calls the same preparation logic and then performs the current repository CAS for non-execution callers. Validation therefore has one implementation.

### Execution coordinator

The coordinator keeps all orchestration prechecks and constructs a `LifecycleCommit` containing:

- proposed campaign state and expected version;
- lifecycle action and resulting event type;
- actor, reason, event details, and authoritative transition time;
- routing plan identifier when one exists; and
- reservation operation: `ACTIVATE`, `RELEASE`, or `NONE`.

### Lifecycle committer

`LifecycleCommitter` is an execution-layer interface. Production wiring requires a PostgreSQL implementation; execution actions fail closed if the committer is absent.

The PostgreSQL implementation lives in the persistence package so it can reuse the campaign CAS statement. Reservation and execution-event stores expose transaction-aware helpers rather than duplicating their SQL.

Memory and fake committers support deterministic unit tests without weakening production wiring.

## Action matrix

| Action | Campaign result | Reservation operation | Required event |
|---|---|---|---|
| Start | `DISPATCHING` | `ACTIVATE` for a routing plan; otherwise `NONE` | `DISPATCH_STARTED` |
| Pause | `PAUSED` | `NONE` | `CAMPAIGN_PAUSED` |
| Resume | `DISPATCHING` | `NONE` | `CAMPAIGN_RESUMED` |
| Cancel | `CANCELLED` | `RELEASE` when a plan exists | `CAMPAIGN_CANCELLED` |
| Complete | Assessed terminal status | `RELEASE` when a plan exists | Completion assessment state |

Capacity assessments remain durable observations made before start/resume admission. They are not state mutations and are not rolled back when a later optimistic conflict occurs.

## Transaction flow

1. Begin a serializable PostgreSQL transaction.
2. Apply the complete campaign update with `WHERE id = ? AND version = expectedVersion`.
3. Require exactly one updated campaign row; otherwise return a version conflict.
4. Apply and verify the required reservation operation.
5. Insert the version-linked execution event.
6. Commit.
7. Return the proposed campaign only after commit succeeds.

Every error before commit causes rollback. The coordinator must not call the legacy standalone event or release methods after a successful lifecycle commit.

## Reservation invariants

`ACTIVATE` changes every `HELD` reservation for the selected plan to `ACTIVE` and advances its fencing version. An entirely `ACTIVE` plan is accepted as an already-converged legacy recovery case. Any `RELEASED`, `EXPIRED`, mixed, missing, or wrong-campaign plan fails closed.

`RELEASE` changes every `HELD` or `ACTIVE` reservation to `RELEASED` and advances its fencing version. An entirely `RELEASED` plan is accepted as converged. Mixed, missing, wrong-campaign, or otherwise unsafe state fails closed.

Reservation verification occurs inside the same transaction and locks the relevant plan/reservation rows. The plan must belong to the campaign being transitioned.

## Execution-event integrity

Add nullable `campaign_version bigint` to `campaign_execution_events`, with a positive-value check and a partial unique index on `(campaign_id, campaign_version)` where the version is not null.

All new lifecycle events must record the resulting campaign version. Historical and diagnostic events may retain a null version, preserving existing data and runner failure evidence.

The unique index enforces one lifecycle evidence record per committed campaign version. Event details remain JSONB and are serialized before opening the transaction.

## Error and retry semantics

- Domain/precheck rejection: no transaction and no writes.
- Stale expected version: rollback and domain conflict.
- Reservation conflict or ownership mismatch: rollback and routing conflict.
- Event constraint/insertion failure: rollback and persistence error.
- Context cancellation or shutdown: rollback.
- PostgreSQL SQLSTATE `40001` or `40P01`: retry the complete transaction a small bounded number of times.
- Exhausted retries or other database errors: fail with no partial commit.

If commit succeeds but the HTTP response is lost, campaign state, reservation state, and evidence are all durable. A later read can confirm the result. This change does not add an `Idempotency-Key` contract to the execution endpoint; API response replay remains a separate review item.

## Concurrency

Optimistic campaign versioning selects one winner for concurrent lifecycle actions. Row locks protect reservation verification and mutation. A losing transaction rolls back its event and reservation work.

The transaction does not hold locks while performing maintenance, consent, capacity, dispatch-window, provider, or gateway checks. Those checks occur first; the version CAS prevents committing a prepared state after the campaign itself changed.

## Migration and compatibility

Create a new forward-only migration after the current migration head. Do not rewrite historical migrations.

The new event column is nullable for existing rows. The application requires it only for lifecycle commits. No execution endpoint request or response schema changes are needed.

Both control API and campaign worker must wire the same atomic committer. Existing generic campaign transition routes remain prohibited from execution lifecycle actions.

## Verification

### Unit tests

- Coordinator maps each action to the correct reservation operation and event.
- Missing committer fails closed.
- Precheck failures never invoke the committer.
- Legacy standalone event/release calls are not invoked.

### PostgreSQL integration tests

- Successful atomic start, pause, resume, cancel, and complete.
- Event insertion failure rolls back campaign and reservations.
- Reservation mutation failure rolls back campaign and event.
- Stale version leaves all three record groups unchanged.

- Two concurrent starts produce exactly one committed winner.
- Start activates reservations and advances each fence once.
- Cancel and complete release reservations and advance each fence once.
- Every committed lifecycle version has exactly one linked event.
- Rolled-back lifecycle versions have no linked event.
- Historical null-version events survive migration.
- Already-converged legacy reservation state is handled only as specified.

Failure injection uses test-only PostgreSQL triggers or constraints and does not add production failpoint code.

### Regression verification

Run focused campaign/execution/httpserver tests, all first-party Go tests, all migrations against a fresh PostgreSQL database, gateway TypeScript and Node tests, OpenAPI validation, Compose health checks, `git diff --check`, and temporary-resource hygiene checks.

## Acceptance criteria

T6-020 can be marked fixed only when:

1. no production execution lifecycle path uses separate campaign/event/reservation commits;
2. failure-injection tests prove rollback at each persistence boundary;
3. concurrency tests prove one complete winner and no partial loser;
4. version-linked lifecycle evidence is enforced by PostgreSQL;
5. control API and worker use the atomic committer;
6. the complete verification matrix passes; and
7. findings and `PROJECT_STATUS_HANDOVER.md` record the implementation evidence.

## Non-goals

This change does not redesign gateway command delivery, introduce distributed transactions, add endpoint idempotency keys, rebuild the generic audit subsystem, or alter frontend/UI behavior.
