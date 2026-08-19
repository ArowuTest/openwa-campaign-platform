#!/usr/bin/env python3
"""Validate the approved Railway/Hostinger/Meta production split without provisioning it."""
from __future__ import annotations

import argparse
import ipaddress
import json
import os
import re
import sys
from pathlib import Path
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_TOPOLOGY = ROOT / "config/deployment-topology.json"
DEFAULT_HOSTINGER = ROOT / "infrastructure/compose/compose.hostinger-openwa-gateway.yaml"
DEFAULT_RAILWAY = ROOT / "infrastructure/railway/service-contracts.json"
EXPECTED_TRANSPORTS = ["OPENWA/BAILEYS", "OPENWA/WHATSAPP_WEB_JS", "META/CLOUD_API"]
EXPECTED_RAILWAY = {
    "control-api", "audience-worker", "campaign-worker", "export-worker",
    "inbound-governance-worker", "metrics-worker", "platform-governance-worker",
}
EXPECTED_META_CONSUMERS = {"control-api", "campaign-worker"}
EXPECTED_ENGINES = {"BAILEYS", "WHATSAPP_WEB_JS"}
DIGEST = re.compile(r"^[^\s@]+@sha256:[0-9a-fA-F]{64}$")
EXPECTED_GATEWAY_SECRETS = {
    "gateway_command_secret", "gateway_command_secret_previous",
    "gateway_callback_secret", "gateway_runtime_secret",
}


def load_json(path: Path, errors: list[str]) -> dict:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        errors.append(f"cannot read valid JSON {path}: {exc}")
        return {}
    if not isinstance(value, dict):
        errors.append(f"{path} must contain a JSON object")
        return {}
    return value


def is_disallowed_production_hostname(hostname: str) -> bool:
    normalized = hostname.strip().lower().strip("[]")
    if (
        normalized in {"control-api", "openwa-gateway", "localhost", "host.docker.internal"}
        or normalized.endswith(".localhost")
        or normalized.endswith(".docker.internal")
    ):
        return True
    try:
        address = ipaddress.ip_address(normalized)
    except ValueError:
        return False
    return address.is_loopback or address.is_link_local or address.is_unspecified

def validate_topology(data: dict, errors: list[str]) -> None:
    if data.get("schema_version") != 1:
        errors.append("deployment topology schema_version must be 1")
    if data.get("transports") != EXPECTED_TRANSPORTS:
        errors.append("deployment topology must declare the three approved sibling transports")
    railway = data.get("railway", {})
    if set(railway.get("services", [])) != EXPECTED_RAILWAY:
        errors.append("Railway must host exactly the seven backend/control services")
    if railway.get("openwa_gateway_allowed") is not False:
        errors.append("Railway split contract must not place openwa-gateway in the control plane")
    hostinger = data.get("hostinger", {})
    if set(hostinger.get("services", [])) != {"openwa-gateway"}:
        errors.append("Hostinger must host only openwa-gateway services")
    if set(hostinger.get("engines", [])) != EXPECTED_ENGINES:
        errors.append("Hostinger OpenWA engines must be BAILEYS and WHATSAPP_WEB_JS")
    for key in ("database_credentials_allowed", "object_store_credentials_allowed", "meta_credentials_allowed"):
        if hostinger.get(key) is not False:
            errors.append(f"Hostinger gateway contract must set {key}=false")
    meta = data.get("meta", {})
    if meta.get("via_openwa_gateway") is not False:
        errors.append("Meta must remain a direct sibling transport, never routed via OpenWA gateway")
    if set(meta.get("credential_consumers", [])) != EXPECTED_META_CONSUMERS:
        errors.append("Meta credentials must be limited to control-api and campaign-worker")
    boundary = data.get("cross_provider_boundary", {})
    if boundary.get("required_scheme") != "https":
        errors.append("cross-provider control boundary must require HTTPS")
    expected_env = {
        "hostinger_bind_address_env": "GATEWAY_BIND_ADDRESS",
        "hostinger_gateway_url_env": "GATEWAY_INTERNAL_URL",
        "hostinger_control_url_env": "CONTROL_API_INTERNAL_URL",
        "hostinger_callback_url_env": "CONTROL_API_CALLBACK_URL",
        "hostinger_inbound_url_env": "CONTROL_API_INBOUND_URL",
        "hostinger_media_url_env": "MEDIA_DOWNLOAD_BASE_URL",
    }
    for key, expected in expected_env.items():
        if boundary.get(key) != expected:
            errors.append(f"cross-provider boundary {key} must be {expected}")


def validate_railway(data: dict, errors: list[str]) -> None:
    services = data.get("services", {})
    if not isinstance(services, dict) or set(services) != EXPECTED_RAILWAY:
        errors.append("Railway service contract must contain exactly the seven backend/control services")
        return
    if "openwa-gateway" in services or "openwa-gateway" not in set(data.get("forbidden_services", [])):
        errors.append("Railway contract must explicitly exclude openwa-gateway")
    if set(data.get("meta_credential_consumers", [])) != EXPECTED_META_CONSUMERS:
        errors.append("Railway Meta credential consumers must be control-api and campaign-worker only")
    for name, service in services.items():
        dockerfile = ROOT / str(service.get("dockerfile", ""))
        if not dockerfile.is_file():
            errors.append(f"Railway service {name} references missing Dockerfile {dockerfile}")
        port = service.get("port")
        if not isinstance(port, int) or port <= 0:
            errors.append(f"Railway service {name} must declare a positive health port")
        elif dockerfile.is_file() and not re.search(rf"^EXPOSE\s+{port}\s*$", dockerfile.read_text(encoding="utf-8"), re.MULTILINE):
            errors.append(f"Railway service {name} Dockerfile does not EXPOSE declared port {port}")
        if service.get("health_path") != "/readyz":
            errors.append(f"Railway service {name} must use /readyz for readiness")
        public = service.get("public_ingress")
        if public is not (name == "control-api"):
            errors.append(f"only control-api may declare Railway public ingress; invalid service {name}")
        if name == "control-api":
            required_environment = set(service.get("required_environment", []))
            required_public_boundary = {"ALLOWED_NETWORK_CIDRS", "TRUSTED_PROXY_CIDRS", "CLAMAV_ADDRESS"}
            if not required_public_boundary <= required_environment:
                errors.append("public control-api must require network allowlist, trusted proxy and ClamAV environment")
        classes = set(service.get("secret_classes", []))
        if ("meta" in classes) != (name in EXPECTED_META_CONSUMERS):
            errors.append(f"Railway Meta secret placement is invalid for {name}")
    dependencies = data.get("shared_dependencies", {})
    for required in ("postgresql", "redis", "object_storage", "clamav"):
        if required not in dependencies:
            errors.append(f"Railway contract is missing shared dependency {required}")


def compose_service_names(text: str) -> set[str]:
    names: set[str] = set()
    in_services = False
    for line in text.splitlines():
        if line == "services:":
            in_services = True
            continue
        if in_services and line and not line.startswith(" "):
            break
        if in_services:
            match = re.fullmatch(r"  ([a-z0-9][a-z0-9_-]*):\s*", line)
            if match:
                names.add(match.group(1))
    return names


def validate_hostinger(path: Path, errors: list[str]) -> None:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        errors.append(f"cannot read Hostinger manifest {path}: {exc}")
        return
    if compose_service_names(text) != {"openwa-gateway"}:
        errors.append("Hostinger manifest must contain only the openwa-gateway service")
    forbidden = ("DATABASE_URL", "S3_ACCESS", "S3_SECRET", "META_CLOUD_CREDENTIALS")
    for token in forbidden:
        if token in text:
            errors.append(f"Hostinger gateway manifest must not contain {token}")
    if "http://control-api" in text or "http://openwa-gateway" in text:
        errors.append("Hostinger manifest must not use Docker-internal cross-provider hostnames")
    for variable in (
        "GATEWAY_BIND_ADDRESS", "GATEWAY_INTERNAL_URL", "CONTROL_API_INTERNAL_URL", "CONTROL_API_CALLBACK_URL",
        "CONTROL_API_INBOUND_URL", "MEDIA_DOWNLOAD_BASE_URL",
    ):
        if not re.search(rf"{variable}:\s*\$\{{{variable}:\?", text):
            errors.append(f"Hostinger manifest must require externally supplied {variable}")
    if "OPENWA_ENGINE: ${OPENWA_ENGINE:?" not in text:
        errors.append("Hostinger manifest must require an explicit OpenWA engine")
    if "${GATEWAY_BIND_ADDRESS:?" not in text:
        errors.append("Hostinger gateway must bind only to an explicitly approved private address")
    for hardening in ("read_only: true", "no-new-privileges:true", "cap_drop:", "- ALL"):
        if hardening not in text:
            errors.append(f"Hostinger gateway is missing hardening control {hardening}")
    secret_block = text.split("\nsecrets:\n", 1)[1] if "\nsecrets:\n" in text else ""
    declared = set(re.findall(r"^  ([a-z0-9][a-z0-9_]*):\s*\{\s*file:", secret_block, re.MULTILINE))
    if declared != EXPECTED_GATEWAY_SECRETS:
        errors.append("Hostinger gateway must declare only command/callback/runtime secret files")


def validate_resolved_hostinger(topology: dict, errors: list[str]) -> None:
    boundary = topology.get("cross_provider_boundary", {}) if isinstance(topology, dict) else {}
    forbidden_hosts = {str(value).strip().lower() for value in boundary.get("docker_internal_hostnames_forbidden", []) if str(value).strip()}
    forbidden_hosts.add("localhost")
    image = os.getenv("OPENWA_GATEWAY_IMAGE", "").strip()
    if not DIGEST.fullmatch(image):
        errors.append("resolved OPENWA_GATEWAY_IMAGE must be digest-pinned")
    engine = os.getenv("OPENWA_ENGINE", "").strip()
    if engine not in EXPECTED_ENGINES:
        errors.append("resolved OPENWA_ENGINE must be BAILEYS or WHATSAPP_WEB_JS")
    bind = os.getenv("GATEWAY_BIND_ADDRESS", "").strip()
    try:
        address = ipaddress.ip_address(bind)
        if address.is_unspecified or address.is_global:
            errors.append("resolved GATEWAY_BIND_ADDRESS must not be a globally routable address")
    except ValueError:
        errors.append("resolved GATEWAY_BIND_ADDRESS must be an IP address")
    gateway_raw = os.getenv("GATEWAY_INTERNAL_URL", "").strip()
    gateway = urlparse(gateway_raw)
    if not gateway.hostname or gateway.scheme.lower() not in {"http", "https"}:
        errors.append("resolved advertised GATEWAY_INTERNAL_URL must name the approved private gateway endpoint")
    elif is_disallowed_production_hostname(gateway.hostname):
        errors.append("resolved advertised GATEWAY_INTERNAL_URL must not use a Docker-local, loopback, or link-local endpoint")
    elif gateway.scheme.lower() == "http":
        try:
            gateway_ip = ipaddress.ip_address(gateway.hostname)
            bind_ip = ipaddress.ip_address(bind)
            if gateway_ip.is_unspecified or gateway_ip.is_global or gateway_ip != bind_ip:
                errors.append("resolved advertised GATEWAY_INTERNAL_URL over HTTP must use the approved private bind address")
        except ValueError:
            errors.append("resolved advertised GATEWAY_INTERNAL_URL over HTTP must use a private IP address")
    control_host = None
    for key in ("CONTROL_API_INTERNAL_URL", "CONTROL_API_CALLBACK_URL", "CONTROL_API_INBOUND_URL", "MEDIA_DOWNLOAD_BASE_URL"):
        parsed = urlparse(os.getenv(key, "").strip())
        if parsed.scheme.lower() != "https" or not parsed.hostname:
            errors.append(f"resolved {key} must use HTTPS with a hostname")
        elif parsed.hostname.lower() in forbidden_hosts or is_disallowed_production_hostname(parsed.hostname):
            errors.append(f"resolved {key} must not use a Docker-local hostname")
        elif control_host is None:
            control_host = parsed.hostname.lower()
        elif parsed.hostname.lower() != control_host:
            errors.append(f"resolved {key} must use the same approved Railway control hostname")
    allowed = {x.strip().lower() for x in os.getenv("SSRF_ALLOWED_HOSTS", "").split(",") if x.strip()}
    if not control_host or allowed != {control_host}:
        errors.append("resolved SSRF_ALLOWED_HOSTS must contain exactly the Railway control hostname")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--topology", type=Path, default=DEFAULT_TOPOLOGY)
    parser.add_argument("--hostinger-compose", type=Path, default=DEFAULT_HOSTINGER)
    parser.add_argument("--railway-contract", type=Path, default=DEFAULT_RAILWAY)
    parser.add_argument("--resolved", action="store_true", help="validate target Hostinger runtime environment")
    args = parser.parse_args()
    errors: list[str] = []
    topology = load_json(args.topology.resolve(), errors)
    railway = load_json(args.railway_contract.resolve(), errors)
    if topology:
        validate_topology(topology, errors)
    if railway:
        validate_railway(railway, errors)
    validate_hostinger(args.hostinger_compose.resolve(), errors)
    if args.resolved:
        validate_resolved_hostinger(topology, errors)
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print(
        "Split deployment topology valid: 7 Railway backend/control services, "
        "Hostinger OpenWA gateway-only nodes, direct Meta sibling transport."
    )
    if args.resolved:
        print("Resolved Hostinger gateway boundary valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
