# Remaining Task 6 Backend Adversarial Review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the remaining Task 6 backend release-readiness review with Docker and disposable-Neon evidence, fixing only vulnerabilities reproduced by deterministic regressions.

**Architecture:** Work through bounded trust boundaries in risk order: database portability, authorization, replay/fencing, ambiguous outcomes and recovery, governance evidence, then gateway/deployment security. Every finding receives a RED regression, the smallest safe fix, focused verification, and a handover checkpoint before moving on.

**Tech Stack:** Go, TypeScript/NestJS, PostgreSQL 17/18, Neon branches, Docker, Docker Compose, PowerShell, OpenAPI.

## Global Constraints

- Preserve the intentionally dirty cumulative Task 6 worktree on `work/backend-production-engineering`; never reset, clean, stash or discard it.
- Do not commit, push, deploy, rebuild the active stack, or begin Task 7/frontend work without separate authorization.
- Run Go only in `campaign-task2-go-builder:latest` with `/usr/local/go/bin/go`.
- Validate database changes on both a fresh Docker PostgreSQL database and a disposable Neon branch.
- Never mutate the Neon production branch; create, explicitly name, reset and delete a temporary branch.
- A hypothesis is not a finding until a deterministic RED test reproduces an unsafe outcome.
- Keep external gateway/network calls outside database transactions.
- Update `docs/program/BACKEND_ADVERSARIAL_FINDINGS.md` and `PROJECT_STATUS_HANDOVER.md` after each fixed finding.
- Remove every temporary database, container, volume and Neon branch created by this plan.

---
### Task 1: Restore the Neon database gate for T6-020

**Files:**
- Test: `database/migrations/0001_*.sql` through `0073_campaign_execution_lifecycle_atomicity.sql`
- Test: `scripts/validate-postgres-migrations.py`
- Test: `internal/execution/lifecycle_postgres_integration_test.go`
- Test: `internal/execution/routing_release_postgres_integration_test.go`
- Modify: `PROJECT_STATUS_HANDOVER.md`

**Interfaces:**
- Uses Neon project `lucky-credit-36128290`; never writes to branch `br-quiet-voice-ayz1yb8q`.
- Produces a deleted disposable branch and recorded PostgreSQL 18 validation evidence.

- [x] Create branch `openwa-task6-remaining-20260810` and record its returned opaque branch ID.
- [x] Tell the user the temporary branch ID before executing SQL on it.
- [x] Obtain its direct connection string without printing or persisting credentials.
- [x] On only that branch, run `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`.
- [x] Run `python3 scripts/validate-postgres-migrations.py` from Docker against the branch; require 73/73 migrations.
- [x] Run the execution lifecycle, duplicate-version, mixed-reservation, concurrency and T6-019 release tests against the branch.
- [x] Query the branch for `campaign_version`, `uq_campaign_execution_event_lifecycle_version`, 128 tables and PostgreSQL server version.
- [x] Delete the temporary Neon branch in `finally` and confirm it is absent.
- [x] Record the exact evidence in the handover; do not alter production.
### Task 2: Authorization, step-up and maker-checker closure

**Files:**
- Review: `internal/platform/httpserver/server.go`
- Review: `internal/platform/httpserver/sender_governance.go`
- Review: `internal/identity/administration.go`
- Review: `internal/commercial/service.go`
- Review: `internal/campaign/service.go`
- Create when RED: `internal/platform/httpserver/task6_authorization_matrix_test.go`
- Create when RED: `internal/platform/httpserver/task6_maker_checker_boundary_test.go`

**Interfaces:**
- Consumes the registered `/api/v1` routes and `identity.User.Permissions`.
- Produces deterministic denial evidence for unauthenticated, permissionless, stale-MFA and same-actor approval attempts.

- [x] Extract every state-changing route registered through `Server.Handler` and map permission plus recent-MFA requirements.
- [x] Test campaign execution, routing release, sender lifecycle/pairing, exports/download grants, privacy legal holds, retention/configuration, provider capabilities and commercial approvals.
- [x] For each route assert: no principal is rejected; wrong permission is rejected; required stale MFA is rejected before mutation.
- [x] Exercise maker-as-checker for consent, message, commercial, provider, organisation policy, reporting privacy and configuration approval paths.
- [x] If any request mutates state before denial, retain that exact RED test, implement the smallest boundary fix and rerun the whole HTTP/identity/service package set.
- [x] Record either a new numbered finding or explicit no-finding evidence.
### Task 3: Idempotency, replay, lease and fencing closure

**Files:**
- Review: `internal/jobs/postgres.go`
- Review: `internal/outbox/postgres.go`
- Review: `internal/delivery/postgres.go`
- Review: `internal/execution/postgres_leases.go`
- Review: `internal/execution/routing_postgres.go`
- Review: `services/openwa-gateway/src/idempotency.service.ts`
- Review: `services/openwa-gateway/src/command-replay.service.ts`
- Create when RED: `scripts/test-gateway-command-replay-and-idempotency.js`
- Create when RED: package-local `*_postgres_integration_test.go` beside the affected Go repository.

**Interfaces:**
- Produces single-winner, monotonic-fence and conflict-on-different-payload behavior under concurrency and restart replay.

- [x] Run two concurrent claims for each durable job/outbox/execution lease and assert one owner/fence winner.
- [x] Replay identical delivery/provider/inbound events and assert no duplicate metrics, events or outbox evidence.
- [x] Reuse the same idempotency key/nonce with different content and assert conflict while original evidence remains unchanged.
- [x] Simulate stale lower-fence completion after a higher fence is accepted and assert it cannot overwrite state.
- [x] Simulate gateway restart with durable replay/idempotency files and assert nonce rejection plus UNKNOWN no-resend behavior survives restart.
- [x] Fix only reproduced failures, then rerun Go race tests and isolated gateway TypeScript verification.
### Task 4: UNKNOWN, cancellation and crash-recovery closure

**Files:**
- Review: `internal/dispatch/handler.go`
- Review: `internal/dispatch/http_gateway.go`
- Review: `internal/delivery/service.go`
- Review: `internal/execution/runner.go`
- Review: `services/openwa-gateway/src/gateway-messaging.service.ts`
- Review: `services/openwa-gateway/src/idempotency.service.ts`
- Create when RED: `internal/dispatch/task6_unknown_recovery_test.go`
- Create when RED: `internal/execution/task6_cancellation_recovery_test.go`
- Create when RED: `scripts/test-gateway-unknown-recovery.js`

**Interfaces:**
- Produces a fail-closed invariant: an ambiguous submission is never automatically resent, and cancellation cannot erase or reassign in-flight evidence.

- [x] Inject timeout before provider acceptance, timeout after provider acceptance, worker cancellation and process restart separately.
- [x] Assert only proven pre-submit failures are retryable; post-submit ambiguity becomes UNKNOWN with reconciliation required.
- [x] Assert UNKNOWN records are not reclaimed or resent by queue repair, execution runner or gateway restart.
- [x] Cancel campaigns with queued, claimed, gateway-accepted and UNKNOWN recipients; assert no new sends and preservation of terminal/ambiguous evidence.
- [x] Exercise stale lease recovery and prove attempt/fence monotonicity.
- [x] Fix only deterministic RED cases and run dispatch, delivery, execution and gateway regression suites.
### Task 5: Retention, legal hold, audit and configuration closure

**Files:**
- Review: `internal/retention/postgres.go`
- Review: `internal/privacy/postgres.go`
- Review: `internal/audit/postgres.go`
- Review: `internal/platformpolicy/postgres.go`
- Review: `internal/operations/service.go`
- Create when RED: `internal/retention/task6_legal_hold_postgres_integration_test.go`
- Create when RED: `internal/audit/task6_atomic_evidence_postgres_integration_test.go`
- Create when RED: `internal/platformpolicy/task6_configuration_activation_postgres_integration_test.go`

**Interfaces:**
- Produces proof that legal holds block deletion, audit chains are append-only/attributed, and configuration activation is versioned and maker-checker governed.

- [x] Create held and unheld records at the same retention cutoff; run retention and assert only unheld eligible data is removed.
- [x] Exercise hold creation/release/expiry races against retention claims and assert the hold wins safely.
- [x] Test audit append contention, idempotent replay, conflicting replay, tamper rejection and actor/correlation attribution.
- [x] Inject audit persistence failure around sensitive governance actions and determine whether mutation can commit without required evidence.
- [x] Concurrently activate two configuration/policy versions; assert one effective version, immutable history and checksum validation.
- [x] Fix reproduced gaps with transaction/outbox boundaries rather than best-effort audit calls.
### Task 6: Gateway, Compose and security closure

**Files:**
- Review: `services/openwa-gateway/src/*.ts`
- Review: `infrastructure/compose/compose.production.yaml`
- Review: `infrastructure/docker/*.Dockerfile`
- Test: `scripts/test-gateway-session-authority.js`
- Test: `scripts/test-gateway-outbox-integrity.js`
- Test: `scripts/test-gateway-session-drain.js`

**Interfaces:**
- Produces a current-source gateway build and a release-security evidence report without contacting real WhatsApp accounts.

- [x] Run strict TypeScript `tsc --noEmit`, emitted build, embedded engine test and all Task 6 gateway scripts in an isolated current-source verifier.
- [x] Validate production Compose interpolation with the configured env/secrets, service healthchecks, read-only filesystems, non-root users, private/public network exposure and secret mounts.
- [x] Scan tracked plus untracked candidate files for high-confidence credentials, private keys, connection strings and unsafe logging of PII.
- [x] Audit gateway command/callback body-size limits, replay windows, nonce durability, authority expiry, teardown, outbound/inbound evidence durability and graceful shutdown.
- [x] Record real-WhatsApp pairing/send/ack/reconnect/endurance as external live gates, not locally completed evidence.
### Task 7: Final Docker + Neon regression and Task 6 handover

**Files:**
- Modify: `docs/program/BACKEND_ADVERSARIAL_FINDINGS.md`
- Modify: `PROJECT_STATUS_HANDOVER.md`
- Modify: this plan's checkboxes as gates complete.

**Interfaces:**
- Produces the evidence-based Task 6 completion decision; does not start Task 7.

- [x] Run `go test ./... -count=1`, `go vet ./...`, `go build ./...` and focused race packages in Docker.
- [x] Run all PostgreSQL integration tests against a fresh Docker PostgreSQL 17 database migrated through the final migration.
- [x] Repeat the migration validator and affected integration tests on a newly created disposable Neon PostgreSQL 18 branch, then delete it.
- [x] Run the isolated gateway TypeScript/build/security suite and OpenAPI route/schema parity checks.
- [x] Require `git diff --check`, zero temporary Task 6 databases/containers/volumes/Neon branches, and 13/13 active-stack services healthy.
- [x] Update every finding status honestly; list external/live and deployment gates separately.
- [x] Update `PROJECT_STATUS_HANDOVER.md` with exact commands, results, remaining blockers and next authorized action.
- [x] Do not call Task 6 complete if any code finding, Neon gate, cleanup item or verification command remains unresolved.
