# Release 0.8.13 — Audience Operational Core III

This release completes the durable asynchronous audience-snapshot materialisation path required for multi-million audiences.

## Changes

- Added durable audience-materialisation jobs with `PENDING`, `RUNNING`, `COMPLETED`, `FAILED` and `CANCELLED` states.
- Added authoritative scheduling from a campaign and either a governed saved segment or validated inline definition.
- Added deterministic request fingerprints so exact replay returns the same active job while conflicting requests are rejected.
- Added bounded page processing of up to 10,000 contacts per page without loading the full audience into worker memory.
- Added persistent contact cursor, processed count and rolling snapshot hash checkpoints.
- Added fenced worker leases, renewal, stale-worker rejection and safe restart from the last committed page.
- Added bounded retry scheduling, maximum-attempt failure handling and non-sensitive failure references.
- Added progress reporting, campaign job history and governed cancellation with actor, reason and optimistic version.
- Added atomic PostgreSQL promotion of staged members into an immutable audience snapshot.
- Added migration `0045_async_audience_materialisation.sql` and worker/runtime configuration.
- Expanded OpenAPI coverage to all 124 implemented control-plane method/path pairs.

## Validation boundary

Automated tests prove deterministic scheduling, conflicting active request rejection, paged continuation, lease fencing, cancellation evidence and final snapshot creation. Live PostgreSQL execution, million-record performance measurement and production query-plan evidence remain external release gates.
