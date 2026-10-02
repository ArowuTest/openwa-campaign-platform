from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"
SHELL = ROOT / "infrastructure/railway/production-services-2026-10-02.json"
EVIDENCE = ROOT / "evidence/release-gates/railway-production-shell-2026-10-02.json"

UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")


def load_json(path: Path) -> dict:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"cannot load {path.relative_to(ROOT)}: {exc}") from exc
    if not isinstance(data, dict):
        raise RuntimeError(f"{path.relative_to(ROOT)} must contain a JSON object")
    return data


def validate() -> list[str]:
    errors: list[str] = []
    contract = load_json(CONTRACT)
    shell = load_json(SHELL)
    evidence = load_json(EVIDENCE)

    services = contract.get("services")
    if not isinstance(services, dict) or not services:
        return ["Railway service contract has no service map"]
    expected_services = set(services)
    if set(shell.get("services", {})) != expected_services:
        errors.append("production shell service set does not match authoritative Railway contract")
    if "openwa-gateway" in shell.get("services", {}) or "openwa-gateway" not in set(contract.get("forbidden_services", [])):
        errors.append("openwa-gateway must remain absent from the Railway shell and forbidden by the contract")
    for service, service_id in shell.get("services", {}).items():
        if not UUID.fullmatch(str(service_id)):
            errors.append(f"Railway service {service} has invalid service id")
        if service not in services:
            continue
        declaration = services[service]
        for key in ("dockerfile", "port", "health_path", "secret_classes"):
            if key not in declaration:
                errors.append(f"Railway service contract {service} is missing {key}")

    project = shell.get("railway_project", {})
    environment = shell.get("environment", {})
    if project.get("name") != "openwa-prod" or not UUID.fullmatch(str(project.get("id", ""))):
        errors.append("production shell must identify the private openwa-prod Railway project")
    if environment.get("name") != "production" or not UUID.fullmatch(str(environment.get("id", ""))):
        errors.append("production shell must identify the Railway production environment")

    notes = " ".join(shell.get("notes", []))
    for required in ("Empty service shell only", "no source deployment", "openwa-gateway remains forbidden"):
        if required.lower() not in notes.lower():
            errors.append(f"production shell notes must include non-claim: {required}")

    if evidence.get("evidence_type") != "supporting-railway-production-shell":
        errors.append("production shell evidence must be supporting evidence, not gate-closure evidence")
    scope = str(evidence.get("scope", ""))
    for required in ("does not close RG-002", "no production secrets", "no code/source deployment"):
        if required.lower() not in scope.lower():
            errors.append(f"supporting evidence scope must include non-claim: {required}")
    alignment = evidence.get("contract_alignment", {})
    if set(alignment.get("expected_backend_services", [])) != expected_services:
        errors.append("supporting evidence service alignment does not match Railway contract")
    if alignment.get("forbidden_service_absent") != "openwa-gateway":
        errors.append("supporting evidence must record openwa-gateway absence")
    return errors


def main() -> int:
    errors = validate()
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Railway production shell evidence valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
