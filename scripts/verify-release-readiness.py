#!/usr/bin/env python3
"""Validate governance artefacts and report release-gate state.

This checker deliberately fails a production-candidate request while open or
blocked hard gates remain. In normal development mode it validates structure
and reports the outstanding gate count without pretending the release is ready.
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def load_json(relative: str) -> object:
    path = ROOT / relative
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise RuntimeError(f"missing required JSON file: {relative}") from exc
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"invalid JSON in {relative}: {exc}") from exc


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--production-candidate",
        action="store_true",
        help="fail unless every hard gate is CLOSED",
    )
    args = parser.parse_args()

    errors: list[str] = []
    config = load_json("config/release-gates.json")
    if not isinstance(config, dict):
        errors.append("release-gates.json must contain an object")
        config = {}

    for relative in config.get("required_documents", []):
        path = ROOT / str(relative)
        if not path.is_file() or path.stat().st_size == 0:
            errors.append(f"required governance document missing or empty: {relative}")

    gates = config.get("hard_gates", [])
    if not isinstance(gates, list) or not gates:
        errors.append("hard_gates must be a non-empty array")
        gates = []

    allowed = {"OPEN", "BLOCKED_EXTERNAL", "CLOSED", "WAIVED"}
    seen: set[str] = set()
    outstanding: list[tuple[str, str, str]] = []
    for gate in gates:
        if not isinstance(gate, dict):
            errors.append("each release gate must be an object")
            continue
        gate_id = str(gate.get("id", ""))
        name = str(gate.get("name", ""))
        status = str(gate.get("status", ""))
        if not gate_id or gate_id in seen:
            errors.append(f"release gate has missing or duplicate id: {gate_id!r}")
        seen.add(gate_id)
        if not name or not gate.get("criterion") or not gate.get("evidence"):
            errors.append(f"release gate {gate_id} is missing name, criterion or evidence")
        if status not in allowed:
            errors.append(f"release gate {gate_id} has invalid status {status!r}")
        if status not in {"CLOSED", "WAIVED"}:
            outstanding.append((gate_id, name, status))

    traceability = load_json("docs/requirements/traceability.json")
    if isinstance(traceability, dict):
        requirements = traceability.get("requirements")
    elif isinstance(traceability, list):
        requirements = traceability
    else:
        requirements = None
    if not isinstance(requirements, list) or not requirements:
        errors.append("traceability.json contains no requirements")

    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1

    print(f"Governance structure valid. Hard gates: {len(gates)}.")
    if outstanding:
        print(f"Outstanding hard gates: {len(outstanding)}")
        for gate_id, name, status in outstanding:
            print(f"- {gate_id} [{status}] {name}")
    else:
        print("All hard gates are closed or formally waived.")

    if args.production_candidate and outstanding:
        print("Production-candidate gate failed.", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
