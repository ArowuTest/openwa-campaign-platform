# Heartbeat Trust-Boundary Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make sender heartbeat data machine-authenticated telemetry that cannot overwrite governed capacity or reduce safety evidence.

**Architecture:** Keep the existing signed gateway runtime HMAC/timestamp/nonce primitive as the machine trust boundary. Remove the obsolete node-heartbeat mutation route because signed runtime registration already owns node health. Keep a session heartbeat endpoint only if it is signed/replay-protected, and make its store mutation telemetry-only: status through the canonical transition guard, engine version, monotonic usage and heartbeat time; configured throughput/in-flight limits remain control-plane authority.

**Tech Stack:** Go 1.23, net/http, PostgreSQL/libpq, existing sender runtime-registration signing, OpenAPI 3.

## Global Constraints

- PostgreSQL remains authoritative for governed sender configuration and evidence.
- Gateway workers may report measured telemetry but cannot enlarge approved capacity.
- Quarantined, restricted and retired sessions cannot be reactivated by heartbeat.
- Use RED -> minimal fix -> GREEN for each behavior.
- Do not change frontend scope or Railway/Hostinger deployment in this task.

---### Task 1: Preserve governed session capacity and monotonic usage

**Files:**
- Modify: `internal/sender/memory_governance.go`
- Modify: `internal/sender/postgres_governance.go`
- Test: `internal/sender/heartbeat_integrity_test.go`
- Test: `internal/sender/heartbeat_integrity_postgres_integration_test.go`

**Interfaces:**
- Consumes: `GovernanceStore.HeartbeatSession(context.Context, string, int64, GovernedSession, time.Time)`.
- Produces: heartbeat mutation that preserves configured capacity and stores `max(current.sent_today, reported.sent_today)`.

- [ ] Verify the existing memory regression fails because heartbeat rewrites governed capacity.
- [ ] Change only the memory heartbeat mutation to preserve `SafeMessagesPerMinute`, `SafeDailyCapacity`, and `InFlightLimit`, and make `SentToday` monotonic.
- [ ] Re-run the focused memory regression and existing quarantine heartbeat tests.
- [ ] Add a PostgreSQL regression proving the same invariants on a fresh migrated database.
- [ ] Verify PostgreSQL RED on the current SQL update.
- [ ] Apply the equivalent minimal SQL fix and verify GREEN.
### Task 2: Remove the obsolete unsigned node-heartbeat path

**Files:**
- Modify: `internal/platform/httpserver/server.go`
- Modify: `internal/platform/httpserver/sender_governance.go`
- Modify: `contracts/openapi/control-api.yaml`
- Test: `internal/platform/httpserver/sender_heartbeat_authorization_test.go`

**Interfaces:**
- Consumes: existing signed `POST /api/v1/internal/gateway-nodes/{id}/runtime` runtime-registration path.
- Produces: no separate human-authenticated node heartbeat capable of refreshing authoritative runtime availability.

- [ ] Add an HTTP regression proving the legacy node-heartbeat route is not exposed by `Server.Handler()`.
- [ ] Verify RED against the current route registration.
- [ ] Remove only the legacy route/handler and its OpenAPI operation.
- [ ] Verify the focused HTTP regression and OpenAPI route verifier GREEN.
### Task 3: Machine-authenticate session heartbeat

**Files:**
- Modify: `internal/platform/httpserver/server.go`
- Modify: `internal/platform/httpserver/sender_governance.go`
- Modify: `contracts/openapi/control-api.yaml`
- Test: `internal/platform/httpserver/sender_heartbeat_authorization_test.go`

**Interfaces:**
- Consumes: `sender.VerifyRuntimeReport`-compatible HMAC headers and `RuntimeRegistrationStore.UseRuntimeNonce` replay protection.
- Produces: signed `POST /api/v1/internal/sender-sessions/{id}/heartbeat` that rejects missing/invalid/replayed machine credentials and never accepts an ordinary bearer session as sufficient authority.

- [ ] Add HTTP regressions for unsigned rejection, valid signed acceptance and nonce replay rejection.
- [ ] Verify RED against the current human-authenticated route.
- [ ] Reuse the existing runtime secret/skew/previous-secret configuration and nonce store; do not add a second service secret.
- [ ] Route the session heartbeat outside human `require(...)` middleware and verify its exact body before applying telemetry.
- [ ] Update OpenAPI security/route documentation to reflect machine authentication.
- [ ] Run sender/httpserver targeted tests, race tests for changed packages, `go vet`, OpenAPI parity, secret scan and `git diff --check`.
- [ ] Update `docs/program/BACKEND_ADVERSARIAL_FINDINGS.md` and `PROJECT_STATUS_HANDOVER.md` with RED/GREEN evidence.