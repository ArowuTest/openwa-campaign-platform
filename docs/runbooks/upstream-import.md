# OpenWA upstream import runbook

1. Obtain the approved OpenWA source archive or release from the recorded upstream repository.
2. Record and verify the source URL, package/release version, import date and archive SHA-256. Record an exact Git commit only when the supplied source contains verifiable commit metadata; otherwise mark the commit identity **UNVERIFIED** rather than guessing.
3. Scan the archive for secrets, unexpected binaries, generated artefacts and unexpected dependency changes.
4. Preserve the upstream MIT licence and original notices in the private repository and release artefacts.
5. Import the canonical source under `third_party/openwa/upstream/` on an isolated review branch. Do not overwrite the currently approved source in place before review.
6. Review the explicit retained-transport allowlist in `services/openwa-gateway/scripts/sync-retained-openwa.mjs`. Add upstream files to the retained set only when a platform transport capability requires them and the dependency/security impact has been reviewed.
7. Review and apply any required compatibility patches to the pinned transport dependencies. Do not delete required OpenWA features merely to make a new upstream version compile.
8. Run `npm ci`, the gateway TypeScript typecheck/build, embedded OpenWA lifecycle/event tests, gateway durability tests, strict Node security validation and the final gateway Docker build.
9. Verify capability parity for the platform transport contract: send, health, get/create/start/stop/logout/delete session, QR and pairing code, plus drain/resume and text/image/video/document send paths.
10. Verify the production image contains the retained transport runtime and Chromium but does not contain or expose the full upstream dashboard/server application unless that has been separately approved as a deliberate architecture change.
11. Run the consolidated stack and verify health/readiness, signed control-plane integration, media SSRF policy, durable events/inbound outboxes and session recovery behaviour.
12. Merge only through the internal review process after provenance, security, capability-preservation and runtime evidence are complete. Automatic upstream merge/deployment remains prohibited.
