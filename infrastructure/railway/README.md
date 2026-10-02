# Railway control-plane deployment contract

Railway hosts the backend/control-plane services only. The OpenWA browser gateway fleet is intentionally excluded from Railway and remains on the Hostinger/OpenWA gateway boundary.

## Authoritative service contract

`service-contracts.json` is the source of truth for the seven Railway backend services, their Dockerfiles, health ports, ingress posture, required environment, and secret classes.

Expected services:

- `control-api`
- `audience-worker`
- `campaign-worker`
- `export-worker`
- `inbound-governance-worker`
- `metrics-worker`
- `platform-governance-worker`

Forbidden service:

- `openwa-gateway`

## Production shell checkpoint

`production-services-2026-10-02.json` records the first private Railway production shell:

- Project: `openwa-prod`
- Project ID: `8633a0b9-3b15-4c8d-b6f2-0306b284f4dd`
- Environment: `production`
- Environment ID: `546fd711-cf80-4402-89b0-cb3ae5671fa9`

This file records empty service slots only. It is not deployment evidence for code, production secrets, PostgreSQL migrations, monitoring, backup/DR, live providers, capacity, or frontend/UAT.

## Evidence discipline

Production shell records are supporting evidence only unless and until the release-gate verifier accepts a gate-bound evidence manifest under `evidence/release-gates/RG-NNN/`. Do not mark any release gate closed from the shell record alone.
