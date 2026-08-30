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


## 18 August 2026 — I3 final infrastructure freeze (I3-FINAL-FREEZE-20260818)

I3 is frozen locally after the complete remediated2 gate and fresh genuinely blind council. Exact full Go gate: `go test -count=1 ./... && go vet ./... && go build ./...` exited 0 with `I3_REMEDIATED2_EXACT_GO_GREEN`. Fresh supporting evidence is also GREEN: governance 56/56, gateway control security 15/15, Linux outbox 9/9, session authority 3/3, lifecycle 18/18 plus serialization, affected race, gateway TypeScript, secret scan, OpenAPI 294/294, split topology/Compose, and zero-vulnerability Node audits.

Final source/config freeze: tracked non-doc diff SHA-256 `0fe4c15839ec7207ed6f6931446c4377fc94c82a0bee6856cfec832e46a3f53e`; untracked non-doc content SHA-256 `529a278c0e3eb86deb87676439f66bf46159b0851b5fea265ab8e0f06aac3a72` across 189 files. Fresh blind packet: 860176 bytes, SHA-256 `d97a1a54d39509312ccf03191a9d62730f6a7bb97ad9b65fd83d77bae7f8b48f`.

Final blind council: Grok 4.6 PASS (attempt 1), Qwen 3.8 Max PASS (attempt 1), Gemini 3.7 Flash PASS (attempt 1), GLM 5.2 PASS (attempt 2; first attempt was an evidence limitation only). Normalization found no material finding and therefore no remediation was authorized.

Release honesty is unchanged: traceability is 398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL; all eight hard release gates remain OPEN or BLOCKED_EXTERNAL. This freeze does not claim live Railway/Hostinger deployment, real-provider validation, production-scale evidence, DR proof, independent penetration testing, operations exercise, or frontend completion.

Final remediated2 round produced no new material finding after blind normalization; no further I3 remediation was authorized.

## 24 August 2026 — I4 Fresh14 adjudication and Fresh15 remediation boundary

Fresh14 produced eight usable blind lanes across three actual model families; two DeepSeek lanes were unusable at the response-length boundary and are correctly labelled as GLM 5.3 fallbacks. Independent adjudication is preserved at `validation/ai-review/infrastructure-i4-fresh14-20260823/I4-fresh14-adjudication.md`. Confirmed defects were governed-node URL drift, case-variant waiver self-approval, a fixed-date test time bomb, and the reachable unassigned-node portion of rejection-evidence loss. The claimed staging allowlist bypass and native IPv6 control-host bypass were rejected. A maximum waiver horizon remains policy authority not supplied by engineering; no arbitrary duration was invented.

The coherent Fresh15 remediation now binds a non-empty governed node URL while preserving first-registration bootstrap; compares waiver actors with canonical Unicode case-folding; uses a time-relative acceptance fixture; and adds migration 0090 so only authenticated `REJECTED` runtime events may lack a governed pool anchor while the declared pool remains in immutable runtime identity. The original deleted-pool sequence is prevented by the existing sender-node foreign key.

Current post-change host evidence is governance **128/128 GREEN**, all Go packages except the Linux-only secret-file mode test GREEN on Windows, `go vet ./...` GREEN, `go build ./...` GREEN, focused URL/schema regressions GREEN, and diff hygiene GREEN. The excluded secret-file test correctly cannot prove Unix group/world bits on Windows and will not be weakened. Exact Go 1.23.2 Linux test/race, fresh PostgreSQL 17 migration histories through 0090, the 40-DSN matrix, gateway/Node/static/security/SBOM gates and Fresh15 exact-source review remain mandatory before acceptance.

The sender-name discussion is now durable authority in ADR-0006 and `docs/requirements/CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`: reusable underlying account, exclusive campaign reservation, requested-versus-observed profile identity, verification before READY, versioned engine capabilities, and disabled-by-default username operations until genuine validation.

All eight release hard gates remain OPEN or BLOCKED_EXTERNAL. No live deployment/provider traffic, production credential mutation, scale proof, DR exercise, penetration test, operational exercise or frontend completion is claimed. Fresh14 is historical after source changes; Fresh15 has not yet been frozen or reviewed.

## 24 August 2026 — I4 Fresh18 sizeable remediation and pre-council boundary

Fresh16 and Fresh17 were sizeable exact-source council boundaries, not file-by-file reviews. Fresh16 confirmed four defects that Fresh17 remediated: atomic nonce/rejection persistence, durable governed-pool rejection anchoring, gateway/control hostname separation, healthcheck-scoped Compose verification, and observed private-bind evidence. Fresh17 then confirmed three remaining defects: wall-clock runtime causality allowed a retired boot to reclaim service and fenced healthy clock corrections; invalid production capacity could be swallowed by deferred registration; and production gate-closure metadata was not bound to the actual candidate evidence artifact.

Fresh18 remediates those findings as one engineering chunk. Signed runtime reports now carry a positive per-boot `runtimeSequence`; memory and PostgreSQL accept only a newer same-boot sequence, retain accepted boot history, reject every state from a retired boot, keep same-boot DRAINING terminal, and treat `observedAt` as freshness evidence rather than a causal clock. Production gateway startup validates capacity before deferred publication. Production-candidate release verification now requires safe local evidence paths, SHA-256 byte binding, schema-versioned JSON manifests bound to the exact gate and candidate fingerprint, non-empty observed evidence, and independent owner/approver identity. Development-mode structural checks and honestly open gates remain unaffected.

Fresh18 exact-source evidence is GREEN: **2,384 files / 0 byte mismatches**; governance **137/137**; Go ordinary, full race, vet and build; fresh PostgreSQL 17 with three histories through migration 0090 and the full **40-DSN** suite; gateway TypeScript/build, control-plane security **30/30**, and **38** retained durability/lifecycle tests; OpenAPI **294/294**; production Compose **7 Railway services / 25 secrets**; resolved Hostinger boundary for both BAILEYS and WHATSAPP_WEB_JS; committed-secret scan; Node audits **0/0 vulnerabilities**; strict CycloneDX **30 components / 8 images** with SHA-256 `84257faddf0d708bf0a9e0b0b95d897fdc5ef40a922b1ff4225aee8fcf1be7c3`. Production-candidate validation correctly exits 2 because all eight release gates remain OPEN or BLOCKED_EXTERNAL.

The campaign-scoped sender-name authority remains ADR-0006 plus `CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`: reusable account identity is distinct from exclusive campaign reservation and requested-versus-observed sender profile identity. No live deployment, provider traffic, production credential/network mutation, capacity proof, DR exercise, penetration test, operational exercise, or frontend completion is claimed. Fresh18 must now be frozen twice and reviewed once as this complete 54-path chunk; no staging or local acceptance commit is permitted before council adjudication.

## 24 August 2026 — I4 Fresh19 council-remediation boundary

Fresh18 completed one valid 8-lane council after the full sizeable chunk; two primary Grok lanes timed out twice and used explicitly labelled GLM 5.3 fallbacks. The council rejected Fresh18. Adjudication confirmed six material issues and rejected the unsupported or overbroad claims: gateway authority URLs admitted credentials or non-authority components and lacked canonical host/port equality; production capacity was not explicitly coupled to the engine session limit; release manifests did not bind each evidence item to governed gate-local bytes; a rejected authenticated DRAINING declaration did not make that boot terminal; governed runtime authority was checked before but not inside the serialized store transaction; and a configured production boot ID could be reused while the runtime sequence restarted.

Fresh19 remediates those findings as one coherent backend/infrastructure chunk. Go and TypeScript now accept only HTTPS authority-only gateway URLs, canonicalize hostname/trailing dot/port (including default 443), and reject invalid ports. Memory and PostgreSQL recheck governed pool, URL, provider, engine and adapter under their write lock/transaction. Any authenticated DRAINING declaration makes its boot terminal even when the replacement drain is rejected. Production gateway startup rejects configured boot IDs and requires explicit 1..1000 `GATEWAY_CAPACITY` equal to `OPENWA_MAX_CONCURRENT_SESSIONS`; Hostinger Compose and resolved topology enforce the same contract. Production closure/waiver manifests now restrict evidence kinds per gate and bind each item to a non-empty, non-self-referential gate-local artifact with its own SHA-256 fingerprint.

Fresh19 must receive a new exact-source ordinary/race/vet/build, fresh-PG/40-DSN, gateway, governance, topology, release, security, SBOM, traceability and byte-comparison matrix after documentation synchronization. Only then may it be frozen twice and sent to one council as the complete 54-path engineering boundary. The staged index remains empty; no acceptance commit, push, deployment, real-provider traffic, production credential/network mutation, capacity claim, DR claim, penetration-test claim, operational-readiness claim or frontend-completion claim is permitted before adjudication.

The campaign-scoped sender-name authority remains ADR-0006 plus `CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`: reusable account identity is separate from exclusive campaign reservation and requested-versus-provider-observed sender identity. This approved later backend tranche is not silently folded into Fresh19 or claimed implemented.

Fresh19 final executable and governance evidence is GREEN: exact candidate snapshot **2,288 files / 0 byte mismatches**; governance **141/141**; Go ordinary, full race (including the 441.393-second HTTP-server suite), vet and all seven native-libpq command builds; fresh PostgreSQL 17 current/pre-0085/pre-Meta histories through 0090 and the full **40-DSN** repository suite at exit 0; gateway TypeScript/build, control-plane security **32/32**, and **38** retained durability/lifecycle tests; OpenAPI **294/294**; production Compose **7 Railway services / 25 secrets**; resolved Hostinger boundary for BAILEYS and WHATSAPP_WEB_JS; committed-secret scan; Node audits **0/0 vulnerabilities**; strict CycloneDX **30 components / 8 images** with SHA-256 `4cebeb30bcec7a19faad0bc2852b67eb4d7bb9c57582f6827d68f378f1fb3a7f`; and traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL** with byte-identical Markdown and semantically identical JSON regeneration. Production-candidate validation correctly exits 2 because all eight hard gates remain OPEN or BLOCKED_EXTERNAL.

The candidate remains **54 paths**, HEAD `887e577565de22642cf4041a223835d122e6ff15`, branch `work/backend-production-engineering`, with **0 staged paths**. Fresh19 is eligible for deterministic double freeze and one council at this complete boundary; it is not accepted or committed unless adjudication finds no remaining material defect.

## 24 August 2026 — I4 Fresh20 council-remediation boundary

Fresh19 completed one valid blind council at the full 54-path boundary: **8/8 usable lanes**, three actual reviewer families, and three transparently labelled GLM 5.3 fallbacks after unusable primary output. Fresh19 is rejected and remains unstaged. Independent adjudication confirmed nine material or governance defects while rejecting or narrowing overbroad claims: the Railway control origin was not authority-only; TypeScript admitted gateway port zero; Go mishandled IPv6 with explicit default 443; a sibling manifest could be laundered as leaf release evidence; Compose health validation trusted a first-match decoy; invalid URL rejection erased the declared URL; deployed node registration admitted unusable authorities; stale-version DRAINING bypassed nonce/evidence/boot terminality; and README checkpoint authority was stale. The full claim-by-claim record is in the Fresh19 validation evidence directory.

Fresh20 remediates the confirmed set as one coherent backend/infrastructure chunk. Gateway, Go config and resolved topology now share a canonical authority-only HTTPS control origin; full callback/inbound/media endpoints reject credentials, fragments and invalid ports. Gateway advertisement rejects port zero; Go correctly brackets IPv6 after default-port elision. Staging/production node registration enforces the governed authority policy without removing local-development HTTP support. Rejection identity preserves `declaredInternalUrl`. Memory and PostgreSQL route stale-version reports through atomic nonce plus rejection evidence, making an authenticated DRAINING identity terminal. Release verification rejects manifest-shaped leaf evidence, and Compose requires exactly one contract health target. A dedicated `docs/handover/AGENT_PICKUP.md` now provides replacement-agent authority, including sender-name decisions and exact next actions.

Focused Fresh20 evidence is GREEN: gateway control security **34/34**; Linux Go sender/config packages; and deployment-topology plus release-readiness **73/73**. The PostgreSQL conflict regression is authored. Full exact-source ordinary Go, race, vet/build, fresh PostgreSQL histories and 40-DSN suite, gateway TypeScript/build/retained suites, governance/OpenAPI/topology/readiness/security/SBOM/traceability, byte comparison, documentation sync, double freeze, council and adjudication remain mandatory. The candidate is currently **56 dirty paths / 0 staged paths** on HEAD `887e577565de22642cf4041a223835d122e6ff15` and branch `work/backend-production-engineering`; it is not accepted, committed, merged, pushed or deployed.

The campaign-scoped sender identity authority remains ADR-0006 plus the Campaign-Scoped Sender Identity Addendum and is not falsely claimed implemented. All hard release gates remain OPEN or BLOCKED_EXTERNAL; no live deployment, provider traffic, production credential/network mutation, scale proof, DR exercise, penetration test, operational exercise or frontend completion is claimed.


## 27 August 2026 - I4 Fresh20 R4 post-R3 adversarial-remediation boundary

<!-- FRESH20_R4_POST_R3_COUNCIL_REGATE_20260827 -->

Fresh20 R3 was rejected after independent council adjudication confirmed four reachable defects: rejection URL-evidence ordering, malformed PostgreSQL pool-ID replay/audit, deployed reserved control-origin classes, and retry-time `ObservedAt` as an insufficient cross-boot causal fence. R4 remediates all four in the same dirty candidate and hardens topology validation for the already-required Hostinger `NODE_ENV=production` literal. Raw signed declaration evidence is separated from canonical route authority before pool lookup; malformed pool IDs now enter the authenticated nonce-consuming rejection path; staging/production control origins reject local/reserved authority classes; and stable signed `bootStartedAt` plus process-uptime validation drives equivalent memory/PostgreSQL boot-origin fencing. The rejected R3 bytes and reviews remain historical evidence only.

Fresh R4 evidence on current non-document source/config bytes is GREEN: focused memory/PostgreSQL/config regressions; governance **147/147**; gateway control security **35/35**; ordinary Go exit 0; full race exit 0 (`internal/platform/httpserver` 337.532s); `go vet` exit 0; seven native-libpq command builds; fresh PostgreSQL 17.10 current/pre-0085/pre-Meta histories at 141/139/136 tables with migration 0090 invariant `YES/1`; clean full 40-DSN repository matrix exit 0; fresh-lock gateway `npm ci` audit **0 vulnerabilities**, 56-file retained sync, TypeScript typecheck and Nest build; retained OpenWA durability/lifecycle boundary; OpenAPI **294/294**; topology/readiness/Compose **7 services / 25 secret classes**; committed-secret scan; strict local CycloneDX **30 components / 8 synthetically pinned image references**; and reproducible traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**. A cached-symlink gateway dependency fixture emitted broad framework type mismatches; it was discarded after authoritative fresh-lock verification passed.

The candidate remains intentionally dirty and unstaged. R4 is not accepted until post-document verification, exact 2,289-file double freeze, a fresh blind GPT-5.5/Grok-4.6/GLM-5.3 council meeting the evidence floor, and independent adjudication. Any surviving material finding reopens remediation and the full gate. RG-001..RG-008 remain OPEN/BLOCKED_EXTERNAL; no deployment, live-provider, production-network, production-credential, scale, DR, penetration-test or operations proof is claimed. ADR-0006 / CSI-001..CSI-014 remain approved later-backend sender-identity requirements and are not claimed implemented here.


## 28 August 2026 - I4 Fresh20 R5 deployed-authority remediation boundary

<!-- FRESH20_R5_PRIVATE_AUTHORITY_REGATE_20260828 -->

Fresh20 R4 is **rejected historical evidence only**. Two independent GPT-5.5 blind-review lanes reproduced the same reachable High-severity defect: staging/production advertised control and gateway authority accepted private literal IPs even though deployed cross-provider authority is intended to be DNS-only. R5 remediates that boundary consistently in Go control configuration, Go runtime registration, gateway TypeScript validation and resolved topology verification, while leaving local/development bind plumbing such as `GATEWAY_BIND_ADDRESS` unchanged.

Focused RED/GREEN now covers RFC1918 IPv4, IPv6 ULA and IPv4-mapped private IPv6. Go config/sender focused tests are GREEN; gateway control-plane security is **35/35 GREEN**; focused topology is **52/52 GREEN**; governance is **148/148 GREEN**. The full executable gate is also GREEN on an exact 2,289-file Linux source volume verified against `git ls-files --cached --others --exclude-standard`: ordinary Go exit 0 (`httpserver` 72.703s), race exit 0 (`httpserver` 302.243s), vet exit 0, seven production command builds GREEN, and a fresh-from-scratch PostgreSQL 17.10 R5B 40-DSN repository matrix exit 0 (`httpserver` 31.129s). A prior repeated 40-DSN run against an already-exercised R5 fixture was rejected as contaminated evidence after duplicate/fixed-state collisions; it is not a source failure.

Supporting R5 evidence remains GREEN: retained OpenWA durability/UNKNOWN/session boundary, OpenAPI **294/294**, topology/readiness, production Compose **7 services / 25 secret classes**, committed-secret scan, gateway fresh-lock build/typecheck and dependency audit **0 vulnerabilities**, strict SBOM generator **30 components**, and traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**. Production-candidate remains expected exit **2** with RG-001..RG-008 OPEN/BLOCKED_EXTERNAL.

R5 remains intentionally dirty and unstaged pending post-document verification, exact double-freeze, a fresh genuine GPT-5.5/Grok-4.6/GLM-5.3 blind council, and independent adjudication. Any surviving material finding reopens RED->GREEN remediation and the full gate. No deployment, live-provider, production-network, scale, DR, penetration-test or operations proof is claimed. ADR-0006 / CSI-001..CSI-014 remain approved later-backend sender-identity requirements and are not claimed implemented here.
