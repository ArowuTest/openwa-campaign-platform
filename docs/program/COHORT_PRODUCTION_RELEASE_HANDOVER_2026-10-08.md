# OpenWA production release handover — 8 October 2026

## Current release

The bounded audience import/cohort/estimate/saved-segment/snapshot tranche is source accepted and deployed. Accepted runtime source is `6d445cf0d5095b006622ef06e2ba251157bbbbd1` (`6d445cf`). It is pushed to GitHub branch `work/backend-production-engineering`. All eight Railway application services are SUCCESS on that exact deployment commit. The authenticated live operator smoke has nine passing checks, zero failures and one explicit governed-fixture blocker.

This is a bounded release, not completion of the 67-screen/eight-journey internal operator platform. Third-party HTML remains an incomplete design reference. The SRS, UX specification, approved later scope decisions and [implementation matrix](UI_UX_IMPLEMENTATION_MATRIX.md) govern remaining work.

The documentation follow-up containing this handover changes no application source. Production stays pinned to accepted runtime source `6d445cf`; do not infer a runtime upgrade from a later documentation commit.

## Repository and continuation

| Item | Value |
|---|---|
| External repository | https://github.com/ArowuTest/openwa-campaign-platform |
| Push remote and branch | `github`, `work/backend-production-engineering`; `origin` is a local bundle |
| Canonical checkout | `C:\Users\sanus\OpenWA\campaign-platform-active\repo` |
| Active release worktree | `C:\Users\sanus\OpenWA\campaign-platform-active\repo\.agent\worktrees\large-audience-ingestion-20261006` |
| Active branch | `feat/large-audience-ingestion-20261006` |
| FS V2B New root | `fs`; supply explicit relative cwd after a tool-state/thread reset |
| Accepted code/contracts/tests aggregate | `1d695ccec5548deb1659889c34c10840502c82ce588ddf0b76548c735dd438b7` (63 files) |
| Frozen accepted manifest | `.agent/review/candidate-manifest-20261007.json`, 66 files, SHA-256 `ebfb7ecec374058e19108bb7697bfc68d420a902df703f6c130c5373bb96ce96` |
| Railway project / environment | `8633a0b9-3b15-4c8d-b6f2-0306b284f4dd` / `546fd711-cf80-4402-89b0-cb3ae5671fa9` |

Use the latest active and canonical FS checkpoint/project context for the documentation HEAD and continuation state. Accepted runtime source and documentation HEAD are distinct. Preserve unrelated edits; do not reset, clean, stash or rebase existing work to resume this task.

The user explicitly authorized commits, GitHub push, production database changes, Railway deployment and Hostinger implementation. Completed actions do not require repeat approval. Never retry an acknowledged production SQL apply or repeat a successful deployment solely because a thread resumed.

## Production database — applied once

Migrations 0095 and 0096 plus corrected bootstrap 002 were applied atomically to PostgreSQL database `railway`, cluster system identifier `7692162600126554174`, at **2026-10-07T22:23:00.5171334Z**. Session `exec-muyoa97m-de431241` exited 0 with PostgreSQL acknowledgement. **Do not replay the upgrade or rotate the service login roles.**

Independent read-only postconditions passed: 62 exact columns, 49 enforced constraints, 13 valid indexes, snapshot immutability trigger/function, control-role CRUD and exact effective audience-worker column privileges. The audience role reads both upload tables; ten session lifecycle/result columns and parts state are writable for the finaliser's existing locks. No broad write privilege was added.

Before production, a disposable old-0094 baseline rehearsed the exact bundle: forced failure rolled back objects/ACLs/role flags; successful apply and application-role proof passed; replay was refused. Production bundle SHA-256 is `8f56044d0fd3c2ad3d24bf9c9fa99ad65ff74eaccb456cc5e85cee22e07c8b9f`.

| Durable receipt | SHA-256 |
|---|---|
| `.agent/review/production-cohort-upgrade-receipt-20261007.json` | `7b85714f17d364c4bd7b2d972d83c55963fabdcffa7f7ff9a3a74b883a1ef6aa` |
| `.agent/review/production-post-upgrade-readonly-receipt-20261007.json` | `1cda2e5d7f413789f6dfdee2463c820d8e3b1ab9a7ecf4aee6d62b4c73295a85` |

Recovery evidence at 21:36 UTC showed six valid matching-cluster backups, fresh archived WAL and zero archive failures. An executed restore/recovery rehearsal remains unproven.

## Railway deployment — complete

Live admin: [Operator portal](https://admin-web-production-016f.up.railway.app)

Live API: [Control API](https://control-api-production-22c0.up.railway.app)

Every deployment below has actual `meta.commitHash == 6d445cf0d5095b006622ef06e2ba251157bbbbd1`. Fresh read-only environment reconciliation found no pending work or staged patch and one running, zero crashed replica per application service. Existing domains target port 8080.

| Service | Actual deployment ID | Result |
|---|---|---|
| control-api | `827ff6ca-d788-4da7-ab59-9b0a80da643a` | SUCCESS |
| audience-worker | `3e67a090-91b4-4373-bdce-b373b35c039a` | SUCCESS |
| campaign-worker | `305da902-b9d6-48d7-bd6a-47f1d016d3fd` | SUCCESS |
| export-worker | `25049d17-9b39-4e95-994a-2b36f3d80d99` | SUCCESS |
| inbound-governance-worker | `18cb3611-ac5e-4384-91b6-a52c180afa90` | SUCCESS |
| metrics-worker | `4b8779f8-432e-4328-a5a2-76c6eac03d0f` | SUCCESS |
| platform-governance-worker | `e7ca73cd-71e9-4042-b999-b475c085796d` | SUCCESS |
| admin-web | `5ee7a15e-8741-4fb1-b6cd-9cb364d0aa49` | SUCCESS |

The first new audience-worker deployment `9030d194-6dbf-4424-b51e-6e40535a096d` failed because the required private `CLAMAV_ADDRESS` was absent. A single private scanner reference was added, and replacement `3e67a090-91b4-4373-bdce-b373b35c039a` is SUCCESS on unchanged accepted source. The historical failed deployment remains visible in Railway's recent-failure count; it is not the current runtime.

Only the current operator IPv4 /32 was previously appended to the admitted-network list; trusted proxy prefixes and other admitted networks were preserved. Direct and proxied actual `/api/v1/auth/me` semantics are 401 AUTHENTICATION_REQUIRED anonymously, with health 200.

Deployment verification receipt: `.agent/review/railway-matched-release-verification-20261008.json`, SHA-256 `b76fec42b1e2f80897d4dd470dfac74ab03d4eacf463105e36f600512329915c`. Railway metadata does not expose runtime image digests. The recorded local frontend Docker image is local build evidence, not a Railway runtime digest.

## Hostinger gateway — deployed, registration evidence

VPS `2032594`, `srv2032594.hstgr.cloud`, `187.7.31.238`, Ubuntu 24.04/KVM 2. Gateway https://gateway.sendam.tech uses the existing `openwa-hostinger-gateway-node` Docker project and `openwa-hostinger-gateway-node-openwa-gateway-1` container.

Accepted live image is `openwa-gateway@sha256:919732a036b9d2d6e9ec3c66b93f15f723ae7f778837732e6aa14eb3dd049c46`, with revision label `6d445cf`. The clean accepted release checkout is `/opt/openwa/releases/6d445cf0d5095b006622ef06e2ba251157bbbbd1/source`. The older `/opt/openwa/repo` checkout contains two local Go edits and was preserved.

The one-service image change preserved both persistent volumes, four read-only secret mounts, private bind `172.17.0.1:2785`, compose configuration and environment except the image reference. Fresh authenticated read-only console inspection confirms Docker healthy and normal-TLS public health 200. No pairing or sends occurred.

The raw VPS deployment receipt records restart requested **2026-10-07T22:31:11.708708+00:00**, Docker start **22:31:12.214817523Z**, and healthy verification **22:31:22.567850 UTC**. A prior human-facing report incorrectly gave 23:31. PostgreSQL independently records the new boot at 22:31:12.320 UTC and registration at 22:31:12.994301 UTC. The original 16/19 verification failure against the incorrect 23:31 lower bound is retained, not erased.

Current boot is `682f3f9f-be57-4760-8e66-fad2a6cb1054`, replacing `4a3801ca-8368-4f04-ab13-8be8ac1f6134`. Node `379d3154-3529-4bc6-b932-a7ce14151d10` and pool `c0d2b686-64f1-4e55-a6f6-c8d1dec46f9a` retain versions 1 and 3. READY runtime, ACTIVE pool, no draining and fresh heartbeat were verified read-only.

Fresh corrected read-only verification used the exact original receipt's restart lower bound and passed **19/19 checks**, session `exec-muywhiwa-078e95b3`, exit 0, at **2026-10-08T02:11:38.190608Z**. Heartbeat age was 24.901 seconds. Corrected receipt: `.agent/review/gateway-corrected-registration-20261008.json`, SHA-256 `9c1ffab0ba5204b9008399e99b2f7372e31b5b285be31b79d6a4bec85d323058`. No restart or deployment was repeated to reconcile the timestamp.

Raw deployment receipt: `.agent/review/hostinger-gateway-raw-deployment-receipt-20261008.json`, SHA-256 `155d373fe41f5bedb30593a11979028faf4ab56f41c0049de35c7d107a8fd2f3`. Original failed verifier: `.agent/review/gateway-final-registration-20261008.json`, SHA-256 `3b75c65a816121dd90191221ce2a9eef248b030f0db7fe0314cb48991a1b49cf`.

## Live operator smoke — nine PASS, zero failures

Session `exec-muyw5nkm-e7d6faff` exited 0 at **2026-10-08T02:02:36Z**, observed 111 requests, recorded no blocked requests or page errors, and left accepted implementation source unchanged.

1. Frontend health 200 and semantic anonymous authentication 401.
2. Real browser login and MFA, returning to the imports workspace.
3. Secure HttpOnly Strict session cookie, Secure Strict CSRF cookie and authoritative platform permissions.
4. Missing-CSRF validation rejected with 403 before business mutation.
5. Real bounded organisation/filter/country envelopes.
6. Imports navigation and disabled empty-source upload.
7. Builder rule/group add/edit/remove and real server validation, 200/valid.
8. Mobile navigation and containment at 390 x 844.
9. Logout 204 and isolated session revocation 401.

Cleanup logged out and closed the browser/context. No credentials, MFA seed, cookies or raw authentication responses were persisted.

Receipt: `.agent/qa/openwa-live-operator-smoke-20261007-81ea2302-e4a1-4167-b392-d2444b9f6d64.json`, SHA-256 `751cc3c2b0d9ab052a2ed41108ae0ce779b586f247f4a734baa22e6fbd6d085c`.

Production inventory has zero organisations. No approved synthetic fixture was selected; no consent review or independent approval was fabricated. Positive upload/estimate/save/reopen/snapshot publication remains untested against governed production business data. A lawful QA organisation/purpose/policy/review and independently authorised approver are needed for that lane.

## Source acceptance and its limits

Recorded source gates include 84 frontend tests/typecheck/lint/build; 28 bounded local desktop/mobile controls; materialisation PostgreSQL ordinary/race 75 results each with no skips; full Linux ordinary/race 1,418 results in 49 packages each plus vet/build, with 142 explicit unrelated environment-gated skips per broad run; 208 governance checks; 306 OpenAPI routes; upload-role importer ordinary/race 97 results each with 13 unrelated skips; schema 17 and new actual-role cases without skips; gateway 104 tests/build/typecheck/audit.

Count-kernel and upload-role external reviews approved; complete independent materialisation peer review and root execution/adjudication closed blocking source findings. Full external councils timed out or returned partial output without verdicts; those are not approvals. See [the dated source acceptance history](COHORT_SNAPSHOT_ACCEPTANCE_STATUS_2026-10-07.md) for exact provenance.

The nine smoke checks do not accept every button, full keyboard/WCAG behavior, every role/state, all eight journeys, real provider traffic, production capacity or executed backup restore. Backend/infra still have unfinished governed functionality and certification work.

## Next implementation and approved operating scope

Execute [campaign preparation foundations](../superpowers/plans/2026-10-07-campaign-preparation-foundations.md) in a new isolated worktree, with independent acceptance per unit:

1. Direct campaign detail GET and client loading: replace the bounded 20-page/2,000-row list scan with authoritative GET by ID.
2. Governed durable DRAFT editing/save/reload: expectedVersion conflict protection, atomic material-change event, current saved organisation/purpose/review binding and preserved authoritative transport identity on unrelated edits.
3. Strictly read-only saved-campaign readiness: advisory checks with safe blockers and current evidence; no reservations, transitions, dispatch or hidden writes. Do not reuse mutating workspace Get/Plan as read-only readiness.

Then complete campaign journey D: immutable snapshot selection, composer/media/version/preview, compatible approved test-recipient/session selectors, controlled exact-route test, capacity hold, independent approvals and declared-timezone/typed-MFA schedule. Complete sender pairing and governed pool setup UI alongside this usable sending path. Contact/suppression, inbound/STOP, approvals, controlled exports, administration and operational journeys remain tracked in the matrix.

Preserve approved scope: authenticated in-house operators; offline client/order vetting; Railway authoritative control/data and Hostinger gateway/session runtime; Meta sibling provider; multiple READY sender numbers within one engine-specific pool per campaign. Mixed-engine/multi-pool campaigns, automatic fallback/reallocation and campaign-scoped mutable sender profile identity remain deferred. UNKNOWN remains sticky and evidence-led, with no blind resend.

Provider UAT still needs approved paired sender/test recipient and governed campaign evidence for text/media, delivery/read, inbound reply, reconnect and safe UNKNOWN handling. Full scale, monitoring, security, rollback and recovery certification remain separate open evidence work.
