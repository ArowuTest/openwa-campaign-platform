# Release 0.8.16 — Dispatch Sharding and Queue Recovery

## Scope

This release strengthens the messaging operational core with deterministic campaign dispatch shards, fenced shard supervision, health-aware allocation and forecasting from the preceding progress commit, adaptive per-session throttling, and automatic reconstruction of missing dispatch jobs from authoritative PostgreSQL outbox evidence.

## Dispatch sharding

- Campaign recipients are deterministically grouped into bounded shards.
- Shard identity (campaign, ordinal and target size) is immutable.
- The campaign worker discovers eligible campaigns, assigns recipients, claims shards with `SKIP LOCKED`, and uses expiring fenced leases.
- Shard progress is derived from canonical recipient states.
- Clean terminal shards become `COMPLETED`; failed or unknown outcomes become `COMPLETED_WITH_EXCEPTIONS`.
- Worker loss is recoverable after lease expiry without changing shard identity.

## Queue repair

- A periodic repair runner detects published recipient outbox events with no corresponding durable dispatch job.
- Missing jobs are recreated with the original deduplication key.
- The durable-jobs unique constraint makes concurrent normal publication and repair safe.
- Only recipients still eligible for dispatch work (`AUTHORISED`, `QUEUED`, or `FAILED_RETRYABLE`) are repaired.
- PostgreSQL remains authoritative; Redis or transient queue loss does not erase obligations.

## Allocation, throttling and forecasting

- Sender allocation excludes daily-capacity-exhausted and in-flight-saturated sessions.
- Allocation prefers healthier, less utilised and lower-failure sessions while retaining deterministic tie-breaking.
- Dispatch throughput is reduced based on health, queue pressure, provider latency and recent failures.
- Campaign execution forecasts expose projected completion, deadline slack, capacity sufficiency, bottlenecks and risk.

## Configuration

- `DISPATCH_SHARD_TARGET_SIZE`
- `DISPATCH_SHARD_CLAIM_BATCH`
- `DISPATCH_QUEUE_REPAIR_BATCH`
- `DISPATCH_QUEUE_REPAIR_INTERVAL`

## Migration

- `0049_campaign_dispatch_shards.sql`

## Validation completed

- `go test ./...`
- `go vet ./...`
- `go build ./...`
- targeted race tests for execution, dispatch, jobs and outbox
- migration/schema validation
- Compose parsing

## External validation still required

Live PostgreSQL execution, production-scale shard performance, dependency-resolved OpenWA builds, genuine WhatsApp traffic, and Hostinger deployment remain release gates.
