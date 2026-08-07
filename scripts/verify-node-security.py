#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CONFIG = ROOT / "config/node-security-exceptions.json"
TARGETS = (
    ("admin", ROOT / "apps/admin-web", True),
    ("openwa-gateway", ROOT / "services/openwa-gateway", True),
)


def validate_exception_register() -> list[str]:
    data = json.loads(CONFIG.read_text(encoding="utf-8"))
    errors: list[str] = []
    if data.get("schemaVersion") != 1:
        errors.append("node security exception register must use schemaVersion 1")
    exceptions = data.get("exceptions")
    if not isinstance(exceptions, list):
        errors.append("node security exception register must contain an exceptions array")
    elif exceptions:
        errors.append("production Node security exceptions require explicit validator support")
    return errors


def run_audit(directory: Path, disable_workspaces: bool) -> dict:
    command = ["npm", "audit", "--json"]
    if disable_workspaces:
        command.insert(2, "--workspaces=false")
    result = subprocess.run(command, cwd=directory, text=True, capture_output=True, check=False)
    if not result.stdout.strip():
        raise RuntimeError(f"npm audit produced no JSON for {directory}: {result.stderr.strip()}")
    return json.loads(result.stdout)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--strict", action="store_true")
    parser.add_argument("--structural-only", action="store_true")
    args = parser.parse_args()
    errors = validate_exception_register()
    if not args.structural_only:
        for name, directory, disable_workspaces in TARGETS:
            audit = run_audit(directory, disable_workspaces)
            total = int(audit.get("metadata", {}).get("vulnerabilities", {}).get("total", 0))
            if total:
                errors.append(f"{name} has {total} npm vulnerabilities")
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    mode = "strict" if args.strict else "normal"
    print(f"Node security validation passed in {mode} mode with no production exceptions.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
