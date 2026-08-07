# OpenWA upstream provenance

- Upstream repository: `https://github.com/rmyndharis/OpenWA`
- Supplied archive: `OpenWA-main.zip`
- Package version: `0.13.0`
- Archive SHA-256: `c8624c1357457e66bd37ad96f209dab91e733431a2cd3e6efd0a2f71d0b1ebea`
- Imported/reviewed: `2026-08-05`
- Exact upstream Git commit: **UNVERIFIED** — the supplied archive contains no `.git` metadata, so no commit SHA may be asserted from the archive alone.
- Licence: MIT; preserve upstream copyright and licence notices.
- Import model: source archive imported into this private repository; **not** a public GitHub fork.
- Update model: manual import into an isolated review branch, provenance/security review, tests, internal review and controlled release.
- Automatic upstream merges or deployments: prohibited.

## Canonical source location

The supplied upstream source is preserved under:

```text
third_party/openwa/upstream/
```

`third_party/openwa/UPSTREAM.md` records the archive-specific provenance.

## Production transport integration

The production campaign platform does not ship the complete upstream OpenWA server/dashboard application.

An explicit audited transport subset is synchronised at gateway build/typecheck time by:

```text
services/openwa-gateway/scripts/sync-retained-openwa.mjs
```

from:

```text
third_party/openwa/upstream/src/
```

into the generated build source location:

```text
services/openwa-gateway/src/retained-openwa/
```

The retained transport code is compiled into the single isolated `openwa-gateway` worker. The generated `retained-openwa` directory is not the provenance source; the canonical imported source remains `third_party/openwa/upstream/`.

Go/PostgreSQL remain authoritative for consent, eligibility, campaign obligations, sender/session governance, canonical delivery state, privacy, reporting and audit. OpenWA remains a replaceable transport implementation and must not become the business system of record.
