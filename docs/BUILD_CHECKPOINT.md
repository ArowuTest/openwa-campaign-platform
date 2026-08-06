# Build checkpoint

Current version: **0.8.27**

Branch: `work/backend-platform-governance`

This checkpoint extends `0.8.26` with shared platform governance, signed gateway runtime discovery, governed gateway-pool/node administration, maintenance and emergency controls, cross-domain retention execution, operational alert escalation and immutable incident timelines.

Implemented code-level scope includes:

- exact-body HMAC gateway runtime registration with durable nonce replay protection;
- governed runtime identity including boot ID, provider/engine, adapter/gateway/worker/configuration versions, capabilities, heartbeat, capacity, sessions, queue, CPU and memory evidence;
- maker-checker gateway-pool lifecycle and in-use retirement guards;
- protected gateway-node drain, offline and retirement operations;
- common configuration draft, submission, approval, activation, effective dating, supersession, retirement and rollback-by-replacement;
- maintenance modes for read-only, admission freeze, draining and emergency stop, enforced inside API and business-service boundaries;
- governed retention policies and durable fenced jobs with legal-hold-aware precedence, retries, evidence and review holds;
- supported retention execution for inbound content, audience-import source files and expired export objects;
- governed operational alert policies, threshold evaluation, cooldown, recovery, escalation, portal delivery and optional automatic incident creation;
- append-only incident assignment, investigation, mitigation, resolution and closure timelines committed atomically with incident state;
- a dedicated `platform-governance-worker` with readiness/liveness, bounded cycles and graceful shutdown;
- migration `0065_platform_governance_runtime_retention_and_alerts.sql`;
- OpenAPI coverage for 277 implemented method/path pairs.

It remains pre-production. Live PostgreSQL migration execution, production-scale telemetry and alert validation, real OpenWA sessions, multi-node Hostinger networking, penetration testing, backup/restore evidence, complete destructive-retention executors, full observability and the production frontend remain release gates.
