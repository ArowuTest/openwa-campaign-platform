from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"
SHELL = ROOT / "infrastructure/railway/production-services-2026-10-02.json"
EVIDENCE = ROOT / "evidence/release-gates/railway-service-config-2026-10-03.json"

UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
FORBIDDEN_SERVICE = "openwa-gateway"


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
        errors.append("service config evidence must use schema_version 1")
    if evidence.get("evidence_type") != "railway-service-runtime-config":
        errors.append("service config evidence_type is invalid")
    if evidence.get("railway_project") != shell.get("railway_project"):
        errors.append("service config project does not match production shell")
    if evidence.get("environment") != shell.get("environment"):
        errors.append("service config environment does not match production shell")

    scope = str(evidence.get("scope", "")).lower()
    for phrase in ("non-secret", "no application service", "no secret variables"):
        if phrase not in scope:
            errors.append(f"service config scope must include {phrase}")

    contract_services = contract.get("services")
    shell_services = shell.get("services")
    services = evidence.get("services")
    if not isinstance(contract_services, dict) or not isinstance(shell_services, dict) or not isinstance(services, dict):
        return errors + ["contract, shell and service config evidence must all contain service maps"]

    expected = set(contract_services)
    if set(services) != expected:
        errors.append("service config service set does not match authoritative Railway contract")
    if FORBIDDEN_SERVICE in services or FORBIDDEN_SERVICE in shell_services or FORBIDDEN_SERVICE not in set(contract.get("forbidden_services", [])):
        errors.append("openwa-gateway must remain forbidden and absent from Railway service config")

    for service, declaration in contract_services.items():
        entry = services.get(service)
        if not isinstance(entry, dict):
            errors.append(f"service config for {service} must be an object")
            continue
        if entry.get("service_id") != shell_services.get(service) or not UUID.fullmatch(str(entry.get("service_id", ""))):
            errors.append(f"service config service_id mismatch for {service}")
        if entry.get("builder") != "DOCKERFILE":
            errors.append(f"service config builder must be DOCKERFILE for {service}")
        if entry.get("build_environment") != "V3":
            errors.append(f"service config build environment must be V3 for {service}")
        if entry.get("dockerfile_path") != declaration.get("dockerfile") or not (ROOT / str(entry.get("dockerfile_path", ""))).is_file():
            errors.append(f"service config dockerfile mismatch for {service}")
        if entry.get("runtime") != "V2":
            errors.append(f"service config runtime must be V2 for {service}")
        if entry.get("healthcheck_path") != declaration.get("health_path"):
            errors.append(f"service config health path mismatch for {service}")
        if entry.get("healthcheck_timeout_seconds") != 300:
            errors.append(f"service config health timeout must be 300 seconds for {service}")
        if entry.get("region") != "ams" or entry.get("replicas") != 1:
            errors.append(f"service config must record one AMS replica for {service}")
        if entry.get("variable_names") != []:
            errors.append(f"service config must not record variables before secret wiring for {service}")
        if entry.get("latest_deployment") is not None:
            errors.append(f"service config must not claim an application deployment for {service}")

    non_claims = " ".join(str(item).lower() for item in evidence.get("explicit_non_claims", []))
    for phrase in (
        "no application service",
        "no backend service has production database_url",
        "no service-specific login",
        "no image digest",
        "no release gate",
        "openwa-gateway",
    ):
        if phrase not in non_claims:
            errors.append(f"service config non-claims must include {phrase}")
    return errors


def main() -> int:
    errors = validate()
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Railway service runtime config evidence valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
