#!/usr/bin/env python3
"""Generate a deterministic CycloneDX source/dependency SBOM."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import uuid
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
DIGEST = re.compile(r"^[^\s@]+@sha256:[0-9a-fA-F]{64}$")
VARIABLE = re.compile(r"^\$\{([A-Z0-9_]+):\?[^}]+\}$")


def component_key(component: dict[str, Any]) -> tuple[str, str, str]:
    return str(component.get("type", "")), str(component.get("name", "")), str(component.get("version", ""))


def npm_components(path: Path, scope: str) -> list[dict[str, Any]]:
    data = json.loads(path.read_text(encoding="utf-8"))
    result: list[dict[str, Any]] = []
    for category in ("dependencies", "devDependencies"):
        for name, version in sorted((data.get(category) or {}).items()):
            clean = str(version).lstrip("^~>=< ")
            result.append({
                "type": "library", "name": name, "version": clean,
                "purl": f"pkg:npm/{name.replace('@', '%40')}@{clean}",
                "scope": "required" if category == "dependencies" else "optional",
                "properties": [{"name": "campaign-platform:manifest", "value": scope}],
            })
    return result


def go_components() -> list[dict[str, Any]]:
    output = subprocess.check_output(["go", "list", "-m", "-json", "all"], cwd=ROOT, text=True)
    decoder = json.JSONDecoder()
    index = 0
    result: list[dict[str, Any]] = []
    while index < len(output):
        while index < len(output) and output[index].isspace():
            index += 1
        if index >= len(output):
            break
        module, index = decoder.raw_decode(output, index)
        if module.get("Main"):
            continue
        path = str(module.get("Path", ""))
        version = str(module.get("Version", "unknown"))
        result.append({"type": "library", "name": path, "version": version, "purl": f"pkg:golang/{path}@{version}"})
    return result



def native_components() -> list[dict[str, Any]]:
    result: list[dict[str, Any]] = []
    try:
        version = subprocess.check_output(
            ["pkg-config", "--modversion", "libpq"], cwd=ROOT, text=True, stderr=subprocess.DEVNULL
        ).strip()
    except (FileNotFoundError, subprocess.CalledProcessError):
        version = "unresolved"
    result.append({
        "type": "library",
        "name": "libpq",
        "version": version or "unresolved",
        "purl": f"pkg:generic/libpq@{version or 'unresolved'}",
        "properties": [
            {"name": "campaign-platform:linkage", "value": "native-runtime"},
            {"name": "campaign-platform:purpose", "value": "PostgreSQL TLS/SCRAM client"},
        ],
    })
    return result

def image_components(compose_path: Path, strict: bool) -> list[dict[str, Any]]:
    text = compose_path.read_text(encoding="utf-8")
    result: list[dict[str, Any]] = []
    in_services = False
    service: str | None = None
    for line in text.splitlines():
        if line == "services:":
            in_services = True
            continue
        if in_services and line and not line.startswith(" "):
            break
        if not in_services:
            continue
        service_match = re.fullmatch(r"  ([a-z0-9][a-z0-9_-]*):", line)
        if service_match:
            service = service_match.group(1)
            continue
        image_match = re.fullmatch(r"    image:\s*(.*?)\s*", line)
        if service and image_match:
            image = image_match.group(1).strip().strip("\"'")
            variable = VARIABLE.fullmatch(image)
            if variable:
                resolved = os.getenv(variable.group(1), "").strip()
                if resolved:
                    image = resolved
                elif strict:
                    raise RuntimeError(f"{variable.group(1)} is not resolved to an immutable image")
            if DIGEST.fullmatch(image):
                name, digest = image.rsplit("@", 1)
                result.append({"type": "container", "name": name, "version": digest, "bom-ref": f"container:{service}:{digest}", "properties": [{"name": "campaign-platform:service", "value": service}]})
            elif not strict:
                result.append({"type": "container", "name": service, "version": "unresolved", "properties": [{"name": "campaign-platform:image-template", "value": image}]})
            else:
                raise RuntimeError(f"service {service} image is not digest-pinned")
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=ROOT / "artifacts/sbom.cdx.json")
    parser.add_argument("--strict", action="store_true")
    args = parser.parse_args()
    if args.strict:
        missing = [str(path.relative_to(ROOT)) for path in (ROOT / "package-lock.json", ROOT / "apps/admin-web/package-lock.json", ROOT / "services/openwa-gateway/package-lock.json") if not path.is_file()]
        if missing:
            raise RuntimeError("strict SBOM requires reproducible lockfiles: " + ", ".join(missing))

    version = (ROOT / "VERSION").read_text(encoding="utf-8").strip()
    components: list[dict[str, Any]] = []
    components.extend(go_components())
    components.extend(native_components())
    for relative in ("package.json", "apps/admin-web/package.json", "services/openwa-gateway/package.json"):
        components.extend(npm_components(ROOT / relative, relative))
    components.extend(image_components(ROOT / "infrastructure/compose/compose.production.yaml", args.strict))
    components = sorted({component_key(item): item for item in components}.values(), key=component_key)

    fingerprint = hashlib.sha256(json.dumps(components, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    serial = uuid.uuid5(uuid.NAMESPACE_URL, f"campaign-platform:{version}:{fingerprint}")
    document = {
        "bomFormat": "CycloneDX", "specVersion": "1.5", "serialNumber": f"urn:uuid:{serial}", "version": 1,
        "metadata": {
            "component": {"type": "application", "name": "campaign-platform", "version": version, "bom-ref": "application:campaign-platform"},
            "properties": [
                {"name": "campaign-platform:source-fingerprint", "value": fingerprint},
                {"name": "campaign-platform:dependency-resolution", "value": "locked" if args.strict else "manifest-level"},
            ],
        },
        "components": components,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(f"Wrote CycloneDX SBOM with {len(components)} components to {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
