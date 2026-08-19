# Production Readiness Checklist

## Usage

This checklist is evidence-driven. A box may be marked complete only when the referenced evidence exists and has been reviewed. “Code exists” is not equivalent to “production validated.”

## 17 August 2026 scope clarification

Production certification now covers three sibling WhatsApp transports: OpenWA/Baileys, OpenWA/WWebJS and direct Meta Cloud API. Local backend green evidence is necessary but does not close target-infrastructure/live-provider gates. Current programme order is backend freeze → Railway/Hostinger + real-provider validation → frontend/UAT.

## 1. Product and requirements

- [ ] All Must SRS requirements are `IMPLEMENTED_TESTED` or have an approved waiver.
- [ ] UI/UX specification screens and exceptional states are mapped to implementation evidence.
- [ ] No public/customer self-service capability has been introduced into the initial internal-only scope without approval.
- [ ] Campaign provider, engine, sender route and fallback behaviour are explicit and immutable at release.
- [ ] Accepted, sent, delivered, read, failed and unknown terminology is consistent across API, UI and reports.

## 2. Architecture and code quality

- [ ] Service boundaries match ADR-0001 and the OpenWA integration approach.
- [ ] OpenWA does not own consent, eligibility, entitlement or canonical delivery state.
- [ ] Redis loss can be recovered from PostgreSQL obligations.
- [ ] No unresolved critical architecture-audit finding remains.
- [ ] Dead code, mock production paths and placeholder implementations are removed or disabled.
- [ ] Go, TypeScript and migration checks pass from a clean clone.

## 3. Identity and privileged access

- [ ] Internal users are individually provisioned; generic accounts are prohibited.
- [ ] MFA is mandatory and step-up is enforced for privileged actions.
- [ ] RBAC coverage is tested for every endpoint and high-risk action.
- [ ] Service identities are separate, scoped and rotatable.
- [ ] Session expiry, revocation, lockout and active-session visibility are tested.
- [ ] Admin portal network access is restricted through VPN and/or approved allowlists.

## 4. Data protection and consent

- [ ] MSISDN encryption, lookup HMAC and masking are validated end to end.
- [ ] Plaintext decryption is limited to authorised sending/export workflows.
- [ ] Consent lineage includes organisation, purpose, channel, wording and evidence.
- [ ] Withdrawals and suppressions override prior consent at final dispatch.
- [ ] Data retention, legal hold, correction and subject-right workflows are tested.
- [ ] Logs, metrics, URLs and queue identifiers contain no raw MSISDN or message content.

## 5. Campaign execution

- [ ] Immutable audience and message/media versions are frozen at approval.
- [ ] Campaign entitlement prevents recipient/message overrun and duplicate sends.
- [ ] Transactional outbox and ledger reconstruction are tested.
- [ ] Pause, resume, cancellation and completion-with-exceptions are tested.
- [ ] Unknown provider outcomes never trigger automatic resend.
- [ ] Capacity admission uses measured evidence and a configured safety margin.

## 6. OpenWA gateway and sessions

- [ ] Exact upstream provenance, licence and local modifications are recorded.
- [ ] Dependency-resolved gateway images build reproducibly.
- [ ] Both enabled engines expose accurate capability declarations.
- [ ] Genuine session pairing, start, drain, stop, logout and recovery are tested.
- [ ] A live session has exactly one active owner with fencing.
- [ ] Signed send commands and signed event callbacks pass replay tests.
- [ ] Text, image, video and document sends are tested where enabled.
- [ ] Sent, delivered, read, inbound and failure events reconcile correctly.

- [ ] Meta sender/WABA/phone-number/credential authority is deployed and verified against the exact tenant and sender pool.
- [ ] Meta approved-template binding and authenticated inbound-text FREE_FORM conversation-window eligibility are validated live.
- [ ] Signed Meta webhooks prove sender/tenant/frozen-route binding for delivery, inbound and STOP evidence.
- [ ] Cross-provider safety is exercised: ambiguous/UNKNOWN outcomes never trigger automatic resend through another provider.
- [ ] Direct Graph API submission, webhook reconciliation and credential rotation are exercised without routing through OpenWA browser engines.

## 7. Database, queue and storage

- [ ] All migrations execute on clean and upgrade-path PostgreSQL instances.
- [ ] Required indexes and query plans are reviewed on production-scale data.
- [ ] PgBouncer and connection limits are configured.
- [ ] Redis persistence/recovery behaviour is validated.
- [ ] Object storage encryption, checksums, signed access and lifecycle policies are tested.
- [ ] Export files expire and are deleted as governed.

## 8. Performance and scale

- [ ] 10-million-contact import and representative cohort queries meet targets.
- [ ] Multi-million-recipient snapshot and ledger creation meet targets.
- [ ] Queue saturation, worker restart and redelivery tests pass.
- [ ] Per-session sustainable capacity is measured independently for each engine.
- [ ] Campaign forecasting accuracy is compared with actual completion.
- [ ] Soak tests show no unacceptable memory, connection or queue growth.

## 9. Security assurance

- [ ] Threat model covers control plane, gateway, storage, workers and admin portal.
- [ ] Secret, dependency, source and container scans have no unresolved critical/high findings.
- [ ] CSP, CSRF, SSRF, upload, formula-injection and path-safety controls are tested.
- [ ] Repository branch protections, CODEOWNERS and no-auto-upstream-merge policy are active.
- [ ] Software bill of materials is generated and archived.
- [ ] Independent penetration test is complete and remediated.

## 10. Observability and operations

- [ ] Health and readiness reflect PostgreSQL, Redis, workers, gateway pools and storage.
- [ ] Metrics, logs and alerts are present for critical workflows and failure modes.
- [ ] Correlation IDs connect campaign, recipient, session, gateway and provider events.
- [ ] Incident, reconciliation and exception queues have documented ownership.
- [ ] Emergency pause and maintenance-mode exercises are complete.
- [ ] On-call contacts, escalation paths and service-level objectives are agreed.

## 11. Backup and disaster recovery

- [ ] PostgreSQL full backup and WAL/PITR recovery are tested.
- [ ] Consent evidence, campaign media and reports restore successfully.
- [ ] OpenWA session-state recovery is tested without dual ownership.
- [ ] Backups are encrypted and stored outside the primary failure domain.
- [ ] RPO and RTO are measured and accepted.
- [ ] Disaster-recovery exercise findings are closed.

## 12. Release and go-live

- [ ] Release-gate verifier passes.
- [ ] Source ZIP, Git bundle and checksums reproduce the approved commit.
- [ ] Deployment and rollback are exercised from a clean environment.
- [ ] Known limitations and residual risks are accepted by named owners.
- [ ] Operational, security, compliance and product owners sign go-live approval.
