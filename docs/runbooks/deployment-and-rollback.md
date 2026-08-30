# Deployment and rollback runbook

This document prepares the production procedure; it is **not** evidence that Railway or Hostinger deployment has occurred.

## Preconditions
- Freeze an approved source/config fingerprint and immutable image digests.
- Validate the seven-service Railway control plane and Hostinger gateway-only topology.
- Resolve environment and secret references outside the repository; never record secret values here.
- Confirm PostgreSQL migration, backup and rollback evidence status before release execution.

## Deployment sequence
1. Validate readiness, topology, Compose, security and release-gate checks against the candidate.
2. Deploy Railway data/control services with private dependencies and only control-api public ingress.
3. Verify each service readiness endpoint before advancing dependent workers.
4. Deploy Hostinger OpenWA nodes using a private bind, digest-pinned image and HTTPS cross-provider URLs.
5. Register gateway runtime evidence and verify node identity/authority before enabling sessions.
6. Keep direct Meta Cloud API on the Railway sibling path; never route Meta through Hostinger OpenWA.

## Rollback
- Stop new campaign admission before reverting infrastructure or application versions.
- Preserve UNKNOWN outcomes and durable outbox/session evidence; never auto-resend ambiguous submissions.
- Roll back only to a previously accepted immutable image/config pair.
- Re-run readiness and health checks before restoring traffic.
- Capture operator, timestamps, source fingerprint, image digest and rollback reason as external evidence.

Exercise status remains `PENDING_EXTERNAL` until a controlled target-environment deployment and rollback are executed and accepted.
