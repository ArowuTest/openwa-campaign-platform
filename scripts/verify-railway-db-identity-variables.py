from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"
SHELL = ROOT / "infrastructure/railway/production-services-2026-10-02.json"
EVIDENCE = ROOT / "evidence/release-gates/railway-db-identity-variables-2026-10-03.json"

EXPECTED_ROLE_BY_SERVICE = {
    "control-api": "campaign_control_api",
    "audience-worker": "campaign_audience_worker",
    "campaign-worker": "campaign_campaign_worker",
    "export-worker": "campaign_export_worker",
    "inbound-governance-worker": "campaign_inbound_governance_worker",
    "metrics-worker": "campaign_metrics_worker",
    "platform-governance-worker": "campaign_platform_governance_worker",
}
REQUIRED_VARIABLES = {"APP_ENV", "DATABASE_DRIVER", "DATABASE_EXPECTED_ROLE"}
FORBIDDEN_VARIABLES = {"DATABASE_URL", "DATABASE_URL_FILE"}


def load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"cannot load {path.relative_to(ROOT)}: {exc}") from exc
    if not isinstance(value, dict):
        raise RuntimeError(f"{path.relative_to(ROOT)} must contain a JSON object")
    return value


def validate() -> list[str]:
    errors: list[str] = []
    contract = load_json(CONTRACT)
    shell = load_json(SHELL)
    evidence = load_json(EVIDENCE)

    if evidence.get("schema_version") != 1:
        errors.append("DB identity variable evidence must use schema_version 1")
    if evidence.get("evidence_type") != "railway-db-identity-variable-wiring":
        errors.append("DB identity variable evidence_type is invalid")
    if evidence.get("railway_project") != shell.get("railway_project"):
        errors.append("DB identity variable evidence project does not match production shell")
    if evidence.get("environment") != shell.get("environment"):
        errors.append("DB identity variable evidence environment does not match production shell")
    if evidence.get("values_redacted") is not True:
        errors.append("DB identity variable evidence must state values_redacted=true")
    if evidence.get("skip_deploys") is not True:
        errors.append("DB identity variable evidence must state skip_deploys=true")

    scope = str(evidence.get("scope", "")).lower()
    for phrase in ("non-secret", "deploys skipped", "database_url"):
        if phrase not in scope:
            errors.append(f"DB identity variable evidence scope must include {phrase}")
    if "not set" not in scope and "not set by this evidence" not in scope:
        errors.append("DB identity variable evidence scope must state DATABASE_URL is not set by this evidence")

    services = evidence.get("services")
    contract_services = contract.get("services")
    shell_services = shell.get("services")
    if not isinstance(services, dict) or not isinstance(contract_services, dict) or not isinstance(shell_services, dict):
        return errors + ["contract, shell and DB identity evidence must contain service maps"]
    if set(services) != set(contract_services) or set(services) != set(EXPECTED_ROLE_BY_SERVICE):
        errors.append("DB identity variable service set does not match authoritative services")

    variable_names = set(evidence.get("variable_names_verified", []))
    if variable_names != REQUIRED_VARIABLES:
        errors.append("DB identity variable evidence must verify exactly APP_ENV, DATABASE_DRIVER and DATABASE_EXPECTED_ROLE")

    for service, expected_role in EXPECTED_ROLE_BY_SERVICE.items():
        entry = services.get(service)
        if not isinstance(entry, dict):
            errors.append(f"DB identity variable evidence for {service} must be an object")
            continue
        if entry.get("service_id") != shell_services.get(service):
            errors.append(f"DB identity variable service_id mismatch for {service}")
        if entry.get("expected_role") != expected_role:
            errors.append(f"DB identity variable expected_role mismatch for {service}")
        present = set(entry.get("variables_present", []))
        if not REQUIRED_VARIABLES.issubset(present):
            errors.append(f"DB identity variable evidence missing required variables for {service}")
        if present & FORBIDDEN_VARIABLES:
            errors.append(f"DB identity variable evidence must not list secret database variables for {service}")
        if entry.get("database_url_present") is not False:
            errors.append(f"DB identity variable evidence must keep DATABASE_URL absent for {service}")
        if entry.get("sealed_variable_names", []) != []:
            errors.append(f"DB identity variable evidence must not claim sealed secrets for {service}")
        if entry.get("latest_deployment", "unexpected") is not None:
            errors.append(f"DB identity variable evidence must not claim deployment for {service}")

    non_claims = " ".join(str(item).lower() for item in evidence.get("explicit_non_claims", []))
    for phrase in ("no database_url secret", "no rotatable service login role", "no service-specific database password", "no app service", "no release gate"):
        if phrase not in non_claims:
            errors.append(f"DB identity variable non-claims must include {phrase}")
    return errors


def main() -> int:
    errors = validate()
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Railway DB identity variable evidence valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
