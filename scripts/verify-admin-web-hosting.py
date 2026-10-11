#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import re
import sys
from pathlib import Path
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_CONTRACT = ROOT / "infrastructure/railway/admin-web-service-contract.json"
DEFAULT_OVERLAY = ROOT / "infrastructure/compose/compose.production.admin-web.yaml"
DEFAULT_TOPOLOGY = ROOT / "config/deployment-topology.json"
DEFAULT_DOCKERFILE = ROOT / "infrastructure/docker/admin-web.Dockerfile"
DEFAULT_PLAN = ROOT / "infrastructure/railway/production-admin-web-plan-2026-10-05.json"
DEFAULT_PROXY = ROOT / "apps/admin-web/lib/control-api-proxy.ts"
DEFAULT_BROWSER_API = ROOT / "apps/admin-web/lib/api.ts"
DEFAULT_ROUTE = ROOT / "apps/admin-web/app/api/[...path]/route.ts"
DEFAULT_HEALTH = ROOT / "apps/admin-web/app/healthz/route.ts"
DEFAULT_NEXT_CONFIG = ROOT / "apps/admin-web/next.config.mjs"

DIGEST = re.compile(r"^[^\s@]+@sha256:[0-9a-fA-F]{64}$")


def load_json(path: Path) -> dict:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"cannot load {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise RuntimeError(f"{path} must contain a JSON object")
    return value


def validate_private_control_url(raw: str) -> str | None:
    try:
        parsed = urlparse(raw)
    except ValueError:
        return "ADMIN_WEB_CONTROL_API_URL must be a valid URL"
    if parsed.scheme not in {"http", "https"}:
        return "ADMIN_WEB_CONTROL_API_URL must use http or https"
    if parsed.username or parsed.password:
        return "ADMIN_WEB_CONTROL_API_URL must not contain credentials"
    if parsed.path not in {"", "/"} or parsed.query or parsed.fragment:
        return "ADMIN_WEB_CONTROL_API_URL must be an origin without path, query or fragment"
    host = (parsed.hostname or "").rstrip(".").lower()
    if not host.endswith(".railway.internal"):
        return "ADMIN_WEB_CONTROL_API_URL must use Railway private DNS (*.railway.internal)"
    try:
        if parsed.port is not None and not (1 <= parsed.port <= 65535):
            return "ADMIN_WEB_CONTROL_API_URL port is invalid"
    except ValueError:
        return "ADMIN_WEB_CONTROL_API_URL port is invalid"
    return None


def validate(contract_path: Path, overlay_path: Path, topology_path: Path, dockerfile_path: Path, plan_path: Path) -> list[str]:
    errors: list[str] = []
    contract = load_json(contract_path)
    topology = load_json(topology_path)
    plan = load_json(plan_path)

    if contract.get("schema_version") != 1 or contract.get("provider") != "railway":
        errors.append("admin-web service contract must use Railway schema_version 1")
    service = contract.get("service")
    if not isinstance(service, dict):
        return errors + ["admin-web service contract must contain service object"]

    expected = {
        "name": "admin-web",
        "dockerfile": "infrastructure/docker/admin-web.Dockerfile",
        "port": 3000,
        "health_path": "/healthz",
        "public_ingress": True,
        "tls_required": True,
        "required_environment": ["ADMIN_WEB_CONTROL_API_URL"],
        "recommended_environment_values": {
            "ADMIN_WEB_CONTROL_API_URL": "http://${{control-api.RAILWAY_PRIVATE_DOMAIN}}:${{control-api.PORT}}"
        },
        "secret_classes": [],
        "upstream_service": "control-api",
        "upstream_network": "railway-private",
        "browser_api_prefix": "/api",
        "network_admission_owner": "control-api",
        "forwarded_client_context": "sanitized-railway-edge-x-real-ip",
    }
    for key, value in expected.items():
        if service.get(key) != value:
            errors.append(f"admin-web service contract {key} must be {value!r}")
    if service.get("secret_classes"):
        errors.append("admin-web must not receive secret classes")

    forbidden = set(contract.get("forbidden_environment", []))
    if "CONTROL_API_INTERNAL_URL" not in forbidden:
        errors.append("admin-web contract must forbid CONTROL_API_INTERNAL_URL reuse")
    if "NEXT_PUBLIC_CONTROL_API_BASE" not in forbidden:
        errors.append("admin-web contract must forbid public browser API base overrides")

    frontend = topology.get("railway_frontend")
    if not isinstance(frontend, dict):
        errors.append("deployment topology must declare railway_frontend")
    else:
        topology_expected = {
            "service": "admin-web",
            "public_ingress": True,
            "tls_required": True,
            "control_api_service": "control-api",
            "control_api_upstream_env": "ADMIN_WEB_CONTROL_API_URL",
            "control_api_upstream_reference": "http://${{control-api.RAILWAY_PRIVATE_DOMAIN}}:${{control-api.PORT}}",
            "browser_api_prefix": "/api",
            "health_path": "/healthz",
            "network_admission_owner": "control-api",
            "forwarded_client_context": "sanitized-railway-edge-x-real-ip",
        }
        for key, value in topology_expected.items():
            if frontend.get(key) != value:
                errors.append(f"railway_frontend {key} must be {value!r}")

    if plan.get("schema_version") != 1 or plan.get("evidence_type") != "railway-admin-web-hosting-plan":
        errors.append("admin-web hosting plan must use schema_version 1 and railway-admin-web-hosting-plan evidence type")
    plan_scope = str(plan.get("scope", "")).lower()
    for phrase in ("no railway service was created", "no variable was set", "no deployment was triggered", "no release gate"):
        if phrase not in plan_scope:
            errors.append(f"admin-web hosting plan scope must include {phrase}")
    plan_service = plan.get("service")
    if not isinstance(plan_service, dict):
        errors.append("admin-web hosting plan must contain service object")
    else:
        plan_expected = {
            "name": "admin-web",
            "service_id": None,
            "create_status": "PENDING_ACCEPTED_SOURCE_AND_EXPLICIT_AUTHORIZATION",
            "image_digest": None,
            "port": 3000,
            "health_path": "/healthz",
            "public_ingress": True,
            "tls_required": True,
            "required_environment_names": ["ADMIN_WEB_CONTROL_API_URL"],
            "recommended_environment_values": {
                "ADMIN_WEB_CONTROL_API_URL": "http://${{control-api.RAILWAY_PRIVATE_DOMAIN}}:${{control-api.PORT}}"
            },
            "secret_classes": [],
            "custom_domain": None,
            "deploy_status": "PENDING_ACCEPTED_SOURCE_COMMIT_PUSH_AND_EXPLICIT_DEPLOY_AUTHORIZATION",
        }
        for key, value in plan_expected.items():
            if plan_service.get(key) != value:
                errors.append(f"admin-web hosting plan service {key} must be {value!r}")

    local_candidate = plan.get("local_candidate")
    if not isinstance(local_candidate, dict):
        errors.append("admin-web hosting plan must contain local_candidate object")
    else:
        if local_candidate.get("branch") != "work/champion-hosting-integration-20261005":
            errors.append("admin-web hosting plan must identify the combined champion+hosting branch")
        for key in ("committed", "pushed", "deployed"):
            if local_candidate.get(key) is not False:
                errors.append(f"admin-web hosting plan local_candidate {key} must remain false before acceptance/deployment")
        if local_candidate.get("full_operator_console") is not True:
            errors.append("admin-web hosting plan combined candidate must contain the full operator console")
        if local_candidate.get("static_page_routes") != 14:
            errors.append("admin-web hosting plan must record the 14 champion page routes")
        if local_candidate.get("runtime_routes") != ["/api/[...path]", "/healthz"]:
            errors.append("admin-web hosting plan must record the hardened API proxy and health routes")
        if local_candidate.get("local_image_registry_digest") is not None:
            errors.append("admin-web hosting plan must not claim a registry image digest before push")

    deployment_preconditions = plan.get("deployment_preconditions")
    expected_preconditions = {
        "accepted_source_committed": False,
        "accepted_source_pushed": False,
        "control_api_source_matches_accepted_commit": False,
        "trusted_proxy_current_reconfirmed": False,
        "actionable_staged_changes_absent": True,
        "explicit_deploy_authorization": False,
    }
    if deployment_preconditions != expected_preconditions:
        errors.append(
            "admin-web hosting plan deployment_preconditions must reflect the current pre-deployment boundary"
        )

    railway_observation = plan.get("railway_observation")
    if not isinstance(railway_observation, dict):
        errors.append("admin-web hosting plan must contain read-only Railway observation")
    else:
        if railway_observation.get("staged_changes") is not None:
            errors.append("admin-web hosting plan must record no staged Railway changes at the observation boundary")
        if railway_observation.get("admin_web_present") is not False:
            errors.append("admin-web hosting plan must not claim admin-web exists in Railway")
        control = railway_observation.get("control_api")
        if not isinstance(control, dict):
            errors.append("admin-web hosting plan must record control-api observation")
        else:
            if control.get("state") != "live":
                errors.append("admin-web hosting plan control-api observation must remain live")
            latest = control.get("latest_deployment")
            if not isinstance(latest, dict) or latest.get("status") != "SUCCESS":
                errors.append("admin-web hosting plan control-api deployment must be observed SUCCESS")
            source = control.get("source")
            if not isinstance(source, dict) or not re.fullmatch(r"[0-9a-f]{40}", str(source.get("commit_sha", ""))):
                errors.append("admin-web hosting plan must record the observed 40-character control-api source commit")
            if control.get("railway_private_domain_variable_available") is not True or control.get("railway_port_variable_available") is not True:
                errors.append("admin-web hosting plan must record Railway private-domain and port variable availability")
            if control.get("trusted_proxy_covers_railway_private_ula") is not True:
                errors.append("admin-web hosting plan must prove control-api trusts Railway private ULA proxy peers")
            if control.get("trusted_proxy_value_disclosed") is not False:
                errors.append("admin-web hosting plan must not persist the live trusted-proxy CIDR value")

    provenance = plan.get("merge_provenance")
    if not isinstance(provenance, dict):
        errors.append("admin-web hosting plan must contain merge_provenance object")
    else:
        parents = {
            "champion_parent": (
                "work/champion-integration-20261005-v3",
                "4d17a10cd130201a5a7473202c689d8ac3fc845dd5f3546ceb31f0a1629d7c8b",
            ),
            "hosting_parent": (
                "work/admin-web-hosting-20261005",
                "2be3e40b1b529101afb5c51735398df4948d14416c2f7b90af6e84eebbb4bb24",
            ),
        }
        for key, (branch, fingerprint) in parents.items():
            parent = provenance.get(key)
            if not isinstance(parent, dict):
                errors.append(f"admin-web hosting plan merge provenance missing {key}")
                continue
            if parent.get("branch") != branch:
                errors.append(f"admin-web hosting plan merge provenance {key} branch drift")
            if parent.get("base_head") != "1511b73f6fff123dca49835fb2a0ddcb9d3093fc":
                errors.append(f"admin-web hosting plan merge provenance {key} base drift")
            if parent.get("candidate_fingerprint_sha256") != fingerprint:
                errors.append(f"admin-web hosting plan merge provenance {key} fingerprint drift")
        if provenance.get("parent_worktrees_modified") is not False:
            errors.append("admin-web hosting plan must record frozen parent worktrees as unmodified")
        method = str(provenance.get("merge_method", "")).lower()
        for phrase in ("replayed byte-for-byte", "api.ts merged surgically"):
            if phrase not in method:
                errors.append(f"admin-web hosting plan merge method must document {phrase}")

    non_claims = " ".join(str(item).lower() for item in plan.get("explicit_non_claims", []))
    for phrase in (
        "admin-web railway service does not yet exist",
        "no frontend registry image digest",
        "no public frontend domain",
        "no production admin-web variable value",
        "no browser smoke test has run against railway admin-web",
        "no source commit contains this combined candidate",
        "no push or deployment",
        "no release gate",
    ):
        if phrase not in non_claims:
            errors.append(f"admin-web hosting plan non-claims must include {phrase}")

    docker_text = dockerfile_path.read_text(encoding="utf-8")
    from_versions = re.findall(r"^FROM node:(\d+)\.(\d+)-alpine", docker_text, re.MULTILINE)
    if not from_versions or any((int(major), int(minor)) < (22, 19) for major, minor in from_versions):
        errors.append("admin-web Dockerfile must use Node 22.19-alpine or newer")
    for required in ("ENV NODE_ENV=production", "ENV PORT=3000", "USER campaign", "EXPOSE 3000", 'CMD ["node", "server.js"]'):
        if required not in docker_text:
            errors.append(f"admin-web Dockerfile missing {required}")

    overlay = overlay_path.read_text(encoding="utf-8")
    for required in (
        "  admin-web:",
        "${ADMIN_WEB_IMAGE:?",
        "NODE_ENV: production",
        "ADMIN_WEB_CONTROL_API_URL: ${ADMIN_WEB_CONTROL_API_URL:?",
        "http://127.0.0.1:3000/healthz",
        "read_only: true",
        "no-new-privileges:true",
        "cap_drop:",
        "- ALL",
    ):
        if required not in overlay:
            errors.append(f"admin-web production overlay missing {required}")
    for forbidden_text in ("CONTROL_API_INTERNAL_URL", "secrets:", "_SECRET", "_PASSWORD", "_TOKEN", "ports:"):
        if forbidden_text in overlay:
            errors.append(f"admin-web production overlay must not contain {forbidden_text}")

    proxy = DEFAULT_PROXY.read_text(encoding="utf-8") if DEFAULT_PROXY.is_file() else ""
    browser_api = DEFAULT_BROWSER_API.read_text(encoding="utf-8") if DEFAULT_BROWSER_API.is_file() else ""
    route = DEFAULT_ROUTE.read_text(encoding="utf-8") if DEFAULT_ROUTE.is_file() else ""
    health = DEFAULT_HEALTH.read_text(encoding="utf-8") if DEFAULT_HEALTH.is_file() else ""
    next_config = DEFAULT_NEXT_CONFIG.read_text(encoding="utf-8") if DEFAULT_NEXT_CONFIG.is_file() else ""
    if "NEXT_PUBLIC_CONTROL_API_BASE" in browser_api or "browserAPIBase = '/api'" not in browser_api:
        errors.append("admin-web browser API must be fixed to same-origin /api with no public override")
    if "ADMIN_WEB_CONTROL_API_URL" not in proxy or "railway.internal" not in proxy:
        errors.append("admin-web runtime proxy must validate ADMIN_WEB_CONTROL_API_URL against Railway private DNS")
    for required_proxy_contract in ("X-Railway-Edge", "X-Real-IP", "NETWORK_CONTEXT_UNAVAILABLE"):
        if required_proxy_contract not in proxy:
            errors.append(f"admin-web runtime proxy must preserve sanitized Railway network context: {required_proxy_contract}")
    if "proxyControlAPI" not in route:
        errors.append("admin-web /api route must delegate to runtime control API proxy")
    if "service: 'admin-web'" not in health:
        errors.append("admin-web /healthz route must identify the service")
    if "CONTROL_API_INTERNAL_URL" in next_config or "rewrites()" in next_config:
        errors.append("admin-web Next config must not bake CONTROL_API_INTERNAL_URL rewrites")

    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--contract", type=Path, default=DEFAULT_CONTRACT)
    parser.add_argument("--overlay", type=Path, default=DEFAULT_OVERLAY)
    parser.add_argument("--topology", type=Path, default=DEFAULT_TOPOLOGY)
    parser.add_argument("--dockerfile", type=Path, default=DEFAULT_DOCKERFILE)
    parser.add_argument("--plan", type=Path, default=DEFAULT_PLAN)
    parser.add_argument("--resolved", action="store_true")
    parser.add_argument("--deployment-ready", action="store_true")
    args = parser.parse_args()

    try:
        errors = validate(
            args.contract.resolve(),
            args.overlay.resolve(),
            args.topology.resolve(),
            args.dockerfile.resolve(),
            args.plan.resolve(),
        )
    except (OSError, RuntimeError) as exc:
        errors = [str(exc)]

    if args.resolved or args.deployment_ready:
        image = os.getenv("ADMIN_WEB_IMAGE", "").strip()
        if not DIGEST.fullmatch(image):
            errors.append("ADMIN_WEB_IMAGE must resolve to name@sha256:<64 hex>")
        upstream = os.getenv("ADMIN_WEB_CONTROL_API_URL", "").strip()
        issue = validate_private_control_url(upstream)
        if issue:
            errors.append(issue)

    if args.deployment_ready:
        accepted = os.getenv("ACCEPTED_SOURCE_COMMIT", "").strip()
        deployed = os.getenv("CONTROL_API_DEPLOYED_COMMIT", "").strip()
        if not re.fullmatch(r"[0-9a-fA-F]{40}", accepted):
            errors.append("ACCEPTED_SOURCE_COMMIT must be the accepted 40-character source commit")
        if not re.fullmatch(r"[0-9a-fA-F]{40}", deployed):
            errors.append("CONTROL_API_DEPLOYED_COMMIT must be the deployed 40-character control-api commit")
        elif accepted and deployed.lower() != accepted.lower():
            errors.append("CONTROL_API_DEPLOYED_COMMIT must match ACCEPTED_SOURCE_COMMIT before admin-web deployment")
        for name in (
            "TRUSTED_PROXY_CURRENT_RECONFIRMED",
            "ACTIONABLE_STAGED_CHANGES_ABSENT",
            "DEPLOY_AUTHORIZED",
        ):
            if os.getenv(name, "").strip().lower() != "true":
                errors.append(f"{name} must be explicitly true before admin-web deployment")

    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1

    print("Admin-web hosting contract valid.")
    if args.resolved or args.deployment_ready:
        print("Resolved admin-web hosting boundary valid.")
    if args.deployment_ready:
        print("Admin-web deployment preconditions valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
