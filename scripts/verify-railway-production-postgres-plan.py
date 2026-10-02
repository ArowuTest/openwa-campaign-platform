from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
PLAN = ROOT / "infrastructure/railway/production-postgres-plan-2026-10-02.json"
EVIDENCE = ROOT / "evidence/release-gates/railway-production-postgres-2026-10-02.json"
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
ALLOWED_PLACEHOLDERS = ("external-secret", "railway-reference", "required", "stable-nologin", "non-secret")
ALLOWED_DATABASE_STATUSES = {
    "PENDING_PROVISIONING",
    "PROVISIONED_PITR_ENABLED_PENDING_ROLE_MIGRATION_RESTORE_EVIDENCE",
    "PROVISIONED_PITR_ENABLED_MIGRATED_PENDING_LOGIN_ROLE_RESTORE_EVIDENCE",
}
REQUIRED_RELATIONS = ("campaign_recipients", "gateway_runtime_nonces", "retention_jobs")


def load_json(path: Path) -> dict[str, Any]:
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
                if text and not any(allowed in text for allowed in ALLOWED_PLACEHOLDERS):
                    return True
            if contains_secret_value(child):
                return True
    elif isinstance(value, list):
        return any(contains_secret_value(item) for item in value)
    elif isinstance(value, str):
        lowered = value.lower()
        if "postgres://" in lowered or "postgresql://" in lowered or "password=" in lowered:
            return True
    return False


def validate_provisioned_database(database: dict[str, Any], evidence: dict[str, Any], errors: list[str]) -> None:
    if str(database.get("status", "")).startswith("PENDING"):
        return
    if not UUID.fullmatch(str(database.get("service_id", ""))):
        errors.append("provisioned Postgres service_id must be a UUID")
    if database.get("name") != "Postgres":
        errors.append("provisioned Railway Postgres service name must match observed service")
    if int(database.get("engine_major_version", 0)) < int(database.get("minimum_major_version", 17)):
        errors.append("observed Postgres major version is below minimum")
    if database.get("deployment_status") != "SUCCESS":
        errors.append("provisioned Postgres deployment must be SUCCESS")
    if "postgres-ssl" not in str(database.get("image", "")):
        errors.append("provisioned Postgres image must be Railway SSL Postgres image")

    volume = database.get("volume", {})
    if not UUID.fullmatch(str(volume.get("id", ""))):
        errors.append("provisioned Postgres volume id must be a UUID")
    if volume.get("mount_path") != "/var/lib/postgresql/data":
        errors.append("provisioned Postgres volume must be mounted at PostgreSQL data path")
    if int(volume.get("size_mb", 0)) <= 0:
        errors.append("provisioned Postgres volume size must be recorded")

    private_network = database.get("private_network", {})
    if private_network.get("hostname") != "postgres.railway.internal":
        errors.append("provisioned Postgres private hostname must be postgres.railway.internal")
    if private_network.get("state") != "ready" or private_network.get("sync_status") != "ACTIVE":
        errors.append("provisioned Postgres private network endpoint must be ready and ACTIVE")

    public_exposure = database.get("public_exposure", {})
    if public_exposure.get("domains") != []:
        errors.append("provisioned Postgres must not have public HTTP domains")
    if public_exposure.get("tcp_proxies") != []:
        errors.append("provisioned Postgres must not have public TCP proxies")

    pitr = database.get("pitr", {})
    if pitr.get("enabled") is not True or pitr.get("bucket_wired") is not True:
        errors.append("provisioned Postgres must have PITR enabled and bucket wired")
    bucket = pitr.get("bucket", {})
    if bucket.get("name") != "Postgres-PITR" or not UUID.fullmatch(str(bucket.get("id", ""))):
        errors.append("PITR bucket identity must be recorded")
    live_probe = pitr.get("live_probe", {})
    if live_probe.get("coverage_status") != "PENDING_RAILWAY_SSH_KEY":
        errors.append("PITR live coverage probe must remain pending until Railway SSH-key coverage evidence exists")
    if live_probe.get("archiver_status") != "PENDING_RAILWAY_SSH_KEY":
        errors.append("PITR archiver probe must remain pending until Railway SSH-key coverage evidence exists")

    if evidence.get("service", {}).get("id") != database.get("service_id"):
        errors.append("Postgres evidence service id must match plan")
    if evidence.get("pitr", {}).get("enabled") is not True:
        errors.append("Postgres evidence must record PITR enabled")
    if evidence.get("public_exposure") != public_exposure:
        errors.append("Postgres evidence public exposure must match plan")


def validate_migrated_database(database: dict[str, Any], evidence: dict[str, Any], errors: list[str]) -> None:
    if database.get("status") != "PROVISIONED_PITR_ENABLED_MIGRATED_PENDING_LOGIN_ROLE_RESTORE_EVIDENCE":
        return
    schema_apply = database.get("schema_apply", {})
    evidence_apply = evidence.get("schema_apply", {})
    if schema_apply != evidence_apply:
        errors.append("schema apply evidence must match plan")
    if schema_apply.get("status") != "APPLIED":
        errors.append("schema apply status must be APPLIED")
    if schema_apply.get("bootstrap_pre") != "PASS" or schema_apply.get("bootstrap_post") != "PASS":
        errors.append("service-role bootstrap must pass before and after migrations")
    if schema_apply.get("migrations_applied") != 93:
        errors.append("production schema apply must record 93 migrations")
    summary = schema_apply.get("schema_summary", {})
    if summary.get("tables") != 142 or summary.get("indexes") != 484 or summary.get("constraints") != 2207:
        errors.append("production schema summary drifted from accepted migration output")
    if summary.get("stable_service_roles") != 7:
        errors.append("production schema summary must record seven stable service roles")
    required_relations = summary.get("required_relations", {})
    for relation in REQUIRED_RELATIONS:
        if required_relations.get(relation) != "present":
            errors.append(f"required relation is not present after migrations: {relation}")

    roles = database.get("service_roles", {})
    evidence_roles = evidence.get("service_roles", {})
    if roles != evidence_roles:
        errors.append("service role evidence must match plan")
    if roles.get("status") != "STABLE_NOLOGIN_ROLES_RECONCILED":
        errors.append("stable service roles must be reconciled")
    if roles.get("stable_roles") != 7 or roles.get("login_roles") != 0:
        errors.append("stable service roles must be seven NOLOGIN roles")
    if roles.get("superuser_roles") != 0 or roles.get("createdb_roles") != 0 or roles.get("createrole_roles") != 0:
        errors.append("stable service roles must not have elevated role attributes")
    for role_name, attributes in roles.get("roles", {}).items():
        if role_name not in set(EXPECTED_ROLES.values()):
            errors.append(f"unexpected stable service role in evidence: {role_name}")
            continue
        if attributes.get("login") is not False or attributes.get("superuser") is not False:
            errors.append(f"stable service role must be NOLOGIN and non-superuser: {role_name}")
        if attributes.get("createdb") is not False or attributes.get("createrole") is not False:
            errors.append(f"stable service role must not create DBs or roles: {role_name}")


def validate() -> list[str]:
    errors: list[str] = []
    plan = load_json(PLAN)
    shell = load_json(SHELL)
    contract = load_json(CONTRACT)
    evidence = load_json(EVIDENCE) if EVIDENCE.is_file() else {}

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
    if database.get("status") not in ALLOWED_DATABASE_STATUSES:
        errors.append("production Postgres status is not recognised")
    for flag in ("ssl_required", "pitr_required", "backup_required", "restore_rehearsal_required"):
        if database.get(flag) is not True:
            errors.append(f"database plan must require {flag}")
    if database.get("public_access") != "disabled-by-default":
        errors.append("production Postgres public access must be disabled by default")
    validate_provisioned_database(database, evidence, errors)
    validate_migrated_database(database, evidence, errors)

    forbidden = set(plan.get("forbidden_database_targets", []))
    for target in ("hostinger-authoritative-postgres", "plain-docker-postgres-without-managed-volume-and-pitr", "supabase-v1-primary"):
        if target not in forbidden:
            errors.append(f"missing forbidden database target: {target}")

    bindings = plan.get("service_role_bindings", {})
    expected_services = set(contract.get("services", {}))
    if set(bindings) != expected_services or bindings != EXPECTED_ROLES:
        errors.append("service role bindings must match all seven Railway backend services exactly")

    if contains_secret_value(plan) or contains_secret_value(evidence):
        errors.append("production Postgres plan/evidence must not contain credential values")

    non_claims = " ".join(plan.get("explicit_non_claims", [])).lower()
    for required in (
        "no application database_url",
        "no rotatable service login roles",
        "no backend service has been wired",
        "accepted-baseline upgrade-path",
        "no backup restore rehearsal",
        "on-demand backup create returned oauth_insufficient_grant",
        "no release gate",
    ):
        if required not in non_claims:
            errors.append(f"missing explicit non-claim: {required}")

    gate_evidence = set(plan.get("required_evidence_before_gate_closure", []))
    for required in (
        "managed Railway PostgreSQL service exists in openwa-prod production",
        "migrations replay cleanly on a production-equivalent database",
        "PITR/WAL enabled and retention recorded",
        "backup restore rehearsal completed",
    ):
        if required not in gate_evidence:
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
