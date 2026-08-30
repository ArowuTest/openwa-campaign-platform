# ADR-0006: Govern campaign-scoped WhatsApp sender profile identity

## Status

Accepted for backend implementation on 24 August 2026. This decision extends the original sender catalogue without changing the current claim that genuine provider behaviour remains externally unproven.

## Decision

A genuine WhatsApp account remains a reusable underlying sender account. The display/profile name, photo, About text and future username presented for a campaign are governed as a campaign-scoped profile identity layered over that account. They are not a free-form mutation of `sender_sessions.profile_display_name` and are not proof of an alphanumeric WhatsApp sender ID.

Only one campaign profile identity may own an underlying account at a time. The control plane enforces this lifecycle:

`AVAILABLE -> RESERVED_FOR_CAMPAIGN -> IDENTITY_CONFIGURING -> IDENTITY_VERIFYING -> READY -> SENDING -> DRAINING -> CAMPAIGN_COMPLETE -> IDENTITY_RELEASING -> AVAILABLE`

Every requested identity is stored separately from the provider-observed identity. `READY` requires a successful observation and comparison under a capability version that was proven for the exact provider, engine and adapter version. Profile mutation is forbidden while the identity is `SENDING` or `DRAINING`.

The capability registry models display-name, photo, About and username read/write operations, recipient visibility and safe identity switching separately. A capability may be `UNSUPPORTED`, `UNKNOWN`, `SUPPORTED_UNVERIFIED` or `SUPPORTED_AND_VERIFIED`; hard campaign guarantees may rely only on `SUPPORTED_AND_VERIFIED`.

Username support is represented in the model but disabled by default. It may be enabled only after genuine-account validation proves engine support, provider acceptance, cooldown/propagation behaviour, recipient rendering and safe switching for the exact adapter version.

## Authority and evidence

The control plane owns reservations, requested/observed snapshots, normalized fingerprints, lifecycle transitions, capability versions and immutable events. Gateway engines may execute and observe profile operations, but they cannot self-declare a capability verified or move an identity to `READY`.

Releasing an identity must prove the configured campaign profile is no longer active before the account returns to `AVAILABLE`. Depending on governed policy, release may restore a neutral/previous profile or apply and verify the next reserved identity. A failed or ambiguous release quarantines the account instead of exposing one campaign's identity to another.

## Consequences

The existing account-level `profile_display_name` remains inventory metadata and migration compatibility state. It must not become campaign identity authority. Backend implementation needs first-class reservation, capability, observation and event records plus lifecycle APIs and workers before the frontend exposes sender-name controls.

Campaign admission must fail closed when the requested guarantee is unsupported or unverified. Production claims remain blocked until genuine WhatsApp accounts prove each enabled engine capability and its operational cooldowns.
