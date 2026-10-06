#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any

FULL_MSISDN = re.compile(r"\+[1-9][0-9]{7,14}(?![0-9])")
FORBIDDEN_KEY_FRAGMENTS = (
    "password",
    "secret",
    "qr_payload",
    "access_token",
    "private_key",
)
SHA40 = re.compile(r"^[0-9a-f]{40}$")
EXPECTED_SOURCE_COMMIT = "daa9f3ddede0ecc5b7d6a52e579fba140a0c5e8d"


def load(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"cannot load UAT evidence: {exc}") from exc
    if not isinstance(value, dict):
        raise RuntimeError("UAT evidence must contain a JSON object")
    return value


def scan_sensitive(value: Any, path: str = "$") -> list[str]:
    errors: list[str] = []
    if isinstance(value, dict):
        for key, child in value.items():
            lowered = str(key).lower()
            if any(fragment in lowered for fragment in FORBIDDEN_KEY_FRAGMENTS):
                errors.append(f"SENSITIVE_EVIDENCE_FORBIDDEN: forbidden field at {path}.{key}")
            errors.extend(scan_sensitive(child, f"{path}.{key}"))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            errors.extend(scan_sensitive(child, f"{path}[{index}]"))
    elif isinstance(value, str) and FULL_MSISDN.search(value):
        errors.append(f"SENSITIVE_EVIDENCE_FORBIDDEN: full MSISDN-like value at {path}")
    return errors


def has_ref(section: dict[str, Any], key: str = "evidence_ref") -> bool:
    return bool(str(section.get(key) or "").strip())


def core_errors(data: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    required_statuses = (
        ("organisation", "ACTIVE"),
        ("consent", "APPROVED"),
        ("test_recipient", "ACTIVE"),
    )
    for section_name, status in required_statuses:
        section = data.get(section_name)
        if not isinstance(section, dict) or section.get("status") != status:
            errors.append(f"CORE_UAT_INCOMPLETE: {section_name}.status must be {status}")

    sender = data.get("sender_session")
    if not isinstance(sender, dict):
        errors.append("CORE_UAT_INCOMPLETE: sender_session is required")
    else:
        if sender.get("status") != "READY":
            errors.append("CORE_UAT_INCOMPLETE: sender_session.status must be READY")
        if sender.get("engine") not in {"BAILEYS", "WHATSAPP_WEB_JS"}:
            errors.append("CORE_UAT_INCOMPLETE: sender_session.engine must be BAILEYS or WHATSAPP_WEB_JS")
        if sender.get("qr_persisted") is not False:
            errors.append("CORE_UAT_INCOMPLETE: sender_session.qr_persisted must be false")
        if not has_ref(sender, "pairing_evidence_ref"):
            errors.append("CORE_UAT_INCOMPLETE: sender_session.pairing_evidence_ref is required")
        if not has_ref(sender, "ready_evidence_ref"):
            errors.append("CORE_UAT_INCOMPLETE: sender_session.ready_evidence_ref is required")

    controlled = data.get("controlled_test")
    if not isinstance(controlled, dict) or controlled.get("status") != "ACCEPTED" or not has_ref(controlled):
        errors.append("CORE_UAT_INCOMPLETE: accepted controlled-test evidence is required")

    routing = data.get("routing")
    if not isinstance(routing, dict) or routing.get("reservation_status") not in {"ACTIVE", "RELEASED"} or not has_ref(routing):
        errors.append("CORE_UAT_INCOMPLETE: authoritative routing reservation evidence is required")

    for name in ("text_send", "media_send"):
        section = data.get(name)
        if not isinstance(section, dict):
            errors.append(f"CORE_UAT_INCOMPLETE: {name} evidence is required")
            continue
        for field in ("provider_accepted", "sent", "delivered", "read"):
            if section.get(field) is not True:
                errors.append(f"CORE_UAT_INCOMPLETE: {name}.{field} must be true")
        if not has_ref(section):
            errors.append(f"CORE_UAT_INCOMPLETE: {name}.evidence_ref is required")

    inbound = data.get("inbound_reply")
    if not isinstance(inbound, dict) or inbound.get("received") is not True or not has_ref(inbound):
        errors.append("CORE_UAT_INCOMPLETE: inbound reply evidence is required")

    reconnect = data.get("reconnect")
    if not isinstance(reconnect, dict) or reconnect.get("disconnect_observed") is not True or reconnect.get("ready_after_reconnect") is not True or not has_ref(reconnect):
        errors.append("CORE_UAT_INCOMPLETE: disconnect/reconnect READY evidence is required")

    if data.get("core_uat_complete") is not True:
        errors.append("CORE_UAT_INCOMPLETE: core_uat_complete must be true after evidence review")
    return errors


def release_errors(data: dict[str, Any]) -> list[str]:
    if data.get("release_certification_claimed") is not True:
        return []
    errors: list[str] = []
    unknown = data.get("unknown_outcome_exercise")
    if not isinstance(unknown, dict) or unknown.get("status") != "OBSERVED_RECONCILED" or not has_ref(unknown):
        errors.append("RELEASE_CERTIFICATION_UNSUPPORTED: reconciled UNKNOWN-outcome evidence is required")
    elif unknown.get("duplicate_send_observed") is not False:
        errors.append("RELEASE_CERTIFICATION_UNSUPPORTED: UNKNOWN exercise observed a duplicate send")

    operational = data.get("operational_release_evidence")
    if not isinstance(operational, dict):
        errors.append("RELEASE_CERTIFICATION_UNSUPPORTED: operational release evidence is required")
    else:
        for key in ("backup_restore", "monitoring", "capacity", "security", "rollback"):
            section = operational.get(key)
            if not isinstance(section, dict) or section.get("status") != "PASS" or not has_ref(section):
                errors.append(f"RELEASE_CERTIFICATION_UNSUPPORTED: {key} PASS evidence is required")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("evidence", type=Path)
    args = parser.parse_args()
    try:
        data = load(args.evidence.resolve())
    except RuntimeError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1

    errors = scan_sensitive(data)
    if data.get("schema_version") != 1 or data.get("evidence_type") != "openwa-live-provider-uat":
        errors.append("CORE_UAT_INCOMPLETE: invalid UAT evidence schema")
    source_commit = str(data.get("source_commit", ""))
    if not SHA40.fullmatch(source_commit):
        errors.append("CORE_UAT_INCOMPLETE: source_commit must be a 40-character SHA")
    elif source_commit != EXPECTED_SOURCE_COMMIT:
        errors.append(
            f"SOURCE_COMMIT_MISMATCH: source_commit must equal accepted deployed source {EXPECTED_SOURCE_COMMIT}"
        )
    if data.get("target_environment") != "production":
        errors.append("CORE_UAT_INCOMPLETE: target_environment must be production")

    errors.extend(core_errors(data))
    errors.extend(release_errors(data))
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1

    print("Core live-provider UAT evidence valid.")
    if data.get("release_certification_claimed") is True:
        print("Production release certification evidence valid.")
    else:
        print("Core UAT passed; release certification remains open.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
