# Controlled test sends and pool reallocation

## Test-send boundary

A test send is a separate governed operational object. It does not create or consume a production campaign-recipient entitlement and cannot be addressed to an arbitrary telephone number.

Flow:

```text
Draft approved-test recipient
→ independent approval
→ select exact campaign message version
→ select OpenWA engine, gateway pool and concrete session
→ render variables server-side
→ apply the session's governed pacing policy
→ submit using a test-specific idempotency key
→ record ACCEPTED, FAILED or UNKNOWN
```

Unknown outcomes are never automatically re-sent.

## Reallocation boundary

Cross-pool movement is not fallback. It is a governed change inside an already approved multi-pool routing plan. The source route must permit movement out and the target route must permit movement in. Only a pending, unleased shard whose recipients remain in pre-submission states can move. Movement changes the shard fencing version and creates append-only evidence.

Movement between sessions inside the same pool remains normal health-aware allocation. Movement between pools or engines requires the explicit routing-plan permissions above.
