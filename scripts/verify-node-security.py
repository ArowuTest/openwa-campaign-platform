#!/usr/bin/env python3
from __future__ import annotations

import argparse
import datetime as dt
import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CONFIG = ROOT / "config/node-security-exceptions.json"
DASHBOARD = ROOT / "third_party/openwa/upstream/dashboard"
TARGETS = (
    ("admin", ROOT / "apps/admin-web", True),
    ("gateway", ROOT / "services/openwa-gateway", True),
    ("openwa-upstream", ROOT / "third_party/openwa/upstream", False),
    ("openwa-dashboard", DASHBOARD, False),
)
RSC_TOKENS = (
    '"use server"', "'use server'", "react-server-dom",
    "@react-router/node", "createRequestHandler", "ServerRouter",
    "RSCStaticRouter", "routeRSCServerRequest", "unstable_RSC",
)


def load_exception() -> dict:
    data = json.loads(CONFIG.read_text(encoding="utf-8"))
    matches = [item for item in data.get("exceptions", []) if item.get("id") == "SEC-EXC-001"]
    if len(matches) != 1:
        raise ValueError("SEC-EXC-001 must exist exactly once")
    return matches[0]


def validate_exception(item: dict, strict: bool) -> list[str]:
    errors: list[str] = []
    expected = {
        "advisory": "GHSA-qwww-vcr4-c8h2",
        "package": "react-router",
        "component": "third_party/openwa/upstream/dashboard",
        "severity": "high",
        "requiredVersion": "7.18.2",
    }
    for key, value in expected.items():
        if item.get(key) != value:
            errors.append(f"SEC-EXC-001 {key} must be {value!r}")
    try:
        expiry = dt.date.fromisoformat(str(item.get("expiresOn", "")))
        if expiry < dt.date.today():
            errors.append("SEC-EXC-001 is expired")
    except ValueError:
        errors.append("SEC-EXC-001 expiresOn must be an ISO date")
    if strict and item.get("status") != "APPROVED":
        errors.append("strict release requires SEC-EXC-001 approval")

    package = json.loads((DASHBOARD / "package.json").read_text(encoding="utf-8"))
    if package.get("dependencies", {}).get("react-router-dom") != "7.18.2":
        errors.append("dashboard must pin react-router-dom exactly to 7.18.2")
    if "vite build" not in package.get("scripts", {}).get("build", ""):
        errors.append("dashboard must remain a Vite client build")
    app = (DASHBOARD / "src/App.tsx").read_text(encoding="utf-8")
    if "BrowserRouter" not in app:
        errors.append("dashboard must retain BrowserRouter-only routing")
    for source in (DASHBOARD / "src").rglob("*"):
        if source.is_file() and source.suffix in {".ts", ".tsx", ".js", ".jsx"}:
            text = source.read_text(encoding="utf-8", errors="ignore")
            for token in RSC_TOKENS:
                if token in text:
                    errors.append(f"RSC token {token!r} found in {source.relative_to(ROOT)}")
    return errors


def run_audit(directory: Path, disable_workspaces: bool) -> dict:
    command = ["npm", "audit", "--json"]
    if disable_workspaces:
        command.insert(2, "--workspaces=false")
    result = subprocess.run(command, cwd=directory, text=True, capture_output=True, check=False)
    if not result.stdout.strip():
        raise RuntimeError(f"npm audit produced no JSON for {directory}: {result.stderr.strip()}")
    return json.loads(result.stdout)


def validate_dashboard_audit(data: dict) -> list[str]:
    errors: list[str] = []
    vulnerabilities = data.get("vulnerabilities", {})
    if set(vulnerabilities) != {"react-router", "react-router-dom"}:
        errors.append(f"unexpected dashboard vulnerabilities: {sorted(vulnerabilities)}")
        return errors
    router = vulnerabilities["react-router"]
    advisories = [item for item in router.get("via", []) if isinstance(item, dict)]
    if len(advisories) != 1 or not str(advisories[0].get("url", "")).endswith("GHSA-qwww-vcr4-c8h2"):
        errors.append("dashboard audit must contain only GHSA-qwww-vcr4-c8h2")
    dom_via = vulnerabilities["react-router-dom"].get("via", [])
    if dom_via != ["react-router"]:
        errors.append(f"unexpected react-router-dom advisory chain: {dom_via}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--strict", action="store_true")
    parser.add_argument("--structural-only", action="store_true")
    args = parser.parse_args()
    errors = validate_exception(load_exception(), args.strict)
    if not args.structural_only:
        for name, directory, disable_workspaces in TARGETS:
            audit = run_audit(directory, disable_workspaces)
            total = int(audit.get("metadata", {}).get("vulnerabilities", {}).get("total", 0))
            if name == "openwa-dashboard":
                errors.extend(validate_dashboard_audit(audit))
            elif total:
                errors.append(f"{name} has {total} npm vulnerabilities")
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    status = load_exception().get("status")
    print(f"Node security validation passed; SEC-EXC-001 status={status}.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
