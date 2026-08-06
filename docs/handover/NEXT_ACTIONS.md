# Next actions

## Planned production-engineering package — 0.8.28

1. Add Prometheus-compatible service metrics and W3C trace-context propagation without placing personal data in labels or traces.
2. Add service-specific PostgreSQL roles, secret-file/external-secret support, controlled dual-key rotation and stricter Redis/service credential boundaries.
3. Complete durable gateway command-nonce governance, bounded idempotency retention and operator-controlled unknown-submission reconciliation.
4. Standardise cursor pagination, bounded history reads and explicit malformed-query errors across every remaining API and worker export path.
5. Prepare high-growth recipient, provider-event, delivery-history and audit schemas for controlled partitioning and retention maintenance.
6. Add PostgreSQL-backed concurrency, crash-boundary and failure-injection tests for release, dispatch, capacity, events, retention, incidents and exports.
7. Expand gateway/runtime telemetry and governed alert metrics to disk, inode, process/PID and network evidence where the runtime can measure them safely.
8. Add supported executors or explicit permanent review policies for campaign media, raw provider events, delivery history and audit archival/destruction.

## External gates retained

- production-like PostgreSQL migration, locking and concurrency validation;
- dependency-resolved gateway build and genuine OpenWA sessions;
- target-volume performance and endurance evidence;
- Hostinger network policy, deployment, backup, restore and disaster-recovery proof;
- independent security assessment and operational key management;
- formal operational-owner approval of runbooks and capacity representation;
- production frontend completion.
