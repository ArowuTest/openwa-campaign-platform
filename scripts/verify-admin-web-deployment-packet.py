#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_PACKET = ROOT / "infrastructure/railway/admin-web-deployment-execution-2026-10-06.json"
DEFAULT_LINEAGE = ROOT / "evidence/release-gates/champion-hosting-commit-lineage-2026-10-06.json"

EXPECTED_PROJECT = "8633a0b9-3b15-4c8d-b6f2-0306b284f4dd"
EXPECTED_ENVIRONMENT = "546fd711-cf80-4402-89b0-cb3ae5671fa9"
EXPECTED_REPO = "ArowuTest/openwa-campaign-platform"
EXPECTED_BRANCH = "work/backend-production-engineering"
EXPECTED_COMMIT = "daa9f3ddede0ecc5b7d6a52e579fba140a0c5e8d"
EXPECTED_REVIEWED_APPLICATION_COMMIT = "f264237829e1caf4b0bac06c3aacd961885fc7c8"
EXPECTED_APPLICATION_DELTA = ["evidence/release-gates/champion-hosting-accepted-source-commit-2026-10-06.json"]
EXPECTED_APPLICATION_TREE = "1df0e7abfaf0177103e30c4f74efb765e3e7d909"
EXPECTED_FOLLOWUP_TREE = "b1e34346843f196532ef221271def4ca6e348a66"
EXPECTED_EVIDENCE_SHA256 = "e2acd4f65385d3e9eab4489531f7d4c78372eb3b4ab5ebeffaae6abbc53d2a3c"
EXPECTED_PRODUCTION_CONTROL_API_COMMIT = "cc125fbfacfd171d179440a780fc4062a8c54c0a"
EXPECTED_VARIABLE = "http://${{control-api.RAILWAY_PRIVATE_DOMAIN}}:${{control-api.PORT}}"
EXPECTED_BACKEND = {
    "control-api": "751fd9e6-e26f-411d-94bb-d00bff06abda",
    "audience-worker": "dede9d43-2cb9-413f-bacd-4f1f750b04ca",
    "campaign-worker": "5830baa1-c94e-465c-afae-b63a4cd47620",
    "export-worker": "b002f23f-c186-414c-8e2f-2221b39ab762",
    "inbound-governance-worker": "8d04dff3-8d7f-4d64-b427-14cd12c66142",
    "metrics-worker": "412f2fa2-288c-4898-9bf2-276f88890270",
    "platform-governance-worker": "9d9fe33b-be49-4881-99c8-4a0f3cebab5e",
}
SHA40 = re.compile(r"^[0-9a-f]{40}$")


def load(path: Path) -> dict:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"cannot load deployment packet: {exc}") from exc
    if not isinstance(value, dict):
        raise RuntimeError("deployment packet must contain a JSON object")
    return value


def git_changed_files(base: str, head: str) -> list[str]:
    result = subprocess.run(
        ["git", "diff", "--name-only", base, head],
        cwd=ROOT,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return [line.strip() for line in result.stdout.splitlines() if line.strip()]


def validate(packet: dict) -> list[str]:
    errors: list[str] = []
    if packet.get("schema_version") != 1:
        errors.append("schema_version must be 1")
    if packet.get("evidence_type") != "openwa-railway-integrated-deployment-execution-packet":
        errors.append("evidence_type is invalid")
    if packet.get("state") not in {"BLOCKED_UNPUSHED", "READY_TO_STAGE"}:
        errors.append("state must be BLOCKED_UNPUSHED or READY_TO_STAGE")
    if packet.get("project_id") != EXPECTED_PROJECT:
        errors.append("project_id mismatch")
    if packet.get("environment_id") != EXPECTED_ENVIRONMENT:
        errors.append("environment_id mismatch")
    if packet.get("repo") != EXPECTED_REPO:
        errors.append("repo mismatch")
    if packet.get("source_branch") != EXPECTED_BRANCH:
        errors.append("source_branch mismatch")
    commit = str(packet.get("accepted_source_commit", ""))
    if commit != EXPECTED_COMMIT or not SHA40.fullmatch(commit):
        errors.append("accepted_source_commit mismatch")

    reviewed_application = str(packet.get("reviewed_application_commit", ""))
    if reviewed_application != EXPECTED_REVIEWED_APPLICATION_COMMIT:
        errors.append("reviewed_application_commit mismatch")
    if packet.get("evidence_followup_commit") != EXPECTED_COMMIT:
        errors.append("evidence_followup_commit must equal accepted_source_commit")
    if packet.get("application_to_source_delta_files") != EXPECTED_APPLICATION_DELTA:
        errors.append("application_to_source_delta_files must contain only the accepted-source evidence record")

    try:
        lineage = load(DEFAULT_LINEAGE)
    except RuntimeError as exc:
        errors.append(str(exc))
        lineage = {}
    if lineage:
        expected_lineage = {
            "schema_version": 1,
            "evidence_type": "openwa-accepted-source-commit-lineage",
            "reviewed_application_commit": EXPECTED_REVIEWED_APPLICATION_COMMIT,
            "reviewed_application_tree": EXPECTED_APPLICATION_TREE,
            "evidence_followup_commit": EXPECTED_COMMIT,
            "evidence_followup_tree": EXPECTED_FOLLOWUP_TREE,
            "evidence_followup_parent": EXPECTED_REVIEWED_APPLICATION_COMMIT,
            "git_verification": "PASS",
        }
        for key, value in expected_lineage.items():
            if lineage.get(key) != value:
                errors.append(f"commit-lineage evidence {key} must be {value!r}")
        expected_delta = [{
            "status": "A",
            "path": EXPECTED_APPLICATION_DELTA[0],
            "sha256": EXPECTED_EVIDENCE_SHA256,
        }]
        if lineage.get("delta") != expected_delta:
            errors.append("commit-lineage evidence delta does not match the approved one-file follow-up")

    accepted_evidence_path = ROOT / EXPECTED_APPLICATION_DELTA[0]
    try:
        accepted_evidence_bytes = accepted_evidence_path.read_bytes()
        accepted_evidence = json.loads(accepted_evidence_bytes.decode("utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        errors.append(f"cannot load accepted-source evidence record: {exc}")
    else:
        actual_hash = hashlib.sha256(accepted_evidence_bytes).hexdigest()
        if actual_hash != EXPECTED_EVIDENCE_SHA256:
            errors.append("accepted-source evidence hash does not match commit-lineage evidence")
        if accepted_evidence.get("accepted_source_commit") != EXPECTED_REVIEWED_APPLICATION_COMMIT:
            errors.append("accepted-source evidence must identify the reviewed application commit")
        git_state = accepted_evidence.get("git_state")
        if not isinstance(git_state, dict) or git_state.get("committed") is not True:
            errors.append("accepted-source evidence must prove the application commit was committed")

    production_commit = str(packet.get("production_control_api_source_commit", ""))
    if production_commit != EXPECTED_PRODUCTION_CONTROL_API_COMMIT:
        errors.append("production_control_api_source_commit mismatch")
    if production_commit != EXPECTED_COMMIT:
        if packet.get("production_backend_requires_update") is not True:
            errors.append("production_backend_requires_update must be true while production source differs from accepted source")
    if packet.get("expected_post_deploy_source_commit") != EXPECTED_COMMIT:
        errors.append("expected_post_deploy_source_commit must equal accepted_source_commit")

    if packet.get("service_name") != "admin-web":
        errors.append("service_name must be admin-web")
    if packet.get("deploy_committed") is not False:
        errors.append("deploy_committed must remain false before execution")

    variables = packet.get("variable_plan")
    if variables != {"ADMIN_WEB_CONTROL_API_URL": EXPECTED_VARIABLE}:
        errors.append("variable_plan must contain only the Railway-private control-api reference")

    forbidden = set(packet.get("forbidden_admin_web_variables", []))
    required_forbidden = {
        "CONTROL_API_INTERNAL_URL",
        "NEXT_PUBLIC_CONTROL_API_BASE",
        "DATABASE_URL",
        "META_CLOUD_CREDENTIALS_JSON",
        "GATEWAY_COMMAND_SECRET",
        "GATEWAY_RUNTIME_SECRET",
        "MSISDN_ENCRYPTION_KEY_BASE64",
        "MSISDN_LOOKUP_KEY_BASE64",
    }
    if not required_forbidden.issubset(forbidden):
        errors.append("forbidden_admin_web_variables is incomplete")

    backend = packet.get("backend_source_updates")
    if not isinstance(backend, list) or len(backend) != len(EXPECTED_BACKEND):
        errors.append("backend_source_updates must contain all seven backend/control services")
    else:
        seen: set[str] = set()
        for item in backend:
            if not isinstance(item, dict):
                errors.append("backend_source_updates entries must be objects")
                continue
            name = item.get("service_name")
            seen.add(str(name))
            if name not in EXPECTED_BACKEND or item.get("service_id") != EXPECTED_BACKEND.get(name):
                errors.append(f"backend service identity mismatch: {name}")
            expected = {
                "action": "connect_service_source",
                "repo": EXPECTED_REPO,
                "branch": EXPECTED_BRANCH,
                "commit_sha": EXPECTED_COMMIT,
                "environment_id": EXPECTED_ENVIRONMENT,
                "staged": True,
            }
            for key, value in expected.items():
                if item.get(key) != value:
                    errors.append(f"backend {name} {key} must be {value!r}")
        if seen != set(EXPECTED_BACKEND):
            errors.append("backend_source_updates names are incomplete or duplicated")

    operations = packet.get("staged_operations")
    if not isinstance(operations, list) or [item.get("action") for item in operations if isinstance(item, dict)] != [
        "create_service", "update_service", "set_variables", "connect_service_source"
    ]:
        errors.append("staged_operations must be create/update/variables/source in order")
    else:
        if any(item.get("staged") is not True for item in operations):
            errors.append("every staged operation must have staged=true")
        create, update, setvars, source = operations
        if create.get("name") != "admin-web" or create.get("project_id") != EXPECTED_PROJECT or create.get("environment_id") != EXPECTED_ENVIRONMENT:
            errors.append("admin-web create_service operation is invalid")
        if update.get("target") != "admin-web" or update.get("dockerfile_path") != "infrastructure/docker/admin-web.Dockerfile" or update.get("healthcheck_path") != "/healthz" or update.get("private_network_endpoint") != "admin-web":
            errors.append("admin-web update_service operation is invalid")
        if setvars.get("target") != "admin-web" or setvars.get("variables") != {"ADMIN_WEB_CONTROL_API_URL": EXPECTED_VARIABLE}:
            errors.append("admin-web set_variables operation is invalid")
        if source.get("target") != "admin-web" or source.get("repo") != EXPECTED_REPO or source.get("branch") != EXPECTED_BRANCH or source.get("commit_sha") != EXPECTED_COMMIT:
            errors.append("admin-web connect_service_source operation is invalid")

    commit_op = packet.get("commit_operation")
    if not isinstance(commit_op, dict):
        errors.append("commit_operation is required")
    else:
        if commit_op.get("action") != "accept_deploy" or commit_op.get("environment_id") != EXPECTED_ENVIRONMENT:
            errors.append("commit_operation must target production accept_deploy")
        if commit_op.get("requires_explicit_user_authorization") is not True:
            errors.append("accept_deploy must require explicit user authorization")

    non_claims = " ".join(str(item).lower() for item in packet.get("explicit_non_claims", []))
    for phrase in ("no accepted commit has been pushed", "no railway change has been staged", "no admin-web service exists", "no deployment has been accepted", "no live-provider uat"):
        if phrase not in non_claims:
            errors.append(f"explicit_non_claims missing: {phrase}")
    return errors


def ready_errors(packet: dict) -> list[str]:
    errors: list[str] = []
    if packet.get("remote_source_ready") is not True:
        errors.append("remote_source_ready must be true before staging deployment")
    if packet.get("explicit_deploy_authorization") is not True:
        errors.append("explicit_deploy_authorization must be true before staging deployment")
    if packet.get("state") != "READY_TO_STAGE":
        errors.append("state must be READY_TO_STAGE before staging deployment")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--packet", type=Path, default=DEFAULT_PACKET)
    parser.add_argument("--ready", action="store_true")
    parser.add_argument("--verify-git-lineage", action="store_true")
    args = parser.parse_args()
    try:
        packet = load(args.packet.resolve())
        errors = validate(packet)
    except RuntimeError as exc:
        errors = [str(exc)]
        packet = {}
    if args.verify_git_lineage:
        try:
            actual_delta = git_changed_files(EXPECTED_REVIEWED_APPLICATION_COMMIT, EXPECTED_COMMIT)
        except (subprocess.CalledProcessError, FileNotFoundError) as exc:
            errors.append(f"cannot verify live Git lineage: {exc}")
        else:
            if actual_delta != EXPECTED_APPLICATION_DELTA:
                errors.append(f"live Git application-to-source delta mismatch: {actual_delta!r}")
    if args.ready:
        errors.extend(ready_errors(packet))
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Deployment packet valid.")
    if args.ready:
        print("Deployment packet is ready to stage.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
