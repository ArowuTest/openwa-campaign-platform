# Railway production backend service configuration evidence — 2026-10-03

## Scope

This records non-secret Railway runtime configuration applied to the seven backend service slots in the `openwa-prod` production environment.

It does **not** claim that application containers are deployed, that production database credentials are wired, that live provider traffic has run, or that a release gate is closed.

## Confirmed configuration

Project: `openwa-prod` (`8633a0b9-3b15-4c8d-b6f2-0306b284f4dd`)

Environment: `production` (`546fd711-cf80-4402-89b0-cb3ae5671fa9`)

All seven backend service slots were configured with:

- Railway builder: `DOCKERFILE`
- Build environment: `V3`
- Runtime: `V2`
- Health check path: `/readyz`
- Health check timeout: `300` seconds
- Region/replica intent: `ams`, `1` replica

| Service | Dockerfile | Service ID |
| --- | --- | --- |
| `control-api` | `infrastructure/docker/control-api.Dockerfile` | `751fd9e6-e26f-411d-94bb-d00bff06abda` |
| `audience-worker` | `infrastructure/docker/audience-worker.Dockerfile` | `dede9d43-2cb9-413f-bacd-4f1f750b04ca` |
| `campaign-worker` | `infrastructure/docker/campaign-worker.Dockerfile` | `5830baa1-c94e-465c-afae-b63a4cd47620` |
| `export-worker` | `infrastructure/docker/export-worker.Dockerfile` | `b002f23f-c186-414c-8e2f-2221b39ab762` |
| `inbound-governance-worker` | `infrastructure/docker/inbound-governance-worker.Dockerfile` | `8d04dff3-8d7f-4d64-b427-14cd12c66142` |
| `metrics-worker` | `infrastructure/docker/metrics-worker.Dockerfile` | `412f2fa2-288c-4898-9bf2-276f88890270` |
| `platform-governance-worker` | `infrastructure/docker/platform-governance-worker.Dockerfile` | `9d9fe33b-be49-4881-99c8-4a0f3cebab5e` |

## Explicit non-claims

- No application service has been deployed.
- No backend service has production `DATABASE_URL` credentials wired.
- No service-specific PostgreSQL `LOGIN` roles are created by this evidence.
- No image digest is present.
- No public endpoint or ingress domain has been validated.
- No live provider traffic has been sent.
- No release gate is closed.
- `openwa-gateway` remains excluded from Railway.

## Next step

The next bounded step is secret and identity wiring: create rotatable service-specific PostgreSQL `LOGIN` roles, set service-specific Railway database variables outside the repository, and then deploy one internal worker first for readiness validation before exposing `control-api`.
