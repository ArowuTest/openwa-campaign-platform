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


def normalize_hostname(hostname: str) -> str:
    return hostname.strip().lower().strip("[]").rstrip(".")


def is_disallowed_production_hostname(hostname: str) -> bool:
    normalized = normalize_hostname(hostname)
    if (
        normalized in {"control-api", "openwa-gateway", "localhost", "host.docker.internal"}
        or normalized.endswith(".localhost")
        or normalized.endswith(".docker.internal")
    ):
        return True
    return is_ip_like_hostname(normalized)


def is_ip_like_hostname(hostname: str) -> bool:
    normalized = normalize_hostname(hostname)
    if not normalized or "%" in normalized:
        return bool(normalized)
    try:
        ipaddress.ip_address(normalized)
        return True
    except ValueError:
        pass
    parts = normalized.split(".")
    if not 1 <= len(parts) <= 4 or any(not part for part in parts):
        return False
    values: list[int] = []
    try:
        for part in parts:
            if part.lower().startswith("0x"):
                values.append(int(part[2:], 16))
            elif len(part) > 1 and part.startswith("0"):
                values.append(int(part[1:] or "0", 8))
            else:
                values.append(int(part, 10))
    except ValueError:
        return False
    if len(parts) == 1:
        return values[0] <= 0xFFFFFFFF
    if any(value > 0xFF for value in values[:-1]):
        return False
    remaining_bits = 8 * (5 - len(parts))
    return values[-1] < (1 << remaining_bits)


def _literal_ip_is_not_private(hostname: str) -> bool:
    try:
        address = ipaddress.ip_address(normalize_hostname(hostname))
    except ValueError:
        return False
    if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped is not None:
        address = address.ipv4_mapped
    if isinstance(address, ipaddress.IPv4Address):
        return not (address in ipaddress.ip_network("10.0.0.0/8") or address in ipaddress.ip_network("172.16.0.0/12") or address in ipaddress.ip_network("192.168.0.0/16"))
    return not address in ipaddress.ip_network("fc00::/7")

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
    hostinger_contracts = hostinger.get("service_contracts", {})
    if not isinstance(hostinger_contracts, dict) or set(hostinger_contracts) != {"openwa-gateway"}:
        errors.append("Hostinger topology must declare exactly the openwa-gateway service contract")
    else:
        gateway_contract = hostinger_contracts["openwa-gateway"]
        if (
            not isinstance(gateway_contract, dict)
            or not isinstance(gateway_contract.get("port"), int)
            or gateway_contract.get("port", 0) <= 0
            or not str(gateway_contract.get("health_path", "")).startswith("/")
        ):
            errors.append("Hostinger openwa-gateway service contract must declare a positive port and health path")
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
        "gateway_runtime_allowed_hosts_env": "GATEWAY_RUNTIME_ALLOWED_HOSTS",
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
        if public is True and service.get("tls_required") is not True:
            errors.append(f"public Railway service {name} must require TLS termination")
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


def compose_port_mappings(text: str, service: str) -> list[str]:
    match = re.search(
        rf"^  {re.escape(service)}:\s*$\n(.*?)(?=^  [a-z0-9][a-z0-9_-]*:\s*$|^[^ ]|\Z)",
        text,
        re.MULTILINE | re.DOTALL,
    )
    if not match:
        return []
    block = match.group(1)
    ports = re.search(r"^    ports:\s*$\n((?:      .*?(?:\n|$))*)", block, re.MULTILINE)
    if not ports:
        return []
    mappings: list[str] = []
    for raw in re.findall(r"^      -\s*(.*?)\s*$", ports.group(1), re.MULTILINE):
        value = raw.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        mappings.append(value)
    return mappings


def validate_hostinger(path: Path, topology: dict, errors: list[str]) -> None:
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
    if not re.search(r"^\s+NODE_ENV:\s*production\s*$", text, re.MULTILINE):
        errors.append("Hostinger gateway must run with NODE_ENV: production so production hardening cannot be bypassed")
    for variable in (
        "GATEWAY_BIND_ADDRESS", "GATEWAY_INTERNAL_URL", "GATEWAY_RUNTIME_ALLOWED_HOSTS", "CONTROL_API_INTERNAL_URL", "CONTROL_API_CALLBACK_URL",
        "CONTROL_API_INBOUND_URL", "MEDIA_DOWNLOAD_BASE_URL", "GATEWAY_CAPACITY", "OPENWA_MAX_CONCURRENT_SESSIONS",
    ):
        if not re.search(rf"{variable}:\s*\$\{{{variable}:\?", text):
            errors.append(f"Hostinger manifest must require externally supplied {variable}")
    if "OPENWA_ENGINE: ${OPENWA_ENGINE:?" not in text:
        errors.append("Hostinger manifest must require an explicit OpenWA engine")
    if "GATEWAY_RESOURCE_FILESYSTEM_PATH: /app/session-data" not in text:
        errors.append("Hostinger gateway resource filesystem must measure /app/session-data")
    if "${GATEWAY_BIND_ADDRESS:?" not in text:
        errors.append("Hostinger gateway must bind only to an explicitly approved private address")
    hostinger = topology.get("hostinger", {}) if isinstance(topology, dict) else {}
    contracts = hostinger.get("service_contracts", {}) if isinstance(hostinger, dict) else {}
    gateway_contract = contracts.get("openwa-gateway", {}) if isinstance(contracts, dict) else {}
    port = gateway_contract.get("port") if isinstance(gateway_contract, dict) else None
    health_path = gateway_contract.get("health_path") if isinstance(gateway_contract, dict) else None
    if not isinstance(port, int) or port <= 0 or not isinstance(health_path, str) or not health_path.startswith("/"):
        errors.append("Hostinger manifest cannot be verified without a valid topology service contract")
        return
    expected_port_mapping = f"${{GATEWAY_BIND_ADDRESS:?approved private bind address required}}:{port}:{port}"
    mappings = compose_port_mappings(text, "openwa-gateway")
    if mappings != [expected_port_mapping]:
        errors.append("Hostinger gateway port publishing must use exactly the approved private GATEWAY_BIND_ADDRESS mapping")
    if f"PORT: {port}" not in text:
        errors.append("Hostinger gateway PORT must match the authoritative topology service contract")
    expected_health_url = f"http://127.0.0.1:{port}{health_path}"
    if expected_health_url not in text:
        errors.append("Hostinger gateway healthcheck must match the authoritative topology service contract")
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
        elif address.is_loopback or address.is_link_local:
            errors.append("resolved GATEWAY_BIND_ADDRESS must not be loopback or link-local")
    except ValueError:
        errors.append("resolved GATEWAY_BIND_ADDRESS must be an IP address")
    runtime_override = os.getenv("GATEWAY_RUNTIME_REGISTRATION_URL", "").strip()
    if runtime_override:
        errors.append("resolved GATEWAY_RUNTIME_REGISTRATION_URL must be unset in production; derive runtime registration from CONTROL_API_INTERNAL_URL")
    capacity_values: dict[str, int] = {}
    for key in ("GATEWAY_CAPACITY", "OPENWA_MAX_CONCURRENT_SESSIONS"):
        raw = os.getenv(key, "").strip()
        if not re.fullmatch(r"\d+", raw):
            errors.append(f"resolved {key} capacity must be an explicit integer between 1 and 1000")
            continue
        value = int(raw)
        if value < 1 or value > 1000:
            errors.append(f"resolved {key} capacity must be between 1 and 1000")
            continue
        capacity_values[key] = value
    if len(capacity_values) == 2 and capacity_values["GATEWAY_CAPACITY"] != capacity_values["OPENWA_MAX_CONCURRENT_SESSIONS"]:
        errors.append("resolved gateway capacity must match OPENWA_MAX_CONCURRENT_SESSIONS")
    allowed_gateway_hosts = {normalize_hostname(value) for value in os.getenv("GATEWAY_RUNTIME_ALLOWED_HOSTS", "").split(",") if value.strip()}
    if not allowed_gateway_hosts:
        errors.append("resolved GATEWAY_RUNTIME_ALLOWED_HOSTS must declare governed gateway runtime host authority")
    elif any(is_disallowed_production_hostname(value) or _literal_ip_is_not_private(value) for value in allowed_gateway_hosts):
        errors.append("resolved GATEWAY_RUNTIME_ALLOWED_HOSTS must not contain Docker-local, loopback, link-local, or globally routable IP hosts")
    gateway_raw = os.getenv("GATEWAY_INTERNAL_URL", "").strip()
    gateway = urlparse(gateway_raw)
    gateway_host = normalize_hostname(gateway.hostname) if gateway.hostname else ""
    try:
        gateway_port_valid = gateway.port is None or 1 <= gateway.port <= 65535
    except ValueError:
        gateway_port_valid = False
    if not gateway_port_valid:
        errors.append("resolved GATEWAY_INTERNAL_URL must use a valid port between 1 and 65535")
    elif gateway.username is not None or gateway.password is not None or gateway.path not in {"", "/"} or gateway.params or gateway.query or gateway.fragment:
        errors.append("resolved GATEWAY_INTERNAL_URL must contain only HTTPS scheme and governed host authority")
    elif not gateway.hostname or gateway.scheme.lower() != "https":
        errors.append("resolved advertised GATEWAY_INTERNAL_URL must use HTTPS and name an approved gateway endpoint")
    elif is_disallowed_production_hostname(gateway.hostname):
        errors.append("resolved advertised GATEWAY_INTERNAL_URL must not use a Docker-local, loopback, or link-local endpoint")
    elif _literal_ip_is_not_private(gateway.hostname):
        errors.append("resolved advertised GATEWAY_INTERNAL_URL literal IP must use an approved private address")
    elif gateway_host not in allowed_gateway_hosts:
        errors.append("resolved advertised GATEWAY_INTERNAL_URL hostname must be present in GATEWAY_RUNTIME_ALLOWED_HOSTS")
    control_host = None
    for key in ("CONTROL_API_INTERNAL_URL", "CONTROL_API_CALLBACK_URL", "CONTROL_API_INBOUND_URL", "MEDIA_DOWNLOAD_BASE_URL"):
        parsed = urlparse(os.getenv(key, "").strip())
        try:
            port_valid = parsed.port is None or 1 <= parsed.port <= 65535
        except ValueError:
            port_valid = False
        authority_only = key == "CONTROL_API_INTERNAL_URL"
        invalid_components = (
            parsed.username is not None or parsed.password is not None or parsed.fragment or not port_valid or
            (authority_only and (parsed.path not in {"", "/"} or parsed.params or parsed.query))
        )
        if parsed.scheme.lower() != "https" or not parsed.hostname:
            errors.append(f"resolved {key} must use HTTPS with a hostname")
        elif invalid_components:
            boundary = "an authority-only control origin" if authority_only else "a credential-free endpoint with a valid port"
            errors.append(f"resolved {key} must use {boundary}")
        elif normalize_hostname(parsed.hostname) in forbidden_hosts or is_disallowed_production_hostname(parsed.hostname):
            errors.append(f"resolved {key} must not use a Docker-local hostname")
        elif control_host is None:
            control_host = normalize_hostname(parsed.hostname)
        elif normalize_hostname(parsed.hostname) != control_host:
            errors.append(f"resolved {key} must use the same approved Railway control hostname")
    if control_host and (control_host in allowed_gateway_hosts or gateway_host == control_host):
        errors.append("resolved gateway authority must not contain or advertise the Railway control hostname")
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
    validate_hostinger(args.hostinger_compose.resolve(), topology, errors)
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
