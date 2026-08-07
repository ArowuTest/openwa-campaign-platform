# Runtime Configuration Governance Design

Date: 2026-08-07
Status: approved direction
Scope: backend runtime, sender/OpenWA transport, operational controls

## Purpose

The platform must avoid embedding operational choices in source code when those choices reasonably need to vary by environment, sender, provider, campaign, or operating policy.

Configuration must remain safe, auditable, versioned where appropriate, and fail closed where missing configuration would create unsafe behaviour.

This design formalises four configuration classes so future backend work does not confuse business policy, infrastructure secrets, runtime tuning, and true invariants.

## Configuration classes

### 1. Code invariants

Keep in code only values that are genuinely part of the protocol, data model, cryptographic contract, or non-negotiable safety boundary.

Examples include canonical lifecycle states, supported provider/engine identifiers, minimum cryptographic key sizes, signature formats, database constraints, and rules such as never automatically retrying an ambiguous `UNKNOWN` delivery.

Code invariants must be named constants or validation rules, not unexplained magic numbers.

### 2. Governed runtime configuration

Business and operational policy that may need controlled change without redeployment belongs in PostgreSQL-backed governed configuration wherever practical.

Examples include pacing, sender/campaign concurrency, rate limits, cooldowns, recovery thresholds, routing preferences, sender allocation limits, and similar operational controls.

These settings should use the platform's existing governance patterns: explicit scope, versioning, optimistic concurrency, audit reason, maker/checker approval where risk warrants it, effective dates, and deterministic resolution precedence.

Environment variables may provide bootstrap/default values only when no governed value exists. Once an approved runtime value exists, that governed value is authoritative for its scope.

The fail-safe default for `maxActiveCampaigns` is `1`. A higher value remains configurable through the governed pacing-policy lifecycle rather than by changing source code.

### 3. Deployment configuration

Infrastructure addresses, deployment identity, process-level limits, and bootstrap values belong in environment variables or deployment configuration.

Examples include database and Redis endpoints, object-store endpoints, OpenWA process identity/engine, service URLs, worker identity, absolute process safety ceilings, and settings that can only be selected safely at process startup.

Operator-managed production transport settings such as reconnect limits, watchdog/probe intervals, sender concurrency, pacing, and transport timeouts should move into governed runtime configuration wherever they can be applied safely and deterministically. Environment values provide bootstrap/fallback values, not the permanent control surface.

### 4. Secrets and credentials

Secrets must never be source-code constants or committed development defaults.

Production secrets use secret files or an approved secret manager. Existing `NAME_FILE` support remains the preferred container contract. Production must fail closed when required secret material is absent or invalid.

Versioned encryption keyrings remain separate by security domain. Sender-proxy credentials use their own `SENDER_PROXY_KEYS_JSON` / `SENDER_PROXY_ACTIVE_KEY_VERSION` keyring rather than reusing identity, MSISDN, inbound-content, or privacy-evidence keys.

Memory-only development/test runtimes may generate ephemeral cryptographic keys in-process when persistence is absent. Those keys must not be reusable fixed values in source.

## Sender proxy configuration

Per-session proxy configuration is an approved operational capability for stable networking/isolation, not rapid IP rotation or concealment.

The control plane remains authoritative. Proxy credentials are encrypted at rest in PostgreSQL, exposed only as a boolean/status to normal APIs, and decrypted only for an authorised session start.

The signed internal session-start command carries the proxy to the owning OpenWA worker. The worker validates it again, keeps it in memory only, and passes it to the retained engine adapter. It must not persist the proxy URL or credentials in the worker session registry.

Proxy changes are allowed only while the governed session is inactive. A live worker must reject a proxy mismatch rather than silently continuing on an old route.

## Resolution and precedence

For settings that support multiple layers, resolution is explicit and deterministic. Code invariants and validation boundaries constrain every candidate value; within those bounds, effective-value precedence is:

1. approved governed runtime value for the most specific applicable scope;
2. deployment/bootstrap value;
3. documented fail-safe default, only when the requirement defines one.

A missing value must not silently become an unsafe unlimited value. Invalid explicit values are rejected rather than replaced with defaults.

Secrets never participate in business-policy precedence: they are resolved only from approved secret configuration and are never returned through governed configuration APIs.

## Audit and observability

Every governed runtime change must record actor, reason, scope, version, effective period, and approval state where applicable.

Resolved operational configuration should be observable without exposing secrets. Diagnostics may report source/scope/version and safe effective values.

Changes that affect an active transport session must either be applied through an explicit safe transition or rejected until the session is inactive. No configuration API may claim a change is active while the underlying engine still runs the previous value.

## Testing requirements

Each configurable behaviour needs tests for default resolution, explicit override, invalid-value rejection, precedence, persistence/restart behaviour where applicable, and audit/version conflict handling.

Secret-bearing paths require tests proving plaintext does not leak into normal API responses, logs, worker registries, or durable non-secret stores.

Deployment tests must verify development configuration remains parameterised and production secret-file wiring remains mandatory.

## Migration strategy

Do not perform a broad rewrite of every existing environment variable at once. Reconcile operational settings incrementally as each backend area is audited.

For each candidate setting, classify it first as invariant, governed runtime configuration, deployment configuration, or secret. Preserve existing behaviour during migration unless the requirement explicitly calls for a safer default.

Priority candidates for governed runtime treatment include sender/campaign concurrency, pacing and throttling policy, cooldown/recovery thresholds, sender allocation controls, and other operator-managed transport behaviour.

Process-local safety timers and worker mechanics may remain deployment configuration when runtime editing would add risk or unclear semantics; they should still be parameterised and documented.

## Non-goals

This design does not move business authority into the OpenWA worker, Redis, or the frontend. PostgreSQL-backed Go services remain authoritative for business and governance state.

It does not make every numeric constant editable. Algorithmic bounds, protocol limits, schema constraints, and security invariants remain code-owned when configurability would weaken correctness.

It does not introduce dynamic configuration solely for convenience. A value becomes runtime-governed only when there is a legitimate operational need to change it without redeployment and the change can be applied safely and audibly.

## Acceptance criteria

- No production secret is committed or given a reusable source-code default.
- Operational values are configurable at the appropriate layer rather than scattered as magic constants.
- Governed values are versioned, auditable, fail closed on invalid configuration, and resolve deterministically.
- OpenWA transport remains deployment-engine-specific and subordinate to the Go/PostgreSQL control plane.
- Existing required capabilities are preserved while configuration hard-coding is reduced strategically.
