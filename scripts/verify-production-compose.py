#!/usr/bin/env python3
"""Fail closed on unsafe production Compose configuration without third-party parsers."""
from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEFAULT = ROOT / "infrastructure/compose/compose.production.yaml"
DEFAULT_SESSION_TOKEN_OVERLAY = ROOT / "infrastructure/compose/compose.production.s3-session-token.yaml"
DIGEST = re.compile(r"^[^\s@]+@sha256:[0-9a-fA-F]{64}$")
VARIABLE = re.compile(r"^\$\{([A-Z0-9_]+):\?[^}]+\}$")
SERVICE = re.compile(r"^  ([a-z0-9][a-z0-9_-]*):\s*$")
KEY_VALUE = re.compile(r"^      ([A-Z0-9_]+):\s*(.*?)\s*$")
SENSITIVE_SUFFIXES = ("_SECRET", "_PASSWORD", "_TOKEN", "_KEY", "_DATABASE_URL")
OBJECT_STORE_CONSUMERS = {"control-api", "audience-worker", "export-worker", "platform-governance-worker"}


def service_blocks(text: str) -> dict[str, list[str]]:
    in_services = False
    current: str | None = None
    result: dict[str, list[str]] = {}
    for line in text.splitlines():
        if line == "services:":
            in_services = True
            continue
        if in_services and line and not line.startswith(" "):
            break
        if not in_services:
            continue
        match = SERVICE.match(line)
        if match:
            current = match.group(1)
            result[current] = []
            continue
        if current is not None:
            result[current].append(line)
    return result


def clean_scalar(value: str) -> str:
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        return value[1:-1]
    return value


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--compose", type=Path, default=DEFAULT)
    parser.add_argument("--resolved", action="store_true", help="require image variables in the current environment to resolve to digests")
    args = parser.parse_args()
    text = args.compose.resolve().read_text(encoding="utf-8")
    errors: list[str] = []
    services = service_blocks(text)
    if not services:
        errors.append("production compose must define services")

    for name, lines in services.items():
        block = "\n".join(lines)
        if re.search(r"^    build:\s*", block, re.MULTILINE):
            errors.append(f"service {name} must use a prebuilt immutable image, not build")
        image_match = re.search(r"^    image:\s*(.*?)\s*$", block, re.MULTILINE)
        image = clean_scalar(image_match.group(1)) if image_match else ""
        variable = VARIABLE.fullmatch(image)
        if DIGEST.fullmatch(image):
            pass
        elif variable:
            env_name = variable.group(1)
            if args.resolved and not DIGEST.fullmatch(os.getenv(env_name, "").strip()):
                errors.append(f"{env_name} for service {name} must resolve to name@sha256:<64 hex>")
        else:
            errors.append(f"service {name} image must be digest-pinned or a required digest variable")

        inherited_hardening = "<<: *service-defaults" in block or "<<: *go-worker-defaults" in block
        if not inherited_hardening and "read_only: true" not in block:
            errors.append(f"service {name} must use a read-only root filesystem")
        if not inherited_hardening and "no-new-privileges:true" not in block:
            errors.append(f"service {name} must set no-new-privileges:true")
        if re.search(r"^    privileged:\s*true\s*$", block, re.MULTILINE):
            errors.append(f"service {name} must not be privileged")
        if name not in OBJECT_STORE_CONSUMERS and ("S3_" in block or "s3_access_key_id" in block or "s3_secret_access_key" in block):
            errors.append(f"service {name} must not receive object-store credentials")

        environment: dict[str, str] = {}
        in_environment = False
        for line in lines:
            if line == "    environment:":
                in_environment = True
                continue
            if in_environment and line.startswith("    ") and not line.startswith("      "):
                in_environment = False
            if in_environment:
                match = KEY_VALUE.match(line)
                if match:
                    environment[match.group(1)] = clean_scalar(match.group(2))
        runtime_env = environment.get("APP_ENV", environment.get("NODE_ENV", "")).lower()
        if not runtime_env and "<<: *go-worker-environment" in block:
            runtime_env = "production"
        if runtime_env != "production":
            errors.append(f"service {name} must explicitly run in production mode")
        for key, raw in environment.items():
            if key.endswith("_FILE"):
                continue
            if key == "DATABASE_URL" or any(key.endswith(suffix) for suffix in SENSITIVE_SUFFIXES):
                if raw and not raw.startswith("${"):
                    errors.append(f"service {name} embeds sensitive value {key}; use a mounted *_FILE secret")

    if "S3_SESSION_TOKEN_FILE" in text or re.search(r"^  s3_session_token:", text, re.MULTILINE):
        errors.append("optional S3 session token belongs in compose.production.s3-session-token.yaml, not the base stack")
    if not re.search(r"^secrets:\s*$", text, re.MULTILINE):
        errors.append("production compose must define mounted secrets")
    secret_count = len(re.findall(r"^  [a-z0-9][a-z0-9_]*:\s*\{\s*file:", text, re.MULTILINE))
    if secret_count == 0:
        errors.append("production compose must provide secret file definitions")

    overlay = DEFAULT_SESSION_TOKEN_OVERLAY.read_text(encoding="utf-8") if DEFAULT_SESSION_TOKEN_OVERLAY.exists() else ""
    for service in sorted(OBJECT_STORE_CONSUMERS):
        if not re.search(rf"^  {re.escape(service)}:\s*$", overlay, re.MULTILINE):
            errors.append(f"session-token overlay must include object-store consumer {service}")
    if "S3_SESSION_TOKEN_FILE: /run/secrets/s3_session_token" not in overlay or not re.search(r"^  s3_session_token:\s*$", overlay, re.MULTILINE):
        errors.append("session-token overlay must mount the optional S3 token as a file secret")
    for service in set(services) - OBJECT_STORE_CONSUMERS:
        pattern = rf"^  {re.escape(service)}:\s*$.*?(?=^  [a-z0-9][a-z0-9_-]*:\s*$|^secrets:|\Z)"
        match = re.search(pattern, overlay, re.MULTILINE | re.DOTALL)
        if match and "s3_session_token" in match.group(0):
            errors.append(f"session-token overlay must not grant S3 credentials to {service}")

    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print(f"Production compose security structure valid: {len(services)} services, {secret_count} secrets.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
