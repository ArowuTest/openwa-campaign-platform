# Meta Free-Form Conversation-Window Implementation Plan

> **For agentic workers:** Execute inline with TDD and fresh review gates. Do not reset, clean, stash, commit, push, deploy, or mutate the active stack.

**Goal:** Complete the approved Meta Cloud requirement so an assigned Meta recipient uses canonical free-form content only when explicit sender/contact-scoped conversation-window evidence permits it, otherwise uses the already-approved template representation.

**Architecture:** Authenticated Meta inbound webhooks create/refresh durable PostgreSQL conversation-window evidence only after credential/WABA/phone sender binding and contact resolution. Final dispatch materialization chooses exactly one Meta representation per recipient. The gateway submits only that chosen representation and never performs template/free-form fallback after the submission boundary.

**Tech Stack:** Go 1.23, PostgreSQL 17/18-compatible SQL, Meta WhatsApp Cloud API, existing delivery ledger and webhook pipeline.

## Global Constraints
- Latest approved repo requirements supersede older SRS wording.
- Canonical provider-neutral campaign content remains authoritative.
- Business-initiated bulk Meta routes retain a compatible approved template fallback.
- Free-form is allowed only with explicit per-recipient, Meta-sender-scoped eligibility evidence.
- UNKNOWN is never automatically retried through another representation or provider.
- Evidence is PostgreSQL-authoritative; no Redis-only eligibility state.
- No external provider/network call occurs inside a database transaction.
- TDD: deterministic RED, minimal implementation, GREEN for each task.
- No commit step is included because the project owner has explicitly prohibited commits without separate authorization.

---
### Task 1: Durable conversation-window evidence

**Files:**
- Create: `database/migrations/0081_meta_conversation_window_evidence.sql`
- Create/modify: `internal/metacloud/conversation_window.go`
- Test: `internal/metacloud/conversation_window_postgres_integration_test.go`
- Modify: `tests/schema/migrations_test.go`
- Modify: `cmd/control-api/runtime.go` readiness marker.

**Produces:** sender/contact-scoped current eligibility keyed by authenticated inbound provider evidence.

- [ ] Write RED PG tests proving no window exists by inference alone; authenticated observation creates one; replay is idempotent; older inbound cannot shorten/replace newer evidence; conflicting provider-message evidence is rejected.
- [ ] Add a PostgreSQL table with `meta_sender_id`, `contact_id`, source provider-message ID, inbound occurrence time, eligible-until, version/update time, ownership FKs/indexes and strict time-shape checks.
- [ ] Add PostgreSQL store method that advances evidence only for a newer/equal replay-safe inbound observation.
- [ ] Run the PG test against fresh current schema and verify GREEN.

### Task 2: Open/refresh evidence only at the authenticated inbound boundary

**Files:**
- Modify: `internal/platform/httpserver/meta_webhooks.go`
- Modify: `internal/platform/httpserver/dependencies.go` or existing dependency definition location.
- Modify: `cmd/control-api/meta_runtime.go`
- Test: `internal/platform/httpserver/meta_webhooks_test.go` and/or PostgreSQL Meta webhook integration fixture.

**Produces:** signed sender-bound inbound text can refresh free-form eligibility after contact resolution.

- [ ] Write RED tests proving bad signature/unbound sender/unresolved contact never writes window evidence.
- [ ] Write RED test proving a valid authenticated inbound writes sender+contact evidence using the inbound provider message ID and occurrence time.
- [ ] Inject the conversation-window store and governed duration into Meta webhook processing.
- [ ] Record/refresh evidence before ordinary inbound/STOP processing; suppression remains independently authoritative for send eligibility.
- [ ] Verify replayed inbound cannot extend the window beyond the deterministic value derived from the same provider event.
### Task 3: One governed conversation-window policy

**Files:**
- Modify: `internal/shared/config/config.go`
- Modify: `internal/shared/config/meta_cloud_config_test.go`
- Modify: `internal/worker/config/config.go`
- Modify: `internal/worker/config/config_test.go`
- Modify: control-api configuration/runtime wiring as required.

**Produces:** one `META_CLOUD_CONVERSATION_WINDOW` duration consumed by both webhook observation and worker final dispatch.

- [ ] Write RED tests for default, valid override and invalid bounds.
- [ ] Add a shared parser with a conservative current default and bounded override rather than burying a provider-policy magic duration in SQL or dispatch.
- [ ] Wire the same resolved duration into control-api inbound observation and campaign-worker material validation.
- [ ] Verify both binaries reject invalid policy values at startup.

### Task 4: Per-recipient Meta representation selection

**Files:**
- Modify: `internal/dispatch/handler.go`
- Modify: `internal/dispatch/meta_material.go`
- Test: `internal/dispatch/meta_material_postgres_integration_test.go`

**Produces:** explicit `TEMPLATE` or `FREE_FORM` representation in `Material`/`GatewayRequest`.

- [ ] Write RED PG tests: no window => TEMPLATE; current sender/contact window => FREE_FORM; expired/wrong-sender/wrong-contact window => TEMPLATE; future/malformed evidence fails closed.
- [ ] Load conversation-window evidence by the frozen Meta sender and campaign recipient contact.
- [ ] Validate it against the injected clock and configured policy duration.
- [ ] Select `FREE_FORM` only for valid explicit evidence; otherwise validate/render the existing approved template binding.
- [ ] Preserve the approved template fallback at route level; do not make Meta template requirements narrow Baileys/WWebJS content.
- [ ] Ensure the chosen representation is immutable for that material load and passed explicitly to the gateway.
### Task 5: Free-form Meta Cloud request adapter

**Files:**
- Modify: `internal/dispatch/meta_gateway.go`
- Modify/add tests: `internal/dispatch/meta_gateway_test.go`

**Produces:** Cloud API text/image/video/document free-form payloads without weakening template behavior.

- [ ] Write RED unit tests for explicit FREE_FORM text and media payloads and for representation/evidence mismatches.
- [ ] Keep TEMPLATE request validation/rendering unchanged when representation is TEMPLATE.
- [ ] For FREE_FORM, require canonical body for text and resolved media URL for media; send `text`, `image`, `video`, or `document` payload shape as appropriate.
- [ ] Reject missing/unknown representation rather than inferring from template fields.
- [ ] Keep gateway/session authority forbidden for both Meta representations.
- [ ] Preserve all existing provider error safety mapping; no automatic representation fallback after a send attempt.

### Task 6: Routing/capability and end-to-end regression

**Files:**
- Modify only if required by RED evidence: `internal/execution/routing_admin.go`, `internal/execution/routing_meta.go`, provider capability fixtures/tests.
- Test: existing Meta routing/material/dispatch suites.

**Produces:** bulk Meta routing remains safely approvable while per-recipient representation selection happens at final materialization.

- [ ] Prove existing route approval retains the approved template fallback and does not assume every recipient has an open window.
- [ ] Prove a free-form-eligible recipient can use canonical content without changing the frozen sender/provider assignment.
- [ ] Prove expiry before final materialization selects TEMPLATE, not a blind free-form send.
- [ ] Prove no post-SUBMITTING template↔free-form fallback exists.

### Task 7: Full gates and fresh review

- [ ] Fresh PG17 replay 0001→latest plus pre-current control and upgrade probe.
- [ ] Full Linux Go suite with all PG opt-ins; `go vet`; `go build`; focused `-race`.
- [ ] Gateway Node 22.19 typecheck/build/audit and acceptance scripts because Meta gateway payload behavior changes.
- [ ] `git diff --check`, migration continuity, readiness/static/security checks.
- [ ] Rebuild blind council packets from exact source. Grok specialists review all severities; adjudicate/rechallenge under protocol.
- [ ] Run identical general packet through Qwen 3.8 Max, GLM 5.2, Grok 4.6-general and Gemini 3.7 Flash only after specialist source review stabilizes.
- [ ] Neon remains explicitly pending unless secure file-for-file migration execution becomes available.
