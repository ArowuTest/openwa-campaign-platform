
## 0.8.17 audience import merge runtime

Approved audience imports are now automatically claimed and merged by the audience worker with PostgreSQL leases and fencing. Validation, canonical merge, conflict evidence and asynchronous materialisation now all have durable worker paths. Live PostgreSQL execution and production-scale performance remain external gates.
# Current status

Version `0.8.13` is the latest operational-core checkpoint on branch `work/operational-core-audience-iii`.

Completed capability milestones in the actual repository history:

- 0.5.x governance, identity, consent, audience, inbound and sender foundations.
- 0.6.0 production OpenWA messaging-engine code boundary.
- 0.7.0 governed campaign execution, capacity admission and completion.
- 0.8.0 reporting and operations.
- 0.8.1 durable asynchronous export rendering.
- 0.8.2 programme governance, independent-audit plan and machine-readable release gates.
- 0.8.3 organisation lifecycle administration and initial audit remediation.
- 0.8.4 organisation fail-closed enforcement, OpenAPI route reconciliation and mandatory worker topology.
- 0.8.5 governed audience merge conflicts and source-trust controls.
- 0.8.6 governed organisation purpose, retention and report-branding policy versions.

The repository remains pre-production. The generated SRS traceability matrix currently contains many `NOT_STARTED` requirements, and the hard release gate intentionally remains open. Live PostgreSQL migration execution, dependency-resolved OpenWA builds, genuine WhatsApp sessions, production-scale performance evidence, frontend completion, security assurance and disaster-recovery evidence are outstanding.

No production-ready or deployment-certified claim should be made until `make release-gate` passes against reviewed evidence.

## 0.8.5 audience merge remediation

The audience-import merge layer now has implemented paths for all declared update policies. `TRUSTED_SOURCE` is governed by organisation/source trust policies, while `MANUAL_CONFLICT` creates durable review items instead of overwriting disputed canonical profile values. Conflict resolution and trust-policy administration are exposed through protected APIs and migration 0037.

## 0.8.7 commercial-governance remediation

Campaign commercial approval is now backed by campaign-specific quotation, invoice, payment and entitlement evidence. A campaign cannot enter `COMMERCIAL_APPROVED` through the service layer unless an independently approved, non-revoked commercial record exists for the same campaign and organisation and authorises at least the campaign maximum recipient count.

## 0.8.10 remediation update

Governed rolling frequency caps are now stored in effective-dated organisation policy versions and applied consistently to cohort compilation, recipient release and final dispatch. The generated traceability baseline is 48 implemented/tested, 31 partial and 319 not started. Live PostgreSQL execution and query-plan evidence remain blocked external release gates.

## 0.8.11 audience operational-core update

- Saved segment definitions now have governed versions and archive controls.
- Cohort counts and snapshot membership can be calculated server-side from authoritative PostgreSQL eligibility queries.
- Client-supplied member lists remain supported for compatibility but are no longer the only materialisation route.
- Durable asynchronous multi-million snapshot jobs and live PostgreSQL performance validation remain open.
- The designer's 25-screen HTML export is retained as a reference package for later production Next.js implementation and may be redesigned to match the final backend workflows.


## 0.8.12 audience operational-core update

- Saved segments now support governed cloning and semantic comparison between immutable versions.
- Audience snapshots support identity-free overlap analysis for intersection and difference counts.
- Manual import conflicts can be resolved atomically in bounded batches.
- Import validation, merge and conflict totals can be previewed and closed with immutable reconciliation evidence.
- The traceability baseline is 54 implemented/tested, 33 partial and 311 not started.
- Durable asynchronous snapshot jobs and live PostgreSQL scale evidence remain outstanding.


## 0.8.13 audience operational-core update

Durable asynchronous audience materialisation now supports bounded pages, fenced leases, persistent progress, restart-safe continuation, retries, cancellation evidence and atomic immutable snapshot completion. Live PostgreSQL and production-scale performance evidence remain open.

## 0.8.15 messaging operational-core update

- Sender sessions can be quarantined and reinstated only through dedicated MFA-protected operations with optimistic versions and durable reasons.
- Runtime heartbeats cannot override quarantined, restricted or retired governance states.
- Quarantined sessions are excluded from sender-pool throughput and daily-capacity calculations.
- Sender health assessment exposes heartbeat age, capacity use, evidence-based score and an allocation recommendation.
- Unknown and contradictory delivery outcomes can be resolved using reviewed provider evidence without silently resending uncertain submissions.
- Confirmed non-submission can become retryable only when no provider identifier or acknowledgement exists.
- Append-only delivery-resolution evidence is introduced by migration 0048.

## Messaging operations II — 0.8.15

- Governed sender quarantine and reinstatement are implemented.
- Runtime heartbeats cannot silently restore quarantined, restricted or retired sessions.
- Health-aware allocation evidence and operator recommendations are exposed.
- Unknown/contradictory delivery exceptions support MFA-protected evidence-backed resolution.
- Append-only resolution evidence is stored in PostgreSQL.

## 0.8.16 messaging operational-core update

- Deterministic bounded campaign dispatch shards are implemented with fenced leases and restart-safe progress refresh.
- Missing durable dispatch jobs are reconstructed from published PostgreSQL outbox evidence using original deduplication keys.
- Health/capacity-aware sender allocation, adaptive throttling and execution forecasting are present.
- Migration `0049_campaign_dispatch_shards.sql` is included but still requires live PostgreSQL execution evidence.
