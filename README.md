# Internal Audience & Campaign Platform

Private, internally operated platform for consent-governed audience management, cohort creation, campaign approvals, WhatsApp dispatch through an isolated OpenWA gateway, and truthful campaign reporting.

## Operating model

External organisations do not receive portal access in the initial release. They request campaigns offline and provide the consent basis, approved content and audience data. Internal operators:

1. Create the organisation record.
2. Record and decide the offline consent review.
3. Preview and approve the audience import.
4. Build a cohort using governed filters.
5. Freeze an immutable audience snapshot.
6. Approve the message, commercial terms and final release.
7. Dispatch through healthy messaging sessions.
8. Report sent, delivered, read, failed and unknown outcomes separately.

## Technology boundaries

- **Next.js/TypeScript** provides the internal operations portal.
- **Go** owns organisations, consent, contacts, segmentation, campaigns, approvals, delivery state, metrics and reporting.
- **PostgreSQL** is the authoritative system of record.
- **Redis** is an execution accelerator, never the sole source of delivery obligations.
- **OpenWA/NestJS** is an isolated, replaceable messaging transport gateway.
- **S3-compatible storage** holds import files, consent evidence, campaign media and generated reports.
- **Hostinger VPS/Docker Compose** is the initial deployment target.

## Current checkpoint

Version `0.3.1` is an integrity-focused development checkpoint. It is not a completed SRS release. The current repository now includes:

- Server-derived identity, MFA/session foundations and maker-checker controls.
- Optimistic concurrency for campaign and consent transitions.
- Immutable message and audience-snapshot evidence.
- Transactional recipient entitlement and outbox creation.
- Fenced leases for imports, outbox records, jobs and metric reconciliation.
- Idempotency keys with exact payload comparison rather than key-only replay.
- Monotonic delivery evidence and signed, replay-resistant gateway callbacks.
- Secure streamed import quarantine, file-signature inspection and ClamAV integration boundary.
- Streaming, resumable audience validation with protected MSISDN staging.
- Final live consent/suppression checks before dispatch.
- Deterministic healthy sender-session allocation with lease and heartbeat checks.
- Executable audience, campaign and metric workers with bounded concurrency, health checks and graceful shutdown.
- Query-path-specific PostgreSQL indexes for cohorts, consent eligibility, import claims, outbox/jobs, sender leases, provider events and metric reconciliation.
- An SRS traceability catalogue covering all 398 requirements.

Verified locally at this checkpoint:

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

Current blockers remain explicit:

- A pinned PostgreSQL `database/sql` driver cannot be downloaded in this isolated workspace, so deployed binaries still fail closed until that dependency is added.
- PostgreSQL migrations and repository adapters have not yet been exercised against a live PostgreSQL server here.
- Docker on the user's desktop is not accessible from this workspace.
- OpenWA source and real WhatsApp transport are not yet imported; the gateway transport remains mocked.
- Full Next.js and NestJS dependency installation, typechecking and production builds remain pending.
- Media dispatch remains disabled until authenticated short-lived object access is implemented.

## Local checks

```bash
make check
```

When Node dependencies are installed:

```bash
npm install
npm run typecheck:web
npm run typecheck:gateway
npm run build:web
npm run build:gateway
```

Docker Compose is prepared under `infrastructure/compose/compose.yaml`. Docker is not exposed inside the current isolated build workspace, so Compose execution must occur on the user's machine or the Hostinger VPS.

## OpenWA source

The public OpenWA source has not yet been imported because this isolated workspace cannot access GitHub. `UPSTREAM.md` records the approved import model. OpenWA will be imported into this private repository as third-party source—not as a public GitHub fork—and isolated behind the messaging-provider contract.
