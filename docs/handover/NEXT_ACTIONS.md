# Next actions after 0.8.24

1. Execute migrations `0001` through `0059` against production-like PostgreSQL and retain migration, locking and rollback/recovery evidence.
2. Run `EXPLAIN (ANALYZE, BUFFERS)` and endurance tests for recipient claims, keyset evidence reads, reporting, reconciliation, routing reservations and provider activation.
3. Complete the remaining repository-wide API pagination, idempotency and concurrency audit and reconcile every affected SRS requirement with code and test evidence.
4. Build and contract-test both retained OpenWA engines, then execute genuine pairing, media, callback, reconnect and unknown-outcome scenarios.
5. Complete Hostinger deployment automation, secret rotation, backup/restore, disaster recovery and operational runbooks.
6. Begin production frontend integration only against the reviewed, stable API surface; do not mark `1.0.0` until the release gates pass.
