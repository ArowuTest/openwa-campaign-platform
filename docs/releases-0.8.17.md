# Release 0.8.17 — Durable Audience Import Merge Runtime

## Scope

This release closes a major operational gap between approved audience-import previews and canonical contact/consent promotion. Approved imports are now claimed and merged by the audience worker through durable PostgreSQL leases rather than depending on an operator or synchronous request to trigger the merge.

## Merge worker

- Claims approved or abandoned importing batches using `FOR UPDATE SKIP LOCKED`.
- Uses expiring leases and fencing versions.
- Renews leases during long merges.
- Reclaims abandoned work after lease expiry.
- Persists bounded retry timing and non-sensitive failure detail.
- Keeps exact merge replay idempotent through the existing immutable merge-result record.
- Runs alongside validation and asynchronous audience materialisation in the audience-worker executable.

## Merge policies

The worker executes the existing governed policies consistently in memory and PostgreSQL:

- `INSERT_ONLY`
- `FILL_NULL`
- `NEWEST_SOURCE`
- `TRUSTED_SOURCE`
- `MANUAL_CONFLICT`

Manual-conflict imports continue to create review records while filling only previously empty profile values. The canonical import is completed only after contact, source-lineage, profile-history and consent-grant writes commit atomically.

## Recovery and safety

- Failed merges return to `APPROVED` with a delayed next-attempt time.
- Successful completion clears merge ownership and failure evidence.
- Stale workers cannot renew or release a newer lease.
- Worker shutdown waits for a substantive merge result where possible so a simultaneous failure is not silently lost.
- PostgreSQL remains the authoritative record; no Redis queue is required for merge discovery.

## Configuration

- `AUDIENCE_MERGE_CONCURRENCY`
- `AUDIENCE_MERGE_CLAIM_BATCH`
- `AUDIENCE_MERGE_LEASE_DURATION`
- `AUDIENCE_MERGE_POLL_INTERVAL`
- `AUDIENCE_MERGE_FAILURE_BACKOFF`

## Migration

- `0050_audience_import_merge_worker.sql`

## Validation completed

- `go test ./...`
- merge-worker success and failure tests
- worker configuration tests
- migration/schema validation

## External validation still required

Live PostgreSQL execution, high-volume import merge profiling, dependency-resolved PostgreSQL driver integration and production object-store testing remain release gates.
