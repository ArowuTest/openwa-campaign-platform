# Multi-pool OpenWA routing and sender pacing

Large campaigns are executed through an immutable routing plan rather than one undifferentiated sender pool.

```text
Campaign
  -> approved multi-pool routing plan
  -> deterministic dispatch shard
  -> reserved engine-compatible sender pool
  -> healthy concrete WhatsApp session
  -> OpenWA gateway node
```

`whatsapp-web.js` and Baileys are process-level OpenWA engine choices, so they are represented by separate gateway pools. A campaign may use both only when both routes are explicitly approved. Movement between sessions in one pool is ordinary allocation. Movement between pools or engines is allowed only by the frozen routing plan. Unknown outcomes are never reallocated for resend until reconciliation proves no submission occurred.

Pacing policy is hierarchical:

```text
Platform -> Provider -> Engine -> Gateway pool -> Sender pool -> Session -> Campaign
```

The most specific active policy wins, and the resolved policy IDs are retained as execution evidence. Administrators can set the minimum and maximum delay between messages for a session, plus jitter, hourly/daily allowances, concurrency, campaign limits and adaptive safety thresholds. Runtime health controls may slow a route but cannot make it faster than the approved minimum wait.
