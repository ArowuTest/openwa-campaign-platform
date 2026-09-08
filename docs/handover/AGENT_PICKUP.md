# OpenWA production-engineering agent pickup

Last updated: 24 August 2026, Fresh20 implementation in progress

This is the first document a replacement agent should read. It consolidates the repository state, the user's operating instructions, the accepted design authority, the council workflow, the current engineering chunk, verification evidence, and the exact next actions. It must be updated at every accepted or rejected council boundary.

## 1. Exact working state

| Item | Current authority |
| --- | --- |
| Repository | `C:\Users\sanus\OpenWA\campaign-platform-active\repo` |
| Validation/evidence workspace | `C:\Users\sanus\OpenWA\campaign-platform-active\validation` |
| Engineering branch | `work/backend-production-engineering` |
| Accepted committed HEAD | `887e577565de22642cf4041a223835d122e6ff15` |
| Active uncommitted candidate | I4 Fresh20, built on the rejected Fresh19 candidate |
| Staged paths | 0 â€” nothing may be staged before Fresh20 council acceptance |
| Dirty paths after adding this pickup document | 56 expected; confirm with `git status --short` |
| Main branch | Not the active branch; no merge or direct commit to `main` is authorized |
| Remote state | No push is authorized or claimed |
| Deployment state | No live Railway/Hostinger deployment or production mutation is authorized or claimed |

Do not use `git reset`, `git clean`, `git stash`, `git checkout --`, rebase, amend, force-push, or broad file deletion. The dirty tree is an intentional, cumulative engineering candidate, not debris. Preserve every path unless a source-backed adjudication specifically changes it.

## 2. User instructions and working agreement

The user wants the backend completed to a best-in-class production standard, followed by infrastructure and then frontend UI. Work directly in this local repository so it remains the live authority. Use sizeable engineering chunks; do not stop for review at every file change and do not convene a council after a small patch set.

### V2B engineering methodology authority

This project does **not** depend on a special `openwa-continuous-engineering` skill. That prior session-level label is obsolete for project continuation. Replacement agents must use the governed **V2B skills and engineering capabilities directly**:

- discover the live V2B registry with `list_skills` and load task-relevant skills with `read_skill`;
- use `agent:tdd-workflow` for feature/bugfix/refactor RED -> GREEN work;
- use `agent:superpowers-systematic-debugging` for failures and unexpected behavior before proposing fixes;
- use `agent:superpowers-verification-before-completion` before any acceptance, completion, commit or delivery claim;
- load additional V2B skills such as backend, API, security, testing or deployment methods when the active tranche requires them;
- use V2B missions, evidence, checkpoints, Git, database, workers, browser and runtime operations as the governed execution/control plane.

A project is not a skill. Repository/runtime evidence and the durable project handover remain authoritative; V2B skills provide the task methodology. Never block continuation because an OpenWA-specific skill name is unavailable.

The established acceptance workflow is:

1. Collect a coherent set of related defects or requirements into one sizeable engineering chunk.
2. Implement test-first and run focused checks while developing.
3. Run the complete exact-source validation matrix on a clean disposable Linux copy of the candidate.
4. Synchronize all living handovers, roadmaps, risk records, and this pickup document.
5. Recreate the exact candidate snapshot and prove candidate-to-snapshot byte identity.
6. Freeze the review packet and source/config fingerprint twice; both runs must match.
7. Run one genuinely blind eight-lane council on the complete frozen chunk.
8. Read every review in full and independently reproduce or disprove every claim.
9. If any material defect is confirmed, reject the candidate, leave the index empty, record adjudication, and roll all confirmed issues into the next sizeable chunk.
10. Only after an accepted council may the exact reviewed candidate be staged, inspected, and committed once on `work/backend-production-engineering`.

A local acceptance commit on the engineering branch is part of this workflow. Merging to `main`, pushing, deploying, changing production credentials/networking, or exercising live providers requires separate explicit authority.

## 3. Repository and deployment model

The backend is a governed campaign messaging platform:

- PostgreSQL is authoritative state; Redis accelerates execution but is never the sole source of recipient or delivery obligations.
- Seven Go control-plane/API/worker services are intended for Railway.
- OpenWA gateways are isolated on Hostinger VPS and use either BAILEYS or WHATSAPP_WEB_JS behind the governed adapter boundary.
- Meta Cloud API is a direct sibling transport, not routed through OpenWA.
- S3-compatible object storage holds import files, consent evidence, campaign media, and generated reports.
- The local Docker environment is a recoverable development/integration test topology. It is not the deployed Railway topology and is not production certification.
- Railway deployment is container-based through the service Dockerfiles/contracts. Hostinger has a separate gateway Compose topology. The existence of local Docker does not mean Railway is running the local Compose stack.

The topology authority is spread across:

- `config/deployment-topology.json`
- `infrastructure/railway/service-contracts.json`
- `infrastructure/compose/compose.production.yaml`
- `infrastructure/compose/compose.hostinger-openwa-gateway.yaml`
- `scripts/verify-deployment-topology.py`
- `scripts/verify-production-compose.py`
- `scripts/verify-deployment-readiness.py`
- `scripts/verify-release-readiness.py`

## 4. Non-negotiable runtime invariants

The current candidate is designed around the following invariants:

- A gateway runtime report is HMAC-authenticated, timestamp-bounded, nonce-protected, node-path-bound, and stored transactionally.
- Runtime identity includes governed gateway pool, provider, engine, adapter version, internal URL, boot ID, configuration/build versions, capabilities, capacity, session/queue/resource observations, and a positive per-boot `runtimeSequence`.
- The gateway URL is a canonical HTTPS authority. Production DNS hosts must be present in `GATEWAY_RUNTIME_ALLOWED_HOSTS`; literal IPs must be approved private addresses; Docker-local, loopback, link-local, and control-plane aliases are forbidden.
- The Railway control origin and the Hostinger gateway authority are separate trust zones. `CONTROL_API_INTERNAL_URL` is an authority-only HTTPS origin used to derive the runtime registration endpoint. Callback, inbound, and media URLs are full Railway endpoints on the same governed control hostname.
- Governed runtime authority is checked both before store entry and again inside the memory mutex or PostgreSQL `FOR UPDATE` transaction.
- Runtime causality is sequence-based, not wall-clock ordered. `observedAt` is freshness evidence only.
- A boot that has declared DRAINING cannot publish READY again. Authenticated rejected DRAINING declarations are also terminal for that boot; recovery requires a new process boot ID.
- A production gateway cannot reuse configured `GATEWAY_BOOT_ID`.
- `GATEWAY_CAPACITY` and `OPENWA_MAX_CONCURRENT_SESSIONS` are explicit, bounded 1..1000, and equal in production.
- Rejected authenticated reports consume their nonce atomically with immutable rejection evidence. Persistence failure rolls back nonce use.
- Release-gate evidence is gate-local, nonempty, byte-hash-bound, candidate-bound, kind-governed, timestamped, and maker-checker separated. Development structural validation must not pretend external gates are closed.

## 5. Sender name and sender identity authority

The recent sender-name discussion is captured and remains authoritative even though its later backend tranche is not yet implemented:

- `docs/decisions/ADR-0006-campaign-scoped-sender-profile-identity.md`
- `docs/requirements/CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`

The core decision is:

- The reusable underlying sender account/number/session identity is not the same thing as the campaign's sender profile identity.
- A campaign takes an exclusive, governed reservation of a sender resource for its active execution window.
- Requested campaign sender/profile display identity and provider-observed identity are separate fields with evidence and reconciliation; neither overwrites the other silently.
- Human-readable profile display names must not be conflated with regulated alphanumeric sender IDs or with the reusable account identity.
- Approval, effective dating, conflicts, audit, and provider observation belong at campaign scope.

Do not mark this feature complete merely because the ADR/addendum exists. Traceability must change only when the later implementation and tests land.

## 6. What was completed before Fresh20

The accepted commit is older than the intentional I4 candidate. The candidate has accumulated a large production-hardening tranche covering deployment contracts, release governance, runtime registration, PostgreSQL rejection evidence, capacity/boot/drain lifecycle, topology validation, SBOM generation, secret handling, documentation, and runbooks.

Fresh18 was fully gated but rejected by council. Six reproduced defects were then fixed in Fresh19:

1. canonical HTTPS gateway authority rather than credentials/path/query/fragment-bearing URLs;
2. explicit production capacity coupled to the OpenWA engine session limit;
3. per-item gate-local SHA-256 release evidence binding;
4. terminality for rejected authenticated DRAINING declarations;
5. governed runtime authority recheck inside the serialized store boundary;
6. prohibition of configured reusable production boot IDs.

Fresh19 then passed the full local matrix on its exact executable source:

- governance 141/141;
- ordinary Go tests, full race, vet, and seven native-libpq command builds;
- PostgreSQL 17.10 current/pre-0085/pre-metadata histories through migration 0090 and the complete 40-DSN native-libpq suite;
- gateway typecheck/build, control security 32/32, and 38 retained durability/lifecycle tests;
- OpenAPI 294/294;
- production Compose seven services / 25 secret classes;
- resolved Hostinger BAILEYS and WHATSAPP_WEB_JS boundaries;
- secret scan and Node audits with zero vulnerabilities;
- strict CycloneDX 30 components / eight container images;
- traceability 398 rows: 168 IMPLEMENTED_TESTED, 181 PARTIAL, 32 NOT_STARTED, 17 BLOCKED_EXTERNAL;
- expected production-candidate exit 2 because all eight hard release gates remain open or externally blocked.

Fresh19 freeze authority:

- exact snapshot `openwa-i4-fresh19-source-20260824d`;
- 2,288 candidate files, zero byte mismatches;
- review packet SHA-256 `f15eea39b2c77632c58ef98fc3e3bc10b201cbfa87df243486664f5fcf8ae9a7`;
- source/config SHA-256 `600d41375b41cfc7fc534dfc85d8f866348f780cba6d92d985fe7deefbf0cf95`;
- double-freeze equality confirmed.

Fresh19 council completed 8/8 usable lanes across three actual model families with three transparently labelled GLM fallbacks. Two lanes labelled PASS and six labelled FAIL. Labels were not treated as authority. The full adjudication is:

`C:\Users\sanus\OpenWA\campaign-platform-active\validation\ai-review\infrastructure-i4-fresh19-20260824\I4-fresh19-adjudication.md`

Fresh19 was rejected and never staged or committed.

## 7. Fresh20 confirmed remediation chunk

Fresh20 consolidates every reproduced Fresh19 defect; it is not a short patch review boundary.

Implemented source/test changes so far:

1. `CONTROL_API_INTERNAL_URL` is a canonical authority-only HTTPS origin in the gateway, shared Go config, and resolved topology verifier. Gateway registration derives its URL from the canonical origin rather than raw concatenation.
2. Callback, inbound, and media endpoint validation rejects embedded credentials, fragments, and invalid/zero ports while preserving their required endpoint paths and common Railway hostname.
3. TypeScript gateway advertisement rejects port zero and empty/invalid authority ports.
4. Go canonicalization brackets IPv6 correctly when explicit default HTTPS port 443 is elided.
5. Deployed staging/production node registration enforces the same governed canonical runtime authority and allowlist/forbidden-host policy. Development retains HTTP/Docker-local support for the local test topology.
6. Signed rejection evidence preserves `declaredInternalUrl` separately from canonical `internalUrl`.
7. Memory and PostgreSQL stale-version reports now enter the transactional rejection path. Their nonce and rejection event commit atomically; a DRAINING identity in that event makes the boot terminal.
8. Release evidence rejects a sibling manifest-shaped file as leaf observed evidence. The council's exact mutually-correct-hash cycle was disproved as requiring a cryptographic fixed point; the simpler constructible sibling-manifest laundering path was reproduced and fixed.
9. Production Compose healthcheck verification requires exactly one loopback service-contract target inside the healthcheck block, defeating an intra-directive decoy.
10. New regressions cover all of the above, including PostgreSQL version-conflict DRAINING behavior.

Focused Fresh20 evidence currently green:

- gateway control-plane security: 34/34;
- Linux Go `internal/sender`, `internal/shared/config`, and `cmd/control-api` packages;
- deployment topology + release readiness: 73/73 tests in Python 3.12 Linux;
- PostgreSQL-specific new test is authored but the full fresh-PostgreSQL matrix has not yet been rerun;
- gateway TypeScript/build and the full Go/control-api/full-race matrices remain pending for the final exact snapshot.

Do not describe Fresh20 as accepted or fully green until the remaining full matrix, documentation sync, double freeze, council, and adjudication complete.

## 8. Release-gate truth

All eight hard production gates remain honestly open or externally blocked:

| Gate | State | Meaning |
| --- | --- | --- |
| RG-001 | OPEN | Complete Must-requirement evidence is not yet closed |
| RG-002 | BLOCKED_EXTERNAL | Live Railway/Hostinger topology and TLS observation require target access |
| RG-003 | BLOCKED_EXTERNAL | Live provider validation requires credentials/provider traffic |
| RG-004 | OPEN | Capacity/soak proof is not yet executed |
| RG-005 | OPEN | Independent security assessment/penetration evidence is not complete |
| RG-006 | BLOCKED_EXTERNAL | Backup/restore and disaster-recovery rehearsal needs deployed infrastructure |
| RG-007 | OPEN | Operational exercise/ownership evidence is not complete |
| RG-008 | OPEN | Frontend/UI and end-to-end product acceptance are not complete |

`scripts/verify-release-readiness.py --production-candidate` must therefore exit 2. Exit 0 would be a false production-readiness claim unless real, bound evidence has closed every hard gate.

## 9. Validation environment and evidence locations

Windows host tools are not the final authority for Go/Python because this repository pins Linux/native-libpq behavior and local `go`/`python3` availability differs. Use cached Docker images and disposable source volumes. Never treat a bind-mounted dependency tree as final exact-source evidence.

Fresh19 evidence root:

`C:\Users\sanus\OpenWA\campaign-platform-active\validation\ai-review\infrastructure-i4-fresh19-20260824`

Important files there:

- `I4-fresh19-gate-summary.txt`
- `I4-fresh19-summary.json`
- `I4-fresh19-adjudication.md`
- `blind-i4-fresh19.md` and `blind-i4-fresh19-run1.md`
- `I4_FINGERPRINT_CURRENT.txt` and `I4_FINGERPRINT_RUN1.txt`
- `I4_SOURCE_CONFIG_FINGERPRINT.txt` and `I4_SOURCE_CONFIG_FINGERPRINT_RUN1.txt`
- all eight `*-review.md` files plus raw response metadata.

Fresh19 PostgreSQL evidence:

- `C:\Users\sanus\OpenWA\campaign-platform-active\validation\run_i4_fresh19_pg.py`
- `C:\Users\sanus\OpenWA\campaign-platform-active\validation\i4-fresh19-pg40.status`
- `C:\Users\sanus\OpenWA\campaign-platform-active\validation\i4-fresh19-pg40-full.log`
- migration history logs in the validation directory.

The Fresh19 PostgreSQL matrix ran against snapshot `openwa-i4-fresh19-source-20260824c`; the later `d` snapshot changed documentation/evidence only, not executable source. Preserve that distinction rather than claiming PostgreSQL ran on `d`.

## 10. Exact next actions

1. Run `git diff --check`, inspect every Fresh20 diff, and correct any test or implementation issue.
2. Keep `README.md` and the living documents synchronized with the Fresh20 adjudicated boundary.
3. Maintain the Fresh19 rejection and Fresh20 remediation boundary consistently in:
   - `PROJECT_STATUS_HANDOVER.md`
   - `docs/BUILD_CHECKPOINT.md`
   - `docs/handover/CURRENT_STATUS.md`
   - `docs/handover/NEXT_ACTIONS.md`
   - `docs/program/BACKEND_ADVERSARIAL_FINDINGS.md`
   - `docs/program/MASTER_IMPLEMENTATION_ROADMAP.md`
   - `docs/program/PRODUCTION_READINESS_CHECKLIST.md`
   - `docs/program/RISK_AND_TECHNICAL_DEBT_REGISTER.md`
4. Create a new exact Fresh20 source snapshot from `git ls-files --cached --others --exclude-standard`; compare every file byte-for-byte and record the true file count.
5. Run the complete final matrix: governance, ordinary Go, full race, vet, all command builds, fresh PostgreSQL histories and 40 DSNs, gateway typecheck/build and retained suites, OpenAPI, Compose/topology/readiness/release, secret scan, audits, SBOM, traceability, diff hygiene, and expected production-candidate exit 2.
6. Recreate the exact snapshot after final documentation changes and repeat every affected static gate. Do not claim an executable gate ran on a later docs-only snapshot without stating the distinction.
7. Freeze the complete Fresh20 packet twice and require identical packet and source/config hashes.
8. Run one eight-lane blind council only after the whole sizeable Fresh20 chunk is frozen.
9. Adjudicate all claims. If accepted, confirm the exact reviewed dirty path set, stage only it, inspect `git diff --cached`, and create one local commit on `work/backend-production-engineering`. Do not merge or push.

## 11. Useful read order

After this file, read:

1. `PROJECT_STATUS_HANDOVER.md`
2. `docs/handover/CURRENT_STATUS.md`
3. `docs/handover/NEXT_ACTIONS.md`
4. `docs/program/MASTER_IMPLEMENTATION_ROADMAP.md`
5. `docs/program/PRODUCTION_READINESS_CHECKLIST.md`
6. `docs/program/RISK_AND_TECHNICAL_DEBT_REGISTER.md`
7. `docs/program/BACKEND_ADVERSARIAL_FINDINGS.md`
8. ADR-0006 and the campaign-scoped sender identity addendum
9. Fresh19 gate summary, all eight reviews, and adjudication in the validation workspace

If any document conflicts with the exact Git state or executable verification output, the source and reproducible evidence win; update the conflicting document before the next freeze.

## 25 August 2026 â€” Fresh20 full local deterministic gate complete; pre-freeze

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
