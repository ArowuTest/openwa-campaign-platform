from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PLAN = ROOT / "infrastructure/railway/production-postgres-plan-2026-10-02.json"
ADR = ROOT / "docs/decisions/ADR-0007-railway-authoritative-postgres.md"
SHELL = ROOT / "infrastructure/railway/production-services-2026-10-02.json"
CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"

UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
EXPECTED_ROLES = {
    "control-api": "campaign_control_api",
    "audience-worker": "campaign_audience_worker",
    "campaign-worker": "campaign_campaign_worker",
    "export-worker": "campaign_export_worker",
    "inbound-governance-worker": "campaign_inbound_governance_worker",
    "metrics-worker": "campaign_metrics_worker",
    "platform-governance-worker": "campaign_platform_governance_worker",
}
SECRET_VALUE_KEYS = {"password", "secret", "token", "database_url", "dsn", "credential", "connection_string"}


def load_json(path: Path) -> dict:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"cannot load {path.relative_to(ROOT)}: {exc}") from exc
    if not isinstance(value, dict):
        raise RuntimeError(f"{path.relative_to(ROOT)} must contain a JSON object")
    return value


def contains_secret_value(value: object) -> bool:
    if isinstance(value, dict):
        for key, child in value.items():
            lowered = str(key).lower()
            if any(marker in lowered for marker in SECRET_VALUE_KEYS):
                text = str(child).strip().lower()
                if text and not any(allowed in text for allowed in ("external-secret", "railway-reference", "required", "stable-nologin", "non-secret")):
                    return True
            if contains_secret_value(child):
                return True
    elif isinstance(value, list):
        return any(contains_secret_value(item) for item in value)
    return False


def validate() -> list[str]:
    errors: list[str] = []
    plan = load_json(PLAN)
    shell = load_json(SHELL)
    contract = load_json(CONTRACT)

    if not ADR.is_file():
        errors.append("ADR-0007 decision record is missing")
    else:
        text = ADR.read_text(encoding="utf-8")
        for required in (
            "Railway is the authoritative production PostgreSQL",
            "Hostinger must not host the authoritative campaign-platform PostgreSQL",
            "Supabase is not part of the V1 production architecture",
            "does not close RG-002",
        ):
            if required not in text:
                errors.append(f"ADR-0007 missing required decision text: {required}")

    if plan.get("schema_version") != 1:
        errors.append("production Postgres plan must use schema_version 1")
    if plan.get("decision") != "ADR-0007-railway-authoritative-postgres":
        errors.append("production Postgres plan must bind to ADR-0007")

    railway_project = plan.get("railway_project", {})
    environment = plan.get("environment", {})
    if railway_project != shell.get("railway_project"):
        errors.append("production Postgres plan project does not match production shell")
    if environment != shell.get("environment"):
        errors.append("production Postgres plan environment does not match production shell")
    if not UUID.fullmatch(str(railway_project.get("id", ""))):
        errors.append("Railway project id is invalid")
    if not UUID.fullmatch(str(environment.get("id", ""))):
        errors.append("Railway environment id is invalid")

    database = plan.get("database", {})
    if database.get("provider") != "railway-managed-postgresql":
        errors.append("authoritative production database must be Railway managed PostgreSQL")
    if database.get("status") != "PENDING_PROVISIONING":
        errors.append("plan must not claim an unproven production database exists")
    for flag in ("ssl_required", "pitr_required", "backup_required", "restore_rehearsal_required"):
        if database.get(flag) is not True:
            errors.append(f"database plan must require {flag}")
    if database.get("public_access") != "disabled-by-default":
        errors.append("production Postgres public access must be disabled by default")

    forbidden = set(plan.get("forbidden_database_targets", []))
    for target in ("hostinger-authoritative-postgres", "plain-docker-postgres-without-managed-volume-and-pitr", "supabase-v1-primary"):
        if target not in forbidden:
            errors.append(f"missing forbidden database target: {target}")

    bindings = plan.get("service_role_bindings", {})
    expected_services = set(contract.get("services", {}))
    if set(bindings) != expected_services or bindings != EXPECTED_ROLES:
        errors.append("service role bindings must match all seven Railway backend services exactly")

    if contains_secret_value(plan):
        errors.append("production Postgres plan must not contain credential values")

    non_claims = " ".join(plan.get("explicit_non_claims", [])).lower()
    for required in ("no database has been provisioned", "no credentials", "no release gate", "supabase is not part of v1", "hostinger remains gateway"):
        if required not in non_claims:
            errors.append(f"missing explicit non-claim: {required}")

    evidence = set(plan.get("required_evidence_before_gate_closure", []))
    for required in (
        "managed Railway PostgreSQL service exists in openwa-prod production",
        "migrations replay cleanly on a production-equivalent database",
        "PITR/WAL enabled and retention recorded",
        "backup restore rehearsal completed",
    ):
        if required not in evidence:
            errors.append(f"missing required pre-closure evidence: {required}")
    return errors


def main() -> int:
    errors = validate()
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Railway production Postgres plan valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
