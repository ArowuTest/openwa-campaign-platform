# Release 0.8.4 — Backend Adversarial Remediation I

## Scope

This remediation checkpoint closes the first group of repository-wide adversarial findings without advancing frontend visual implementation ahead of the external designer reference.

## Organisation lifecycle enforcement

- New organisations now start `UNDER_REVIEW`, not `ACTIVE`.
- Campaign creation requires an active organisation.
- Campaign progression rechecks organisation status; pause, cancellation and completion safety controls remain available when an organisation is inactive.
- Consent-review creation and approval require an active organisation.
- Audience-import creation and approval require an active organisation.
- Final campaign-recipient release rechecks organisation status immediately before obligations are created.
- Memory and PostgreSQL runtime compositions use the same organisation reader.

## API contract remediation

- Removed duplicated `/api/v1` prefixes from OpenAPI paths.
- Added every implemented control API method/path pair missing from the OpenAPI document.
- Added `scripts/verify_openapi_routes.py` to compare implemented routes with the contract.
- CI now fails when an implemented route is absent or when a path duplicates the server prefix.

## Deployment topology remediation

- Audience, campaign, metrics, export and inbound-governance workers are no longer hidden behind an optional Compose profile.
- The default topology now represents the operational services required by the application.
- The PostgreSQL driver remains a separate Critical release blocker until a pinned dependency can be obtained and linked reproducibly.

## Verification boundary

This release does not claim live PostgreSQL, resolved Node dependency builds, genuine WhatsApp operation or production certification.
