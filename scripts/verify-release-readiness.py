#!/usr/bin/env python3
"""Validate governance artefacts and report release-gate state.

This checker deliberately fails a production-candidate request while open or
blocked hard gates remain. In normal development mode it validates structure
and reports the outstanding gate count without pretending the release is ready.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_RELEASE_GATES = ROOT / "config/release-gates.json"
DEFAULT_TRACEABILITY = ROOT / "docs/requirements/traceability.json"
DEFAULT_DEPLOYMENT_READINESS = ROOT / "config/deployment-readiness.json"
DEFAULT_EVIDENCE_ROOT = ROOT / "evidence/release-gates"
EXPECTED_GATE_IDS = {f"RG-{index:03d}" for index in range(1, 9)}
EXPECTED_REQUIRED_DOCUMENTS = {
    "docs/program/MASTER_IMPLEMENTATION_ROADMAP.md",
    "docs/program/PRODUCTION_READINESS_CHECKLIST.md",
    "docs/program/RISK_AND_TECHNICAL_DEBT_REGISTER.md",
    "docs/decisions/README.md",
    "docs/audit/INDEPENDENT_ENGINEERING_AUDIT_PLAN.md",
    "docs/requirements/TRACEABILITY.md",
    "docs/handover/CURRENT_STATUS.md",
    "docs/handover/NEXT_ACTIONS.md",
}
EXPECTED_GATE_DEFINITIONS = {
    "RG-001": ("Authoritative requirements traceability", "All Must requirements are IMPLEMENTED_TESTED or have an approved, time-bounded external waiver."),
    "RG-002": ("Live PostgreSQL migration validation", "All migrations execute successfully on a clean production-equivalent PostgreSQL instance and rollback/recovery is demonstrated."),
    "RG-003": ("Live OpenWA transport validation", "Real sessions prove pairing, text/media send, inbound events, sent/delivered/read acknowledgements and uncertain-outcome handling."),
    "RG-004": ("Capacity and performance evidence", "10M-contact query tests, campaign-ledger scale tests and measured per-session sustainable capacity are complete."),
    "RG-005": ("Security assurance", "Threat model, dependency/container scans, secret scan, SAST and independent penetration test have no unresolved critical/high findings."),
    "RG-006": ("Backup and disaster recovery proof", "Encrypted backups, point-in-time recovery, object restore and OpenWA session recovery are tested against agreed RPO/RTO."),
    "RG-007": ("Operational readiness", "Monitoring, alerting, runbooks, on-call ownership, incident process and maintenance controls are exercised."),
    "RG-008": ("Frontend workflow completion", "All required UI/UX workflows and exceptional states are implemented, accessible and permission-aware."),
}
EXPECTED_EVIDENCE_KINDS = {
    "RG-001": {"traceability-report"},
    "RG-002": {"migration-report", "rollback-report"},
    "RG-003": {"provider-validation-report"},
    "RG-004": {"capacity-report", "performance-report"},
    "RG-005": {"security-report", "penetration-test-report"},
    "RG-006": {"recovery-report", "backup-report"},
    "RG-007": {"operations-exercise-report", "monitoring-report"},
    "RG-008": {"frontend-validation-report", "accessibility-report"},
}
ALLOWED_GATE_STATUSES = {"OPEN", "BLOCKED_EXTERNAL", "CLOSED", "WAIVED"}
SOURCE_FINGERPRINT = re.compile(r"^[0-9a-fA-F]{64}$")


def load_json_path(path: Path, label: str) -> object:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise RuntimeError(f"missing required JSON file: {label}") from exc
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"invalid JSON in {label}: {exc}") from exc


def parse_utc_timestamp(raw: object) -> datetime | None:
    text = str(raw or "").strip()
    if not text:
        return None
    try:
        value = datetime.fromisoformat(text.replace("Z", "+00:00"))
    except ValueError:
        return None
    if value.tzinfo is None or value.utcoffset() != timezone.utc.utcoffset(value):
        return None
    return value


def valid_provenance(record: object, *, owner_required: bool = True) -> bool:
    if not isinstance(record, dict):
        return False
    if owner_required and not str(record.get("owner", "")).strip():
        return False
    if not str(record.get("evidence_reference", "")).strip():
        return False
    if not SOURCE_FINGERPRINT.fullmatch(str(record.get("source_fingerprint", "")).strip()):
        return False
    observed = parse_utc_timestamp(record.get("observed_at"))
    if observed is None:
        return False
    return observed <= datetime.now(timezone.utc)


def valid_closure_evidence(value: object) -> bool:
    return (
        isinstance(value, list)
        and bool(value)
        and all(valid_provenance(record, owner_required=True) for record in value)
    )


def valid_waiver(value: object) -> bool:
    if not isinstance(value, dict) or not valid_provenance(value, owner_required=True):
        return False
    if not str(value.get("approver", "")).strip() or not str(value.get("waiver_reason", "")).strip():
        return False
    if str(value.get("owner", "")).strip().casefold() == str(value.get("approver", "")).strip().casefold():
        return False
    observed = parse_utc_timestamp(value.get("observed_at"))
    expires = parse_utc_timestamp(value.get("expires_at"))
    if observed is None or expires is None:
        return False
    now = datetime.now(timezone.utc)
    return expires > observed and expires > now


def safe_repo_document(relative: object) -> Path | None:
    if not isinstance(relative, str):
        return None
    raw = relative.strip()
    if not raw or "\\" in raw or raw.startswith("/") or ":" in raw.split("/", 1)[0]:
        return None
    candidate = (ROOT / raw).resolve()
    try:
        candidate.relative_to(ROOT.resolve())
    except ValueError:
        return None
    return candidate


def requirement_satisfied(requirement: object) -> bool:
    if not isinstance(requirement, dict):
        return False
    status = str(requirement.get("status", ""))
    if status == "IMPLEMENTED_TESTED":
        return True
    return status == "WAIVED" and valid_waiver(requirement.get("waiver"))


def validate_release_config(config: object, errors: list[str]) -> tuple[list[dict], list[tuple[str, str, str]]]:
    if not isinstance(config, dict):
        errors.append("release-gates.json must contain an object")
        return [], []
    if config.get("schema_version") != 1:
        errors.append("release-gates.json must use schema_version 1")

    required_documents = config.get("required_documents")
    if not isinstance(required_documents, list):
        errors.append("required_documents must be an array")
    else:
        normalized: list[str] = []
        for relative in required_documents:
            candidate = safe_repo_document(relative)
            if candidate is None:
                errors.append(f"required governance document must use a safe repo-relative path: {relative!r}")
                continue
            normalized.append(str(relative))
            if not candidate.is_file() or candidate.stat().st_size == 0:
                errors.append(f"required governance document missing or empty: {relative}")
        if len(normalized) != len(set(normalized)):
            errors.append("required governance documents contain duplicate declarations")
        if len(normalized) != len(EXPECTED_REQUIRED_DOCUMENTS) or set(normalized) != EXPECTED_REQUIRED_DOCUMENTS:
            errors.append("release config must contain the exact required governance document set")

    gates_value = config.get("hard_gates")
    if not isinstance(gates_value, list) or not gates_value:
        errors.append("hard_gates must be a non-empty array")
        return [], []

    gates: list[dict] = []
    gate_ids: list[str] = []
    outstanding: list[tuple[str, str, str]] = []
    for gate in gates_value:
        if not isinstance(gate, dict):
            errors.append("each release gate must be an object")
            continue
        gates.append(gate)
        gate_id = str(gate.get("id", ""))
        gate_ids.append(gate_id)
        name = str(gate.get("name", ""))
        status = str(gate.get("status", ""))
        expected_definition = EXPECTED_GATE_DEFINITIONS.get(gate_id)
        if expected_definition is not None and (name, str(gate.get("criterion", ""))) != expected_definition:
            errors.append(f"release gate {gate_id} gate definition does not match the authoritative release policy")
        if not name or not gate.get("criterion") or not gate.get("evidence"):
            errors.append(f"release gate {gate_id} is missing name, criterion or evidence")
        if status not in ALLOWED_GATE_STATUSES:
            errors.append(f"release gate {gate_id} has invalid status {status!r}")
        if status == "CLOSED" and not valid_closure_evidence(gate.get("closure_evidence")):
            errors.append(f"release gate {gate_id} CLOSED status requires valid structured closure evidence")
        if status == "WAIVED" and not valid_waiver(gate.get("waiver")):
            errors.append(f"release gate {gate_id} WAIVED status requires formal waiver metadata")
        if status not in {"CLOSED", "WAIVED"}:
            outstanding.append((gate_id, name, status))

    if len(gate_ids) != len(EXPECTED_GATE_IDS) or set(gate_ids) != EXPECTED_GATE_IDS:
        errors.append("release config must contain the exact hard gate set RG-001 through RG-008")
    elif len(gate_ids) != len(set(gate_ids)):
        errors.append("release config hard gate ids must be unique")
    return gates, outstanding


def validate_traceability(traceability: object, gates: list[dict], errors: list[str]) -> None:
    if isinstance(traceability, dict):
        requirements = traceability.get("requirements")
    elif isinstance(traceability, list):
        requirements = traceability
    else:
        requirements = None
    if not isinstance(requirements, list) or not requirements:
        errors.append("traceability.json contains no requirements")
        return
    malformed = [requirement for requirement in requirements if not isinstance(requirement, dict)]
    if malformed:
        errors.append("traceability.json contains malformed requirement entries")
        return
    gate_status = {str(g.get("id")): str(g.get("status")) for g in gates}
    rg001_status = gate_status.get("RG-001")
    if rg001_status in {"CLOSED", "WAIVED"}:
        incomplete = [
            requirement for requirement in requirements
            if isinstance(requirement, dict)
            and str(requirement.get("priority", "")).lower() == "must"
            and not requirement_satisfied(requirement)
        ]
        if incomplete:
            errors.append(
                f"RG-001 cannot be {rg001_status} while {len(incomplete)} Must traceability requirements are incomplete or lack a formal waiver"
            )


def validate_deployment_readiness_coupling(
    release_path: Path,
    readiness_path: Path,
    errors: list[str],
) -> None:
    command = [
        sys.executable,
        str(ROOT / "scripts/verify-deployment-readiness.py"),
        "--contract",
        str(readiness_path),
        "--release-gates",
        str(release_path),
    ]
    result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True, check=False)
    if result.returncode == 0:
        return
    error_count = len(errors)
    for line in result.stderr.splitlines():
        line = line.strip()
        if not line:
            continue
        if line.startswith("ERROR: "):
            line = line[7:]
        errors.append(line)
    if len(errors) == error_count:
        errors.append(
            f"deployment-readiness coupling verifier failed with exit {result.returncode} and no diagnostics"
        )


def safe_evidence_artifact(evidence_root: Path, reference: object) -> Path | None:
    if not isinstance(reference, str):
        return None
    raw = reference.strip()
    if (
        not raw
        or "\\" in raw
        or raw.startswith("/")
        or ":" in raw.split("/", 1)[0]
        or any(part in {"", ".", ".."} for part in raw.split("/"))
    ):
        return None
    root = evidence_root.resolve()
    candidate = (root / raw).resolve()
    try:
        candidate.relative_to(root)
    except ValueError:
        return None
    return candidate


def validate_manifest_evidence(
    gate_id: str,
    value: object,
    evidence_root: Path,
    manifest_artifact: Path,
) -> list[str]:
    errors: list[str] = []
    if not isinstance(value, list) or not value:
        return [f"release gate {gate_id} evidence manifest must contain a non-empty evidence array"]
    gate_root = (evidence_root.resolve() / gate_id).resolve()
    allowed_kinds = EXPECTED_EVIDENCE_KINDS.get(gate_id, set())
    for index, item in enumerate(value):
        label = f"release gate {gate_id} evidence item {index}"
        if not isinstance(item, dict):
            errors.append(f"{label} must be an object")
            continue
        kind = str(item.get("kind", "")).strip()
        if kind not in allowed_kinds:
            errors.append(f"{label} kind must be one of the governed kinds for {gate_id}")
        observed = parse_utc_timestamp(item.get("observed_at"))
        if observed is None or observed > datetime.now(timezone.utc):
            errors.append(f"{label} observed_at must be a non-future UTC timestamp")
        reference = item.get("reference")
        artifact = safe_evidence_artifact(evidence_root, reference)
        if artifact is None:
            errors.append(f"{label} must reference a safe artifact beneath the evidence root")
            continue
        try:
            artifact.relative_to(gate_root)
        except ValueError:
            errors.append(f"{label} must reference a gate-local artifact beneath {gate_id}")
            continue
        if artifact == manifest_artifact.resolve():
            errors.append(f"{label} must not reference its own evidence manifest")
            continue
        try:
            raw = artifact.read_bytes()
        except OSError:
            raw = b""
        if not raw:
            errors.append(f"{label} referenced artifact is missing or empty: {reference}")
            continue
        try:
            leaf = json.loads(raw)
        except (json.JSONDecodeError, UnicodeDecodeError):
            leaf = None
        if isinstance(leaf, dict) and {"schema_version", "gate_id", "evidence"}.issubset(leaf):
            errors.append(f"{label} must reference non-manifest observed evidence, not a sibling evidence manifest")
            continue
        fingerprint = str(item.get("source_fingerprint", "")).strip().lower()
        if not SOURCE_FINGERPRINT.fullmatch(fingerprint) or hashlib.sha256(raw).hexdigest() != fingerprint:
            errors.append(f"{label} referenced artifact hash does not match source_fingerprint: {reference}")
    return errors


def validate_production_evidence(
    gates: list[dict],
    evidence_root: Path,
    candidate_fingerprint: str,
    errors: list[str],
) -> None:
    governed = [gate for gate in gates if str(gate.get("status", "")) in {"CLOSED", "WAIVED"}]
    if not governed:
        return
    candidate_fingerprint = candidate_fingerprint.strip().lower()
    if not SOURCE_FINGERPRINT.fullmatch(candidate_fingerprint):
        errors.append("production closure evidence requires a 64-character release candidate fingerprint")
        return

    for gate in governed:
        gate_id = str(gate.get("id", ""))
        status = str(gate.get("status", ""))
        records = gate.get("closure_evidence") if status == "CLOSED" else [gate.get("waiver")]
        if not isinstance(records, list):
            continue
        for record in records:
            if not isinstance(record, dict):
                continue
            owner = str(record.get("owner", "")).strip()
            approver = str(record.get("approver", "")).strip()
            if not approver or owner.casefold() == approver.casefold():
                errors.append(f"release gate {gate_id} production evidence requires an independent approver")
                continue
            reference = record.get("evidence_reference")
            artifact = safe_evidence_artifact(evidence_root, reference)
            if artifact is None:
                errors.append(f"release gate {gate_id} evidence artifact must use a safe path relative to the evidence root")
                continue
            try:
                raw = artifact.read_bytes()
            except OSError:
                raw = b""
            if not raw:
                errors.append(f"release gate {gate_id} evidence artifact is missing or empty: {reference}")
                continue
            digest = hashlib.sha256(raw).hexdigest()
            if digest != str(record.get("source_fingerprint", "")).strip().lower():
                errors.append(f"release gate {gate_id} evidence artifact hash does not match source_fingerprint: {reference}")
                continue
            try:
                manifest = json.loads(raw)
            except json.JSONDecodeError:
                errors.append(f"release gate {gate_id} evidence artifact is not valid JSON: {reference}")
                continue
            if not isinstance(manifest, dict) or manifest.get("schema_version") != 1:
                errors.append(f"release gate {gate_id} evidence artifact must use manifest schema_version 1: {reference}")
                continue
            if str(manifest.get("gate_id", "")) != gate_id:
                errors.append(f"release gate {gate_id} evidence artifact is bound to a different gate: {reference}")
            if str(manifest.get("candidate_fingerprint", "")).strip().lower() != candidate_fingerprint:
                errors.append(f"release gate {gate_id} evidence artifact is bound to a different release candidate: {reference}")
            if str(manifest.get("owner", "")).strip() != owner or str(manifest.get("approver", "")).strip() != approver:
                errors.append(f"release gate {gate_id} evidence artifact actor binding does not match release governance: {reference}")
            errors.extend(validate_manifest_evidence(gate_id, manifest.get("evidence"), evidence_root, artifact))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--production-candidate",
        action="store_true",
        help="fail unless every hard gate is CLOSED or formally WAIVED",
    )
    parser.add_argument("--release-gates", type=Path, default=DEFAULT_RELEASE_GATES)
    parser.add_argument("--traceability", type=Path, default=DEFAULT_TRACEABILITY)
    parser.add_argument("--deployment-readiness", type=Path, default=DEFAULT_DEPLOYMENT_READINESS)
    parser.add_argument("--evidence-root", type=Path, default=DEFAULT_EVIDENCE_ROOT)
    parser.add_argument("--candidate-fingerprint", default=os.environ.get("RELEASE_CANDIDATE_FINGERPRINT", ""))
    args = parser.parse_args()

    errors: list[str] = []
    try:
        config = load_json_path(args.release_gates, str(args.release_gates))
        traceability = load_json_path(args.traceability, str(args.traceability))
    except RuntimeError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1

    gates, outstanding = validate_release_config(config, errors)
    validate_traceability(traceability, gates, errors)
    validate_deployment_readiness_coupling(args.release_gates, args.deployment_readiness, errors)
    if args.production_candidate:
        validate_production_evidence(gates, args.evidence_root, args.candidate_fingerprint, errors)

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
