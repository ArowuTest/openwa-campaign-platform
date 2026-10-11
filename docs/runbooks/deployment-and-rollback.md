# Deployment and rollback runbook

This document prepares the production procedure; it is **not** evidence that Railway or Hostinger deployment has occurred.

## Preconditions
- Freeze an approved source/config fingerprint and immutable image digests.
- Validate the seven-service Railway backend control plane, the separate admin-web frontend contract, and the Hostinger gateway-only topology.
- Resolve environment and secret references outside the repository; never record secret values here.
- Keep `CONTROL_API_INTERNAL_URL` reserved for the canonical cross-provider HTTPS control authority.
- Configure admin-web with `ADMIN_WEB_CONTROL_API_URL` pointing only to the Railway-private control-api origin.
- Confirm PostgreSQL migration, backup and rollback evidence status before release execution.

## Deployment sequence
1. Validate readiness, backend topology, admin-web hosting, Compose, security and release-gate checks against the accepted candidate. Immediately before any admin-web Railway mutation, run `python3 scripts/verify-admin-web-hosting.py --deployment-ready` with the accepted source commit, currently deployed control-api commit, fresh trusted-proxy reconfirmation, actionable-staged-change absence, digest-pinned frontend image/private control URL, and explicit deploy authorization supplied as ephemeral environment evidence. Never persist those live evidence values in the repository.
2. Deploy the seven Railway backend/data-control services first when their deployed source does not already match the accepted commit. Workers remain private; `control-api` keeps public TLS ingress for governed gateway/Meta callback and control-authority endpoints. Do not create or deploy admin-web until control-api reports the same accepted commit and readiness passes.
3. Verify backend service readiness endpoints before advancing dependent workers or the frontend.
4. Create/deploy the separate Railway `admin-web` service only after accepted source is available. Give it public TLS ingress, no secret classes, and the Railway-private `ADMIN_WEB_CONTROL_API_URL` dependency.
5. Verify `admin-web /healthz`, then run authenticated browser smoke/accessibility checks through the public frontend origin. Browser `/api/*` requests must proxy server-side to the private control-api origin while preserving only Railway `X-Railway-Edge` plus validated `X-Real-IP`; `control-api` remains the owner of `ALLOWED_NETWORK_CIDRS` admission and ignores `X-Forwarded-For` for Railway-marked requests.
6. Deploy Hostinger OpenWA nodes using a private bind, digest-pinned image and HTTPS cross-provider URLs.
7. Register gateway runtime evidence and verify node identity/authority before enabling sessions.
8. Keep direct Meta Cloud API on the Railway sibling path; never route Meta through Hostinger OpenWA.

## Rollback
- Stop new campaign admission before reverting infrastructure or application versions.
- Preserve UNKNOWN outcomes and durable outbox/session evidence; never auto-resend ambiguous submissions.
- Roll back only to a previously accepted immutable image/config pair.
- If the frontend fails, remove or roll back admin-web ingress without changing gateway/control authority URLs.
- Re-run backend and frontend readiness/health checks before restoring traffic.
- Capture operator, timestamps, source fingerprint, image digest and rollback reason as external evidence.

Exercise status remains `PENDING_EXTERNAL` until a controlled target-environment deployment and rollback are executed and accepted.
