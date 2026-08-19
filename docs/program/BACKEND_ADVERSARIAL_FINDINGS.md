# Backend Adversarial Findings Register

Status: **Task 6 findings closed; R19 historical freeze reopened; I2 council defects remediated; I3 locally re-gated and awaiting fresh blind council; external/deployment gates remain open**  
Started: 2026-08-09  
Baseline: post-Task-5 HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77`

This register records evidence-backed findings from the Task 6 adversarial backend release-readiness review. It does not close live/deployment gates. A code finding is marked FIXED only after a focused regression reproduces the failure on PostgreSQL and the unchanged scenario passes after the minimal fix.

| ID | Severity | Status | Component | Finding |
|---|---|---|---|---|
| T6-001 | High | FIXED | Reporting privacy governance | ACTIVE policy transition used `ExecContext` for row-returning `pg_advisory_xact_lock`, so libpq rejected activation. |
| T6-002 | High | FIXED | Provider capability governance | ACTIVE provider-capability transition had the same row-returning advisory-lock misuse, blocking governed route activation. |
| T6-003 | High | FIXED | Saved segment definitions | PostgreSQL create/update/version-history bound marshalled definitions as `[]byte` to JSONB, so saved segment persistence failed. |
| T6-004 | High | FIXED | Saved segment definitions | Segment update reused one prepared parameter for integer `definition_version` and bigint optimistic `version`, producing inconsistent parameter typing. |
| T6-005 | High | FIXED | Audience import reconciliation | Reconciliation evidence JSONB was bound as raw bytes, so durable reconciliation closure failed. |
| T6-006 | Medium | FIXED | Campaign metric reconciliation | Canonical/stored metric evidence was bound as raw bytes, so reconciliation result persistence failed. |
| T6-007 | High | FIXED | Governed test sends | Test-send `variable_values` JSONB was bound as raw bytes, so persisted governed test sends failed before dispatch. |
| T6-008 | High | FIXED | Message versions | Destination links/template variables were bound as raw bytes, so message draft persistence failed. |
| T6-009 | High | FIXED | Campaign persistence | Frozen required-capability evidence and material-change `changed_fields` used raw byte JSON bindings; campaign create/amend failed on PostgreSQL. |
| T6-010 | High | FIXED | Secure exports | Export request criteria/frozen payload used raw byte JSON bindings; export creation failed before approval/generation. |
| T6-011 | High | FIXED | Campaign completion scheduler | `PostgreSQLStore.Metrics` was anchored on `campaign_metrics`, so a legitimate existing campaign without a metrics row returned `sql.ErrNoRows`; the scheduler then emitted an unbounded `COMPLETION_ASSESSMENT_FAILED` event on every poll. Active-stack evidence showed ~12.6 duplicate events/second and >2.2m rows before the repository query was corrected. |
| T6-012 | High | FIXED | Campaign PostgreSQL round-trip | Nullable transport fields were read as empty Go strings but written back without `NULLIF` during campaign CAS/material updates, causing status-only transitions to violate transport constraints. |
| T6-013 | High | FIXED | Sender heartbeat trust/authority | Heartbeat could overwrite governed capacity and reduce usage evidence, while legacy node/session HTTP paths accepted human bearer authority outside the signed runtime-report boundary. Memory and PostgreSQL mutations now preserve governed limits, enforce monotonic same-day usage with UTC rollover and canonical status transitions; the obsolete node route is removed and session telemetry requires exact-body HMAC, timestamp, nonce/replay protection and node/session ownership. |
| T6-014 | High | FIXED | Gateway session authority | Concurrent accepted session-authority validations were not serialized per session, so out-of-order durable replaces could regress the stored lease fence after a higher fence had already been accepted. Per-session validation queues now preserve the highest accepted fence. |
| T6-015 | High | FIXED | Gateway durable event outboxes | Provider and inbound outboxes relied on POSIX `rename` raising `EEXIST`, but Linux rename replaces an existing destination; reuse of an event ID with different evidence could silently overwrite the original durable record. Exclusive create now preserves identical idempotent replay and rejects conflicting evidence. |
| T6-016 | High | FIXED | Campaign execution trust boundary | The generic campaign transition route accepted execution actions directly, bypassing the execution coordinator's capacity admission, maintenance/window checks, routing reservation lifecycle, completion reconciliation and execution-event recording. The generic route and OpenAPI contract now expose approval/build transitions only; execution actions must use the governed execution endpoint. |
| T6-017 | High | FIXED | Gateway session teardown | Session stop, logout and delete marked the pipeline draining but immediately tore down the provider while accepted sends were still active, creating avoidable ambiguous/UNKNOWN outcomes. Teardown now rejects queued work, waits for active sends to settle, blocks resume during teardown and only then invokes the provider lifecycle operation. |
| T6-018 | High | FIXED | Campaign start reservation recovery | Start activated routing capacity before the campaign CAS and could leave reservations ACTIVE after a failed transition. The interim compensation repair was subsequently superseded by T6-020: start now commits campaign state, reservation activation and version-linked evidence in one transaction, so failed or losing starts leave the prior campaign/reservation state unchanged. |
| T6-019 | High | FIXED | Active routing-reservation release | The operator release endpoint could release ACTIVE reservations while a campaign was DISPATCHING or PAUSED/resumable, making committed capacity appear available for overbooking. Administration rejects those states and PostgreSQL repeats the status predicate atomically inside the update; safe terminal release is idempotent. |
| T6-020 | High | FIXED | Campaign execution state/event atomicity | Start, pause, resume, cancel and complete now prepare domain state without persistence, then commit campaign CAS, required reservation activation/release and a version-linked lifecycle event in one serializable PostgreSQL transaction with bounded retry for serialization/deadlock failures. Injected event and reservation failures roll back all effects; concurrent starts produce exactly one winner, one fence increment and one lifecycle event. |
| T6-021 | High | FIXED | Maker-checker approval boundaries | Organisation, commercial, provider-capability, opt-out, inbound-retention and sender-pacing decisions rejected the submitter but not the original creator. A second actor could submit the creator's draft and the creator could then approve their own work. All six domain guards now reject both creator and submitter; three-actor regressions preserve PENDING state after denial. |
| T6-022 | High | FIXED | Campaign execution lease fencing | An unexpired execution lease could be reclaimed by the same owner string, claims returned no fence, release checked owner only, and scheduler lifecycle commits were not bound to the lease. Overlapping polls or a restarted process reusing an owner could execute the same campaign and a stale worker could release or commit across a newer lease. Claims now exclude every unexpired lease, return owner/fence/expiry, release requires the exact fence, and scheduler lifecycle commits lock and validate the unexpired lease fence inside the same serializable campaign transaction. |
| T6-023 | High | FIXED | Dispatch UNKNOWN replay boundary | A recipient already persisted as `UNKNOWN` could be replayed through the dispatch handler and reach the gateway again because the ledger reducer ignored lower-stage progress but the handler continued execution. `UNKNOWN` is now a handler-level no-send state; replay returns without material loading or provider submission, and queue repair continues to reconstruct only AUTHORISED/QUEUED/FAILED_RETRYABLE recipients. |
| T6-024 | High | FIXED | Campaign cancellation/submission race | A worker could pass final eligibility while a campaign was DISPATCHING, then cancellation could commit before the worker persisted `SUBMITTING`; the delivery transaction locked only the recipient and still crossed the submission boundary after cancellation. `EventSubmitting` now locks and validates the owning campaign in the same PostgreSQL transaction; cancellation is explicitly classified as `CAMPAIGN_CANCELLED`, the handler records canonical recipient cancellation, and no gateway call occurs when cancellation wins. |
| T6-025 | High | FIXED | Worker crash after SUBMITTING | A reclaimed dispatch job whose recipient was already `SUBMITTING` could call the gateway again after a worker/process death between submission evidence and result persistence. Recovery now converts stale `SUBMITTING` to durable `UNKNOWN` without resubmission, and every newly established UNKNOWN outcome is marked `reconciliation_required`. Gateway restart idempotency continues to recover crash-left PENDING evidence as UNKNOWN with zero resubmissions. |
| T6-026 | High | FIXED | Privacy legal-hold serialization | An erasure transaction could pass its no-hold check, then a legal hold could become ACTIVE before destructive writes committed; `SERIALIZABLE` alone permitted the erasure to win that ordering. Erasure, hold activation and hold release now share a subject-scoped transaction advisory lock, so either the hold commits first and blocks erasure or erasure owns the subject before activation can complete. |
| T6-027 | High | FIXED | Governance audit durability | Sensitive incident, export, download and delivery-reconciliation mutations could commit before a best-effort global audit write, leaving committed state without durable audit evidence. PostgreSQL paths now enqueue an exact prepared audit event inside the business transaction; the outbox publishes it idempotently into the append-only hash chain, including canonical JSON replay normalization. |
| T6-028 | High | FIXED | Retention scheduling | Retention scheduling always bound seven parameters although inbound-content, audience-import and export-object queries reference only six; PostgreSQL rejected those schedulers before jobs could be created. Cutoff is now appended only for query families that actually use `$7`, while six-parameter and seven-parameter families are regression-tested. |
| T6-029 | High | FIXED | Inbound content retention/encryption | The legacy plaintext length constraint rejected both normal encrypted inbound writes and governed retention redaction because both intentionally use `message_text=''`. Migration 0074 permits empty legacy plaintext only when encrypted content exists or the row is retention-redacted, while still rejecting unencrypted/unredacted empty content. |
| T6-030 | High | FIXED | Export revocation atomicity | Export revocation committed REVOKED state plus audit evidence before unused download grants were revoked in a second transaction; a grant-revocation failure could therefore leave an audited REVOKED export with a still-usable grant. Export state, unused-grant revocation and audit outbox evidence now commit or roll back together in one serializable PostgreSQL transaction. |
| T6-031 | High | FIXED | Signed control-plane redirect boundary | Provider-event callbacks and runtime-registration heartbeats used fetch redirect-following defaults while carrying HMAC trust headers. Both outbound signed control-plane paths now use `redirect: manual`, matching the existing inbound callback fail-closed behavior. |
| T6-032 | Medium | FIXED | Bounded callback error handling | Provider and inbound callback failures called `response.text()` before truncating, allowing an unbounded control-plane response body to be buffered in gateway memory. A shared streaming reader now consumes at most 300 bytes, cancels the remainder and sanitises control characters. |
| T6-033 | High | FIXED | Session-authority crash durability | Accepted higher session lease fences were written to a temporary file and renamed without fsyncing the file or containing directory. A host crash could therefore lose newly accepted authority evidence and reopen an older fence after restart. Authority replacement now uses exclusive temp creation, file fsync, atomic rename and directory fsync before success returns. |
| T6-034 | Medium | FIXED | Executable gateway tmpfs | Production Compose mounted gateway `/tmp` writable with `nosuid` but without `noexec`. Packaged Chromium was verified to run with a `noexec` tmpfs, so the gateway now uses `/tmp:size=512m,noexec,nosuid` like the hardened Go services. |
| T6-035 | Medium | FIXED | Previous callback-secret least privilege | The gateway loaded and mounted `GATEWAY_CALLBACK_SECRET_PREVIOUS` even though previous callback keys are verified only by the control API. The previous key remains on the control API for rotation compatibility but is no longer exposed to the gateway process. |
| T6-036 | Medium | FIXED | Unused gateway secret exposure | `PROFILING_TOKEN` and `MEDIA_DOWNLOAD_SECRET` were loaded and mounted into the gateway with no runtime consumer. Both were removed from the gateway bootstrap and secret mount while their legitimate control-api/worker consumers remain unchanged; retained media SSRF protection continues to use `SSRF_ALLOWED_HOSTS=control-api`. |
| T6-037 | Medium | FIXED | Structured email PII logging | The shared JSON logger redacted credentials, tokens, DSNs and MSISDNs but allowed structured `email` fields through unchanged; bootstrap-admin creation logged the configured email. Structured email keys are now classified sensitive and redacted by the shared logger. |
| T6-038 | High | FIXED | Audit-chain serialization retry exhaustion | PostgreSQL audit append already retried SQLSTATE 40001/40P01 inside a serializable transaction, but exhausting that inner retry returned the raw database error. The recorder only refreshes the chain head on `ErrChainConflict`, so sufficient contention could fail an otherwise valid governed audit write. Exhausted retryable PostgreSQL transaction errors are now normalized to `ErrChainConflict`, allowing the recorder's existing bounded head-refresh loop to continue safely. |
## Post-Task-6 Meta/backend blind-council hardening — 17 August 2026

The historical T6-001…T6-038 register remains closed. Subsequent Meta and final-backend review rounds R11–R19 found additional defects in the intentionally dirty worktree; these are not retroactively renumbered as Task-6 items. R18 changed source after two confirmed routing-capacity defects. Fresh R19 is the local backend source-freeze authority because no R19 source defect survived independent adjudication.

| Review | Confirmed hardening closed before R19 |
|---|---|
| R11–R13 | Meta sender/tenant/frozen-route authority, durable conversation windows, UNKNOWN handling, configuration fail-close, worker bounds, immutable evidence/readiness and final migration authority through 0087. |
| R14-A | Quoted-WAMID sender mismatch now falls back to authenticated From for inbound STOP/window evidence; decoder-enforced text/timestamp claims were rejected as false positives. |
| R14-B | Frozen-route unavailability cannot ADMIT; live capacity reservations reject TRUNCATE; missing campaign creation is classified invalid; migration 0088 pins the live-capacity truncate fence in readiness. |
| R14-C | Provider ambiguity begins only around provider.send; delete tombstone is durable before provider deletion; provider/engine is part of durable same-lease authority; tombstoned create/QR/pairing are blocked; pipeline state is bounded. |
| R14-D | Future-effective provider retirement cannot invert its effective window; message variable/link identity is canonicalised before persistence/hash. |
| R15-C | Lifecycle create/start/QR/pairing now use one tombstone-serialised authority guard; declared command bodies fail closed without raw-body evidence; pre-network control rejection is rate-neutral while provider failure still backs off; permanently deleted pipeline state is reclaimed. |
| R16-A | Signed Meta batches now process bound sibling STOP/window evidence despite a replay-conflicting status or an unbound sibling change; replay conflicts are deferred until sibling processing completes and all-unbound batches still fail 403. |
| R16-C | Session lifecycle command MACs now bind node/version/pool/provider/engine/adapter target identity end-to-end between the Go signer and gateway verifier, preventing cross-worker retargeting under a shared command secret. |
| R17-A | Frozen-plan unsharded Meta recipients now resolve through the exact approved active/quarantined Meta-sender route; a conversation-window replay conflict no longer prevents independently authoritative STOP suppression, and the batch conflict is returned only after consent processing. |
| R17-B | Every weighted frozen route must have usable capacity meeting its frozen reservation before admission; zero-total-weight shard assignment fails closed; migration 0089 rejects DELETE of live HELD/ACTIVE reservations while preserving RELEASED tombstones, and readiness requires the migration-89 capability. |
| R18-B | Capacity creation now rejects DISPATCHING/PAUSED and terminal COMPLETED/COMPLETED_WITH_EXCEPTIONS/CANCELLED campaigns at both service and locked PostgreSQL boundaries. Daily reservation authority is evaluated per UTC day touched by the campaign window, so sequential same-day windows cannot exceed the pool's independent daily entitlement; strict interval overlap remains correct for MPM/hourly authority. |
| R19-FREEZE | Historical freeze authority: A/C/D each had independent Grok+Qwen PASS evidence; B had Qwen+Gemini PASS and one rejected unreachable timezone claim. |
| I1-DIAGNOSTIC | Infrastructure review invalidated the R19 freeze for forward development by exposing static OpenWA network submission behind node-specific authority and an obsolete all-in-one production Compose. |
| I2-INFRA | Node-addressed OpenWA dispatch/test sends, real-PG route-validator corrections and split Railway/Hostinger production references were locally GREEN before blind review. |
| I2-COUNCIL | Four confirmed defects: cross-provider media used Docker-local HTTP; production runtime could advertise Docker-local gateway URL; selected governed node could fall back to static routing when destination was blank; platform-governance S3 validation incorrectly required filesystem root. All four were RED-proven and fixed in the I2→I3 batch. |
| I3-PRE-REVIEW | Post-remediation exact candidate is locally re-gated, traceability remains 168/181/32/17, all 8 release hard gates remain open/external, and fresh exact-source four-model blind council is mandatory before freeze. |

Current local verification at the R19 source freeze: both accepted R18 findings remain GREEN on PostgreSQL 17 and 18; complete execution unit/integration/race gates are GREEN; fresh isolated exact-source complete Go test/vet/build is GREEN with marker R19_EXACT_GO_GREEN; production Compose/OpenAPI/secret/Node-security checks are GREEN; gateway/admin npm audits report zero vulnerabilities. R19 blind review produced no surviving source defect. External/live gates remain separate.

Local backend source is frozen at R19. Any later production-source change reopens the substantial-chunk gate and requires fresh deterministic verification plus blind review before a new freeze. The next engineering boundary is infrastructure/platform proof; live Railway/Hostinger/provider mutation remains separately authorised.

## Direct regression evidence

- `internal/operations/reporting_privacy_activation_postgres_integration_test.go`
- `internal/provider/registry_postgres_activation_integration_test.go`
- `internal/segment/definition_postgres_integration_test.go`
- `internal/audience/importer/reconciliation_postgres_integration_test.go`
- `internal/delivery/reconciliation/postgres_record_integration_test.go`
- `internal/testmessage/postgres_send_integration_test.go`
- `internal/message/postgres_create_integration_test.go`
- `internal/persistence/postgres/campaign_repository_postgres_integration_test.go`
- `internal/operations/export_postgres_integration_test.go`
- `internal/execution/postgres_metrics_integration_test.go`
- `internal/sender/heartbeat_integrity_test.go`
- `internal/sender/heartbeat_postgres_integration_test.go`
- `internal/sender/session_heartbeat_test.go`
- `internal/platform/httpserver/sender_heartbeat_authorization_test.go`
- `scripts/test-gateway-session-authority.js`
- `scripts/test-gateway-outbox-integrity.js`
- `internal/platform/httpserver/campaign_execution_transition_boundary_test.go`
- `scripts/test-gateway-session-drain.js`
- `internal/execution/start_reservation_recovery_test.go`
- `internal/execution/routing_release_postgres_integration_test.go`
- `database/migrations/0073_campaign_execution_lifecycle_atomicity.sql`
- `internal/campaign/service_prepare_transition_test.go`
- `internal/execution/lifecycle_coordinator_test.go`
- `internal/execution/lifecycle_postgres_integration_test.go`
- `internal/execution/postgres_event_integration_test.go`
- `internal/platform/httpserver/campaign_execution_atomicity_test.go`
- `internal/platform/httpserver/task6_authorization_matrix_test.go`
- `internal/commercial/service_test.go`
- `internal/organisation/policy_test.go`
- `internal/provider/registry_test.go`
- `internal/consent/optout_policy_test.go`
- `internal/inbound/retention_policy_test.go`
- `internal/sender/pacing_test.go`
- `internal/execution/postgres_lease_fencing_integration_test.go`
- `internal/execution/runner_test.go`
- `internal/outbox/postgres_concurrency_integration_test.go`
- `internal/delivery/metrics_idempotency_postgres_integration_test.go`
- `scripts/test-gateway-durability.js`
- `scripts/test-gateway-crash-unknown.js`
- `internal/dispatch/task6_unknown_recovery_test.go`
- `internal/delivery/task6_cancellation_submission_postgres_integration_test.go`
- `internal/dispatch/task6_queue_repair_unknown_postgres_integration_test.go`
- `internal/dispatch/http_gateway_test.go`
- `internal/jobs/postgres_claim_integration_test.go`
- `internal/delivery/attempt_history_postgres_integration_test.go`

- `internal/privacy/task6_legal_hold_race_postgres_integration_test.go`
- `internal/retention/task6_legal_hold_postgres_integration_test.go`
- `internal/retention/task6_privacy_hold_release_expiry_postgres_integration_test.go`
- `internal/audit/task6_atomic_evidence_postgres_integration_test.go`
- `internal/platformpolicy/task6_configuration_activation_postgres_integration_test.go`
- `internal/operations/task6_audit_atomicity_postgres_integration_test.go`
- `internal/operations/task6_export_audit_atomicity_postgres_integration_test.go`
- `internal/operations/task6_remaining_audit_atomicity_postgres_integration_test.go`
- `internal/operations/task6_export_revocation_atomicity_postgres_integration_test.go`
- `internal/outbox/audit_publisher_test.go`
- `database/migrations/0074_inbound_reply_encrypted_and_redacted_content.sql`
- `scripts/test-gateway-control-plane-security.js`
- `internal/observability/observability_test.go`
- `internal/audit/postgres_retry_test.go`
## Review domains still open

All local Task 6 adversarial-review domains and the final Task-7 verification gate are complete. T6-001 through T6-038 are FIXED with direct regression evidence. External/live and deployment/release gates remain separate and are not represented as locally completed evidence.

External/live findings remain separate from code findings: genuine WhatsApp authentication/pairing/sends/acks/reconnect/ambiguous outcomes; sustained target-volume/endurance; real sender-session resource measurements; independent security assurance; Railway+Hostinger deployment/network validation; backup/restore/DR; and operational-owner approval.
