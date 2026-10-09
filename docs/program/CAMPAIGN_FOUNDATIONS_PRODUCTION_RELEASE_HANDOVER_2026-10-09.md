# Campaign foundations, production release handover — 9 October 2026

The accepted campaign-foundation source is now deployed to the live Railway production environment. The release source is bfb4bee1d03105e14e97577d279e5bc5632266fe with tree 62dbb4293fb61169d71d0f6f1c570cbbbbfa76f0. All eight OpenWA applications have been reconciled against that source, with one running replica and a successful deployment for each service. The 8 October handover remains the immutable record of the earlier cancelled deployment attempt; this document records the later successful release and its evidence boundary.

This handover is a release checkpoint, not a claim that the complete 67-screen operator product or provider UAT is finished. The positive campaign path remains gated until a lawful production organisation and governed campaign fixture are available.

## Release identity and evidence

| Item | Accepted evidence |
|---|---|
| Accepted product commit / tree | bfb4bee1d03105e14e97577d279e5bc5632266fe / 62dbb4293fb61169d71d0f6f1c570cbbbbfa76f0 |
| Production repository and branch | ArowuTest/openwa-campaign-platform / work/backend-production-engineering |
| Railway project / environment | 8633a0b9-3b15-4c8d-b6f2-0306b284f4dd / 546fd711-cf80-4402-89b0-cb3ae5671fa9 |
| Backend source patch | 62d96884-6fc8-400b-8e2b-99b3e5659bd5, accepted and triggered through the normal Railway release path |
| Admin source patch | 97ed185a-f321-440b-beae-0f500b674883, accepted and triggered through the normal Railway release path |
| Backend deployment workflow | commitChanges/546fd711-cf80-4402-89b0-cb3ae5671fa9/62d96884-6fc8-400b-8e2b-99b3e5659bd5 |
| Admin deployment | b3258b67-be1e-417d-afeb-01d691550fac |
| Final all-eight reconciliation | .agent/sdd/campaign-preparation-20261008/deployment-evidence-reconcile-3066657d892e402f99c67bbf440f0a14.json |
| Final reconciliation SHA-256 | 614aeeaf608b13a021792366057d857bfb052fc7df7124445160f9751deebfbf |
| Final reconciliation state | READ_ONLY_DEPLOYMENT_RECONCILIATION_COMPLETE; source unchanged before and after; staged candidate absent; rollback metadata retained |
| Hosted eight-image proof | Run 37774603350, attempt 1, job 113302197676; proof SHA-256 09d3af7d8c326c8d93dfe5b10db50a8967239f8a28fafcc6a6ebb7f9c1bdf274 |
| Hosted attestation | SHA-256 5891e3513d96e55e295729d6de290231a06d79b07540eb2b4264ae8b6d07269a |
| Hosted tooling | d5201815307cdd1362df52e04ff9d831181abfa9; producer 6f100018759f6e82d147fda580b0d4d0bbb08b5fe898b43d84a6f5729480bcb3; workflow c3473ea1299b9babafe0b97d633e63dc7580c6ecf3d87899961bb0146e80d629 |
| Hosted artifact | Artifact 11550116167, ZIP SHA-256 2b25f15543e4d6f3861b34289759e700584e9a2b9599f2712a66223fb3a87e2a; this is runner evidence, not a Railway runtime digest |

The first hosted attempt (37771816100) remains recorded as a failed metadata-manifest binding run. Its failure was not rewritten into a success; the accepted second run and its actual proof are the release evidence.

## Actual Railway runtime

The following service and deployment identities are from the final read-only reconciliation. Every row had configured source and actual source at bfb4bee, status SUCCESS, and one RUNNING replica.

| Service | Railway service ID | Deployment ID | Actual image digest |
|---|---|---|---|
| control-api | 751fd9e6-e26f-411d-94bb-d00bff06abda | fc8217b1-da78-4435-81b9-f5ba043ad92a | sha256:7e758015648e33ac059e37f596c82d266a192550e9099cb3031ba3d93b5f34aa |
| audience-worker | dede9d43-2cb9-413f-bacd-4f1f750b04ca | 437cdfec-864b-4fcb-b8fb-914b11ea3379 | sha256:ab972f559cee7c75eed402c7be51256093d85ba4b87f65938e9803c5608bc3be |
| campaign-worker | 5830baa1-c94e-465c-afae-b63a4cd47620 | e8b39872-f495-42f9-af65-483ece9cd300 | sha256:3f917790d6de488e13f426bd2de0f996819daef7bdbd43bbe8802887706fdfdf |
| export-worker | b002f23f-c186-414c-8e2f-2221b39ab762 | c433f695-8969-4d01-9c99-363b00dc6cbc | sha256:fd3882792074eb6f3bfe83ac9be41c42ca1b98b90667cae6d6c3f7e65211814e |
| inbound-governance-worker | 8d04dff3-8d7f-4d64-b427-14cd12c66142 | 9e879df1-a805-43a3-9803-6b79fe042e96 | sha256:6083b406c1b46cce013a1d3c3ade939f1e1c8b92bfe3479d7af530120154b2c3 |
| metrics-worker | 412f2fa2-288c-4898-9bf2-276f88890270 | 97de8765-6d2e-4439-bd7c-ba1f56835fca | sha256:681a6ecf1a927fd74321162cf6d8ba5c5a5338abdf17f6051aa3c95aa6e23a74 |
| platform-governance-worker | 9d9fe33b-be49-4881-99c8-4a0f3cebab5e | 218766ce-18ca-4d5d-a76a-b0e3541aaea6 | sha256:cc02e930014ac19b39b811e17eecaebb5d929a6d12eb82e96467388710efab14 |
| admin-web | 542e7bef-02f6-40de-8efc-c6e07e7af90f | b3258b67-be1e-417d-afeb-01d691550fac | sha256:aae3daf0d7a0a71907d35048e79fc5e39a51314b75e847b4bc6414b4fe33815f |

The old 6d deployment identities remain rollback baselines only. No rollback, restore, migration replay, or credential reset was performed.

## Platform health and access

The control API is publicly reachable at https://control-api-production-22c0.up.railway.app; /readyz and /healthz returned HTTP 200. Admin health returned HTTP 200. Direct and proxied anonymous calls to /api/v1/auth/me returned HTTP 401 with AUTHENTICATION_REQUIRED. The public gateway health endpoint https://gateway.sendam.tech/healthz returned HTTP 200. Railway environment status was read within the connector limit, with no pending issues or failures and one running replica per service.

The gateway public TLS and health surface were observed. A fresh private VPS SSH, bind, mount, and process-boot inspection was not performed, so those details remain operational follow-up evidence rather than a release claim.

Interim admin access remains:
- URL: https://admin-web-production-016f.up.railway.app
- Account: admin@sendam.tech
- Existing Railway control-api variables: BOOTSTRAP_ADMIN_EMAIL, BOOTSTRAP_ADMIN_PASSWORD, BOOTSTRAP_ADMIN_TOTP_SECRET

Retrieve the password and TOTP secret privately from the authorised Railway control plane. They are deliberately absent from this document and all receipts.

## Live admin smoke

At the time this handover was authored, the final authenticated smoke receipt was still being closed out. Its required evidence is a same-origin rendered login, MFA challenge, campaign route, hardened cookie attributes, direct/proxied anonymous-auth behavior, missing-campaign/readiness route behavior, logout and revoked-session verification, browser/context closure, and a clear statement that the production organisation inventory is empty. The smoke helper's origin-response guard correction is retained in the ignored operational task folder; its focused synthetic guard tests passed.

Replace this section with the final receipt path and SHA-256 after the live smoke process finishes. Until then, do not describe authenticated smoke as complete and do not manufacture a positive campaign fixture.

## Positive campaign readiness boundary

The production organisation inventory currently contains zero organisations (hasMore: false). Therefore the positive readiness path is explicitly SKIPPED — PRODUCTION_ORGANISATIONS_EMPTY. No organisation, consent review, campaign, audience, approval, reservation, test message, or provider send was created solely to manufacture evidence.

The accepted Day-1 operating shape is an internal operator workflow using one engine-specific SENDER_POOL with multiple compatible READY sessions when available. A campaign may therefore use several sender numbers within the same selected engine pool. Mixed Baileys/WWebJS execution for one campaign, automatic cross-engine fallback or reallocation, and a sender-profile mutation flow remain deferred decisions. UNKNOWN remains sticky and must not become a blind retry button.

## Five Root rulings and bounded cost

1. Exact GetPool plus ACTIVE validation for DRAFT. Keep the additive interface and extra read; retain targeted regressions.
2. UUID identifiers for newly created memory pools. Adjust fixtures and tests; make no persistent-ID change.
3. Required evidence[] in readiness. Align the schema and clients with the accepted response contract.
4. Atomic SaveDraft returns persisted Campaign,error. Execute under the SQL transaction row lock and memory mutex, preserve trigger 0005, and do not add a post-commit Get. The cost is method and test-double churn, with no migration or general compare-and-swap redesign.
5. Exact read-only GetPool in readiness. Add the dependency, read and targeted tests without adding schema or execution authority.

These rulings are bounded compatibility and test work. They do not add general CAS, replay migrations, transport fallback, or campaign execution authority.

## Remaining product and contract work

The backend foundations and production deployment are in place; the operator product is not finished.

- The admin UI must become one coherent internal, offline-vetted-client flow. The third-party HTML prototypes are visual references and need alignment to actual route names, permissions, state machine and evidence contracts.
- Organisation, purpose and consent-review selectors need authoritative labels, expiry/restriction state and an attributable offline-order reference. The campaign must hand off to the correct audience builder and bind one server-resolved, frozen snapshot.
- The composer needs safe media upload/scan feedback, immutable content versions, preview and fallback validation, content history and a safe media delivery/rights lifecycle.
- Sender selection must expose compatible engine, pool, gateway, node and concrete READY sessions with stale, draining, quarantined and mismatched states explained. Several numbers in one engine pool are supported; cross-engine fallback is a separate backlog item.
- Controlled tests need approved masked recipients, current content/version indicators, MFA and exact frozen route evidence. ACCEPTED means provider/gateway acceptance, not recipient delivery; FAILED and UNKNOWN need explicit handling.
- Readiness and routing screens need measured gross capacity, the campaign's exact held window and reservation status. Competing reservations and restricted calendars remain unassessed by the read-only readiness contract.
- Reviewer screens need typed content, commercial, consent and final evidence with maker-checker attribution. Existing APIs still have typed-contract gaps around message versions, preview, test messages, routing plans, commercial records and material amendments.
- Post-approval content and audience rebuild paths, efficient finance detail, attributable stage decisions, safe asset-library delivery, rights/expiry and tracked-link/STOP policy need deliberate contract decisions.
- Real provider UAT is still separate: pairing, text/media sends, receipts, inbound handling, reconnect, UNKNOWN, scale, restore, upgrade and accessibility have not been credited by this release checkpoint.

## Next continuation order

1. Finish and attach the final live admin smoke receipt, including the empty-production-organisations skip.
2. Build the authoritative operator campaign flow against the existing backend contracts, with pagination, optimistic version handling, MFA boundaries and accessible error/recovery states.
3. Obtain a lawful governed fixture outside the empty production state, then exercise create/save/reopen, audience snapshot, composer, controlled test, readiness and approval paths without inventing evidence.
4. Perform private Hostinger gateway/provider UAT with the configured sender pools and record real send, media, receipt, inbound and reconnect evidence.
5. Reconcile the remaining typed API contracts and explicitly decide post-approval rebuild, media rights, calendar feasibility and any later mixed-engine/fallback scope.

The 8 October blocked-release document remains historical provenance. This 9 October handover is the current release checkpoint and should be updated only with new receipts or source changes that can be independently reproduced.
