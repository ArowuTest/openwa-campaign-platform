# SRS Requirement Traceability Matrix

> Generated from the authoritative SRS requirement catalogue. A requirement is only marked IMPLEMENTED_TESTED when code and automated evidence exist; deployment or external-control requirements remain partial until environment evidence is available.

Total requirements: **398**

| Status | Count |
|---|---:|
| IMPLEMENTED_TESTED | 43 |
| PARTIAL | 31 |
| BLOCKED_EXTERNAL | 0 |
| NOT_STARTED | 324 |

| ID | Priority | Status | Requirement | Evidence / notes |
|---|---|---|---|---|
| REP-001 | Must | NOT_STARTED | Create a new private repository with no public fork relationship. |  |
| REP-002 | Must | IMPLEMENTED_TESTED | Retain the OpenWA MIT licence and original notices. | `LICENSE`, `THIRD_PARTY_NOTICES.md`, `UPSTREAM.md` |
| REP-003 | Must | PARTIAL | Record the exact imported upstream URL, commit SHA and import date. | `UPSTREAM.md` — Exact OpenWA source commit has not yet been imported. |
| REP-004 | Must | NOT_STARTED | Configure upstream as read-only reference only. |  |
| REP-005 | Must | NOT_STARTED | Fetch upstream changes into an isolated review branch. |  |
| REP-006 | Must | NOT_STARTED | Disable or restrict private repository forking. |  |
| REP-007 | Must | NOT_STARTED | Protect main and release branches. |  |
| REP-008 | Must | NOT_STARTED | Require code-owner approval for gateway, security, database and infrastructure changes. |  |
| REP-009 | Must | PARTIAL | Pin dependencies and container images. | `infrastructure/compose/compose.yaml` — Images are version-tagged but not yet digest-pinned; Node lockfiles remain pending. |
| REP-010 | Must | NOT_STARTED | Generate a software bill of materials for releases. |  |
| REP-011 | Must | NOT_STARTED | Scan source, dependencies, secrets and images. |  |
| REP-012 | Must | NOT_STARTED | Do not automatically deploy upstream or dependency updates. |  |
| GWY-001 | Must | PARTIAL | Expose a versioned internal provider API independent of the OpenWA public API. | `services/openwa-gateway/src/provider/messaging-provider.ts`, `contracts/events/provider-message-event.schema.json` |
| GWY-002 | Must | PARTIAL | Require an idempotency key for every delivery command. | `services/openwa-gateway/src/send.controller.ts`, `services/openwa-gateway/src/provider/mock.provider.ts` — Current idempotency store is not durable. |
| GWY-003 | Must | PARTIAL | Own each live session on exactly one worker node. | `internal/gateway/lease.go`, `internal/gateway/lease_test.go`, `database/migrations/0006_integrity_identity_jobs_and_leases.sql` |
| GWY-004 | Must | NOT_STARTED | Persist session state on an encrypted worker volume. |  |
| GWY-005 | Must | NOT_STARTED | Report heartbeat, build version, capacity, queue depth and resource health. |  |
| GWY-006 | Must | NOT_STARTED | Report session connection, authentication, last-success and failure state. |  |
| GWY-007 | Must | NOT_STARTED | Accept work only for sessions currently leased to the worker. |  |
| GWY-008 | Must | PARTIAL | Use a bounded per-session pipeline and configurable in-flight limit. | `internal/jobs/runner.go`, `internal/jobs/queue_test.go` — Generic worker concurrency is bounded; per-OpenWA-session integration is pending. |
| GWY-009 | Must | NOT_STARTED | Support graceful drain before deployment or maintenance. |  |
| GWY-010 | Must | NOT_STARTED | Return provider message identifiers and raw provider status metadata where safe. |  |
| GWY-011 | Must | NOT_STARTED | Forward inbound messages and delivery/read events through signed callbacks or a trusted event channel. |  |
| GWY-012 | Must | NOT_STARTED | Never store full campaign audience lists locally. |  |
| GWY-013 | Must | NOT_STARTED | Use object references for media and enforce size/type limits. |  |
| GWY-014 | Must | NOT_STARTED | Sanitise logs and exclude raw MSISDN, message content and credentials by default. |  |
| GWY-015 | Must | NOT_STARTED | Support safe shutdown and uncertain-state reporting. |  |
| GWY-016 | Must | NOT_STARTED | Expose engine capability and version. |  |
| GWY-017 | Must | NOT_STARTED | Support per-session stable proxy configuration where legitimately required. |  |
| GWY-018 | Must | NOT_STARTED | Do not implement rapid IP rotation or enforcement-evasion behaviour. |  |
| IAM-001 | Must | IMPLEMENTED_TESTED | Permit access only to provisioned internal users. | `internal/identity/administration.go`, `internal/identity/administration_test.go`, `internal/platform/httpserver/identity_admin.go` |
| IAM-002 | Must | IMPLEMENTED_TESTED | Support mandatory multi-factor authentication. | `internal/identity/totp.go`, `internal/identity/service_test.go` |
| IAM-003 | Must | NOT_STARTED | Support configurable passwordless/SSO integration later without replacing domain authorisation. |  |
| IAM-004 | Must | IMPLEMENTED_TESTED | Implement role-based access control with granular permissions. | `internal/identity/model.go`, `internal/identity/administration.go`, `internal/platform/httpserver/server.go` |
| IAM-005 | Must | IMPLEMENTED_TESTED | Support users holding multiple roles with least-privilege union of permissions. | `internal/identity/administration.go`, `internal/identity/administration_test.go`, `internal/persistence/postgres/identity_admin.go` |
| IAM-006 | Must | IMPLEMENTED_TESTED | Enforce maker-checker separation for campaign release. | `internal/campaign/model.go`, `internal/campaign/model_test.go` |
| IAM-007 | Must | PARTIAL | Require step-up authentication for exports, key changes, emergency overrides and destructive actions. | `internal/identity/service.go`, `internal/platform/httpserver/server.go` — Final campaign approval uses recent MFA; export and emergency workflows are not yet built. |
| IAM-008 | Must | IMPLEMENTED_TESTED | Enforce configurable idle and absolute session expiry. | `internal/identity/service.go`, `internal/identity/service_test.go` |
| IAM-009 | Must | IMPLEMENTED_TESTED | Provide account disable, lock and credential reset. | `internal/identity/administration.go`, `internal/identity/administration_test.go`, `internal/platform/httpserver/identity_admin.go` |
| IAM-010 | Must | IMPLEMENTED_TESTED | Detect repeated failed login attempts and apply configurable controls. | `internal/identity/service.go`, `internal/identity/service_test.go` |
| IAM-011 | Must | NOT_STARTED | Record authentication, authorisation failure and privileged-action events. |  |
| IAM-012 | Must | NOT_STARTED | Display the user’s last successful login and active sessions. |  |
| IAM-013 | Must | IMPLEMENTED_TESTED | Allow authorised administrators to revoke all sessions for a user. | `internal/identity/service.go`, `internal/identity/administration.go`, `internal/platform/httpserver/identity_admin.go` |
| IAM-014 | Must | IMPLEMENTED_TESTED | Prevent shared generic accounts. | `internal/identity/administration.go`, `internal/identity/administration_test.go` |
| IAM-015 | Must | NOT_STARTED | Use separate scoped service identities for each internal service. |  |
| IAM-016 | Must | IMPLEMENTED_TESTED | Support IP allowlisting or VPN-only access as a configurable deployment control. | `internal/platform/httpserver/network.go`, `internal/platform/httpserver/network_test.go`, `internal/shared/config/config.go`, `internal/shared/config/config_test.go` — Optional CIDR allowlisting supports trusted-proxy-aware client resolution and records network denials. |
| ORG-001 | Must | IMPLEMENTED_TESTED | Create internal organisation records without creating external portal users. | `internal/organisation/model.go`, `internal/organisation/service.go`, `internal/persistence/postgres/organisation.go`, `internal/platform/httpserver/server.go`, `internal/platform/httpserver/server_test.go`, `database/migrations/0036_organisation_lifecycle_governance.sql` — Internal organisation creation, retrieval, listing and persistent runtime composition are implemented without external portal identities. |
| ORG-002 | Must | NOT_STARTED | Store legal/trading name, country, registration reference, industry and status. |  |
| ORG-003 | Must | NOT_STARTED | Store one or more internal and external contacts with role and communication details. |  |
| ORG-004 | Must | NOT_STARTED | Record controller, processor or joint-controller role where known. |  |
| ORG-005 | Must | IMPLEMENTED_TESTED | Record organisation-level restrictions and prohibited purposes. | `internal/organisation/policy.go`, `internal/organisation/policy_test.go`, `internal/persistence/postgres/organisation_policy.go`, `database/migrations/0038_organisation_policy_governance.sql`, `internal/platform/httpserver/server.go` — Organisation-specific allowed and prohibited campaign purposes are governed through versioned maker-checker policy records and enforced during campaign creation. |
| ORG-006 | Must | NOT_STARTED | Support active, suspended, under-review and closed status. |  |
| ORG-007 | Must | IMPLEMENTED_TESTED | Record commercial references such as quote, invoice and payment status. | `internal/commercial/service.go`, `internal/commercial/service_test.go`, `internal/persistence/postgres/commercial.go`, `database/migrations/0039_campaign_commercial_governance.sql`, `internal/platform/httpserver/server.go`, `internal/campaign/commercial_gate_test.go` — Versioned campaign commercial records now capture quotation, invoice, currency, recipient volume, pricing, payment reference and payment timestamp with maker-checker approval and revocation. |
| ORG-008 | Must | IMPLEMENTED_TESTED | Retain an audit history of organisation changes. | `internal/organisation/service.go`, `internal/persistence/postgres/organisation.go`, `database/migrations/0036_organisation_lifecycle_governance.sql`, `internal/organisation/lifecycle_test.go`, `internal/platform/httpserver/server_test.go` — Material profile and lifecycle changes create immutable versioned organisation events with actor, reason and before/after status evidence. |
| ORG-009 | Must | IMPLEMENTED_TESTED | Prevent accidental duplicate organisation creation. | `internal/organisation/service.go`, `internal/persistence/postgres/organisation.go`, `database/migrations/0036_organisation_lifecycle_governance.sql`, `internal/organisation/lifecycle_test.go` — Active organisations are protected by a normalised legal-name and country uniqueness rule in both memory and PostgreSQL repositories. |
| ORG-010 | Must | IMPLEMENTED_TESTED | Support organisation-specific data-retention and report branding settings. | `internal/organisation/policy.go`, `internal/organisation/policy_test.go`, `internal/persistence/postgres/organisation_policy.go`, `database/migrations/0038_organisation_policy_governance.sql` — Effective-dated organisation policy versions govern contact/campaign retention periods and report branding metadata. |
| CRV-001 | Must | NOT_STARTED | Create a consent review at organisation, source or campaign scope. |  |
| CRV-002 | Must | NOT_STARTED | Record consent collection method, source system, URL/form identifier and date range. |  |
| CRV-003 | Must | NOT_STARTED | Record the consent wording or an immutable reference and wording version. |  |
| CRV-004 | Must | NOT_STARTED | Record permitted organisation/controller, purpose, channel and partner use. |  |
| CRV-005 | Must | NOT_STARTED | Record evidence files or external references with checksums. |  |
| CRV-006 | Must | NOT_STARTED | Malware-scan uploaded evidence before availability. |  |
| CRV-007 | Must | NOT_STARTED | Record reviewer, decision, date, expiry and restrictions. |  |
| CRV-008 | Must | NOT_STARTED | Support approved, approved-with-restrictions, rejected, expired and revoked states. |  |
| CRV-009 | Must | NOT_STARTED | Require a reason for rejection, revocation or restriction. |  |
| CRV-010 | Must | NOT_STARTED | Support sample-record review notes without storing unnecessary sample PII. |  |
| CRV-011 | Must | NOT_STARTED | Require re-review when consent wording, collection method, controller, purpose or source materially changes. |  |
| CRV-012 | Must | NOT_STARTED | Link every campaign to a valid consent review or documented non-consent lawful basis where separately approved. |  |
| CRV-013 | Must | NOT_STARTED | Expose review expiry warnings and block expired approvals. |  |
| CRV-014 | Must | NOT_STARTED | Allow authorised compliance override only with reason, expiry and second approval. |  |
| CON-001 | Must | NOT_STARTED | Maintain one canonical contact for each normalised MSISDN within the configured identity scope. |  |
| CON-002 | Must | IMPLEMENTED_TESTED | Normalise MSISDNs to E.164 using country context and validation rules. | `internal/audience/importer/normalizer.go`, `internal/audience/importer/normalizer_test.go` |
| CON-003 | Must | PARTIAL | Store MSISDN encrypted at application level. | `internal/shared/crypto/msisdn.go`, `internal/audience/importer/csv.go` — Encryption is implemented and wired to preview; persistent contact storage is pending. |
| CON-004 | Must | PARTIAL | Store a deterministic keyed HMAC for exact lookup and deduplication. | `internal/shared/crypto/msisdn.go`, `internal/audience/importer/csv.go` |
| CON-005 | Must | IMPLEMENTED_TESTED | Display only masked MSISDN to ordinary users. | `internal/shared/crypto/msisdn.go`, `internal/platform/httpserver/server_test.go` |
| CON-006 | Must | NOT_STARTED | Restrict plaintext decryption to the minimum sending/export services and authorised workflows. |  |
| CON-007 | Must | PARTIAL | Store country, state and LGA using controlled reference data. | `internal/geography/catalogue.go`, `database/reference-data/geography` |
| CON-008 | Must | NOT_STARTED | Support unknown/not-provided geography without inventing values. |  |
| CON-009 | Must | NOT_STARTED | Store self-declared numeric age and age-recorded timestamp. |  |
| CON-010 | Must | NOT_STARTED | Calculate estimated current age using configured approximation rules. |  |
| CON-011 | Must | NOT_STARTED | Record age source, verification flag and last-confirmed date. |  |
| CON-012 | Must | NOT_STARTED | Never overwrite the original reported age without retaining history. |  |
| CON-013 | Must | NOT_STARTED | Support configurable age-data staleness bands. |  |
| CON-014 | Must | NOT_STARTED | Store gender only when provided under an approved data purpose. |  |
| CON-015 | Must | NOT_STARTED | Support approved optional attributes such as language and interest category. |  |
| CON-016 | Must | NOT_STARTED | Record attribute source, collected-at time and confidence. |  |
| CON-017 | Must | NOT_STARTED | Support contact active, inactive, invalid, deceased/closed and possible-recycled-number states. |  |
| CON-018 | Must | NOT_STARTED | Maintain source-system identifiers and import provenance. |  |
| CON-019 | Must | NOT_STARTED | Merge duplicate contacts through an approved, reversible process. |  |
| CON-020 | Must | NOT_STARTED | Prevent ordinary users from searching by partial plaintext MSISDN. |  |
| CON-021 | Must | NOT_STARTED | Support data correction while preserving history. |  |
| CON-022 | Must | NOT_STARTED | Support at least 10 million canonical contacts without schema redesign. |  |
| IMP-001 | Must | NOT_STARTED | Support CSV and XLSX imports through an internal controlled workflow. |  |
| IMP-002 | Must | NOT_STARTED | Provide downloadable versioned import templates. |  |
| IMP-003 | Must | NOT_STARTED | Support configurable column mapping and saved mappings per source. |  |
| IMP-004 | Must | NOT_STARTED | Validate file type, extension, MIME signature, size and malware status. |  |
| IMP-005 | Must | PARTIAL | Protect against CSV formula injection in previews and exports. | `internal/audience/importer/csv.go` — Import preview is safe; export formula protection is pending. |
| IMP-006 | Must | IMPLEMENTED_TESTED | Stream or chunk large files rather than load entire content into memory. | `internal/audience/importer/csv.go`, `internal/audience/importer/csv_test.go` |
| IMP-007 | Must | NOT_STARTED | Create an immutable import-batch record with checksum, uploader and time. |  |
| IMP-008 | Must | IMPLEMENTED_TESTED | Validate required fields and controlled values. | `internal/audience/importer/csv.go`, `internal/geography/catalogue.go` |
| IMP-009 | Must | IMPLEMENTED_TESTED | Normalise MSISDN using configured default country only where explicit. | `internal/audience/importer/normalizer.go` |
| IMP-010 | Must | PARTIAL | Detect duplicate rows within the file. | `internal/audience/importer/csv.go` — Preview deduplication is in-memory and capped; durable database deduplication is pending. |
| IMP-011 | Must | NOT_STARTED | Detect contacts already present in the repository. |  |
| IMP-012 | Must | NOT_STARTED | Support configurable update policy: fill-null, newest-source, trusted-source or manual conflict. |  |
| IMP-013 | Must | NOT_STARTED | Never silently broaden existing consent based on a profile import. |  |
| IMP-014 | Must | PARTIAL | Provide import preview and estimated effects before commit. | `internal/platform/httpserver/server.go`, `internal/audience/importer/csv.go` |
| IMP-015 | Must | NOT_STARTED | Use database bulk-load techniques and bounded transactions. |  |
| IMP-016 | Must | NOT_STARTED | Provide row-level error export with masked identifiers. |  |
| IMP-017 | Must | NOT_STARTED | Reconcile uploaded, accepted, rejected, duplicate, inserted, updated and unchanged counts. |  |
| IMP-018 | Must | NOT_STARTED | Support rollback of an unconsumed erroneous import where legally/operationally safe. |  |
| IMP-019 | Must | NOT_STARTED | Block campaign use of a batch until required consent review is approved. |  |
| IMP-020 | Must | NOT_STARTED | Support encrypted import file retention or prompt deletion according to policy. |  |
| CNS-001 | Must | NOT_STARTED | Represent consent as a separate grant, not a Boolean contact field. |  |
| CNS-002 | Must | NOT_STARTED | Scope consent to organisation/controller, purpose and channel. |  |
| CNS-003 | Must | NOT_STARTED | Record grant time, source, wording version and evidence reference. |  |
| CNS-004 | Must | NOT_STARTED | Support active, pending-verification, expired, withdrawn, revoked and superseded states. |  |
| CNS-005 | Must | NOT_STARTED | Support effective-from and expiry timestamps. |  |
| CNS-006 | Must | NOT_STARTED | Record consent withdrawal with source, time and scope. |  |
| CNS-007 | Must | NOT_STARTED | Treat withdrawal as higher priority than an older grant. |  |
| CNS-008 | Must | NOT_STARTED | Support a new valid grant after withdrawal only with new evidence and timestamp. |  |
| CNS-009 | Must | NOT_STARTED | Maintain append-only consent-event history. |  |
| CNS-010 | Must | NOT_STARTED | Support global, organisation, purpose and channel suppression scopes. |  |
| CNS-011 | Must | IMPLEMENTED_TESTED | Create suppression automatically from recognised STOP replies after confirmation policy. | `internal/consent/optout.go`, `internal/consent/optout_test.go`, `internal/platform/httpserver/inbound_optout_test.go` — Signed inbound gateway replies are resolved through the authoritative campaign-recipient relationship; exact recognised STOP commands create an immediate idempotent global suppression. |
| CNS-012 | Must | IMPLEMENTED_TESTED | Support configured STOP synonyms and language variants. | `internal/consent/optout.go`, `internal/consent/optout_policy.go`, `internal/consent/optout_policy_test.go`, `internal/platform/httpserver/optout_policy_test.go` — Exact opt-out commands and language variants are governed through versioned effective-dated policy records with maker-checker submission and approval; inbound processing resolves the active policy at execution time. |
| CNS-013 | Must | NOT_STARTED | Allow manual suppression with reason and authority. |  |
| CNS-014 | Must | NOT_STARTED | Retain minimised suppression identifiers to prevent re-import contact. |  |
| CNS-015 | Must | NOT_STARTED | Support invalid-number and possible-recycled-number suppression. |  |
| CNS-016 | Must | NOT_STARTED | Run eligibility at segment estimate, snapshot creation and immediately before dispatch. |  |
| CNS-017 | Must | NOT_STARTED | Prevent consent review approval from overwriting individual withdrawal. |  |
| CNS-018 | Must | NOT_STARTED | Expose the exact exclusion reason for authorised operations users. |  |
| CNS-019 | Must | NOT_STARTED | Support configurable consent-expiry and renewal policies. |  |
| CNS-020 | Must | NOT_STARTED | Provide consent and suppression audit/export for authorised privacy workflows. |  |
| SEG-001 | Must | PARTIAL | Provide a visual segment builder requiring no SQL access. | `internal/audience/filter`, `internal/audience/cohort` |
| SEG-002 | Must | IMPLEMENTED_TESTED | Support equality, inclusion, exclusion, range, null and recency operators. | `internal/audience/filter`, `internal/audience/cohort/compiler_test.go` |
| SEG-003 | Must | IMPLEMENTED_TESTED | Support filters for country, state, LGA, age, gender, language, interests, source and consent attributes. | `internal/audience/filter` |
| SEG-004 | Must | NOT_STARTED | Support exact age range such as 18 through 35 using self-declared/estimated age policy. |  |
| SEG-005 | Must | NOT_STARTED | Display the age calculation method, source recency and confidence. |  |
| SEG-006 | Must | NOT_STARTED | Allow exclusion of age data older than a configurable period. |  |
| SEG-007 | Must | NOT_STARTED | Require active channel/purpose/organisation consent in the eligibility policy. |  |
| SEG-008 | Must | NOT_STARTED | Apply suppression, contact status and frequency caps to final eligibility. |  |
| SEG-009 | Must | NOT_STARTED | Support AND/OR groups with a maximum configurable complexity. |  |
| SEG-010 | Must | NOT_STARTED | Provide a human-readable summary of the segment logic. |  |
| SEG-011 | Must | NOT_STARTED | Estimate matching counts without exposing raw MSISDNs. |  |
| SEG-012 | Must | NOT_STARTED | Display a count waterfall: demographic, consent, expiry, suppression, cap and final eligible. |  |
| SEG-013 | Must | NOT_STARTED | Save, version, clone, retire and compare segment definitions. |  |
| SEG-014 | Must | IMPLEMENTED_TESTED | Restrict segment fields based on user permission and data-use policy. | `internal/audience/filter/query.go`, `internal/audience/cohort/compiler.go`, `internal/platform/httpserver/server.go`, `internal/audience/filter/administration_test.go`, `internal/audience/cohort/compiler_test.go` — Sensitive governed filter definitions require the configured permission during HTTP validation, compilation and snapshot creation; direct code submission cannot bypass hidden UI fields. |
| SEG-015 | Must | NOT_STARTED | Create an immutable audience snapshot with hash and policy versions. |  |
| SEG-016 | Must | NOT_STARTED | Store snapshot creation time, rule version and initiating user. |  |
| SEG-017 | Must | NOT_STARTED | Perform final live suppression/consent check before each send. |  |
| SEG-018 | Must | NOT_STARTED | Support cohort overlap analysis without exporting identities. |  |
| SEG-019 | Must | NOT_STARTED | Prevent tiny cohort reporting below configurable privacy threshold. |  |
| SEG-020 | Must | NOT_STARTED | Support asynchronous snapshot creation for multi-million audiences with progress and resumability. |  |
| CAM-001 | Must | NOT_STARTED | Create campaigns linked to one internal organisation and consent-review basis. |  |
| CAM-002 | Must | NOT_STARTED | Store campaign name, purpose, category, owner, requested window and completion deadline. |  |
| CAM-003 | Must | NOT_STARTED | Support draft, review, approved, scheduled, dispatching, paused, completed, exception, cancelled and expired states. |  |
| CAM-004 | Must | NOT_STARTED | Attach exactly one immutable audience snapshot to an approved release version. |  |
| CAM-005 | Must | NOT_STARTED | Compose text, image-caption, video, document and supported provider message types. |  |
| CAM-006 | Must | NOT_STARTED | Store media in object storage with checksum, type, size and scan status. |  |
| CAM-007 | Must | NOT_STARTED | Support approved personalisation variables with typed values and fallback. |  |
| CAM-008 | Must | NOT_STARTED | Preview message using representative test records with masked data. |  |
| CAM-009 | Must | NOT_STARTED | Send controlled test messages only to approved test numbers. |  |
| CAM-010 | Must | NOT_STARTED | Version message text, media, variables and links. |  |
| CAM-011 | Must | NOT_STARTED | Hash the approved message version and include it in campaign release. |  |
| CAM-012 | Must | NOT_STARTED | Require reapproval after material message, media, link, audience, sender or schedule change. |  |
| CAM-013 | Must | NOT_STARTED | Support optional reply/STOP instruction policy by campaign type. |  |
| CAM-014 | Must | NOT_STARTED | Support tracked links using a controlled redirect domain and signed identifiers. |  |
| CAM-015 | Must | NOT_STARTED | Validate destination URLs against configured safety rules. |  |
| CAM-016 | Must | NOT_STARTED | Schedule campaigns in a declared timezone and persist UTC. |  |
| CAM-017 | Must | NOT_STARTED | Support quiet hours and jurisdiction/organisation sending windows. |  |
| CAM-018 | Must | NOT_STARTED | Support start, deadline and latest-admission time. |  |
| CAM-019 | Must | NOT_STARTED | Allow pause, resume and cancel with reason and authority. |  |
| CAM-020 | Must | NOT_STARTED | Display impact before cancel or audience invalidation. |  |
| CAM-021 | Must | NOT_STARTED | Prevent deletion of campaigns with audit or delivery history. |  |
| CAM-022 | Must | NOT_STARTED | Support campaign tags and internal notes with visibility controls. |  |
| CAM-023 | Must | NOT_STARTED | Support cloning while generating new IDs and requiring fresh approvals. |  |
| CAM-024 | Must | NOT_STARTED | Support campaign archiving according to retention policy. |  |
| APR-001 | Must | NOT_STARTED | Implement configurable approval stages by campaign risk/size/category. |  |
| APR-002 | Must | NOT_STARTED | Require consent review approval before audience finalisation. |  |
| APR-003 | Must | NOT_STARTED | Require message/content approval before scheduling. |  |
| APR-004 | Must | NOT_STARTED | Require final campaign release approval by an authorised user. |  |
| APR-005 | Must | NOT_STARTED | Enforce maker-checker based on configuration and campaign threshold. |  |
| APR-006 | Must | NOT_STARTED | Record approval decision, actor, time, comments and object version. |  |
| APR-007 | Must | NOT_STARTED | Invalidate approval when approved data changes. |  |
| APR-008 | Must | IMPLEMENTED_TESTED | Record quote, invoice, payment and commercial-approval references without requiring online payment. | `internal/commercial/service.go`, `internal/commercial/service_test.go`, `internal/persistence/postgres/commercial.go`, `database/migrations/0039_campaign_commercial_governance.sql`, `internal/platform/httpserver/server.go`, `internal/campaign/commercial_gate_test.go` — Commercial evidence is recorded offline without requiring a public wallet or online payment flow; approval requires independent finance review and verified payment evidence. |
| APR-009 | Must | IMPLEMENTED_TESTED | Optionally block release until payment/commercial approval. | `internal/commercial/service.go`, `internal/commercial/service_test.go`, `internal/persistence/postgres/commercial.go`, `database/migrations/0039_campaign_commercial_governance.sql`, `internal/platform/httpserver/server.go`, `internal/campaign/commercial_gate_test.go` — Campaign commercial progression now resolves an approved campaign-specific commercial record and enforces the authorised recipient ceiling before COMMERCIAL_APPROVED. |
| APR-010 | Must | NOT_STARTED | Create a campaign entitlement defining maximum unique recipients and messages per recipient. |  |
| APR-011 | Must | NOT_STARTED | Tie entitlement to organisation, campaign, snapshot and message version. |  |
| APR-012 | Must | NOT_STARTED | Consume/reserve entitlement atomically with recipient ledger creation. |  |
| APR-013 | Must | NOT_STARTED | Require a new approved entitlement for an additional message or resend outside policy. |  |
| APR-014 | Must | NOT_STARTED | Support emergency stop by authorised role without deleting evidence. |  |
| APR-015 | Must | NOT_STARTED | Support approved exception workflow with reason, expiry and second approval. |  |
| SND-001 | Must | NOT_STARTED | Maintain an inventory of genuine sender MSISDNs and WhatsApp account metadata. |  |
| SND-002 | Must | NOT_STARTED | Store sender MSISDN encrypted and display masked by default. |  |
| SND-003 | Must | NOT_STARTED | Record ownership, registration country, profile/display name, recovery information reference and lifecycle status. |  |
| SND-004 | Must | NOT_STARTED | Support logical sender pools that map to one or more actual sender accounts. |  |
| SND-005 | Must | NOT_STARTED | Clearly disclose that logical sender is not an alphanumeric WhatsApp sender ID. |  |
| SND-006 | Must | NOT_STARTED | Support ready, connecting, disconnected, paused, draining, restricted, retired and quarantined sender states. |  |
| SND-007 | Must | NOT_STARTED | Allow each session to have exactly one active worker lease. |  |
| SND-008 | Must | NOT_STARTED | Renew leases through heartbeat and expire stale leases safely. |  |
| SND-009 | Must | NOT_STARTED | Require manual or controlled recovery before another worker activates an uncertain live session. |  |
| SND-010 | Must | NOT_STARTED | Record worker public/stable IP and infrastructure identity. |  |
| SND-011 | Must | NOT_STARTED | Allow several sessions per VPS only within configured tested resource limits. |  |
| SND-012 | Must | NOT_STARTED | Support per-session proxy settings only for approved stable routing. |  |
| SND-013 | Must | NOT_STARTED | Maintain per-sender configured and measured safe throughput. |  |
| SND-014 | Must | NOT_STARTED | Maintain per-sender hourly/daily safety limits and cooldown. |  |
| SND-015 | Must | NOT_STARTED | Maintain per-sender failure, disconnect and delivery-health thresholds. |  |
| SND-016 | Must | NOT_STARTED | Prevent automatic allocation to a newly paired sender until readiness checks pass. |  |
| SND-017 | Must | NOT_STARTED | Support sender reservation for a campaign or organisation. |  |
| SND-018 | Must | NOT_STARTED | Support one-active-campaign-per-sender policy as configurable default. |  |
| SND-019 | Must | NOT_STARTED | Provide pairing/QR access only to technical administrators with step-up authentication. |  |
| SND-020 | Must | NOT_STARTED | Never log session tokens, QR payloads or credentials. |  |
| SND-021 | Must | NOT_STARTED | Support controlled retirement and secure session-state deletion. |  |
| SND-022 | Must | NOT_STARTED | Maintain warm standby workers/senders without counting them as guaranteed capacity until health-checked. |  |
| DLV-001 | Must | PARTIAL | Create one authoritative campaign-recipient record per unique campaign, contact and message version. | `internal/delivery/model.go`, `database/migrations/0003_segments_campaigns_delivery.sql` |
| DLV-002 | Must | IMPLEMENTED_TESTED | Use a deterministic idempotency key for every recipient obligation. | `internal/delivery/model.go`, `internal/delivery/model_test.go` |
| DLV-003 | Must | IMPLEMENTED_TESTED | Create entitlement reservation, recipient record and outbox event atomically. | `internal/delivery/model.go`, `internal/delivery/model_test.go` |
| DLV-004 | Must | PARTIAL | Publish execution jobs from a transactional outbox. | `internal/jobs/queue.go`, `internal/jobs/postgres.go`, `database/migrations/0006_integrity_identity_jobs_and_leases.sql` |
| DLV-005 | Must | PARTIAL | Use Redis/BullMQ or equivalent only as execution queue, not source of truth. | `internal/jobs/queue.go`, `internal/jobs/queue_test.go` |
| DLV-006 | Must | IMPLEMENTED_TESTED | Partition large campaigns into bounded dispatch shards. | `internal/delivery/model.go`, `internal/delivery/model_test.go` |
| DLV-007 | Must | IMPLEMENTED_TESTED | Claim jobs atomically with lease/visibility timeout. | `internal/delivery/model.go`, `internal/delivery/model_test.go` |
| DLV-008 | Must | NOT_STARTED | Allocate jobs only to ready sender sessions with available capacity. |  |
| DLV-009 | Must | NOT_STARTED | Respect campaign schedule, quiet hours, deadline and pause state at dispatch time. |  |
| DLV-010 | Must | NOT_STARTED | Perform a final consent, suppression and contact-status check before submission. |  |
| DLV-011 | Must | NOT_STARTED | Apply per-sender rate and in-flight limits. |  |
| DLV-012 | Must | NOT_STARTED | Record every delivery attempt separately from final recipient state. |  |
| DLV-013 | Must | NOT_STARTED | Use configurable retry policy by failure category. |  |
| DLV-014 | Must | NOT_STARTED | Apply exponential/backoff with jitter for infrastructure errors. |  |
| DLV-015 | Must | NOT_STARTED | Avoid automatic resend after an uncertain provider outcome. |  |
| DLV-016 | Must | NOT_STARTED | Support safe operator retry only when duplicate risk is understood and approved. |  |
| DLV-017 | Must | NOT_STARTED | Support campaign pause that stops new claims while allowing controlled treatment of in-flight jobs. |  |
| DLV-018 | Must | NOT_STARTED | Support campaign cancellation that marks undispatched obligations cancelled and preserves attempts. |  |
| DLV-019 | Must | NOT_STARTED | Recover expired claims without losing or duplicating obligations. |  |
| DLV-020 | Must | NOT_STARTED | Reconstruct missing queue jobs from authoritative eligible states. |  |
| DLV-021 | Must | NOT_STARTED | Expose queue depth and oldest-job age by campaign/shard/sender. |  |
| DLV-022 | Must | NOT_STARTED | Prioritise jobs using configurable campaign priority without starving normal campaigns. |  |
| DLV-023 | Must | NOT_STARTED | Reserve capacity for high-priority campaigns only through explicit policy. |  |
| DLV-024 | Must | NOT_STARTED | Forecast completion using remaining jobs and measured available capacity. |  |
| DLV-025 | Must | NOT_STARTED | Reject or flag campaigns whose required rate exceeds measured safe capacity. |  |
| EVT-001 | Must | NOT_STARTED | Authenticate provider/gateway events using signature or trusted channel. |  |
| EVT-002 | Must | NOT_STARTED | Require event identifier, provider message identifier, type and occurrence time where available. |  |
| EVT-003 | Must | NOT_STARTED | Deduplicate repeated provider events. |  |
| EVT-004 | Must | NOT_STARTED | Handle out-of-order events using monotonic state precedence and event history. |  |
| EVT-005 | Must | NOT_STARTED | Preserve raw redacted event metadata for diagnosis according to retention. |  |
| EVT-006 | Must | NOT_STARTED | Map provider-specific states and errors to canonical statuses/reason codes. |  |
| EVT-007 | Must | NOT_STARTED | Correlate events using provider ID and fallback identifiers where safe. |  |
| EVT-008 | Must | NOT_STARTED | Never downgrade a terminal higher-confidence state because of a late lower state. |  |
| EVT-009 | Must | IMPLEMENTED_TESTED | Process inbound STOP and configured opt-out commands promptly. | `internal/gateway/inbound.go`, `internal/platform/httpserver/server.go`, `internal/platform/httpserver/inbound_optout_test.go` — Authenticated inbound callbacks process recognised opt-out commands immediately and idempotently. |
| EVT-010 | Must | IMPLEMENTED_TESTED | Store inbound replies with minimised content and access restrictions. | `internal/inbound/service.go`, `internal/inbound/service_test.go`, `internal/platform/httpserver/server.go`, `database/migrations/0025_inbound_reply_privacy_retention.sql`, `internal/inbound/postgres.go`, `database/migrations/0026_inbound_reply_content_encryption.sql` — Inbound reply content is permission-separated, purpose-bound encrypted in PostgreSQL, audited on reveal, and securely cleared by retention while preserving minimised operational evidence. |
| EVT-011 | Must | NOT_STARTED | Support manual classification/escalation of replies where required. |  |
| EVT-012 | Must | NOT_STARTED | Run scheduled reconciliation for submitted/pending/unknown records. |  |
| EVT-013 | Must | NOT_STARTED | Declare final unknown only after configured reconciliation window. |  |
| EVT-014 | Must | NOT_STARTED | Record discrepancy between gateway acceptance and later delivery outcome. |  |
| EVT-015 | Must | NOT_STARTED | Support bulk provider-status import/reconciliation if an adapter later provides it. |  |
| MET-001 | Must | NOT_STARTED | Provide campaign dashboard with audience, dispatch, delivery, engagement and performance sections. |  |
| MET-002 | Must | NOT_STARTED | Show uploaded/imported, valid, duplicate, invalid, suppressed, consent-ineligible, capped and final-eligible counts. |  |
| MET-003 | Must | NOT_STARTED | Show authorised, queued, claimed, submitted, gateway-accepted, sent, delivered, read, failed, unknown and cancelled counts. |  |
| MET-004 | Must | NOT_STARTED | Do not label submitted or gateway-accepted messages as delivered. |  |
| MET-005 | Must | NOT_STARTED | Show denominator and definition for every rate. |  |
| MET-006 | Must | NOT_STARTED | Show current, average and peak submission throughput. |  |
| MET-007 | Must | NOT_STARTED | Show queue depth, oldest job age and forecast completion. |  |
| MET-008 | Must | NOT_STARTED | Show actual start, completion and deadline variance. |  |
| MET-009 | Must | NOT_STARTED | Show retry count, retry rate and major failure reasons. |  |
| MET-010 | Must | NOT_STARTED | Show sender allocation, success, failure, delivery ratio, latency and reconnect count. |  |
| MET-011 | Must | NOT_STARTED | Show geography and demographic breakdowns using snapshot attributes. |  |
| MET-012 | Must | NOT_STARTED | Apply configurable minimum-cohort privacy threshold. |  |
| MET-013 | Must | NOT_STARTED | Show replies, opt-outs and link clicks where reliably available. |  |
| MET-014 | Must | NOT_STARTED | Maintain incremental aggregate counters rather than rescan all recipient rows for each dashboard load. |  |
| MET-015 | Must | NOT_STARTED | Make metric event consumers idempotent. |  |
| MET-016 | Must | NOT_STARTED | Provide near-real-time updates with configurable refresh interval. |  |
| MET-017 | Must | NOT_STARTED | Generate an organisation-facing campaign report without portal access. |  |
| MET-018 | Must | NOT_STARTED | Support PDF-ready, XLSX and CSV report data outputs. |  |
| MET-019 | Must | NOT_STARTED | Exclude internal notes, secrets, raw error payloads and unnecessary MSISDNs from client report. |  |
| MET-020 | Must | NOT_STARTED | Support approved branding and explanatory caveats. |  |
| MET-021 | Must | NOT_STARTED | Allow campaign report regeneration from retained summary data. |  |
| MET-022 | Must | NOT_STARTED | Record report generator, time, version and checksum. |  |
| MET-023 | Must | NOT_STARTED | Support masked recipient-level exception export for authorised operations. |  |
| MET-024 | Must | NOT_STARTED | Expose metric-quality flags for incomplete provider events or reconciliation gaps. |  |
| ADM-001 | Must | PARTIAL | Provide an internal configuration catalogue with typed values and validation. | `internal/audience/filter/administration.go`, `internal/persistence/postgres/filter_definitions.go`, `database/migrations/0019_filter_definition_administration.sql`, `internal/platform/httpserver/server.go` — Governed typed audience-filter catalogue is implemented with validation, versioning, history and protected administration APIs. The broader platform configuration catalogue remains incomplete. |
| ADM-002 | Must | PARTIAL | Version and effective-date material configuration. | `internal/audience/filter/administration.go`, `database/migrations/0019_filter_definition_administration.sql` — Audience-filter definitions are versioned with immutable history and optimistic concurrency. Effective-dated activation for the complete material configuration catalogue remains outstanding. |
| ADM-003 | Must | PARTIAL | Require approval for security, consent, retention, throughput and retry policy changes. | `internal/consent/optout_policy.go`, `internal/platform/httpserver/optout_policy.go`, `internal/inbound/retention_policy.go`, `internal/shared/crypto/keyring.go`, `database/migrations/0028_governed_inbound_retention_and_key_rotation.sql`, `internal/inbound/rotation.go`, `internal/inbound/rotation_postgres.go`, `cmd/inbound-governance-worker/main.go`, `database/migrations/0029_resumable_inbound_governance_operations.sql` — Opt-out policy changes require independent approval. Other security, retention, throughput and retry configuration groups still require the same governed approval model. Version 0.4.8 adds maker-checker inbound retention policy and versioned content-key rotation controls. Version 0.4.9 adds durable, lease-fenced and resumable key-rotation runs plus scheduled retention-sweep evidence. |
| ADM-004 | Must | NOT_STARTED | Support rollback to a prior configuration version. |  |
| ADM-005 | Must | NOT_STARTED | Configure consent purposes, channels, expiry and review rules. |  |
| ADM-006 | Must | NOT_STARTED | Configure age staleness and estimation policy. |  |
| ADM-007 | Must | NOT_STARTED | Configure state/LGA reference data and validation. |  |
| ADM-008 | Must | IMPLEMENTED_TESTED | Configure suppression precedence and STOP keywords. | `internal/consent/optout_policy.go`, `internal/persistence/postgres/optout_policy.go`, `database/migrations/0024_governed_opt_out_policy.sql`, `internal/consent/eligibility.go`, `internal/consent/eligibility_test.go`, `internal/orchestration/postgres.go`, `internal/dispatch/postgres_material.go`, `database/migrations/0040_consent_precedence_and_dispatch_guards.sql` — STOP commands are versioned, effective-dated, maker-checker governed and retained with immutable PostgreSQL history; suppression precedence remains enforced at final dispatch. Version 0.8.8 adds deterministic latest-effective-grant precedence and revalidates live consent-review evidence at release and dispatch. |
| ADM-009 | Must | NOT_STARTED | Configure frequency caps by organisation/purpose/channel. |  |
| ADM-010 | Must | NOT_STARTED | Configure campaign approval thresholds and roles. |  |
| ADM-011 | Must | NOT_STARTED | Configure sender/session limits, rate caps and health thresholds. |  |
| ADM-012 | Must | NOT_STARTED | Configure retry, reconciliation and unknown-outcome windows. |  |
| ADM-013 | Must | NOT_STARTED | Configure report templates, privacy thresholds and retention. |  |
| ADM-014 | Must | PARTIAL | Record business and security audit events append-only. | `internal/audit/event.go`, `database/migrations/0004_security_audit.sql`, `internal/inbound/service.go`, `internal/platform/httpserver/server.go`, `database/migrations/0026_inbound_reply_content_encryption.sql`, `internal/inbound/postgres.go`, `database/migrations/0027_inbound_reply_legal_hold_governance.sql` |
| ADM-015 | Must | PARTIAL | Include actor, action, object, before/after summary, reason, IP/device, time and correlation ID in audit where applicable. | `internal/audit/event.go`, `internal/inbound/service.go`, `internal/platform/httpserver/server.go`, `database/migrations/0026_inbound_reply_content_encryption.sql`, `internal/inbound/postgres.go`, `database/migrations/0027_inbound_reply_legal_hold_governance.sql` |
| ADM-016 | Must | NOT_STARTED | Provide searchable audit views with export controls. |  |
| ADM-017 | Must | IMPLEMENTED_TESTED | Protect audit integrity using restricted roles and tamper-evident chaining or external archival. | `internal/audit/event.go`, `internal/audit/event_test.go`, `database/migrations/0006_integrity_identity_jobs_and_leases.sql` |
| ADM-018 | Must | NOT_STARTED | Alert on privileged configuration changes and audit-control failures. |  |
| PRV-001 | Must | NOT_STARTED | Support authorised exact contact lookup using HMAC and controlled decryption. |  |
| PRV-002 | Must | NOT_STARTED | Provide a data-subject access package containing profile, sources, consent, suppression and campaign history according to policy. |  |
| PRV-003 | Must | NOT_STARTED | Support correction requests with source/reason and history. |  |
| PRV-004 | Must | IMPLEMENTED_TESTED | Support objection/opt-out with immediate effective suppression. | `internal/consent/optout.go`, `internal/platform/httpserver/inbound_optout_test.go` — A recognised authenticated opt-out creates an immediate effective global suppression and blocks future eligibility. |
| PRV-005 | Must | NOT_STARTED | Support deletion/anonymisation workflow subject to lawful retention and suppression requirements. |  |
| PRV-006 | Must | NOT_STARTED | Support contact export in structured format for approved portability requests. |  |
| PRV-007 | Must | NOT_STARTED | Track DSAR status, deadlines, owner and evidence. |  |
| PRV-008 | Must | NOT_STARTED | Restrict disclosure of third-party or security-sensitive information. |  |
| PRV-009 | Must | PARTIAL | Support retention holds and legal holds with approval. | `internal/inbound/service.go`, `internal/inbound/postgres.go`, `internal/platform/httpserver/server.go`, `database/migrations/0027_inbound_reply_legal_hold_governance.sql`, `internal/inbound/retention_policy.go`, `internal/shared/crypto/keyring.go`, `database/migrations/0028_governed_inbound_retention_and_key_rotation.sql`, `internal/inbound/rotation.go`, `internal/inbound/rotation_postgres.go`, `cmd/inbound-governance-worker/main.go`, `database/migrations/0029_resumable_inbound_governance_operations.sql` — Inbound reply content supports reasoned, actor-attributed, optimistic-concurrency legal holds and controlled release; broader platform-wide legal hold coverage remains incomplete. Version 0.4.8 adds maker-checker inbound retention policy and versioned content-key rotation controls. Version 0.4.9 adds durable, lease-fenced and resumable key-rotation runs plus scheduled retention-sweep evidence. |
| PRV-010 | Must | NOT_STARTED | Provide privacy-impact and data-inventory reports. |  |
| BR-001 | Must | NOT_STARTED | A demographic match alone never authorises a campaign send. |  |
| BR-002 | Must | NOT_STARTED | Eligibility requires a valid organisation/controller, purpose and channel basis plus no overriding suppression. |  |
| BR-003 | Must | NOT_STARTED | An individual withdrawal overrides an earlier or organisation-level approval. |  |
| BR-004 | Must | PARTIAL | The same campaign/message version shall not be sent more than once to the same contact unless a separately approved resend is created. | `internal/delivery/model.go`, `database/migrations/0003_segments_campaigns_delivery.sql` |
| BR-005 | Must | NOT_STARTED | An audience snapshot is immutable; changes require a new snapshot and approval. |  |
| BR-006 | Must | NOT_STARTED | A message version is immutable after approval; any material change creates a new version. |  |
| BR-007 | Must | NOT_STARTED | Campaign entitlement cannot be transferred or reused by another campaign. |  |
| BR-008 | Must | IMPLEMENTED_TESTED | Gateway acceptance does not equal sent, delivered or read. | `internal/delivery/model.go`, `internal/delivery/model_test.go` |
| BR-009 | Must | NOT_STARTED | Only actual registered WhatsApp MSISDNs may be used as OpenWA senders. |  |
| BR-010 | Must | NOT_STARTED | A logical sender pool is an internal routing abstraction, not a recipient-facing alphanumeric sender ID. |  |
| BR-011 | Must | PARTIAL | A live WhatsApp session shall have exactly one active worker owner. | `internal/gateway/lease.go`, `internal/gateway/lease_test.go` |
| BR-012 | Must | NOT_STARTED | Stable worker IPs may be used for isolation; rapid IP rotation or evasion logic is prohibited. |  |
| BR-013 | Must | NOT_STARTED | A sender’s safe capacity is based on measured evidence and configured headroom, not theoretical batch concurrency. |  |
| BR-014 | Must | NOT_STARTED | The platform shall not accept a hard deadline when measured safe capacity is insufficient without an explicit exception. |  |
| BR-015 | Must | NOT_STARTED | Unknown outcomes are reconciled before any resend decision. |  |
| BR-016 | Must | NOT_STARTED | Contact profile updates cannot silently broaden consent. |  |
| BR-017 | Must | NOT_STARTED | Reported age remains as collected; derived estimated age is labelled approximate. |  |
| BR-018 | Must | NOT_STARTED | No gender, age, location or interest value shall be inferred without an approved source and policy. |  |
| BR-019 | Must | PARTIAL | Raw MSISDN values shall not appear in standard logs, URLs or monitoring dimensions. | `internal/shared/crypto/msisdn.go`, `internal/platform/httpserver/server_test.go` |
| BR-020 | Must | NOT_STARTED | External organisations do not receive portal access in release one. |  |
| BR-021 | Must | NOT_STARTED | All critical overrides require a reason, expiry and audit; high-risk overrides require a second approver. |  |
| BR-022 | Must | NOT_STARTED | Every report shall state its metric definitions and provider-status limitations. |  |
| BR-023 | Must | NOT_STARTED | Suppression is checked again immediately before dispatch. |  |
| BR-024 | Must | PARTIAL | Redis loss must not erase authorised campaign obligations. | `internal/jobs/postgres.go`, `database/migrations/0006_integrity_identity_jobs_and_leases.sql` |
| BR-025 | Must | IMPLEMENTED_TESTED | A worker crash during submission produces an uncertain state until reconciled; it does not automatically send again. | `internal/delivery/model.go`, `internal/delivery/model_test.go` |
| BR-026 | Must | NOT_STARTED | Configuration used for consent, segmentation, dispatch and reporting is versioned and retained with the campaign. |  |
| BR-027 | Must | NOT_STARTED | Small demographic report cells are suppressed or grouped according to privacy threshold. |  |
| BR-028 | Must | NOT_STARTED | Campaign evidence and source files are retained or deleted only according to approved retention rules. |  |
| BR-029 | Must | IMPLEMENTED_TESTED | A suspended organisation or sender cannot be selected for a new release. | `internal/organisation/service.go`, `internal/campaign/service.go`, `internal/consent/review.go`, `internal/audience/importer/import.go`, `internal/orchestration/release.go`, `internal/campaign/organisation_policy_test.go` — Inactive organisations fail closed across campaign creation/progression, consent review, import approval and release; safety actions such as pause/cancel remain available. |
| BR-030 | Must | NOT_STARTED | All campaign and sender status changes are attributable to a user, service or system policy. |  |
| API-001 | Must | NOT_STARTED | Gateway delivery command shall not contain raw consent or unnecessary demographic data. |  |
| API-002 | Must | NOT_STARTED | Recipient data shall be decrypted only within the minimum authorised boundary required to send. |  |
| API-003 | Must | NOT_STARTED | Gateway shall reject work for an unowned, paused or incompatible session. |  |
| API-004 | Must | PARTIAL | Gateway shall return the same accepted result for a repeated idempotency key. | `services/openwa-gateway/src/provider/mock.provider.ts` |
| API-005 | Must | NOT_STARTED | Gateway events shall include stable delivery and provider correlation identifiers. |  |
| API-006 | Must | NOT_STARTED | Gateway capability response shall include message types, engine version and event support. |  |
| API-007 | Must | NOT_STARTED | Service-to-service credentials shall be scoped, rotated and stored outside source code. |  |
| API-008 | Must | NOT_STARTED | API schemas shall be backward compatible within a supported major version. |  |
| UX-001 | Must | NOT_STARTED | Use an accessible responsive desktop-first interface for internal operations. |  |
| UX-002 | Must | NOT_STARTED | Show environment and classification banners. |  |
| UX-003 | Must | NOT_STARTED | Use masked PII by default with explicit authorised reveal. |  |
| UX-004 | Must | NOT_STARTED | Show status, owner, next required action and blocking reasons on every campaign. |  |
| UX-005 | Must | NOT_STARTED | Provide progressive count waterfall in segment builder. |  |
| UX-006 | Must | NOT_STARTED | Warn clearly when age is approximate or stale. |  |
| UX-007 | Must | NOT_STARTED | Require explicit confirmation for final release, pause, cancel, export and override. |  |
| UX-008 | Must | NOT_STARTED | Show immutable version identifiers for message, audience, consent review and config. |  |
| UX-009 | Must | NOT_STARTED | Show actual sender MSISDN masked and explain logical pool behaviour. |  |
| UX-010 | Must | NOT_STARTED | Provide live operational campaign view with queue, throughput, forecast and incidents. |  |
| UX-011 | Must | NOT_STARTED | Use reason codes plus human-readable remediation for failures. |  |
| UX-012 | Must | NOT_STARTED | Support saved filters and views without changing authorisation. |  |
| UX-013 | Must | NOT_STARTED | Provide secure export workflow with purpose, approval and expiry. |  |
| UX-014 | Must | NOT_STARTED | Provide keyboard navigation, labelled controls, focus visibility and semantic tables. |  |
| UX-015 | Must | IMPLEMENTED_TESTED | Prevent dangerous browser caching of sensitive pages. | `internal/platform/httpserver/server.go`, `internal/platform/httpserver/server_test.go` |
| HST-001 | Must | NOT_STARTED | Expose only reverse-proxy HTTPS endpoints required for the portal/API/webhooks. |  |
| HST-002 | Must | NOT_STARTED | Restrict SSH to approved source IP/VPN and use keys. |  |
| HST-003 | Must | NOT_STARTED | Block public PostgreSQL and Redis access. |  |
| HST-004 | Must | NOT_STARTED | Allow gateway/control communication only between approved node identities. |  |
| HST-005 | Must | NOT_STARTED | Use DNS and certificates that can move to another provider. |  |
| HST-006 | Must | NOT_STARTED | Synchronise time securely across nodes. |  |
| HST-007 | Must | NOT_STARTED | Record node inventory, OS, IP, role and deployed version. |  |
| HST-008 | Must | NOT_STARTED | Apply OS and container security updates through controlled maintenance. |  |
| HST-009 | Must | NOT_STARTED | Separate production, staging and monitoring credentials. |  |
| HST-010 | Must | NOT_STARTED | Implement disk, memory, CPU, inode, process/PID and network alerts. |  |
| BKP-001 | Must | NOT_STARTED | Perform scheduled PostgreSQL full backups and continuous/incremental WAL archiving. |  |
| BKP-002 | Must | NOT_STARTED | Encrypt backups before or during transfer to an independent target. |  |
| BKP-003 | Must | NOT_STARTED | Keep backup credentials separate from primary database credentials. |  |
| BKP-004 | Must | NOT_STARTED | Back up object metadata and evidence/media according to retention. |  |
| BKP-005 | Must | NOT_STARTED | Back up configuration, infrastructure code and deployment manifests through private repository. |  |
| BKP-006 | Must | NOT_STARTED | Do not rely solely on Hostinger weekly whole-VPS backup. |  |
| BKP-007 | Must | NOT_STARTED | Test restoration at least monthly during initial production maturity. |  |
| BKP-008 | Must | NOT_STARTED | Validate restored data integrity, row counts, keys and application compatibility. |  |
| BKP-009 | Must | NOT_STARTED | Document and test replica promotion or recovery-target activation. |  |
| BKP-010 | Must | NOT_STARTED | Protect backups against ordinary application deletion and ransomware. |  |
| AC-001 | Must | NOT_STARTED | An authorised internal user can create an organisation and record a consent review with evidence, restrictions and expiry. |  |
| AC-002 | Must | NOT_STARTED | The system securely imports and reconciles a large contact file, preserving source and validation reasons. |  |
| AC-003 | Must | PARTIAL | The platform stores MSISDN encrypted, deduplicates using HMAC and masks it in UI/logs. | `internal/shared/crypto/msisdn.go`, `internal/audience/importer/csv.go` |
| AC-004 | Must | NOT_STARTED | The segment builder creates a Lagos age 18–35 cohort using the configured self-declared-age policy. |  |
| AC-005 | Must | NOT_STARTED | The final eligible count applies organisation/purpose/channel consent, expiry, suppression and frequency caps. |  |
| AC-006 | Must | NOT_STARTED | The system creates an immutable audience snapshot and preserves policy/configuration versions. |  |
| AC-007 | Must | NOT_STARTED | A campaign message and media are versioned, scanned, tested and approved through maker-checker. |  |
| AC-008 | Must | NOT_STARTED | Campaign entitlement prevents excess or duplicate recipients. |  |
| AC-009 | Must | NOT_STARTED | Recipient records/outbox are created atomically and queue can be rebuilt. |  |
| AC-010 | Must | NOT_STARTED | A gateway worker sends only for sessions it exclusively owns and honours idempotency. |  |
| AC-011 | Must | NOT_STARTED | Worker and queue failure tests do not silently lose obligations or automatically duplicate uncertain sends. |  |
| AC-012 | Must | NOT_STARTED | Campaign metrics accurately distinguish gateway accepted, sent, delivered, read, failed and unknown. |  |
| AC-013 | Must | PARTIAL | An opt-out after snapshot prevents subsequent dispatch and appears in reporting. | `internal/consent/optout.go`, `internal/orchestration/postgres.go`, `internal/consent/eligibility.go`, `internal/consent/eligibility_test.go`, `internal/dispatch/postgres_material.go`, `database/migrations/0040_consent_precedence_and_dispatch_guards.sql` — Post-snapshot opt-outs are excluded by the final live suppression check with a specific SUPPRESSED reason. Campaign-report presentation remains outstanding. Version 0.8.8 adds deterministic latest-effective-grant precedence and revalidates live consent-review evidence at release and dispatch. |
| AC-014 | Must | NOT_STARTED | A five-million-recipient synthetic campaign ledger can be created, resumed, queried and reported within accepted performance. |  |
| AC-015 | Must | NOT_STARTED | A ten-million-contact synthetic repository supports required cohort queries within accepted performance. |  |
| AC-016 | Must | NOT_STARTED | The Hostinger pilot environment is reproducible, monitored, backed up and successfully restored. |  |
| AC-017 | Must | NOT_STARTED | Security testing shows role isolation, MFA, safe uploads, no PII logs and protected exports. |  |
| AC-018 | Must | NOT_STARTED | Audit evidence reconstructs the full path from consent review through final report. |  |
| AC-019 | Must | PARTIAL | The private repository retains MIT attribution, blocks direct main changes and does not auto-merge upstream. | `LICENSE`, `UPSTREAM.md`, `.github/workflows/ci.yml` |
| AC-020 | Must | NOT_STARTED | Operational owners approve runbooks, emergency stop, incident response and capacity representation. |  |
