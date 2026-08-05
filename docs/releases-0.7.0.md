# 0.7.0 — Campaign Execution Engine

This release completes the code-level campaign execution milestone.

## Capabilities

- Evidence-based campaign admission using measured sender throughput, remaining daily capacity, campaign deadline and a configurable safety margin.
- Immutable capacity assessments with explicit `ADMIT`, `HOLD` or `REJECT` decisions and forecast completion timestamps.
- Automatic scheduler claims for due campaigns using fenced PostgreSQL leases and `FOR UPDATE SKIP LOCKED`.
- Governed start, pause, resume, cancel and completion controls with optimistic concurrency, MFA step-up at the API boundary and append-only execution events.
- Resume is capacity-gated again; a paused campaign cannot resume merely because it was previously approved.
- Automatic completion distinguishes clean completion from completion with failed or unknown outcomes and does not complete while retryable or in-flight obligations remain.
- Specific-session and sender-pool routes both use measured live capacity.
- Campaign worker now runs outbox publication, dispatch processing and execution scheduling in one supervised process with shared health reporting and graceful shutdown.

## Honest validation boundary

All Go tests, vet, builds, schema tests and OpenAPI parsing pass. Live PostgreSQL migration execution, Docker deployment, genuine WhatsApp execution and measured production capacity remain external release gates.
