# Release 0.8.6 — Governed organisation policy controls

This backend-remediation checkpoint replaces unstructured organisation restrictions with effective-dated, maker-checker policy versions.

## Added

- Governed organisation policy lifecycle: DRAFT, PENDING_APPROVAL, ACTIVE, REJECTED and RETIRED.
- Organisation-specific allowed and prohibited campaign-purpose catalogues.
- Organisation-specific contact and campaign retention periods.
- Organisation report brand name and footer metadata.
- Maker-checker enforcement and optimistic version fencing.
- Automatic retirement of the previously active policy when a new version is approved.
- Campaign creation pre-flight against the active organisation policy.
- Protected policy administration APIs and OpenAPI definitions.
- PostgreSQL migration `0038_organisation_policy_governance.sql`.
- Updated SRS evidence for ORG-005, ORG-010 and BR-029.

## Validation

- `go test ./...`
- `go vet ./...`
- `go build ./...`
- TypeScript/TSX syntax transpilation
- OpenAPI route coverage and YAML parsing
- Governance structure validation

## Still blocked

A pinned PostgreSQL driver and live PostgreSQL execution remain unresolved because the isolated workspace cannot reach the Go module registry. No live-database validation is claimed.
