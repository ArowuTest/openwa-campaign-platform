# Railway deployment contracts

Railway hosts the seven backend/control-plane services plus a separately governed internal operator frontend. The OpenWA browser gateway fleet is intentionally excluded from Railway and remains on the Hostinger/OpenWA gateway boundary.

## Backend control-plane contract

`service-contracts.json` remains the source of truth for the seven Railway backend services, their Dockerfiles, health ports, ingress posture, required environment, and secret classes.

Backend services:

- `control-api`
- `audience-worker`
- `campaign-worker`
- `export-worker`
- `inbound-governance-worker`
- `metrics-worker`
- `platform-governance-worker`

`openwa-gateway` remains forbidden on Railway.

## Internal operator frontend contract

`admin-web-service-contract.json` is the separate source of truth for the Railway-hosted internal operator console.

- Service: `admin-web`
- Public ingress: TLS required.
- Health: `/healthz` on port 3000.
- Backend dependency: `control-api` over Railway private networking.
- Runtime upstream variable: `ADMIN_WEB_CONTROL_API_URL`.
- Secret classes: none.
- Browser API prefix: same-origin `/api`.
- Network admission remains owned by `control-api`; admin-web forwards only Railway's edge marker and validated `X-Real-IP`, so `ALLOWED_NETWORK_CIDRS` remains authoritative and spoofed `X-Forwarded-For` is ignored for Railway-marked requests.
- `CONTROL_API_INTERNAL_URL` is not reused: it remains the canonical cross-provider control authority used by the Go/gateway trust boundary.

`production-admin-web-plan-2026-10-05.json` records only the intended hosting contract. It does not claim that a Railway frontend service, image, domain, variable value, browser smoke test, or release gate exists.

## Production shell checkpoint

`production-services-2026-10-02.json` records the original seven-backend Railway production shell:

- Project: `openwa-prod`
- Project ID: `8633a0b9-3b15-4c8d-b6f2-0306b284f4dd`
- Environment: `production`
- Environment ID: `546fd711-cf80-4402-89b0-cb3ae5671fa9`

That historical file remains seven-service evidence and is not rewritten to pretend admin-web already exists.

## Evidence discipline

Production shell and frontend hosting plans are supporting evidence only unless and until the release-gate verifier accepts gate-bound external evidence. Do not mark any release gate closed from a plan or empty service shell alone.
