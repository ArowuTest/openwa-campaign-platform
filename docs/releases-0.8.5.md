# Release 0.8.5 — Audience Merge and Conflict Governance

This remediation release closes the incomplete audience merge-policy paths identified during the adversarial review.

## Implemented

- Complete support for `INSERT_ONLY`, `FILL_NULL`, `NEWEST_SOURCE`, `TRUSTED_SOURCE`, and `MANUAL_CONFLICT` merge policies in both memory and PostgreSQL compositions.
- Governed organisation/source-system trust scores with optimistic versioning, MFA-protected administration, immutable database event history, and API contracts.
- Durable profile-conflict capture for country, state, LGA, reported age, and gender disagreements.
- Conflict list and resolution APIs with permissions, recent MFA, mandatory reason, optimistic concurrency, and controlled `KEEP_EXISTING` / `USE_INCOMING` decisions.
- PostgreSQL migration `0037_audience_profile_conflict_governance.sql`.
- Runtime readiness checks for the new governance schema.
- OpenAPI coverage expanded to all 95 implemented `/api/v1` method/path pairs.

## Verification

- `go test ./...`
- `go vet ./...`
- `go build ./...`
- OpenAPI route verifier
- OpenAPI and Compose YAML parsing

## Remaining hard blockers

- A pinned PostgreSQL driver is still not linked because outbound dependency retrieval is blocked in this workspace.
- Migrations and repository behavior have not yet been executed against a live PostgreSQL instance.
- First-party Node dependency installation, typechecking, tests, and production builds remain outstanding.
- Genuine OpenWA and WhatsApp-network validation remains outstanding.
