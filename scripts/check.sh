#!/usr/bin/env sh
set -eu

unformatted="$(gofmt -l cmd internal tests)"
if [ -n "$unformatted" ]; then
  printf 'Go files require formatting:\n%s\n' "$unformatted" >&2
  exit 1
fi

go test ./...
go test -race ./...
go vet ./...
go build ./...
node scripts/check-typescript-syntax.js
node scripts/test-gateway-durability.js
python3 scripts/verify_openapi_routes.py
python3 scripts/verify-deployment-topology.py
python3 scripts/verify-admin-web-hosting.py
python3 scripts/verify-admin-web-deployment-packet.py
python3 scripts/verify-deployment-readiness.py
python3 scripts/verify-railway-production-shell.py
python3 scripts/verify-railway-production-variable-plan.py
python3 scripts/verify-railway-production-image-plan.py
python3 scripts/verify-railway-production-postgres-plan.py
./scripts/release-security-check.sh
python3 scripts/generate-traceability.py
git diff --exit-code -- docs/requirements/TRACEABILITY.md docs/requirements/traceability.json
printf 'Repository checks passed.\n'
