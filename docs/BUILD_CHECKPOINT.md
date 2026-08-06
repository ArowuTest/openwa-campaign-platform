# Build checkpoint

Current version: **0.8.28**

Branch: `work/backend-production-engineering`

Checkpoint type: **interim runtime-validated recovery checkpoint**

This checkpoint extends the earlier `620887aa063e622b678bc3eeb2d69700c9cfac74` safety commit with defects and controls discovered through a genuine whole-stack Docker build and startup test.

## Runtime evidence

The Docker Compose project `openwa0828smoke` built and ran 14 containers. At the final snapshot every container was healthy, every restart count was zero, the public UI and edge health/readiness routes returned HTTP 200, and there were no fresh application or PostgreSQL errors.

The validated services were PostgreSQL, Redis, ClamAV, OpenWA upstream, OpenWA gateway, control API, admin web, nginx and all seven Go workers.

## Material corrections

- Gateway gauge and summary metrics now compile and render safely.
- Native `libpq` handles typed nils and named scalar/domain values without panics or invalid parameter typing.
- Campaign, inbound, identity, capacity and governance-policy SQL now conforms to the migrated PostgreSQL schema.
- Control, campaign and export services receive the required governed keyrings.
- Retention and opt-out stores preserve approval actors and JSONB contracts.
- Control readiness validates canonical schema relations.
- Admin web binds to all container interfaces and nginx exposes public `/healthz` and `/readyz`.
- First-party Node images use committed lockfiles and `npm ci`.
- Next.js and NestJS were upgraded to audit-clean compatible releases.
- OpenWA server dependencies audit clean; the sole dashboard RSC advisory is explicitly governed by pending `SEC-EXC-001` and blocks strict release until independent approval.
- Root Docker context excludes Git history, dependency caches and generated output.

## Verification

The source has passed full Go tests, `go vet ./...`, changed-package race detection, gateway durability, admin typecheck and build, OpenWA dashboard typecheck and 273 tests, OpenAPI parity, production Compose validation, committed-secret scanning, node-security validation and SBOM generation.

The CycloneDX SBOM contains 24 components. Live Neon migration and concurrency evidence remains documented separately.

## Not yet final release

The checkpoint remains pre-production. Genuine continuation pagination, the 398-row requirements reconciliation, final Backend Release Readiness Review, independent security disposition, authenticated WhatsApp sessions, capacity/endurance evidence, Hostinger deployment, backup/DR, penetration testing, operational approval and the production frontend remain open gates.
