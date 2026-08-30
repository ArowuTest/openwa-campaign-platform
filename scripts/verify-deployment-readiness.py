#!/usr/bin/env python3
from __future__ import annotations

import argparse
import ipaddress
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CONTRACT = ROOT / "config/deployment-readiness.json"
RAILWAY_CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"
TOPOLOGY_CONTRACT = ROOT / "config/deployment-topology.json"
RELEASE_GATES = ROOT / "config/release-gates.json"
EXPECTED = {
    "railway": {
        "control-api", "audience-worker", "campaign-worker", "export-worker",
        "inbound-governance-worker", "metrics-worker", "platform-governance-worker",
    },
    "hostinger": {"openwa-gateway"},
}
ALLOWED_STATUSES = {"PENDING_EXTERNAL", "BLOCKED_EXTERNAL", "ACCEPTED", "WAIVED"}
DIGEST_IMAGE = re.compile(r"^[^@\s]+@sha256:[0-9a-fA-F]{64}$")
SOURCE_FINGERPRINT = re.compile(r"^[0-9a-fA-F]{64}$")


def valid_private_bind_address(value: object) -> bool:
    try:
        address = ipaddress.ip_address(str(value).strip())
    except ValueError:
        return False
    if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped is not None:
        address = address.ipv4_mapped
    if isinstance(address, ipaddress.IPv4Address):
        return any(address in network for network in (
            ipaddress.ip_network("10.0.0.0/8"),
            ipaddress.ip_network("172.16.0.0/12"),
            ipaddress.ip_network("192.168.0.0/16"),
        ))
    return address in ipaddress.ip_network("fc00::/7")


def valid_provenance(record: dict, owner_required: bool = False) -> bool:
    if not str(record.get("evidence_reference", "")).strip() or not SOURCE_FINGERPRINT.fullmatch(str(record.get("source_fingerprint", "")).strip()):
        return False
    if owner_required and not str(record.get("owner", "")).strip():
        return False
    raw = str(record.get("observed_at", "")).strip()
    try:
        observed = datetime.fromisoformat(raw.replace("Z", "+00:00"))
    except ValueError:
        return False
    return (
        observed.tzinfo is not None
        and observed.utcoffset() == timezone.utc.utcoffset(observed)
        and observed <= datetime.now(timezone.utc)
    )


def valid_waiver(record: dict) -> bool:
    if not valid_provenance(record, owner_required=True):
        return False
    if not str(record.get("approver", "")).strip() or not str(record.get("waiver_reason", "")).strip():
        return False
    if str(record.get("owner", "")).strip().casefold() == str(record.get("approver", "")).strip().casefold():
        return False
    try:
        observed = datetime.fromisoformat(str(record.get("observed_at", "")).strip().replace("Z", "+00:00"))
        expires = datetime.fromisoformat(str(record.get("expires_at", "")).strip().replace("Z", "+00:00"))
    except ValueError:
        return False
    return (
        expires.tzinfo is not None
        and expires.utcoffset() == timezone.utc.utcoffset(expires)
        and expires > observed
        and expires > datetime.now(timezone.utc)
    )


def load_contract(contract: Path = CONTRACT) -> dict:
    try:
        data = json.loads(contract.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"cannot load deployment-readiness contract: {exc}") from exc
    if not isinstance(data, dict):
        raise RuntimeError("deployment-readiness contract must contain an object")
    return data


def validate(data: dict) -> list[str]:
    errors: list[str] = []
    try:
        railway_contract = json.loads(RAILWAY_CONTRACT.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return ["authoritative Railway service contract is unavailable"]
    railway_services = railway_contract.get("services") if isinstance(railway_contract, dict) else None
    expected_railway = EXPECTED["railway"]
    if not isinstance(railway_services, dict) or set(railway_services) != expected_railway:
        return ["authoritative Railway service contract service set is invalid"]
    try:
        topology_contract = json.loads(TOPOLOGY_CONTRACT.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return ["authoritative deployment topology contract is unavailable"]
    topology_hostinger = topology_contract.get("hostinger") if isinstance(topology_contract, dict) else None
    hostinger_services = topology_hostinger.get("service_contracts") if isinstance(topology_hostinger, dict) else None
    if not isinstance(hostinger_services, dict) or set(hostinger_services) != EXPECTED["hostinger"]:
        return ["authoritative Hostinger service contract service set is invalid"]
    hostinger_gateway_contract = hostinger_services.get("openwa-gateway")
    if (
        not isinstance(hostinger_gateway_contract, dict)
        or not isinstance(hostinger_gateway_contract.get("port"), int)
        or hostinger_gateway_contract.get("port", 0) <= 0
        or not str(hostinger_gateway_contract.get("health_path", "")).startswith("/")
    ):
        return ["authoritative Hostinger openwa-gateway health declaration is invalid"]
    for service, contract in railway_services.items():
        if not isinstance(contract, dict):
            return [f"authoritative Railway service contract {service} declaration is invalid"]
        if not isinstance(contract.get("port"), int) or not str(contract.get("health_path", "")).startswith("/"):
            return [f"authoritative Railway service contract {service} health declaration is invalid"]
        if not isinstance(contract.get("secret_classes"), list) or not all(isinstance(item, str) and item.strip() for item in contract.get("secret_classes", [])):
            return [f"authoritative Railway service contract {service} secret classes are invalid"]
        if not isinstance(contract.get("public_ingress"), bool):
            return [f"authoritative Railway service contract {service} public_ingress is invalid"]
    if data.get("schema_version") != 1:
        errors.append("deployment-readiness contract must use schema_version 1")
    providers = data.get("providers")
    requirements = data.get("service_evidence_requirements")
    if not isinstance(providers, dict) or not isinstance(requirements, dict):
        return errors + ["provider placement and service evidence requirements are required"]
    if set(providers) != set(EXPECTED) or set(requirements) != set(EXPECTED):
        errors.append("provider set does not match the approved topology")
    for provider, expected in EXPECTED.items():
        placement = providers.get(provider, {})
        service_declarations = placement.get("services", []) if isinstance(placement, dict) else []
        valid_service_declarations = isinstance(service_declarations, list) and all(isinstance(item, str) and item.strip() for item in service_declarations)
        if not valid_service_declarations:
            errors.append(f"{provider} service placement contains invalid declaration")
            services = set()
        else:
            if len(service_declarations) != len(set(service_declarations)):
                errors.append(f"{provider} service placement contains duplicate declaration")
            services = set(service_declarations)
        if services != expected:
            errors.append(f"{provider} service placement does not match the approved topology")
        evidence = requirements.get(provider, {})
        if not isinstance(evidence, dict) or set(evidence) != expected:
            errors.append(f"{provider} service evidence requirements do not match placement")
            continue
        for service, record in evidence.items():
            if not isinstance(record, dict):
                errors.append(f"{provider}/{service} service evidence has invalid declaration")
                continue
            nested = {}
            for field in ("image", "health", "secrets", "network"):
                value = record.get(field)
                if not isinstance(value, dict):
                    errors.append(f"{provider}/{service} {field} evidence has invalid declaration")
                    value = {}
                nested[field] = value
            image, health, secrets, network = nested["image"], nested["health"], nested["secrets"], nested["network"]
            if network.get("status") not in ALLOWED_STATUSES:
                errors.append(f"{provider}/{service} has invalid network evidence requirements")
            if network.get("status") == "ACCEPTED" and not all(str(network.get(key, "")).strip() for key in ("owner", "evidence_reference", "source_fingerprint", "observed_at")):
                errors.append(f"{provider}/{service} accepted network evidence requires owner and provenance")
            if network.get("status") == "ACCEPTED" and not valid_provenance(network, owner_required=True):
                errors.append(f"{provider}/{service} accepted network evidence must use valid provenance metadata")
            if network.get("status") == "WAIVED" and not valid_waiver(network):
                errors.append(f"{provider}/{service} WAIVED evidence requires formal waiver metadata")
            if provider == "railway":
                service_contract = railway_contract["services"][service]
                if network.get("public_ingress") is not service_contract["public_ingress"]:
                    errors.append(f"{provider}/{service} network exposure does not match authoritative service contract")
                expected_tls = service_contract.get("tls_required", False)
                if network.get("tls_required", False) is not expected_tls:
                    errors.append(f"{provider}/{service} network TLS requirement does not match authoritative service contract")
                if network.get("status") == "ACCEPTED" and expected_tls and network.get("tls_observed") is not True:
                    errors.append(f"{provider}/{service} accepted public network evidence must explicitly observe TLS")
            if provider == "hostinger" and (network.get("public_ingress") is not False or network.get("private_bind_required") is not True or network.get("cross_provider_tls_required") is not True):
                errors.append("Hostinger network boundary must remain private-bind, non-public, and cross-provider TLS protected")
            if provider == "hostinger" and network.get("status") == "ACCEPTED" and (
                network.get("private_bind_observed") is not True
                or not valid_private_bind_address(network.get("observed_bind_address", ""))
            ):
                errors.append(f"{provider}/{service} accepted network evidence requires an observed private bind address")
            if network.get("status") == "ACCEPTED" and network.get("cross_provider_tls_required") is True and network.get("tls_observed") is not True:
                errors.append(f"{provider}/{service} accepted network evidence must explicitly observe TLS")
            if not isinstance(secrets.get("classes"), list) or secrets.get("status") not in ALLOWED_STATUSES:
                errors.append(f"{provider}/{service} has invalid secret evidence requirements")
            secret_class_declarations = secrets.get("classes")
            valid_secret_class_declarations = isinstance(secret_class_declarations, list) and all(isinstance(item, str) and item.strip() for item in secret_class_declarations)
            if not valid_secret_class_declarations:
                errors.append(f"{provider}/{service} secret classes contain invalid declaration")
            else:
                if len(secret_class_declarations) != len(set(secret_class_declarations)):
                    errors.append(f"{provider}/{service} secret classes contain duplicate declaration")
            expected_secret_classes = set(railway_contract["services"][service]["secret_classes"]) if provider == "railway" else {"gateway-control"}
            if valid_secret_class_declarations and set(secret_class_declarations) != expected_secret_classes:
                errors.append(f"{provider}/{service} secret classes do not match authoritative service contract")
            if any(key in secrets for key in ("value", "values", "secret_value", "credential_value")):
                errors.append(f"{provider}/{service} readiness evidence must not contain secret values")
            if secrets.get("status") == "ACCEPTED" and not all(str(secrets.get(key, "")).strip() for key in ("owner", "evidence_reference", "source_fingerprint", "observed_at")):
                errors.append(f"{provider}/{service} accepted secret evidence requires owner and provenance")
            if secrets.get("status") == "ACCEPTED" and not valid_provenance(secrets, owner_required=True):
                errors.append(f"{provider}/{service} accepted secret evidence must use valid provenance metadata")
            if secrets.get("status") == "WAIVED" and not valid_waiver(secrets):
                errors.append(f"{provider}/{service} WAIVED evidence requires formal waiver metadata")
            if image.get("immutable_digest_required") is not True or image.get("status") not in ALLOWED_STATUSES:
                errors.append(f"{provider}/{service} has invalid image evidence requirements")
            if image.get("status") in {"ACCEPTED", "WAIVED"} and image.get("immutable_digest_required") is True and not DIGEST_IMAGE.fullmatch(str(image.get("resolved_image", "")).strip()):
                errors.append(f"{provider}/{service} {image.get('status')} image evidence must be digest-pinned")
            if image.get("status") == "ACCEPTED" and not all(str(image.get(key, "")).strip() for key in ("owner", "evidence_reference", "source_fingerprint", "observed_at")):
                errors.append(f"{provider}/{service} ACCEPTED image evidence requires owner and provenance metadata")
            if image.get("status") == "ACCEPTED" and not valid_provenance(image, owner_required=True):
                errors.append(f"{provider}/{service} ACCEPTED image evidence must use valid provenance metadata")
            if image.get("status") == "WAIVED" and not valid_waiver(image):
                errors.append(f"{provider}/{service} WAIVED evidence requires formal waiver metadata")
            if health.get("status") not in ALLOWED_STATUSES or not isinstance(health.get("port"), int) or not health.get("path"):
                errors.append(f"{provider}/{service} has invalid health evidence requirements")
            expected_health = (
                (railway_contract["services"][service]["port"], railway_contract["services"][service]["health_path"])
                if provider == "railway"
                else (hostinger_gateway_contract["port"], hostinger_gateway_contract["health_path"])
            )
            if (health.get("port"), health.get("path")) != expected_health:
                errors.append(f"{provider}/{service} health endpoint does not match authoritative service contract")
            if health.get("status") == "ACCEPTED" and not all(str(health.get(key, "")).strip() for key in ("owner", "evidence_reference", "source_fingerprint", "observed_at")):
                errors.append(f"{provider}/{service} accepted health evidence requires owner and provenance")
            if health.get("status") == "ACCEPTED" and not valid_provenance(health, owner_required=True):
                errors.append(f"{provider}/{service} accepted health evidence must use valid provenance metadata")
            if health.get("status") == "WAIVED" and not valid_waiver(health):
                errors.append(f"{provider}/{service} WAIVED evidence requires formal waiver metadata")
    operations = data.get("operational_evidence_requirements")
    if not isinstance(operations, dict) or not isinstance(operations.get("monitoring"), dict) or not isinstance(operations.get("recovery"), dict):
        errors.append("monitoring and recovery evidence requirements are required")
        return errors
    expected_monitoring = {"control-plane", "openwa-gateway-fleet", "postgresql", "redis", "object-storage", "meta-cloud"}
    monitoring = operations["monitoring"]
    if set(monitoring) != expected_monitoring:
        errors.append("monitoring evidence domains do not match the approved readiness contract")
    for name, evidence in monitoring.items():
        if not isinstance(evidence, dict):
            errors.append(f"{name} monitoring evidence has invalid declaration")
            continue
        if evidence.get("owner_required") is not True or evidence.get("status") not in ALLOWED_STATUSES:
            errors.append(f"{name} has invalid monitoring evidence requirements")
        if evidence.get("status") == "ACCEPTED" and not all(str(evidence.get(key, "")).strip() for key in ("owner", "evidence_reference", "source_fingerprint", "observed_at")):
            errors.append(f"{name} accepted monitoring evidence requires owner and provenance")
        if evidence.get("status") == "ACCEPTED" and not valid_provenance(evidence, owner_required=True):
            errors.append(f"{name} accepted monitoring evidence must use valid provenance metadata")
        if evidence.get("status") == "WAIVED" and not valid_waiver(evidence):
            errors.append(f"{name} WAIVED evidence requires formal waiver metadata")
    expected_recovery = {"postgresql-pitr", "object-restore", "openwa-session-recovery"}
    recovery = operations["recovery"]
    if set(recovery) != expected_recovery:
        errors.append("recovery evidence domains do not match the approved readiness contract")
    for name, evidence in recovery.items():
        if not isinstance(evidence, dict):
            errors.append(f"{name} recovery evidence has invalid declaration")
            continue
        if evidence.get("status") not in ALLOWED_STATUSES or evidence.get("rpo_target_required") is not True or evidence.get("rto_target_required") is not True:
            errors.append(f"{name} has invalid recovery evidence requirements")
        if evidence.get("status") == "ACCEPTED":
            rpo, rto = evidence.get("measured_rpo_seconds"), evidence.get("measured_rto_seconds")
            target_rpo, target_rto = evidence.get("target_rpo_seconds"), evidence.get("target_rto_seconds")
            measured = type(rpo) in (int, float) and rpo >= 0 and type(rto) in (int, float) and rto >= 0
            targets = type(target_rpo) in (int, float) and target_rpo >= 0 and type(target_rto) in (int, float) and target_rto >= 0
            provenance = all(str(evidence.get(key, "")).strip() for key in ("owner", "evidence_reference", "source_fingerprint", "observed_at"))
            if not measured or not targets or not provenance:
                errors.append(f"{name} accepted recovery evidence requires owner, explicit target/measured RPO/RTO and provenance")
            elif rpo > target_rpo or rto > target_rto:
                errors.append(f"{name} accepted recovery evidence exceeds its agreed RPO/RTO target")
            if not valid_provenance(evidence):
                errors.append(f"{name} accepted recovery evidence must use valid provenance metadata")
        if evidence.get("status") == "WAIVED" and not valid_waiver(evidence):
            errors.append(f"{name} WAIVED evidence requires formal waiver metadata")
    expected_runbooks = {
        "deployment-and-rollback": "docs/runbooks/deployment-and-rollback.md",
        "monitoring-and-incident-ownership": "docs/runbooks/monitoring-and-incident-ownership.md",
        "backup-and-restore-rehearsal": "docs/runbooks/backup-and-restore-rehearsal.md",
        "live-provider-validation": "docs/runbooks/live-provider-validation.md",
    }
    runbooks = data.get("runbook_requirements", {})
    if not isinstance(runbooks, dict) or set(runbooks) != set(expected_runbooks):
        errors.append("runbook requirements do not match the approved set")
    else:
        for name, expected_path in expected_runbooks.items():
            record = runbooks[name]
            if not isinstance(record, dict):
                errors.append(f"{name} runbook evidence has invalid declaration")
                continue
            if record.get("path") != expected_path:
                errors.append(f"{name} runbook path does not match the approved path")
            if record.get("exercise_status") not in ALLOWED_STATUSES:
                errors.append(f"{name} has invalid exercise status")
            if record.get("exercise_status") == "ACCEPTED" and not all(str(record.get(key, "")).strip() for key in ("owner", "evidence_reference", "source_fingerprint", "observed_at")):
                errors.append(f"{name} accepted runbook exercise requires owner and provenance")
            if record.get("exercise_status") == "ACCEPTED" and not valid_provenance(record, owner_required=True):
                errors.append(f"{name} accepted runbook exercise must use valid provenance metadata")
            if record.get("exercise_status") == "WAIVED" and not valid_waiver(record):
                errors.append(f"{name} WAIVED exercise requires formal waiver metadata")
            if not (ROOT / expected_path).is_file():
                errors.append(f"{name} approved runbook file is missing")
    return errors


def validate_release_coupling(data: dict, release: dict) -> list[str]:
    errors: list[str] = []
    if not isinstance(release, dict):
        return ["release gates contract must contain an object"]
    hard_gates = release.get("hard_gates")
    expected_ids = {f"RG-{index:03d}" for index in range(1, 9)}
    if not isinstance(hard_gates, list) or not hard_gates or any(not isinstance(gate, dict) for gate in hard_gates):
        return ["release gates contract hard_gates must be a non-empty array of gate objects"]
    gate_ids = [str(gate.get("id", "")) for gate in hard_gates]
    if len(gate_ids) != len(expected_ids) or set(gate_ids) != expected_ids:
        return ["release gates contract hard_gates must contain exactly RG-001 through RG-008"]
    gates = {str(gate.get("id")): str(gate.get("status")) for gate in hard_gates}
    operations = data.get("operational_evidence_requirements", {})
    for gate_id, section in (("RG-006", "recovery"), ("RG-007", "monitoring")):
        status = gates.get(gate_id)
        if status not in {"CLOSED", "WAIVED"}:
            continue
        records = operations.get(section, {}) if isinstance(operations, dict) else {}
        if (
            not isinstance(records, dict)
            or not records
            or any(not isinstance(record, dict) or record.get("status") != "ACCEPTED" for record in records.values())
        ):
            errors.append(f"{gate_id} cannot be {status} while {section} evidence is not fully ACCEPTED")
        if gate_id == "RG-007":
            runbooks = data.get("runbook_requirements", {})
            if (
                not isinstance(runbooks, dict)
                or not runbooks
                or any(not isinstance(record, dict) or record.get("exercise_status") != "ACCEPTED" for record in runbooks.values())
            ):
                errors.append(f"RG-007 cannot be {status} while runbook exercises are not fully ACCEPTED")
    runbooks = data.get("runbook_requirements", {})
    for gate_id, runbook_name in (
        ("RG-003", "live-provider-validation"),
        ("RG-006", "backup-and-restore-rehearsal"),
    ):
        status = gates.get(gate_id)
        if status not in {"CLOSED", "WAIVED"}:
            continue
        record = runbooks.get(runbook_name) if isinstance(runbooks, dict) else None
        if not isinstance(record, dict) or record.get("exercise_status") != "ACCEPTED":
            errors.append(
                f"{gate_id} cannot be {status} while {runbook_name} runbook exercise is not ACCEPTED"
            )
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--contract", type=Path, default=CONTRACT)
    parser.add_argument("--release-gates", type=Path, default=RELEASE_GATES)
    args = parser.parse_args()
    try:
        data = load_contract(args.contract)
    except RuntimeError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    errors = validate(data)
    try:
        release = json.loads(args.release_gates.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        print(f"ERROR: cannot load release gates: {exc}", file=sys.stderr)
        return 1
    errors.extend(validate_release_coupling(data, release))
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Deployment readiness contract valid. External evidence remains pending where declared.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
