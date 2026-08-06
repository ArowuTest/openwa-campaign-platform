#!/usr/bin/env sh
set -eu

python3 scripts/verify-production-compose.py
python3 scripts/scan-committed-secrets.py
python3 scripts/verify-node-security.py
python3 scripts/generate-sbom.py --output artifacts/sbom.cdx.json

if [ "${STRICT_RELEASE_SECURITY:-0}" = "1" ]; then
  python3 scripts/verify-production-compose.py --resolved
  python3 scripts/verify-node-security.py --strict
  python3 scripts/generate-sbom.py --strict --output artifacts/sbom.cdx.json
  for tool in gitleaks trivy syft; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      echo "ERROR: strict release security requires $tool" >&2
      exit 1
    fi
  done
  gitleaks detect --source . --no-banner --redact
  trivy fs --exit-code 1 --severity HIGH,CRITICAL --ignore-unfixed .
  syft dir:. -o cyclonedx-json=artifacts/sbom.syft.cdx.json
fi

printf 'Release security checks passed%s.\n' "$( [ "${STRICT_RELEASE_SECURITY:-0}" = "1" ] && printf ' in strict mode' || true )"
