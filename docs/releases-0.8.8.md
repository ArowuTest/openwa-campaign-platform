# Release 0.8.8 — Consent Precedence and Live Evidence Revalidation

This remediation release closes a high-risk consent correctness gap across campaign approval, recipient release and final dispatch.

## Changes

- Campaign consent-review evidence is revalidated at consent approval, final-approval request, final approval, dispatch start and resume.
- Safety actions such as cancellation remain available after evidence invalidation.
- Recipient release now fails closed when the organisation is inactive or the consent review is no longer approved, unexpired and channel-compatible.
- Final dispatch now returns precise exclusion reasons for inactive contacts/organisations, invalid campaign/review state, suppression, withdrawal, revocation, expiry and missing consent.
- Consent precedence now resolves the latest effective grant. A later valid re-consent can supersede an earlier withdrawal while preserving the full evidence history.
- Active suppression always overrides an otherwise valid grant.
- Migration 0040 adds indexes for latest-effective-grant and final suppression checks.

## Validation

`go test ./...`, `go vet ./...` and `go build ./...` pass. Live PostgreSQL execution remains a release blocker.
