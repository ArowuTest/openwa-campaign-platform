# OpenWA upstream provenance

- Upstream repository: `https://github.com/rmyndharis/OpenWA`
- Approved baseline commit: `6fc0bb87631f3b6b5e6e4410736db92e0edf2c61`
- Import model: source archive imported into this private repository; **not** a GitHub public fork
- Update model: manual fetch into an isolated review branch, security review, tests, internal pull request, staging, then controlled release
- Automatic upstream merges: prohibited

## Import destination

The approved OpenWA transport code will be placed under:

```text
services/openwa-gateway/vendor/openwa/
```

Business logic must never be moved into the vendored gateway. The Go control plane remains authoritative for consent, eligibility, campaigns and delivery state.
