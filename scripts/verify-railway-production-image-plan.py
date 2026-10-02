from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"
SHELL = ROOT / "infrastructure/railway/production-services-2026-10-02.json"
PLAN = ROOT / "infrastructure/railway/production-image-plan-2026-10-02.json"

UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
SHA = re.compile(r"^[0-9a-f]{40}$")
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
    plan = load_json(PLAN)
    if plan.get("schema_version") != 1:
        errors.append("image plan must use schema_version 1")
    if plan.get("evidence_type") != "railway-production-image-plan":
        errors.append("image plan evidence_type is invalid")
    if plan.get("railway_project") != shell.get("railway_project"):
        errors.append("image plan project does not match production shell")
    if plan.get("environment") != shell.get("environment"):
        errors.append("image plan environment does not match production shell")

    scope = str(plan.get("scope", "")).lower()
    for phrase in ("no image digest", "no railway source deployment", "no release gate closure"):
        if phrase not in scope:
            errors.append(f"image plan scope must include {phrase}")

    source = plan.get("source", {})
    if not SHA.fullmatch(str(source.get("local_head", ""))):
        errors.append("image plan source.local_head must be a 40-character Git SHA")
    if source.get("github_remote_configured") is not True:
        errors.append("image plan must record GitHub remote as configured after main push")
    if source.get("push_status") != "PUSHED_TO_GITHUB_MAIN":
        errors.append("image plan must record GitHub main push status")
    if source.get("github_repository") != "ArowuTest/openwa-campaign-platform":
        errors.append("image plan GitHub repository must match accepted production source")
    if source.get("remote_branch") != "main":
        errors.append("image plan remote branch must be main")
    if source.get("remote_head") != source.get("local_head") or not SHA.fullmatch(str(source.get("remote_head", ""))):
        errors.append("image plan remote_head must match local_head")

    services = contract.get("services")
    shell_services = shell.get("services")
    planned = plan.get("services")
    if not isinstance(services, dict) or not isinstance(shell_services, dict) or not isinstance(planned, dict):
        return errors + ["contract, shell and image plan must all contain service maps"]
    expected = set(services)
    if set(planned) != expected:
        errors.append("image plan service set does not match authoritative Railway contract")
    if FORBIDDEN_SERVICE in planned or FORBIDDEN_SERVICE in shell_services or FORBIDDEN_SERVICE not in set(contract.get("forbidden_services", [])):
        errors.append("openwa-gateway must remain forbidden and absent from Railway image plan")

    local_head = str(source.get("local_head", ""))
    for service, declaration in services.items():
        entry = planned.get(service)
        if not isinstance(entry, dict):
            errors.append(f"image plan for {service} must be an object")
            continue
        if entry.get("service_id") != shell_services.get(service) or not UUID.fullmatch(str(entry.get("service_id", ""))):
            errors.append(f"image plan service_id mismatch for {service}")
        if entry.get("dockerfile") != declaration.get("dockerfile") or not (ROOT / str(entry.get("dockerfile", ""))).is_file():
            errors.append(f"image plan dockerfile mismatch for {service}")
        if entry.get("image_variable") != service.upper().replace("-", "_") + "_IMAGE":
            errors.append(f"image variable mismatch for {service}")
        if entry.get("port") != declaration.get("port") or entry.get("health_path") != declaration.get("health_path"):
            errors.append(f"image plan health drift for {service}")
        if entry.get("public_ingress") is not declaration.get("public_ingress"):
            errors.append(f"image plan public ingress drift for {service}")
        if entry.get("source_commit") != local_head:
            errors.append(f"image plan source commit drift for {service}")
        if entry.get("image_digest") is not None:
            errors.append(f"image plan must not contain digest before a real build for {service}")
        if entry.get("build_status") != "PENDING_DIGEST_PINNED_BUILD_AND_RAILWAY_DEPLOYMENT":
            errors.append(f"image plan build status must remain pending for {service}")

    non_claims = " ".join(str(item).lower() for item in plan.get("explicit_non_claims", []))
    for phrase in ("no image digest", "no source image", "no railway source deployment", "no release gate", "openwa-gateway"):
        if phrase not in non_claims:
            errors.append(f"image plan non-claims must include {phrase}")
    return errors


def main() -> int:
    errors = validate()
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Railway production image plan valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
