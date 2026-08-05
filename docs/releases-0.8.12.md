# Release 0.8.12 — Audience Operational Core II

This release closes several operational gaps around reusable segments, audience comparison and import completion evidence.

## Changes

- Added governed segment cloning with independent identifiers, immutable histories and permission-aware validation.
- Added semantic comparison between two immutable segment versions, including added, removed and changed rules.
- Added identity-free audience snapshot overlap analysis with intersection, left-only, right-only and union counts.
- Added audience-import reconciliation previews across uploaded, valid, invalid, duplicate, suppressed, inserted, updated and conflict totals.
- Added immutable reconciliation closure evidence with SHA-256 integrity, maker identity, reason and append-only PostgreSQL storage.
- Added atomic batch resolution for up to 100 pending audience profile conflicts with optimistic version enforcement.
- Added migration `0044_audience_import_reconciliation.sql`.
- Expanded OpenAPI coverage to all 120 implemented control-plane method/path pairs.
- Updated traceability to 54 implemented/tested, 33 partial and 311 not started requirements.

## Remaining scope

- Durable asynchronous snapshot materialisation with fenced leases, checkpoints, progress and restart-safe resume.
- A distinct unchanged-contact count in import reconciliation.
- Import rollback for legally and operationally safe unconsumed imports.
- Database-backed count waterfalls, anonymised samples and privacy-threshold suppression.
- Live PostgreSQL execution and high-volume query-plan evidence.
