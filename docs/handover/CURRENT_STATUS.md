# Current status

Version `0.8.4` is the latest backend-remediation checkpoint on branch `work/backend-adversarial-remediation`.

Completed capability milestones in the actual repository history:

- 0.5.x governance, identity, consent, audience, inbound and sender foundations.
- 0.6.0 production OpenWA messaging-engine code boundary.
- 0.7.0 governed campaign execution, capacity admission and completion.
- 0.8.0 reporting and operations.
- 0.8.1 durable asynchronous export rendering.
- 0.8.2 programme governance, independent-audit plan and machine-readable release gates.
- 0.8.3 organisation lifecycle administration and initial audit remediation.
- 0.8.4 organisation fail-closed enforcement, OpenAPI route reconciliation and mandatory worker topology.

The repository remains pre-production. The generated SRS traceability matrix currently contains many `NOT_STARTED` requirements, and the hard release gate intentionally remains open. Live PostgreSQL migration execution, dependency-resolved OpenWA builds, genuine WhatsApp sessions, production-scale performance evidence, frontend completion, security assurance and disaster-recovery evidence are outstanding.

No production-ready or deployment-certified claim should be made until `make release-gate` passes against reviewed evidence.

## 0.8.5 audience merge remediation

The audience-import merge layer now has implemented paths for all declared update policies. `TRUSTED_SOURCE` is governed by organisation/source trust policies, while `MANUAL_CONFLICT` creates durable review items instead of overwriting disputed canonical profile values. Conflict resolution and trust-policy administration are exposed through protected APIs and migration 0037.
