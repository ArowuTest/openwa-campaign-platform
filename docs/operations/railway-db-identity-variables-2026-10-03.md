# Railway DB identity variables — 2026-10-03

This checkpoint records the non-secret Railway database identity variables applied to the seven production backend service slots in `openwa-prod` / `production`.

## Applied, with deploys skipped

For each backend service, the following non-secret variable names are present:

- `APP_ENV`
- `DATABASE_DRIVER`
- `DATABASE_EXPECTED_ROLE`

The values were set through Railway with deploys skipped. No backend service was deployed or restarted by this slice.

## Service role mapping

| Service | Expected stable role |
|---|---|
| `control-api` | `campaign_control_api` |
| `audience-worker` | `campaign_audience_worker` |
| `campaign-worker` | `campaign_campaign_worker` |
| `export-worker` | `campaign_export_worker` |
| `inbound-governance-worker` | `campaign_inbound_governance_worker` |
| `metrics-worker` | `campaign_metrics_worker` |
| `platform-governance-worker` | `campaign_platform_governance_worker` |

These match `docs/operations/database-service-identities.md` and the production compose contract.

## Not yet done

`DATABASE_URL` is intentionally still absent from all seven backend services. The direct all-in-one role/password/secret command was blocked by the safety layer because it would generate and pass production database passwords through a shell flow. That guardrail was respected.

Still pending:

- Create rotatable service-specific PostgreSQL `LOGIN` roles safely without exposing passwords.
- Set each service-specific `DATABASE_URL` secret outside source.
- Prove each connected login is not superuser, not createdb, not createrole, and is a member of exactly one expected stable service role.
- Deploy one internal worker only after safe `DATABASE_URL` wiring is complete.

## Non-claims

This checkpoint does not close RG-002, RG-006 or any release gate. It does not prove application readiness, DB login isolation, or production traffic.
