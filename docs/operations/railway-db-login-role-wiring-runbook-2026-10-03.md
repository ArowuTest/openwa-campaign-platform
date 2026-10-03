# Railway production DB LOGIN-role wiring runbook

Date: 2026-10-03

This runbook addresses the credential automation block by moving secret generation and transport into an operator-controlled local terminal. ChatGPT tooling must not generate, receive, print or transport production database passwords.

## Current prerequisites already completed

- Railway project: `openwa-prod` (`8633a0b9-3b15-4c8d-b6f2-0306b284f4dd`)
- Environment: `production` (`546fd711-cf80-4402-89b0-cb3ae5671fa9`)
- Production Postgres is online and private.
- Production schema migrations have been applied.
- Stable `NOLOGIN` roles exist and are reconciled.
- Seven backend Railway services have non-secret identity variables set:
  - `APP_ENV=production`
  - `DATABASE_DRIVER=postgres`
  - `DATABASE_EXPECTED_ROLE=<stable privilege role>`

## What this runbook does

The operator script `scripts/wire-railway-production-db-logins.ps1` creates one rotatable PostgreSQL `LOGIN` role per backend service and sets each service's `DATABASE_URL` in Railway with redeploys skipped.

The script is intentionally **dry-run by default**. It only mutates production when `-Apply` is supplied.

## Secret-handling rules

- Do not paste generated passwords or DSNs into ChatGPT.
- Do not commit output containing secrets.
- Do not run with shell tracing enabled.
- Use `--skip-deploys`; deployment must be a separate controlled step.
- The script writes only a non-secret summary if `-SummaryPath` is supplied.

## Dry run

```powershell
powershell -ExecutionPolicy Bypass -File scripts\wire-railway-production-db-logins.ps1
```

## Apply

```powershell
powershell -ExecutionPolicy Bypass -File scripts\wire-railway-production-db-logins.ps1 -Apply -SummaryPath .agent\db-login-wiring-summary.json
```

Expected safe output:

- One line per service saying `Configured DATABASE_URL ... secret redacted`.
- Non-secret role verification with login role names and stable roles only.
- A non-secret summary JSON at `.agent/db-login-wiring-summary.json`.

## Post-run verification

After running the apply command, ask ChatGPT to verify. The verification must use only non-secret evidence:

- Railway variable names include `DATABASE_URL` for each backend service.
- Railway app services still have no deployment unless a separate deployment was intentionally run.
- PostgreSQL has seven new login roles, none superuser, none createdb, none createrole, each member of exactly one stable service role.

## Non-claims

This runbook does not deploy application containers, close RG-002, complete restore rehearsal, validate live PITR coverage, prove OpenWA transport, or close any production release gate by itself.
