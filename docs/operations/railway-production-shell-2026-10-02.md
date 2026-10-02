# Railway production shell checkpoint - 2026-10-02

## Purpose

This checkpoint records the first production Railway control-plane shell for the internal OpenWA Campaign Platform. It is supporting infrastructure evidence only. It does not close a release gate and does not claim production readiness.

## Railway project

- Project: `openwa-prod`
- Project ID: `8633a0b9-3b15-4c8d-b6f2-0306b284f4dd`
- Environment: `production`
- Environment ID: `546fd711-cf80-4402-89b0-cb3ae5671fa9`
- Visibility: private

## Service shell

The following empty Railway service slots were created to match `infrastructure/railway/service-contracts.json`:

| Service | Railway service ID |
| --- | --- |
| `control-api` | `751fd9e6-e26f-411d-94bb-d00bff06abda` |
| `audience-worker` | `dede9d43-2cb9-413f-bacd-4f1f750b04ca` |
| `campaign-worker` | `5830baa1-c94e-465c-afae-b63a4cd47620` |
| `export-worker` | `b002f23f-c186-414c-8e2f-2221b39ab762` |
| `inbound-governance-worker` | `8d04dff3-8d7f-4d64-b427-14cd12c66142` |
| `metrics-worker` | `412f2fa2-288c-4898-9bf2-276f88890270` |
| `platform-governance-worker` | `9d9fe33b-be49-4881-99c8-4a0f3cebab5e` |

`openwa-gateway` was intentionally not created on Railway. The gateway fleet remains a Hostinger/OpenWA boundary and must be validated separately.

## Explicit non-claims

This checkpoint does not claim any of the following:

- source deployment to production services;
- production secrets configured;
- production PostgreSQL migration success;
- Redis, object storage, ClamAV or PgBouncer readiness;
- live OpenWA/Baileys, OpenWA/WWebJS or Meta Cloud provider validation;
- capacity, load, backup, disaster recovery, monitoring, on-call or frontend/UAT readiness.

Those items remain external release-gate evidence.

## Supporting evidence

- Machine-readable Railway mapping: `infrastructure/railway/production-services-2026-10-02.json`
- Supporting evidence record: `evidence/release-gates/railway-production-shell-2026-10-02.json`
- Guardrail test: `tests/governance/test_railway_production_shell.py`

## Next production-shell steps

1. Configure managed dependencies and service-specific secret mounts without committing secrets.
2. Attach exact accepted source images per service using digest-pinned builds.
3. Run clean and upgrade-path PostgreSQL migration validation against the production-equivalent database.
4. Create Hostinger gateway fleet evidence, including encrypted session volume and genuine session lifecycle validation.
5. Add live-provider, capacity, backup/DR, monitoring, operations and frontend/UAT evidence before any gate closure.
