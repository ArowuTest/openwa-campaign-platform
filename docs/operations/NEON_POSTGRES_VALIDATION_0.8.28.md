# Neon PostgreSQL validation — 0.8.28

## Scope

This record covers code-side PostgreSQL validation for release 0.8.28. It does not certify production performance, backup/restore, disaster recovery or Hostinger deployment.

## Isolated environment

- Neon project: `Test2`
- PostgreSQL version: 18.4
- Validation branch: `openwa-0828-validation`
- Branch ID: `br-green-pine-aykv09eo`
- Parent branch was not modified.
- No connection credential is stored in this repository or in this evidence file.

## Clean migration result

Migrations `0001` through `0066` were executed in order against a clean database. The complete chain passed after correcting stale schema references found by live execution.

Final object inventory:

- 128 base tables
- 411 indexes
- 61 user triggers
- 1 view
- 5 active high-volume partition policies

Live validation found and corrected references to obsolete names:

- `role_definitions` -> `roles`
- `internal_roles` -> `roles`
- `users` -> `internal_users`
- `user_accounts` -> `internal_users`
- `segment_definitions` -> `segments`

`tests/schema/migrations_test.go` now rejects those obsolete names.

## Service identity verification

The role bootstrap was executed after the full migration chain. It was then rerun after revoking prior worker grants, proving that the explicit policy itself grants the required access.

Verified role boundaries:

| Role | SELECT tables | INSERT tables | UPDATE tables | DELETE tables |
|---|---:|---:|---:|---:|
| campaign_control_api | 128 | 128 | 128 | 128 |
| campaign_audience_worker | 36 | 24 | 24 | 24 |
| campaign_campaign_worker | 49 | 31 | 32 | 31 |
| campaign_export_worker | 3 | 0 | 1 | 0 |
| campaign_inbound_governance_worker | 4 | 4 | 4 | 4 |
| campaign_metrics_worker | 5 | 1 | 1 | 0 |
| campaign_platform_governance_worker | 35 | 10 | 13 | 10 |

A required-permission query returned no missing permissions. The export and metrics workers were specifically checked for removal of unrelated broad access.

## Multi-session adversarial scenarios

Six tests were executed with independent PostgreSQL sessions:

1. Concurrent optimistic campaign-release transition: exactly one session advanced the expected version.
2. `FOR UPDATE SKIP LOCKED` job claims: two workers claimed different rows while one lock was held.
3. Duplicate provider event insertion: two simultaneous inserts produced one durable event row.
4. Session-fence replacement: exactly one session advanced the expected fence; the stale writer lost.
5. Outbox/obligation transaction boundary: rollback left neither row, while commit preserved both rows.
6. Overlapping capacity reservation: the exclusion constraint admitted one of two overlapping reservations.

The equivalent opt-in test is `tests/integration/postgres_adversarial_test.go`. Run it with a disposable database:

```bash
POSTGRES_ADVERSARIAL_DATABASE_URL='postgresql://...' go test -count=1 ./tests/integration -run TestPostgresAdversarialConcurrency
```

The test creates a unique temporary schema and removes it on completion.

## Remaining external gates

This evidence does not close:

- production-volume load and endurance testing;
- backup, restore and disaster-recovery exercises;
- Hostinger deployment and production networking;
- penetration testing and independent vulnerability assessment;
- genuine `whatsapp-web.js` and Baileys pairing and messaging;
- production monitoring and alert-delivery validation.
