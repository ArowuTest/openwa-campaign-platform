# Meta Cloud Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add admin-governed Meta WhatsApp Cloud API as a production-capable third transport with AUTO/WEIGHTED multi-transport campaign routing.

**Architecture:** Existing campaign routing plans become the authoritative multi-transport selection. OPENWA routes continue through gateway sessions; META/CLOUD_API routes use governed Meta senders and a direct Graph API adapter. Sender pools, capacity reservations, delivery ledger and UNKNOWN/no-resend remain shared.

**Tech Stack:** Go, PostgreSQL, Docker, Neon PostgreSQL, Meta Graph/WhatsApp Cloud API, OpenAPI.

## Global Constraints
- Preserve the current dirty Task-6 worktree; no reset, clean, stash or split.
- No commit, push, deploy, frontend work or active-stack mutation without separate authorization.
- TDD for every production change: deterministic RED, minimal fix, GREEN.
- Go tests run only in Docker using `/usr/local/go/bin/go`.
- SQL is verified on fresh Docker PostgreSQL and a disposable Neon branch.
- No external provider/network call occurs inside a database transaction.
- UNKNOWN is never automatically retried through another transport.
- Meta credential values never enter PostgreSQL, logs, audit evidence or API responses.
- The approved provider-neutral OpenWA message version is authoritative; Meta template/policy constraints must never narrow generic Baileys/WWebJS free-form personalisation or supported message/media types.
- Business-initiated bulk Meta delivery requires a compatible approved Meta representation; Meta free-form delivery is allowed only with explicit per-recipient conversation-window/policy eligibility.
- Meta template rendering is component-driven and extensible; adding a Meta component type must not require redesigning the canonical campaign/message model.

**2026-08-13 approved refinement:** Re-open the Meta template/dispatch delta before Task 7 to enforce the message-authority invariant, generic component binding and strict ambiguous-5xx => UNKNOWN semantics.

---
### Task 1: Provider and routing domain extension

**Files:**
- Modify: `internal/campaign/transport.go`
- Modify: `internal/provider/registry.go`
- Modify: `internal/execution/routing_plan.go`
- Test: `internal/campaign/transport_test.go`
- Test: `internal/execution/routing_plan_test.go`

**Produces:** `META/CLOUD_API`, `SEND_TEMPLATE`, `AUTO|WEIGHTED`, provider-specific route validation.

- [ ] Write failing tests proving Meta is valid, invalid provider/engine pairs are rejected, all seven transport subsets can form routing plans, and one route cannot reference both gateway and Meta endpoints.
- [ ] Run focused tests in Docker and record deterministic RED evidence.
- [ ] Add provider/engine/capability/distribution constants and provider-specific validation with legacy routing behavior preserved.
- [ ] Run focused tests in Docker to GREEN.

### Task 2: PostgreSQL Meta sender and routing schema

**Files:**
- Create: `database/migrations/0075_meta_cloud_transport.sql`
- Create: `internal/metacloud/sender.go`
- Create: `internal/metacloud/sender_postgres.go`
- Create: `internal/metacloud/sender_test.go`
- Create: `internal/metacloud/sender_postgres_integration_test.go`

**Produces:** governed Meta senders with maker-checker lifecycle, sender-pool ownership, endpoint health and routing FK support.
- [ ] Write failing unit tests for independent approver enforcement, credential-key-only exposure, effective periods and health transitions.
- [ ] Write failing PostgreSQL test for schema constraints and optimistic-version lifecycle.
- [ ] Implement migration 0075: Meta senders/events/templates/bindings, nullable gateway route endpoint, Meta route endpoint FK, provider/engine constraint updates, distribution mode and pacing engine support.
- [ ] Implement Meta sender service/store and run focused Docker tests to GREEN.

### Task 3: Credential resolver and Meta HTTP client

**Files:**
- Create: `internal/metacloud/credentials.go`
- Create: `internal/metacloud/client.go`
- Create: `internal/metacloud/client_test.go`
- Modify: `internal/shared/config/config.go`
- Test: `internal/shared/config/config_test.go`

**Produces:** secret-file credential resolution and hardened Graph API client with no redirects.

- [ ] Write RED tests for keyed credentials, missing/duplicate credential keys, redirect rejection, Bearer send, bounded responses, 429 retry safety and ambiguous timeout/5xx classification, including an explicit 503 => UNKNOWN regression.
- [ ] Add optional `META_CLOUD_CREDENTIALS_JSON_FILE` config resolution; absent config leaves Meta disabled without breaking existing deployments.
- [ ] Implement credential resolver and HTTP client with injected test base URL and configurable Graph version.
- [ ] Run focused Docker tests to GREEN and scan output for secret leakage.

### Task 4: Template catalogue and approved message binding

**Files:**
- Create: `internal/metacloud/template.go`
- Create: `internal/metacloud/template_postgres.go`
- Create: `internal/metacloud/template_test.go`
- Modify: `internal/message/postgres.go`
- Modify: `internal/platform/httpserver/message*.go` where the current message admin handlers live.
**Produces:** WABA template sync/cache plus immutable provider-specific Meta binding while the canonical OpenWA message remains authoritative.

- [ ] Write RED tests for template decoding/hash, approved-status filtering, typed component parameter ordering, body/media compatibility, binding immutability, and proof that Meta binding requirements do not invalidate the same canonical message for Baileys/WWebJS.
- [ ] Implement template sync from `/{WABA-ID}/message_templates` and PostgreSQL upsert without external calls inside transactions.
- [ ] Implement message-version binding create/read APIs using an extensible typed component-binding contract; require an approved message version plus compatible cached Meta template when the Meta route needs template delivery.
- [ ] Run focused Docker tests to GREEN.

### Task 5: Routing governance, AUTO and WEIGHTED

**Files:**
- Modify: `internal/execution/routing_admin.go`
- Modify: `internal/execution/routing_postgres.go`
- Modify: `internal/execution/postgres.go`
- Modify: `internal/execution/routing_admin_test.go`
- Add/modify PostgreSQL routing integration tests.

**Produces:** mixed OPENWA+META approved routing plans with frozen endpoint evidence and provider-specific capacity.

- [ ] Write RED tests for Meta-only, OpenWA-only and mixed plans, AUTO normalization, WEIGHTED targets, unavailable Meta sender exclusion and provider-endpoint mismatch rejection.
- [ ] Implement provider-specific route freezing: gateway governance for OPENWA, Meta-sender governance/template compatibility for META.
- [ ] Implement Meta route capacity from sender pool + current Meta sender health rather than gateway sessions.
- [ ] Persist/load nullable gateway/meta endpoint references and distribution mode; keep legacy plans readable.
- [ ] Run focused unit and fresh-PostgreSQL integration tests to GREEN.

### Task 6: Transport-neutral dispatch and Meta submission

**Files:**
- Modify: `internal/dispatch/handler.go`
- Modify: `internal/dispatch/postgres_material.go`
- Create: `internal/dispatch/transport_router.go`
- Create: `internal/dispatch/meta_gateway.go`
- Test: `internal/dispatch/meta_gateway_test.go`
- Add/modify PostgreSQL material-loader integration tests.

**Produces:** one dispatch handler that routes OPENWA to the existing signed gateway and META to Graph API while retaining the existing safety state machine.

- [ ] Write RED tests proving Meta material needs no browser session, OpenWA still requires fenced session authority, provider routing is exact, and unsupported providers fail permanently.
- [ ] Write RED tests proving pre-submit validation never marks UNKNOWN, 429 is retryable, timeout/ambiguous 5xx/malformed success are UNKNOWN, and UNKNOWN never invokes another provider.
- [ ] Refactor material/request validation by provider and add a transport router without changing the delivery state machine.
- [ ] Add Meta material-loader branch: validate frozen route/sender/provider-specific representation, render typed template components (or explicitly eligible free-form Meta payload), resolve media, never call session allocator.
- [ ] Implement Meta representation send and run focused Docker tests to GREEN; prove Baileys/WWebJS still render the canonical free-form personalised message independently of Meta template availability.

### Task 7: Meta webhook and inbound ingestion

**Files:**
- Create: `internal/metacloud/webhook.go`
- Create: `internal/metacloud/webhook_test.go`
- Create: `internal/platform/httpserver/meta_webhooks.go`
- Modify: `internal/platform/httpserver/server.go`
- Add: `internal/platform/httpserver/meta_webhooks_test.go`

**Produces:** authenticated Meta verification, delivery/read/failure callbacks and inbound STOP processing.

- [ ] Write RED tests for GET challenge verification, invalid verify token, valid/forged `X-Hub-Signature-256`, body limit, replay, out-of-order status, inbound text and quoted-message resolution.
- [ ] Implement raw-body signature verification before parsing and Meta webhook decoding.
- [ ] Map provider statuses into the existing delivery ledger and inbound text into the existing opt-out processor.
- [ ] Run focused Docker tests to GREEN.

### Task 8: Admin/runtime/OpenAPI wiring

**Files:**
- Create: `internal/platform/httpserver/meta_senders.go`
- Modify: `internal/platform/httpserver/server.go`
- Modify: `cmd/control-api/runtime.go`
- Modify: `cmd/campaign-worker/main.go`
- Modify: `contracts/openapi/control-api.yaml`
**Produces:** governed Meta admin APIs, runtime dependency wiring, capability bootstrap and documented contract.

- [ ] Write RED authorization/HTTP tests for Meta sender lifecycle, verify, template sync/list and message binding; assert no secret fields are serialized.
- [ ] Wire PostgreSQL stores, credential resolver, transport router and webhook dependencies into control API/campaign worker.
- [ ] Bootstrap `META/CLOUD_API` provider capability definition with `SEND_TEMPLATE`, text/media and callback capabilities; do not create an ACTIVE Meta sender automatically.
- [ ] Extend OpenAPI and run route-contract verification to GREEN.
- [ ] Add optional deployment-secret wiring only where it does not make existing non-Meta deployments require a Meta credential file.

### Task 9: Regression, PostgreSQL/Neon parity and handover

**Files:**
- Modify: `PROJECT_STATUS_HANDOVER.md`
- Modify: `docs/program/BACKEND_ADVERSARIAL_FINDINGS.md` only if new adversarial defects are discovered.
- Modify: this plan to check completed tasks.

**Produces:** evidence-backed local completion of the Meta backend extension, with live Meta acceptance explicitly external.

- [ ] Run migration validator from 0001 through all new migrations on fresh Docker PostgreSQL and replay it.
- [ ] Run focused Meta PostgreSQL integration suite and the full first-party Go suite in Docker.
- [ ] Run `go vet ./...`, `go build ./...`, focused race tests, secret scan, production-compose verification and OpenAPI verification.
- [ ] Create a unique disposable Neon child branch, report its opaque ID before SQL, migrate the full chain on PG18, run affected Go test binaries, delete the branch and confirm absence.
- [ ] Remove all disposable containers/networks/temp files and prove zero Task-Meta residue.
- [ ] Recheck active `openwa0828active` stack remains exactly 13/13 healthy and untouched.
- [ ] Run `git diff --check`, record branch/HEAD/dirty-worktree status, update handover and do not commit/push/deploy.

## Completion boundary
Local backend completion means Meta provider code, schema, governance, routing, template binding, dispatch, webhook ingestion and regression evidence are green. Real Meta credentials, WABA/template access, live sends, real delivery webhooks, production endurance and production deployment remain separate external/release acceptance gates.