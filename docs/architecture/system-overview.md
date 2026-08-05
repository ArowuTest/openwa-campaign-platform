# System overview

## Authority boundaries

1. PostgreSQL is authoritative for consent, audience eligibility, campaign entitlement and delivery state.
2. Redis may accelerate work execution but cannot be the only record of pending delivery.
3. The OpenWA gateway may submit messages and receive provider events, but it may not decide who is eligible or describe gateway acceptance as delivery.
4. Next.js displays and collects operator intent; it does not duplicate business rules from Go.
5. Object storage stores binary evidence and media; PostgreSQL stores metadata, checksums and retention state.

## Designer integration

Designer HTML is a reference prototype. It will be reviewed, decomposed into reusable React components, accessibility-corrected, expanded for missing states and connected to typed APIs. Production code must not inherit poor semantics, embedded secrets, duplicated CSS, inaccessible controls or unlicensed assets from the prototype.
