# Current status

## Current repository baseline

Version `0.8.25` on branch `work/backend-trust-boundary-hardening` is the current backend checkpoint.

The platform remains an internal managed-service campaign control plane. Go and PostgreSQL own consent, audience eligibility, campaign approval, recipient obligations, canonical delivery state, reporting and audit. OpenWA remains an isolated replaceable transport.

## Latest hardening

- Dispatch and controlled test sends carry expiring node, session lease, configuration and route-fencing evidence to the gateway.
- Gateway requests fail closed on runtime identity, pool, provider, engine, adapter, version or fence mismatch.
- Sender sessions follow a canonical governed lifecycle and no longer default to ready.
- Pairing and QR operations are controlled through the Go API and require technical-administrator access plus recent MFA.
- Real inbound-message forwarding now uses a durable gateway outbox and signed callback path.
- Message media and consent evidence use trusted intake with derived metadata, malware scanning, byte-integrity revalidation and governed revocation.
- Consent reviews support organisation/source/campaign scope, draft submission, independent decision, restrictions, expiry, revocation and superseding revisions.
- Migrations `0060` and `0061` add the corresponding database authority and evidence controls.

## Honest readiness position

The backend is not yet software-complete or deployment-certified. The next code-side package covers secure export retrieval, real XLSX audience processing, privacy/data-subject cases, reporting privacy thresholds, fuller contact lifecycle, expanded audit coverage and production S3-compatible object storage. Live infrastructure and frontend gates remain separately open.
