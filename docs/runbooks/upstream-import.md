# OpenWA upstream import runbook

1. Download the approved source archive for the exact commit in `UPSTREAM.md`.
2. Verify the archive checksum and commit identity.
3. Scan the archive for secrets, binaries and unexpected generated files.
4. Preserve the upstream MIT licence and notices.
5. Import under `services/openwa-gateway/vendor/openwa/` on an isolated branch.
6. Review dependencies and container privileges.
7. Adapt behind the `MessagingProvider` contract; do not import business logic into the gateway.
8. Run gateway unit, integration and security tests.
9. Merge through an internal pull request only after approval.
