from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"
SHELL = ROOT / "infrastructure/railway/production-services-2026-10-02.json"
PLAN = ROOT / "infrastructure/railway/production-variable-plan-2026-10-02.json"

UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
VALUE_KEYS = {"value", "values", "secret", "secret_value", "credential", "credential_value", "password", "token"}
FORBIDDEN_SERVICE = "openwa-gateway"


def load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"cannot load {path.relative_to(ROOT)}: {exc}") from exc
    if not isinstance(value, dict):
        raise RuntimeError(f"{path.relative_to(ROOT)} must contain a JSON object")
    return value


def find_value_keys(value: Any, path: str = "$", found: list[str] | None = None) -> list[str]:
    if found is None:
        found = []
    if isinstance(value, dict):
        for key, child in value.items():
            if str(key).lower() in VALUE_KEYS:
                found.append(f"{path}.{key}")
            find_value_keys(child, f"{path}.{key}", found)
    elif isinstance(value, list):
        for index, child in enumerate(value):
            find_value_keys(child, f"{path}[{index}]", found)
    return found


def validate() -> list[str]:
    errors: list[str] = []
    contract = load_json(CONTRACT)
    shell = load_json(SHELL)
    plan = load_json(PLAN)

    if plan.get("schema_version") != 1:
        errors.append("variable plan must use schema_version 1")
    if plan.get("evidence_type") != "railway-production-variable-plan":
        errors.append("variable plan evidence_type is invalid")
    scope = str(plan.get("scope", "")).lower()
    for phrase in ("non-secret", "no variable values", "no production deployment"):
        if phrase not in scope:
            errors.append(f"variable plan scope must explicitly say {phrase}")

    forbidden_value_keys = find_value_keys(plan)
    if forbidden_value_keys:
        errors.append("variable plan must not contain value-like keys: " + ", ".join(forbidden_value_keys))

    if plan.get("railway_project") != shell.get("railway_project"):
        errors.append("variable plan project does not match production shell")
    if plan.get("environment") != shell.get("environment"):
        errors.append("variable plan environment does not match production shell")

    contract_services = contract.get("services")
    shell_services = shell.get("services")
    plan_services = plan.get("services")
    if not isinstance(contract_services, dict) or not isinstance(shell_services, dict) or not isinstance(plan_services, dict):
        return errors + ["contract, shell and variable plan must all contain service maps"]

    expected = set(contract_services)
    if set(plan_services) != expected:
        errors.append("variable plan service set does not match authoritative Railway contract")
    if set(shell_services) != expected:
        errors.append("production shell service set does not match authoritative Railway contract")
    if FORBIDDEN_SERVICE in plan_services or FORBIDDEN_SERVICE in shell_services or FORBIDDEN_SERVICE not in set(contract.get("forbidden_services", [])):
        errors.append("openwa-gateway must remain forbidden and absent from Railway service maps")

    for service, declaration in contract_services.items():
        planned = plan_services.get(service)
        if not isinstance(planned, dict):
            errors.append(f"variable plan for {service} must be an object")
            continue
        if planned.get("service_id") != shell_services.get(service) or not UUID.fullmatch(str(planned.get("service_id", ""))):
            errors.append(f"variable plan service_id mismatch for {service}")
        expected_env = declaration.get("required_environment", [])
        expected_secrets = declaration.get("secret_classes", [])
        if sorted(planned.get("required_environment", [])) != sorted(expected_env):
            errors.append(f"required_environment drift for {service}")
        if sorted(planned.get("secret_classes", [])) != sorted(expected_secrets):
            errors.append(f"secret_classes drift for {service}")
        if planned.get("public_ingress") is not declaration.get("public_ingress"):
            errors.append(f"public_ingress drift for {service}")
        if planned.get("tls_required", False) is not declaration.get("tls_required", False):
            errors.append(f"tls_required drift for {service}")
        health = planned.get("health", {})
        if health.get("port") != declaration.get("port") or health.get("path") != declaration.get("health_path"):
            errors.append(f"health declaration drift for {service}")

    non_claims = " ".join(str(item).lower() for item in plan.get("explicit_non_claims", []))
    for phrase in ("no secret values", "no railway variables", "no source image", "no release gate", "openwa-gateway"):
        if phrase not in non_claims:
            errors.append(f"variable plan non-claims must include {phrase}")
    return errors


def main() -> int:
    errors = validate()
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Railway production variable plan valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
