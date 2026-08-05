# Risk and Technical Debt Register

## Rating model

- Severity: Critical, High, Medium, Low.
- Status: Open, Mitigating, Blocked External, Accepted, Closed.
- Release blocker: Yes when the risk prevents deployment certification.

| ID | Type | Risk or debt | Severity | Status | Release blocker | Mitigation / next evidence |
|---|---|---|---|---|---|---|
| R-001 | Transport | Unofficial OpenWA sessions may be restricted or behave inconsistently at commercial volume. | Critical | Mitigating | Yes | Controlled pilot, conservative rate limits, measured capacity, explicit provider route, future official Meta adapter. |
| R-002 | Validation | No genuine WhatsApp pairing or live delivery/read evidence exists in the current environment. | Critical | Blocked External | Yes | Deploy staging gateway, pair genuine numbers and execute the approved live test plan. |
| R-003 | Database | Migrations have not been executed through the full chain on production-equivalent PostgreSQL. | High | Blocked External | Yes | Clean-install and upgrade-path migration tests with backup and rollback evidence. |
| R-004 | Scale | Ten-million-contact and multi-million-recipient performance targets lack production-like benchmark evidence. | High | Open | Yes | Generate synthetic datasets, capture query plans, load/soak test and tune indexes. |
| R-005 | Security | Independent penetration testing and full dependency/container assurance are outstanding. | High | Open | Yes | Threat model, SAST/SCA/secret scans, SBOM, image scan and independent penetration test. |
| R-006 | Resilience | PostgreSQL failover, PITR and OpenWA session-state recovery are unproven. | High | Blocked External | Yes | Execute restore, worker-loss and disaster-recovery exercises against agreed RPO/RTO. |
| R-007 | Frontend | Production Next.js workflows are incomplete relative to the UI/UX specification. | High | Open | Yes | Implement screen matrix, RBAC, masking, accessibility and exceptional-state tests. |
| R-008 | Traceability | The majority of SRS requirements remain NOT_STARTED in the generated matrix. | Critical | Open | Yes | Domain-by-domain backlog and evidence-based status updates; no premature 1.0 claim. |
| R-009 | Provenance | Exact upstream OpenWA Git commit is not provable from the supplied source ZIP. | Medium | Open | No | Obtain upstream Git bundle/clone or document source snapshot fingerprint and limitation. |
| R-010 | Supply chain | Container images are version-tagged but not consistently digest-pinned; Node dependency resolution remains incomplete. | High | Open | Yes | Resolve lockfiles, pin immutable digests, generate SBOM and verify reproducible builds. |
| R-011 | Operations | Monitoring dashboards, alert thresholds and on-call ownership are not yet exercised. | High | Open | Yes | Deploy observability stack and run incident simulations. |
| R-012 | Reporting | Authenticated short-lived export download and watermark policy remain incomplete. | Medium | Open | Yes | Implement download authorisation, audit, expiry, watermarking and revocation tests. |
| TD-001 | Technical debt | Some traceability entries describe early scaffolding while later code may exist; evidence catalogue needs systematic reconciliation. | Medium | Open | No | Repository-wide evidence audit and regeneration of traceability metadata. |
| TD-002 | Technical debt | Current Makefile build list may omit newer worker executables. | Medium | Closed | No | Closed in 0.8.3: build target discovers and compiles every `cmd/*` executable. |
| TD-003 | Technical debt | Gateway TypeScript has not been fully dependency-resolved and typechecked in this environment. | High | Open | Yes | Restore pinned dependencies and run lint, typecheck, tests and production container build. |
| TD-004 | Technical debt | Live infrastructure configuration remains represented mainly by Compose templates rather than validated immutable deployment automation. | Medium | Open | Yes | Validate Hostinger provisioning, secret injection, firewalling, rollback and drift controls. |

| R-013 | Governance | Suspended or closed organisations were not enforced across campaign, consent, import and release boundaries. | High | Closed | No | Closed in 0.8.4: active status is rechecked at campaign and consent creation/progression, import creation/approval and final recipient release; safety cancellation remains available. |
| TD-005 | Technical debt | Organisation restrictions, retention and report-branding settings are not yet structured governed policy versions. | Medium | Open | No | Implement organisation policy records with versioning, approval and effective dates. |

## Register rules

1. Every Critical or High item requires a named owner before staging deployment.
2. A release blocker may be closed only with evidence, not narrative assurance.
3. Accepted risks require expiry date, approver and compensating controls.
4. New audit findings are added here before remediation work starts.
5. Closed items remain in the register for historical traceability.
