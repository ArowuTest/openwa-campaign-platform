# Master Implementation Roadmap

## Purpose

This roadmap is the controlled programme view for taking the OpenWA Internal Campaign Platform from the current code checkpoint to a deployment-certified production release. It is subordinate to the authoritative SRS and UI/UX specification. It does not reclassify an unmet requirement as complete merely because supporting scaffolding exists.

## 17 August 2026 execution-order override

The numbered workstreams below are a scope taxonomy, **not the current execution sequence**. R19 is historical freeze evidence; infrastructure review deliberately reopened the source boundary to correct node-addressed OpenWA dispatch and the split production topology. I2 blind review produced four confirmed production-boundary defects; those are remediated in I3. I3 is locally re-gated and awaiting its fresh exact-source blind council. Current approved sequence remains: **infrastructure/platform engineering and production-like proof → separately authorised Railway/Hostinger deployment + real Baileys/WWebJS/Meta validation → production Next.js portal → UAT/accessibility → release certification**. Infrastructure/live-provider work therefore precedes production frontend work even though WS-7 is numbered before WS-8.

## Current baseline

- Historical release label remains `0.8.28`; the current backend/Meta candidate is a later intentional dirty worktree, not a new published version.
- Current branch: `work/backend-production-engineering`; committed HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77`.
- Authoritative generated catalogue: 398 original SRS rows (168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL) plus later approved Meta design/FREE_FORM addenda.
- Local schema/test state: migrations 0001→0089 remain verified from zero on PostgreSQL 17 and 18; R18 daily-capacity/terminal-state PostgreSQL regressions are green on both; complete execution unit/integration/race gates and fresh exact-source complete Go test/vet/build are green; R19 blind review has no surviving source defect.
- Deployment status: pre-production; R19 freeze was reopened after a real split-topology defect was found. I3 backend+infrastructure config is locally re-gated after the I2 council remediation and requires fresh exact-source blind review before a new freeze. Live-provider, scale, DR, security-assurance, operational and frontend acceptance remain open.
- Genuine Baileys/WWebJS/Meta live-network and target-infrastructure acceptance remains external/not yet accepted.

## Delivery principles

1. Build vertical capabilities, not disconnected files.
2. PostgreSQL remains authoritative for business obligations and canonical state.
3. OpenWA remains an isolated transport gateway and never determines consent or campaign eligibility.
4. Every material configuration and approval decision is versioned and attributable.
5. Claims of completion require implementation evidence and automated or live test evidence.
6. External-environment gates remain visibly blocked until evidence exists.
7. Release packaging includes source ZIP, Git bundle, checksums, release notes and handover state.

## Workstreams

### WS-1 — Requirements and architecture assurance

**Objective:** Maintain an accurate mapping between requirements, implementation and evidence.

Deliverables:

- Generated SRS traceability matrix.
- ADR register and architecture review log.
- Requirements-gap triage by domain and priority.
- Controlled waivers for genuinely external gates only.
- Independent engineering audit findings and remediation plan.

Exit criteria:

- Every Must requirement has an owner and planned milestone.
- No requirement is marked implemented without code and evidence.
- Architecture decisions affecting security, durability or transport are recorded.

### WS-2 — Core data and consent completion

**Objective:** Complete organisation, contact, consent-review, import and data-subject capabilities that remain partial or not started.

Scope:

- Organisation records and commercial references.
- Consent-review workflow and evidence governance.
- Persistent canonical contacts and provenance.
- CSV/XLSX import workflow, mapping, conflicts and durable batches.
- Data correction, merge, suppression and subject-right workflows.
- 10-million-contact data model and query evidence.

Exit criteria:

- Organisation-to-consent-to-contact lineage is complete.
- Import is resumable, auditable and safe for large files.
- Plaintext MSISDN access is restricted to explicitly authorised workflows.

### WS-3 — Campaign and content completion

**Objective:** Complete all campaign, message, media, entitlement and approval requirements.

Scope:

- Immutable content and media lifecycle.
- Commercial approval and campaign entitlement.
- Campaign transport selection and provider capability pre-flight.
- Scheduling, pause, resume, cancellation and completion evidence.
- Test-send governance and message-variable validation.

Exit criteria:

- Material campaign artefacts are immutable at release.
- Maker-checker and MFA controls are enforced for release.
- Every recipient obligation is unique, durable and reconstructable.

### WS-4 — OpenWA messaging engine live validation

**Objective:** Prove the real gateway boundary under production-like conditions.

Scope:

- Dependency-resolved NestJS/OpenWA build.
- `whatsapp-web.js` and Baileys gateway pools.
- Pairing, ownership, heartbeat, drain and recovery.
- Signed commands and signed event ingestion.
- Text, image, video and document sends.
- Unknown-outcome reconciliation and no-silent-resend controls.
- Measured sustainable capacity by sender and engine.

Exit criteria:

- Genuine sessions demonstrate the complete event lifecycle.
- Session loss and worker loss recovery are exercised.
- Capacity records are based on observed results, not assumptions.

### WS-5 — Reporting, exports and operations

**Objective:** Complete trusted operational and organisation-facing reporting.

Scope:

- Campaign dashboards and metric aggregation.
- Incidents, delivery exceptions and reconciliation queues.
- Audit search and controlled export workflow.
- Authenticated short-lived downloads, watermark policy and expiry.
- PDF, XLSX, CSV and JSON report validation.
- Small-cohort suppression and privacy controls.

Exit criteria:

- Reports distinguish accepted, sent, delivered, read, failed and unknown.
- Exports are approved, traceable, expiring and downloadable only by authorised users.

### WS-6 — Administration and platform services

**Objective:** Deliver governed configuration and operational platform controls.

Scope:

- Versioned effective-dated platform configuration.
- Approval rules, feature controls and maintenance mode.
- Service identities and secret rotation.
- Platform health aggregation.
- Repository protection and upstream-review process.

Exit criteria:

- Material settings are not hard-coded.
- Emergency actions require reason, MFA and immutable audit evidence.
- Upstream OpenWA changes cannot enter production automatically.

### WS-7 — Production Next.js portal

**Objective:** Implement the complete internal operational user experience.

Scope:

- Authentication and MFA journeys.
- Dashboard, organisations, consent, audience, campaigns, senders, operations, reports and administration.
- Designer HTML conversion into typed reusable components.
- Permission-aware actions and masking.
- Loading, empty, warning, failure, unknown and recovery states.
- WCAG 2.2 AA and responsive desktop/tablet support.

Exit criteria:

- UI/UX screen identifiers map to implemented routes and tests.
- No business rule exists only in the browser.
- Accessibility checks and keyboard journeys pass.

### WS-8 — Infrastructure, resilience and release certification

**Objective:** Produce deployment evidence for a production candidate.

Scope:

- Railway control-plane/API/worker/data-service topology and release configuration.
- Hostinger isolated OpenWA gateway fleet/topology for Baileys and WWebJS/Chromium.
- Direct Meta Cloud API ingress/egress, webhook and secret/network boundary as a sibling transport.
- PostgreSQL primary/replica, PgBouncer and backups.
- Redis persistence/HA decision and recovery.
- Object storage and encrypted off-site backup.
- Monitoring, alerting and log aggregation.
- Load, soak, failover and restore testing.
- Security assurance and penetration testing.

Exit criteria:

- All hard release gates in `config/release-gates.json` are closed or formally waived.
- Production readiness checklist is signed by accountable owners.
- Release bundle is reproducible and verifiable.

## Milestone sequence

| Milestone | Intended outcome | Release condition |
|---|---|---|
| 0.8.2 | Programme governance and release-gate baseline | Governance artefacts and automated consistency checks pass |
| 0.9.0 | Remaining backend/platform capability completion | Must-have backend gaps closed with automated evidence |
| 0.10.0 | Production Next.js portal | Required UI/UX workflows implemented and tested |
| 0.11.0 | Staging deployment candidate | Full topology runs on production-like infrastructure |
| 0.12.0 | Live OpenWA pilot | Genuine sessions and event lifecycle validated |
| 0.13.0 | Scale and resilience candidate | Load, recovery and DR evidence accepted |
| 1.0.0-rc1 | Formal production candidate | All release gates closed except approved residual risks |
| 1.0.0 | Deployment-certified release | Go-live approval and signed readiness checklist |

## Governance cadence

- Weekly: requirements-gap and risk review.
- Per substantial commit: tests, traceability generation and handover update.
- Per milestone: architecture review, release notes, ZIP, Git bundle and checksums.
- Before release candidate: independent engineering, security and operational-readiness review.
