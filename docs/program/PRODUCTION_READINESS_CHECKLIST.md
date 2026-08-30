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

## I4 large production-hardening boundary — pre-council evidence (2026-08-18)

<!-- I4_LARGE_PRODUCTION_HARDENING_PRECOUNCIL -->

The former narrow I4 deployment-readiness slice has been deliberately widened into one substantial local backend production-hardening boundary. It now covers deployment-readiness evidence contracts plus the release-governance trust boundary: exact hard-gate membership and authoritative definitions, exact governance-document membership, safe repo-relative paths, structured CLOSED evidence, formal time-bounded WAIVED evidence, future-timestamp rejection, malformed-evidence fail-closed handling, Must-traceability coupling, and RG-006/RG-007 coupling to accepted recovery/monitoring evidence.

Pre-council gate evidence on the exact local candidate:
- governance: **97/97 GREEN**
- exact Go: `go test -count=1 ./... && go vet ./... && go build ./...` → `I4_LARGE_EXACT_GO_GREEN`
- full race: `go test -race -count=1 ./...` → `I4_LARGE_FULL_RACE_GREEN`
- gateway TypeScript: `I4_LARGE_GATEWAY_TSC_GREEN`
- Linux gateway authority/lifecycle/durability: `I4_LARGE_LINUX_GATEWAY_GREEN`
- Node audits: admin **0 vulnerabilities**, gateway **0 vulnerabilities**
- CycloneDX SBOM: **29 components**
- production Compose structure: **7 Railway services / 25 secrets**
- OpenAPI: **294** implemented `/api/v1` method/path pairs
- traceability: **398 total = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**, regenerated byte-for-byte unchanged
- production-candidate honesty check: actual **exit 2**, with all eight hard gates still outstanding
- source/config fingerprint: tracked diff `61309317112221e3dcd9131e0793e377dd6176055d37dfeb79e548af12995ce5`; untracked content `d75d5ba7722cea667d5d1927c8667db8fc40154dcc68613d3b4e64577c521f47`; untracked non-doc files **192**

No live Railway/Hostinger deployment, real provider traffic, live monitoring proof, DR exercise, external penetration test, or other external release evidence is claimed. All hard gates remain OPEN or BLOCKED_EXTERNAL exactly as declared. The next boundary is an exact-source blind council using Grok 4.6, DeepSeek V4 Pro, Gemini 3.7 Flash, and GLM 5.2; adjudication precedes any remediation.

## I4 council adjudication, remediation, and full re-gate (2026-08-19)

<!-- I4_POST_COUNCIL_REMEDIATION_REGATE -->

The exact-source I4 blind council produced two independently adjudicated valid defects. Grok 4.6 and Gemini 3.7 Flash identified a metrics-worker health-port contract drift (`:8093` runtime default versus authoritative Railway port `8096`); Grok 4.6 identified a control-api readiness-probe drift (`/healthz` in production Compose versus authoritative `/readyz`). DeepSeek V4 Pro returned PASS. GLM 5.2 returned no usable review because its response hit the model length limit; that is recorded as an evidence limitation, not as a pass or a programme blocker.

Both valid findings were reproduced RED before remediation, then fixed as one coherent I4 batch. Metrics-worker now defaults to `:8096`. Control-api production Compose now probes `/readyz`, and `verify-production-compose.py` fail-closes service health port/path drift against `infrastructure/railway/service-contracts.json` for every Railway service.

Post-remediation gate evidence on the changed local candidate:
- focused regressions: **GREEN** for metrics-worker default-port contract and production healthcheck contract drift
- governance: **98/98 GREEN**
- exact Go: `go test -count=1 ./... && go vet ./... && go build ./...` -> `I4_NATIVE_EXACT_GO_GREEN`
- full race: `go test -race -count=1 ./...` -> `I4_NATIVE_FULL_RACE_GREEN`
- gateway TypeScript: **GREEN**
- Linux gateway control-plane/outbox/session-authority/lifecycle/durability/recovery suite: **GREEN**
- Node production audits: admin **0 vulnerabilities**, gateway **0 vulnerabilities**
- CycloneDX SBOM: **29 components**
- static/security/topology/release/OpenAPI bundle: `I4_POST_COUNCIL_STATIC_SECURITY_GREEN`; OpenAPI remains **294/294** implemented `/api/v1` method/path pairs
- production-candidate honesty check: actual **exit 2** as required, with all eight hard release gates still outstanding

No live Railway/Hostinger deployment, real provider traffic, live monitoring proof, DR exercise, external penetration test, or other external release evidence is claimed. RG-001 through RG-008 remain OPEN or BLOCKED_EXTERNAL exactly as declared. Because reviewed source changed during remediation, the next mandatory boundary is a new exact-source/config fingerprint and a fresh genuinely blind council before I4 can be accepted; any fresh valid findings must again be adjudicated before remediation.

## 24 August 2026 — I4 Fresh14 adjudication and Fresh15 remediation boundary

Fresh14 produced eight usable blind lanes across three actual model families; two DeepSeek lanes were unusable at the response-length boundary and are correctly labelled as GLM 5.3 fallbacks. Independent adjudication is preserved at `validation/ai-review/infrastructure-i4-fresh14-20260823/I4-fresh14-adjudication.md`. Confirmed defects were governed-node URL drift, case-variant waiver self-approval, a fixed-date test time bomb, and the reachable unassigned-node portion of rejection-evidence loss. The claimed staging allowlist bypass and native IPv6 control-host bypass were rejected. A maximum waiver horizon remains policy authority not supplied by engineering; no arbitrary duration was invented.

The coherent Fresh15 remediation now binds a non-empty governed node URL while preserving first-registration bootstrap; compares waiver actors with canonical Unicode case-folding; uses a time-relative acceptance fixture; and adds migration 0090 so only authenticated `REJECTED` runtime events may lack a governed pool anchor while the declared pool remains in immutable runtime identity. The original deleted-pool sequence is prevented by the existing sender-node foreign key.

Current post-change host evidence is governance **128/128 GREEN**, all Go packages except the Linux-only secret-file mode test GREEN on Windows, `go vet ./...` GREEN, `go build ./...` GREEN, focused URL/schema regressions GREEN, and diff hygiene GREEN. The excluded secret-file test correctly cannot prove Unix group/world bits on Windows and will not be weakened. Exact Go 1.23.2 Linux test/race, fresh PostgreSQL 17 migration histories through 0090, the 40-DSN matrix, gateway/Node/static/security/SBOM gates and Fresh15 exact-source review remain mandatory before acceptance.

The sender-name discussion is now durable authority in ADR-0006 and `docs/requirements/CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`: reusable underlying account, exclusive campaign reservation, requested-versus-observed profile identity, verification before READY, versioned engine capabilities, and disabled-by-default username operations until genuine validation.

All eight release hard gates remain OPEN or BLOCKED_EXTERNAL. No live deployment/provider traffic, production credential mutation, scale proof, DR exercise, penetration test, operational exercise or frontend completion is claimed. Fresh14 is historical after source changes; Fresh15 has not yet been frozen or reviewed.

## 24 August 2026 — I4 Fresh18 sizeable remediation and pre-council boundary

Fresh16 and Fresh17 were sizeable exact-source council boundaries, not file-by-file reviews. Fresh16 confirmed four defects that Fresh17 remediated: atomic nonce/rejection persistence, durable governed-pool rejection anchoring, gateway/control hostname separation, healthcheck-scoped Compose verification, and observed private-bind evidence. Fresh17 then confirmed three remaining defects: wall-clock runtime causality allowed a retired boot to reclaim service and fenced healthy clock corrections; invalid production capacity could be swallowed by deferred registration; and production gate-closure metadata was not bound to the actual candidate evidence artifact.

Fresh18 remediates those findings as one engineering chunk. Signed runtime reports now carry a positive per-boot `runtimeSequence`; memory and PostgreSQL accept only a newer same-boot sequence, retain accepted boot history, reject every state from a retired boot, keep same-boot DRAINING terminal, and treat `observedAt` as freshness evidence rather than a causal clock. Production gateway startup validates capacity before deferred publication. Production-candidate release verification now requires safe local evidence paths, SHA-256 byte binding, schema-versioned JSON manifests bound to the exact gate and candidate fingerprint, non-empty observed evidence, and independent owner/approver identity. Development-mode structural checks and honestly open gates remain unaffected.

Fresh18 exact-source evidence is GREEN: **2,384 files / 0 byte mismatches**; governance **137/137**; Go ordinary, full race, vet and build; fresh PostgreSQL 17 with three histories through migration 0090 and the full **40-DSN** suite; gateway TypeScript/build, control-plane security **30/30**, and **38** retained durability/lifecycle tests; OpenAPI **294/294**; production Compose **7 Railway services / 25 secrets**; resolved Hostinger boundary for both BAILEYS and WHATSAPP_WEB_JS; committed-secret scan; Node audits **0/0 vulnerabilities**; strict CycloneDX **30 components / 8 images** with SHA-256 `84257faddf0d708bf0a9e0b0b95d897fdc5ef40a922b1ff4225aee8fcf1be7c3`. Production-candidate validation correctly exits 2 because all eight release gates remain OPEN or BLOCKED_EXTERNAL.

The campaign-scoped sender-name authority remains ADR-0006 plus `CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`: reusable account identity is distinct from exclusive campaign reservation and requested-versus-observed sender profile identity. No live deployment, provider traffic, production credential/network mutation, capacity proof, DR exercise, penetration test, operational exercise, or frontend completion is claimed. Fresh18 must now be frozen twice and reviewed once as this complete 54-path chunk; no staging or local acceptance commit is permitted before council adjudication.

## 24 August 2026 — I4 Fresh19 council-remediation boundary

Fresh18 completed one valid 8-lane council after the full sizeable chunk; two primary Grok lanes timed out twice and used explicitly labelled GLM 5.3 fallbacks. The council rejected Fresh18. Adjudication confirmed six material issues and rejected the unsupported or overbroad claims: gateway authority URLs admitted credentials or non-authority components and lacked canonical host/port equality; production capacity was not explicitly coupled to the engine session limit; release manifests did not bind each evidence item to governed gate-local bytes; a rejected authenticated DRAINING declaration did not make that boot terminal; governed runtime authority was checked before but not inside the serialized store transaction; and a configured production boot ID could be reused while the runtime sequence restarted.

Fresh19 remediates those findings as one coherent backend/infrastructure chunk. Go and TypeScript now accept only HTTPS authority-only gateway URLs, canonicalize hostname/trailing dot/port (including default 443), and reject invalid ports. Memory and PostgreSQL recheck governed pool, URL, provider, engine and adapter under their write lock/transaction. Any authenticated DRAINING declaration makes its boot terminal even when the replacement drain is rejected. Production gateway startup rejects configured boot IDs and requires explicit 1..1000 `GATEWAY_CAPACITY` equal to `OPENWA_MAX_CONCURRENT_SESSIONS`; Hostinger Compose and resolved topology enforce the same contract. Production closure/waiver manifests now restrict evidence kinds per gate and bind each item to a non-empty, non-self-referential gate-local artifact with its own SHA-256 fingerprint.

Fresh19 must receive a new exact-source ordinary/race/vet/build, fresh-PG/40-DSN, gateway, governance, topology, release, security, SBOM, traceability and byte-comparison matrix after documentation synchronization. Only then may it be frozen twice and sent to one council as the complete 54-path engineering boundary. The staged index remains empty; no acceptance commit, push, deployment, real-provider traffic, production credential/network mutation, capacity claim, DR claim, penetration-test claim, operational-readiness claim or frontend-completion claim is permitted before adjudication.

The campaign-scoped sender-name authority remains ADR-0006 plus `CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`: reusable account identity is separate from exclusive campaign reservation and requested-versus-provider-observed sender identity. This approved later backend tranche is not silently folded into Fresh19 or claimed implemented.

Fresh19 final executable and governance evidence is GREEN: exact candidate snapshot **2,288 files / 0 byte mismatches**; governance **141/141**; Go ordinary, full race (including the 441.393-second HTTP-server suite), vet and all seven native-libpq command builds; fresh PostgreSQL 17 current/pre-0085/pre-Meta histories through 0090 and the full **40-DSN** repository suite at exit 0; gateway TypeScript/build, control-plane security **32/32**, and **38** retained durability/lifecycle tests; OpenAPI **294/294**; production Compose **7 Railway services / 25 secrets**; resolved Hostinger boundary for BAILEYS and WHATSAPP_WEB_JS; committed-secret scan; Node audits **0/0 vulnerabilities**; strict CycloneDX **30 components / 8 images** with SHA-256 `4cebeb30bcec7a19faad0bc2852b67eb4d7bb9c57582f6827d68f378f1fb3a7f`; and traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL** with byte-identical Markdown and semantically identical JSON regeneration. Production-candidate validation correctly exits 2 because all eight hard gates remain OPEN or BLOCKED_EXTERNAL.

The candidate remains **54 paths**, HEAD `887e577565de22642cf4041a223835d122e6ff15`, branch `work/backend-production-engineering`, with **0 staged paths**. Fresh19 is eligible for deterministic double freeze and one council at this complete boundary; it is not accepted or committed unless adjudication finds no remaining material defect.

## 24 August 2026 — I4 Fresh20 council-remediation boundary

Fresh19 completed one valid blind council at the full 54-path boundary: **8/8 usable lanes**, three actual reviewer families, and three transparently labelled GLM 5.3 fallbacks after unusable primary output. Fresh19 is rejected and remains unstaged. Independent adjudication confirmed nine material or governance defects while rejecting or narrowing overbroad claims: the Railway control origin was not authority-only; TypeScript admitted gateway port zero; Go mishandled IPv6 with explicit default 443; a sibling manifest could be laundered as leaf release evidence; Compose health validation trusted a first-match decoy; invalid URL rejection erased the declared URL; deployed node registration admitted unusable authorities; stale-version DRAINING bypassed nonce/evidence/boot terminality; and README checkpoint authority was stale. The full claim-by-claim record is in the Fresh19 validation evidence directory.

Fresh20 remediates the confirmed set as one coherent backend/infrastructure chunk. Gateway, Go config and resolved topology now share a canonical authority-only HTTPS control origin; full callback/inbound/media endpoints reject credentials, fragments and invalid ports. Gateway advertisement rejects port zero; Go correctly brackets IPv6 after default-port elision. Staging/production node registration enforces the governed authority policy without removing local-development HTTP support. Rejection identity preserves `declaredInternalUrl`. Memory and PostgreSQL route stale-version reports through atomic nonce plus rejection evidence, making an authenticated DRAINING identity terminal. Release verification rejects manifest-shaped leaf evidence, and Compose requires exactly one contract health target. A dedicated `docs/handover/AGENT_PICKUP.md` now provides replacement-agent authority, including sender-name decisions and exact next actions.

Focused Fresh20 evidence is GREEN: gateway control security **34/34**; Linux Go sender/config packages; and deployment-topology plus release-readiness **73/73**. The PostgreSQL conflict regression is authored. Full exact-source ordinary Go, race, vet/build, fresh PostgreSQL histories and 40-DSN suite, gateway TypeScript/build/retained suites, governance/OpenAPI/topology/readiness/security/SBOM/traceability, byte comparison, documentation sync, double freeze, council and adjudication remain mandatory. The candidate is currently **56 dirty paths / 0 staged paths** on HEAD `887e577565de22642cf4041a223835d122e6ff15` and branch `work/backend-production-engineering`; it is not accepted, committed, merged, pushed or deployed.

The campaign-scoped sender identity authority remains ADR-0006 plus the Campaign-Scoped Sender Identity Addendum and is not falsely claimed implemented. All hard release gates remain OPEN or BLOCKED_EXTERNAL; no live deployment, provider traffic, production credential/network mutation, scale proof, DR exercise, penetration test, operational exercise or frontend completion is claimed.

## 25 August 2026 — Fresh20 full local deterministic gate complete; pre-freeze

<!-- FRESH20_FULL_LOCAL_GATE_20260825 -->

Fresh20 remains the active uncommitted I4 remediation candidate on `work/backend-production-engineering` at HEAD `887e577565de22642cf4041a223835d122e6ff15`. The working tree remains intentionally dirty and unstaged. Do not reset, clean, stash, rebase, amend, merge, push or deploy it.

The complete current-source local deterministic matrix is now GREEN: governance **145/145**; Go 1.23.2 Linux/amd64 ordinary tests; full `-race` repository suite; `go vet`; all seven native-libpq command builds; PostgreSQL 17.10 clean, pre-0085 and pre-Meta histories through migration 0090 plus the full **40-DSN** repository suite; gateway TypeScript/build; control-plane security **34/34**; retained OpenWA crash-to-UNKNOWN, durability, outbox **9/9**, session authority **3/3**, drain/lifecycle **18/18**, fencing, serialization, pipeline-bound, retained Baileys and embedded-OpenWA checks; OpenAPI **294/294**; deployment topology/readiness; production Compose **7 Railway services / 25 secret classes**; committed-secret scan; admin and gateway production audits **0 vulnerabilities**; strict CycloneDX **30 components / 8 container images**; traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**, regenerated reproducibly. Production-candidate verification correctly exits **2** because all eight hard release gates remain OPEN or BLOCKED_EXTERNAL.

Fresh20 is **not accepted yet**. Documentation synchronization is now part of the candidate. The mandatory next boundary is: rerun affected post-document static/governance/hygiene checks, build the exact candidate-only snapshot, prove zero byte mismatch, freeze the unchanged candidate twice with identical fingerprints/packet bytes, run one genuinely blind eight-lane council, then independently adjudicate every material finding. Any confirmed defect rejects this freeze and requires RED reproduction, coherent batch remediation, full re-gate, new freeze and fresh review before acceptance.

ADR-0006 plus `docs/requirements/CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md` remain approved later-backend authority for CSI-001..CSI-014. They are deliberately **not** claimed implemented by Fresh20. The core product remains the governed campaign/audience/consent/operations platform; release-one sender-node runtime authority remains OpenWA session-backed infrastructure. Later direct Meta functionality present in source does not redefine that release-one OpenWA node model.

## 26 August 2026 — Fresh20 council remediation and mandatory re-gate

<!-- FRESH20_REMEDIATION_REGATE_20260826 -->

The first Fresh20 freeze was **REJECTED** after blind-council adjudication. Two findings were confirmed and remediated in the same candidate; two material claims were rejected from current source/test evidence. No reviewed freeze authorizes a commit after source changes.

Confirmed remediation 1: non-production governance could admit a governed HTTP OpenWA runtime authority while runtime registration canonicalization was HTTPS-only. This was reachable in the supported local Compose topology, whose gateway authority is `http://openwa-gateway:2785`. The failure was reproduced RED. Runtime registration/governance canonicalization is now mode-aware: staging/production remain canonical HTTPS + allowlist governed, while development/test may use governed HTTP/Docker-local authority. Regressions cover both a private HTTP authority and the actual Compose service authority.

Confirmed remediation 2: a boot with only authenticated `REJECTED` runtime evidence could be followed by an accepted replacement boot, then retry and reclaim node authority because retired-boot fencing counted only accepted REGISTERED/HEARTBEAT history. The reclaim was reproduced RED. Memory and PostgreSQL stores now treat prior authenticated evidence at or before the active boot's registration boundary as evidence that a differing incoming boot is older/retired, while allowing a genuinely newer replacement boot to recover after rejection. Memory and PostgreSQL regressions are GREEN.

Rejected council claims: PostgreSQL runtime events do not insert an empty UUID because `insertRuntimeEvent` assigns `id.New()` when needed; staging is already in the production-strength config validation branch and therefore requires `GATEWAY_RUNTIME_ALLOWED_HOSTS` and governed HTTPS `CONTROL_API_INTERNAL_URL`.

The full remediated acceptance matrix must be rerun before any new freeze. The first Fresh20 freeze (`574fd0822239431e97f4f6e21b02791d0d31f0841c34b949b1e1d04495c49088`) and council are historical evidence only. The next valid boundary is: full deterministic re-gate -> post-doc verification -> new exact double freeze -> fresh blind council on changed bytes using GPT-5.5, Grok 4.6 and GLM as genuine model families -> independent adjudication -> exact accepted staging/commit only if no material finding survives.

ADR-0006 and CSI-001..CSI-014 remain approved later-backend sender-identity authority and are not claimed implemented by Fresh20. The core product remains the governed campaign/audience/consent/operations platform with release-one OpenWA session-backed sender-node runtime authority.

## 27 August 2026 - Fresh20 R3 late-arrival boot remediation

<!-- FRESH20_R3_LATE_ARRIVAL_BOOT_20260827 -->

The Fresh20 R2 freeze was **REJECTED**. Its deterministic gate and double freeze were valid evidence for the reviewed bytes, but GPT-5.5 identified a reachable cross-boot ordering defect and the council did not meet the required usable-lane/model-family floor. No R2 freeze can authorize a commit after source changes.

Confirmed R2 finding: the previous retired-boot remediation fenced already-persisted authenticated evidence using control-plane `OccurredAt`, but a boot could produce a signed runtime report with `ObservedAt=T0`, have that first report delayed, allow replacement boot B to register at `T1`, then deliver boot A's still-valid first report at `T2`. Because A had no prior persisted history, the old R2 fence could accept A and reclaim node authority from B. The sequence was reproduced RED in `TestRuntimeRegistrationRejectsDelayedOlderBootFirstReportAfterReplacement`.

R3 remediation: when a different boot is active and has a registration boundary, both memory and PostgreSQL runtime stores now also treat an incoming signed report whose authenticated `ObservedAt` is at or before the active boot's `RegisteredAt` as older/retired evidence. The existing persisted-history and DRAINING fences remain. A genuinely newer boot may still recover after an initial rejection once its authenticated observation time is after the active registration boundary. Memory and PostgreSQL regressions are GREEN, including `TestPostgreSQLRejectsDelayedOlderBootFirstReportAfterReplacement`, the prior rejection-only-boot regression, and the development HTTP/Compose authority regressions.

R2 council evidence remains historical. The fresh R3 acceptance boundary is: complete full deterministic re-gate on the changed source -> post-document verification -> new exact double freeze -> new blind council using only GPT-5.5, Grok 4.6 and GLM 5.3 genuine model families -> independent adjudication -> exact accepted local commit only if no material finding survives and the reviewer evidence floor is met. Qwen and Gemini are excluded from the active reviewer fleet.

External/live release gates RG-001..RG-008 remain OPEN or BLOCKED_EXTERNAL as previously recorded; Fresh20 does not claim production deployment readiness. ADR-0006 and CSI-001..CSI-014 remain approved later-backend sender-identity authority and are not claimed implemented by this infrastructure remediation.


## 27 August 2026 - Fresh20 R4 post-R3 council remediation full re-gate

<!-- FRESH20_R4_POST_R3_COUNCIL_REGATE_20260827 -->

The Fresh20 R3 freeze is **REJECTED historical evidence only**. Independent adjudication confirmed four reachable defects in the reviewed bytes: pool-unavailable rejection conflated raw declared URL with canonical authority; malformed `gatewayPoolId` could reach the PostgreSQL UUID cast before authenticated rejection/nonce consumption; deployed Go control authority accepted reserved/local hostname classes; and retry-time `ObservedAt` was not a valid cross-boot causal fence. The Hostinger `NODE_ENV=production` finding was rejected as unreachable in the approved manifest, but the topology verifier was hardened to assert that literal explicitly.

R4 preserves the same 56-path candidate and remediates those findings in place. Runtime URL canonicalization now occurs before pool-miss rejection while raw signed declaration evidence remains separate; PostgreSQL pool lookup rejects non-canonical UUID text into the governed authenticated rejection path; staging/production control origins reject local/reserved authority classes; and gateway runtime reports now carry stable signed `bootStartedAt`, validated against process uptime and used by memory/PostgreSQL boot-origin fencing so an older process cannot reclaim authority by retrying with a newer observation timestamp. Focused memory/PostgreSQL/config regressions are GREEN, including malformed pool-ID replay/audit, raw-vs-canonical rejection evidence, reserved deployed control origins, and older-boot retry with newer `ObservedAt`. Gateway control security is **35/35 GREEN**, including stable `bootStartedAt` across heartbeats; governance discovery is **147/147 GREEN**.

Fresh R4 executable evidence on the current non-document source/config bytes is GREEN: ordinary `go test -count=1 ./...` exit 0; full `go test -race -count=1 ./...` exit 0 with `internal/platform/httpserver` 337.532s; `go vet ./...` exit 0; seven native-libpq production command builds GREEN; fresh PostgreSQL 17.10 histories are 141/139/136 public tables with migration 0090 invariant `gateway_pool_id nullable=YES` and exactly one pool-anchor check in each history; the clean 40-DSN repository matrix exits 0; changed Go files are gofmt-clean; and `git diff --check` is GREEN. Gateway authoritative lockfile verification is GREEN after fresh `npm ci` (517 packages audited, 0 vulnerabilities), retained-source sync of 56 pinned files, TypeScript typecheck, and Nest build. A separate cached-symlink dependency fixture produced broad framework type mismatches and was discarded as harness evidence after the fresh-lock reproduction passed.

Supporting R4 gates remain GREEN on the same source bytes: retained OpenWA UNKNOWN/durability/outbox/session/drain/Baileys/fencing/serialization/pipeline/embedded boundary; OpenAPI **294/294**; split deployment topology/readiness; production Compose **7 services / 25 secret classes**; committed-secret scan; admin and gateway production audits with **0 vulnerabilities**; strict local CycloneDX generation **30 components / 8 synthetically pinned container-image references** (generator proof only, not deployed-image evidence); and reproducible traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**. The production-candidate verifier still intentionally exits **2** because RG-001..RG-008 remain OPEN/BLOCKED_EXTERNAL.

No R4 freeze or council is accepted yet. The mandatory next boundary is: post-document governance/static/traceability/secret verification -> exact 2,289-file double freeze with zero byte drift -> fresh blind council on the new packet using GPT-5.5, Grok 4.6 and GLM 5.3 genuine model families with no cross-family fallback -> independent adjudication -> remediate and fully re-gate if any material finding survives; otherwise stage exact accepted paths and create one local commit. No push, deployment, live-provider action or production-readiness claim is authorized by this local gate. ADR-0006 and CSI-001..CSI-014 remain approved later-backend sender-identity authority and are not claimed implemented by Fresh20.

## 28 August 2026 - Fresh20 R5 private-authority remediation and full executable gate

<!-- FRESH20_R5_PRIVATE_AUTHORITY_REGATE_20260828 -->

Fresh20 R4 is **rejected historical evidence only**. Two independent GPT-5.5 blind-review lanes reproduced the same reachable High-severity defect in the R4 freeze: staging/production advertised control and gateway authority accepted private literal IPs (RFC1918 IPv4, IPv6 ULA and IPv4-mapped private IPv6) even though deployed cross-provider authority is intended to be DNS-only. R5 remediates that boundary consistently in Go control configuration, Go runtime registration, the OpenWA gateway TypeScript validator and resolved deployment-topology verification. Development/local network plumbing remains separate; `GATEWAY_BIND_ADDRESS` and governed development HTTP behavior are unchanged.

R5 RED-to-GREEN evidence is explicit. Regression coverage now exercises `10/8`, `172.16/12`, `192.168/16`, `fd00::/8` and IPv4-mapped private IPv6 across Go config/runtime, topology governance and gateway control-plane security. Focused Go config/sender tests are GREEN; gateway control-plane security is **35/35 GREEN**; focused topology is **52/52 GREEN**; full governance discovery is **148/148 GREEN**.

The complete R5 executable acceptance gate is GREEN on an authoritative Linux source volume copied from exactly `git ls-files --cached --others --exclude-standard` and independently verified against its 2,289-file SHA-256 manifest with zero missing, mismatched or extra files. Ordinary `go test -count=1 ./...` exits 0 (`internal/platform/httpserver` 72.703s); full `go test -race -count=1 ./...` exits 0 (`internal/platform/httpserver` 302.243s); `go vet ./...` exits 0; all seven production commands build successfully. The first repeated R5 40-DSN database run was discarded as contaminated fixture evidence because prior interrupted matrices had left fixed-ID integration state. A brand-new PostgreSQL 17.10 R5B fixture was created from scratch and the full 40-DSN repository matrix exits 0 (`internal/platform/httpserver` 31.129s). Fresh migration histories remain 141 / 139 / 136 public tables with `gateway_runtime_events.gateway_pool_id` nullable and exactly one `gateway_runtime_events_pool_anchor_check` in each.

Supporting R5 gates remain GREEN: retained OpenWA UNKNOWN/durability/outbox/session/drain/Baileys/fencing/serialization/pipeline/embedded boundary; OpenAPI **294/294**; deployment topology/readiness; production Compose **7 services / 25 secret classes**; committed-secret scan; admin and gateway production dependency audits **0 vulnerabilities**; gateway fresh-lock `npm ci` **517 packages / 0 vulnerabilities**, retained sync **56** pinned files, TypeScript typecheck and Nest build; strict local CycloneDX generation **30 components**; and reproducible traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**. The production-candidate verifier still intentionally exits **2** because RG-001..RG-008 remain OPEN/BLOCKED_EXTERNAL.

The repository remains intentionally dirty and unstaged on `work/backend-production-engineering` at HEAD `887e577565de22642cf4041a223835d122e6ff15`; the boundary is **56 dirty paths / 0 staged** and `git diff --check` is GREEN. R5 is **not yet accepted**. Mandatory next boundary: post-document governance/static/release/traceability/secret verification -> exact double-freeze -> fresh blind council using genuine GPT-5.5, Grok 4.6 and GLM 5.3 families with the same packet and no cross-family fallback -> independent adjudication -> full remediation/re-gate/re-review if any material finding survives. No push, deployment, live-provider action or production-readiness claim is authorized. ADR-0006 / CSI-001..CSI-014 remain approved later-backend sender-identity authority and are not claimed implemented by Fresh20.


## 30 August 2026 - Fresh20 R6 DNS-only authority remediation full executable acceptance

<!-- FRESH20_R6_DNS_ONLY_AUTHORITY_ACCEPTANCE_20260830 -->

Fresh20 R5 is **rejected historical evidence only**. The R5 blind council reproduced a reachable DNS-authority bypass: legacy numeric IPv4 spellings such as octal, hexadecimal, single-integer and shortened forms could be interpreted as IP addresses by runtime resolvers, and production gateway allowlists could still carry IP-like authorities beside valid DNS names. R6 closes that boundary consistently across Go configuration/runtime registration, OpenWA gateway TypeScript validation, deployment topology verification, and the associated governance/security regressions. Staging/production advertised control and gateway authorities are DNS-only; development/local bind plumbing remains separate and `GATEWAY_BIND_ADDRESS` remains listener-only.

The pre-document executable R6 candidate is 2,289 Git-visible files from `git ls-files --cached --others --exclude-standard`, aggregate SHA-256 `e7a3410cd8a7ece7355f0415eb19495ac2cbc8ad5450adc0ad1e9edfb2ced7d2`, independently verified in the Linux source volume with zero missing, mismatched or extra files. The final post-document freeze hash is recorded in external validation evidence rather than self-embedded in repository text. Fresh executable evidence is GREEN: ordinary `go test -count=1 ./...`; full `go test -race -count=1 ./...`; `go vet ./...`; all seven production commands build; a brand-new PostgreSQL 17.10 R6D three-history fixture plus full 40-DSN repository matrix; governance discovery **149/149**; OpenAPI **294/294**; deployment topology/readiness; production Compose **7 services / 25 secret classes**; committed-secret scan; gateway fresh-lock `npm ci` **517 packages / 0 vulnerabilities**, TypeScript typecheck and Nest build; gateway control security **36/36**; retained OpenWA UNKNOWN/durability/outbox/session/drain/Baileys/fencing/serialization/pipeline/embedded boundary; admin production audit **0 vulnerabilities**; strict CycloneDX generation **30 components**; and traceability **398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**. The previously suspicious dispatch race test passed 20/20 focused under `-race` and the full race suite is GREEN.

The production-candidate verifier still intentionally exits **2** because RG-001..RG-008 remain OPEN/BLOCKED_EXTERNAL; this local acceptance does **not** claim production deployment readiness. The mandatory next boundary is exact double-freeze -> fresh blind council across GPT-5.5, Grok 4.6 and GLM 5.3 families -> independent adjudication -> remediate and fully re-gate if any material finding survives; otherwise stage the exact accepted tranche and create one local commit. No push, deployment, live-provider action or production-readiness claim is authorized by this local gate. ADR-0006 / CSI-001..CSI-014 remain approved later-backend sender-identity authority and are not claimed implemented by Fresh20.


## 30 August 2026 - Fresh20 R7 post-council remediation acceptance

<!-- FRESH20_R7_POST_COUNCIL_REMEDIATION_20260830 -->

Fresh20 R6 is rejected historical evidence and must not be committed as accepted. The paid blind council found a reachable Go panic in runtime authority canonicalization: malformed URL input could make `url.Parse` return an error with a nil URL while `parsed.Scheme` was dereferenced first. A focused regression reproduced the nil-pointer panic, the implementation now rejects `err != nil || parsed == nil` before dereference, and the regression is GREEN. The same council identified a case-sensitive `MEDIA_DOWNLOAD_SECRET` placeholder check; focused production-config tests reproduced acceptance of `Development-...` / `DEVELOPMENT-...`, and R7 now lowercases once before checking both `development-` and `change-me`.

Two other council claims were independently adjudicated and rejected. Node 22 WHATWG URL parsing canonicalizes the cited legacy numeric host forms (`0177.0.0.1`, `0x7f.0.0.1`, `010.020.030.050`, `0x0a.0x14.0x1e.0x28`, `167772161`, `10.1`) to canonical IP literals, so the existing TypeScript fallback classifies them as IP-like; the alleged TS bypass did not reproduce. Authenticated rejected DRAINING declarations intentionally make that boot ID terminal: dedicated memory and PostgreSQL regression tests require a new boot ID before READY can re-enter, preserving fail-closed drain/fencing semantics; the Grok alternative policy was therefore not accepted as a defect.

The pre-document executable R7 candidate is 2,289 Git-visible files, aggregate SHA-256 `f1db641058bbb233261a936e1221a35a672f9ca20a4b32cd647995bee24e8643`, independently mirrored into a read-only verifier volume with zero missing, mismatched or extra files. Fresh R7 executable evidence is GREEN: ordinary `go test -count=1 ./...`; full `go test -race -count=1 ./...`; `go vet ./...`; all seven production command builds; fresh PostgreSQL 17.10 three-history fixture plus full 40-DSN matrix; governance 149/149; OpenAPI 294/294; deployment topology/readiness; production Compose; committed-secret scan; gateway fresh-lock `npm ci` / audit / typecheck / build with 0 vulnerabilities; gateway security 36/36; retained OpenWA UNKNOWN/durability/outbox/session/drain/Baileys/fencing/serialization/pipeline/embedded boundary; admin audit 0 vulnerabilities; strict CycloneDX SBOM 30 components; traceability 398 = 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL.

The production-candidate verifier still intentionally exits 2 because RG-001..RG-008 remain OPEN/BLOCKED_EXTERNAL. R7 is a local acceptance candidate only; it does not claim production deployment readiness. The next mandatory boundary is final post-document fingerprint/double-freeze, fresh blind re-review of the remediated bytes, independent adjudication of any material finding, then exact staging and one local commit if the review floor clears. No push, deployment or live-provider mutation is authorized.
