import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
READINESS = ROOT / "config/deployment-readiness.json"
VERIFY_READINESS = ROOT / "scripts/verify-deployment-readiness.py"


class DeploymentReadinessTests(unittest.TestCase):
    def test_contract_declares_approved_provider_service_placement(self):
        self.assertTrue(READINESS.is_file(), "deployment-readiness contract is missing")
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        self.assertEqual(data["schema_version"], 1)
        self.assertEqual(set(data["providers"]["railway"]["services"]), {
            "control-api", "audience-worker", "campaign-worker", "export-worker",
            "inbound-governance-worker", "metrics-worker", "platform-governance-worker",
        })
        self.assertEqual(data["providers"]["hostinger"]["services"], ["openwa-gateway"])
        self.assertNotIn("openwa-gateway", data["providers"]["railway"]["services"])

    def test_every_service_requires_pending_external_image_and_health_evidence(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        railway = json.loads((ROOT / "infrastructure/railway/service-contracts.json").read_text(encoding="utf-8"))
        topology = json.loads((ROOT / "config/deployment-topology.json").read_text(encoding="utf-8"))
        requirements = data["service_evidence_requirements"]
        for provider, placement in data["providers"].items():
            self.assertEqual(set(requirements[provider]), set(placement["services"]))
            for service, evidence in requirements[provider].items():
                self.assertTrue(evidence["image"]["immutable_digest_required"], service)
                self.assertEqual(evidence["image"]["status"], "PENDING_EXTERNAL", service)
                self.assertEqual(evidence["health"]["status"], "PENDING_EXTERNAL", service)
        for service, contract in railway["services"].items():
            self.assertEqual(requirements["railway"][service]["health"]["port"], contract["port"])
            self.assertEqual(requirements["railway"][service]["health"]["path"], contract["health_path"])
        hostinger = topology["hostinger"]["service_contracts"]["openwa-gateway"]
        self.assertEqual(requirements["hostinger"]["openwa-gateway"]["health"], {
            "port": hostinger["port"], "path": hostinger["health_path"], "status": "PENDING_EXTERNAL"
        })


    def test_contract_keeps_monitoring_and_recovery_evidence_external_until_proven(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        operations = data["operational_evidence_requirements"]
        self.assertEqual(set(operations["monitoring"]), {
            "control-plane", "openwa-gateway-fleet", "postgresql", "redis", "object-storage", "meta-cloud"
        })
        for name, evidence in operations["monitoring"].items():
            self.assertTrue(evidence["owner_required"], name)
            self.assertEqual(evidence["status"], "PENDING_EXTERNAL", name)
        self.assertEqual(set(operations["recovery"]), {
            "postgresql-pitr", "object-restore", "openwa-session-recovery"
        })
        for name, evidence in operations["recovery"].items():
            self.assertEqual(evidence["status"], "BLOCKED_EXTERNAL", name)
            self.assertTrue(evidence["rpo_target_required"], name)
            self.assertTrue(evidence["rto_target_required"], name)
    def test_verifier_accepts_honest_pending_development_contract(self):
        self.assertTrue(VERIFY_READINESS.is_file(), "deployment-readiness verifier is missing")
        result = subprocess.run(
            ["python3", str(VERIFY_READINESS)], cwd=ROOT,
            text=True, capture_output=True, check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Deployment readiness contract valid", result.stdout)
    def test_verifier_rejects_accepted_mutable_image_evidence(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        image = data["service_evidence_requirements"]["railway"]["control-api"]["image"]
        image.update({
            "status": "ACCEPTED",
            "resolved_image": "example.invalid/control-api:latest",
            "evidence_reference": "artifact://control-api-image",
            "source_fingerprint": "a" * 64,
            "observed_at": "2026-08-18T08:00:00Z",
        })
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("digest-pinned", result.stderr)
    def test_verifier_rejects_accepted_image_without_provenance_metadata(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        image = data["service_evidence_requirements"]["railway"]["control-api"]["image"]
        image.update({
            "status": "ACCEPTED",
            "resolved_image": "example.invalid/control-api@sha256:" + "a" * 64,
        })
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("provenance metadata", result.stderr)
    def test_contract_declares_secret_classes_without_secret_values(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        railway = json.loads((ROOT / "infrastructure/railway/service-contracts.json").read_text(encoding="utf-8"))
        requirements = data["service_evidence_requirements"]
        for service, contract in railway["services"].items():
            secrets = requirements["railway"][service]["secrets"]
            self.assertEqual(set(secrets["classes"]), set(contract["secret_classes"]), service)
            self.assertEqual(secrets["status"], "PENDING_EXTERNAL", service)
            self.assertNotIn("value", secrets)
            self.assertNotIn("values", secrets)
        gateway = requirements["hostinger"]["openwa-gateway"]["secrets"]
        self.assertEqual(gateway["classes"], ["gateway-control"])
        self.assertEqual(gateway["status"], "PENDING_EXTERNAL")
        self.assertNotIn("value", gateway)
        self.assertNotIn("values", gateway)
    def test_verifier_rejects_secret_values_in_evidence_contract(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        data["service_evidence_requirements"]["railway"]["control-api"]["secrets"]["value"] = "not-allowed"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("secret values", result.stderr)
    def test_verifier_rejects_secret_class_drift_from_service_authority(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        data["service_evidence_requirements"]["railway"]["control-api"]["secrets"]["classes"] = ["database", "owner"]
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("secret classes do not match", result.stderr)
    def test_contract_declares_network_exposure_requirements(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        railway = json.loads((ROOT / "infrastructure/railway/service-contracts.json").read_text(encoding="utf-8"))
        requirements = data["service_evidence_requirements"]
        for service, contract in railway["services"].items():
            network = requirements["railway"][service]["network"]
            self.assertEqual(network["public_ingress"], contract["public_ingress"], service)
            self.assertEqual(network["status"], "PENDING_EXTERNAL", service)
        gateway = requirements["hostinger"]["openwa-gateway"]["network"]
        self.assertFalse(gateway["public_ingress"])
        self.assertTrue(gateway["private_bind_required"])
        self.assertTrue(gateway["cross_provider_tls_required"])
        self.assertEqual(gateway["status"], "PENDING_EXTERNAL")
    def test_verifier_rejects_network_exposure_drift(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        data["service_evidence_requirements"]["railway"]["control-api"]["network"]["public_ingress"] = False
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("network exposure does not match", result.stderr)
    def test_verifier_rejects_relaxed_hostinger_network_boundary(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        network = data["service_evidence_requirements"]["hostinger"]["openwa-gateway"]["network"]
        network.update({"public_ingress": True, "private_bind_required": False, "cross_provider_tls_required": False})
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Hostinger network boundary", result.stderr)

    def test_hostinger_accepted_network_requires_observed_private_bind(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        network = data["service_evidence_requirements"]["hostinger"]["openwa-gateway"]["network"]
        network.update({
            "status": "ACCEPTED", "owner": "operations", "tls_observed": True,
            "evidence_reference": "artifact://hostinger/network",
            "source_fingerprint": "a" * 64, "observed_at": "2026-08-20T10:00:00Z",
        })
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("private bind", result.stderr.lower())

    def test_hostinger_accepted_network_allows_observed_rfc1918_bind(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        network = data["service_evidence_requirements"]["hostinger"]["openwa-gateway"]["network"]
        network.update({
            "status": "ACCEPTED", "owner": "operations", "tls_observed": True,
            "private_bind_observed": True, "observed_bind_address": "10.20.30.40",
            "evidence_reference": "artifact://hostinger/network",
            "source_fingerprint": "a" * 64, "observed_at": "2026-08-20T10:00:00Z",
        })
        with tempfile.TemporaryDirectory() as td:
            accepted = Path(td) / "readiness.json"
            accepted.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(accepted)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_verifier_rejects_accepted_monitoring_without_owner_and_provenance(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        data["operational_evidence_requirements"]["monitoring"]["control-plane"]["status"] = "ACCEPTED"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("accepted monitoring evidence requires owner and provenance", result.stderr)
    def test_verifier_rejects_accepted_recovery_without_measured_rpo_rto_and_provenance(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        data["operational_evidence_requirements"]["recovery"]["postgresql-pitr"]["status"] = "ACCEPTED"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("accepted recovery evidence requires owner, explicit target/measured RPO/RTO and provenance", result.stderr)
    def test_verifier_rejects_release_gate_closure_without_accepted_evidence(self):
        gates = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        for gate_id, expected in (("RG-006", "recovery"), ("RG-007", "monitoring")):
            tampered = json.loads(json.dumps(gates))
            next(g for g in tampered["hard_gates"] if g["id"] == gate_id)["status"] = "CLOSED"
            with tempfile.TemporaryDirectory() as td:
                path = Path(td) / "release-gates.json"
                path.write_text(json.dumps(tampered), encoding="utf-8")
                result = subprocess.run(
                    ["python3", str(VERIFY_READINESS), "--release-gates", str(path)], cwd=ROOT,
                    text=True, capture_output=True, check=False,
                )
            self.assertNotEqual(result.returncode, 0, gate_id)
            self.assertIn(f"{gate_id} cannot be CLOSED", result.stderr)
            self.assertIn(expected, result.stderr)

    def test_rg003_and_rg006_require_their_specific_runbook_exercises(self):
        base = json.loads(READINESS.read_text(encoding="utf-8"))
        gates = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        provenance = {
            "status": "ACCEPTED", "owner": "ops", "evidence_reference": "artifact://accepted",
            "source_fingerprint": "a" * 64, "observed_at": "2026-08-20T10:00:00Z",
        }
        for record in base["operational_evidence_requirements"]["recovery"].values():
            record.update(provenance)
            record.update({"target_rpo_seconds": 60, "target_rto_seconds": 120, "measured_rpo_seconds": 30, "measured_rto_seconds": 60})

        for gate_id, runbook in (("RG-003", "live-provider-validation"), ("RG-006", "backup-and-restore-rehearsal")):
            data = json.loads(json.dumps(base))
            release = json.loads(json.dumps(gates))
            gate = next(item for item in release["hard_gates"] if item["id"] == gate_id)
            gate["status"] = "CLOSED"
            gate["closure_evidence"] = [{
                "owner": "release", "evidence_reference": f"artifact://{gate_id}",
                "source_fingerprint": "b" * 64, "observed_at": "2026-08-20T10:00:00Z",
            }]
            with tempfile.TemporaryDirectory() as td:
                contract = Path(td) / "readiness.json"
                release_path = Path(td) / "release.json"
                contract.write_text(json.dumps(data), encoding="utf-8")
                release_path.write_text(json.dumps(release), encoding="utf-8")
                result = subprocess.run(
                    ["python3", str(VERIFY_READINESS), "--contract", str(contract), "--release-gates", str(release_path)],
                    cwd=ROOT, text=True, capture_output=True, check=False,
                )
            self.assertNotEqual(result.returncode, 0, gate_id)
            self.assertIn(gate_id.lower(), result.stderr.lower(), gate_id)
            self.assertIn(runbook, result.stderr, gate_id)
    def test_repository_checks_run_deployment_readiness_verifier(self):
        check = (ROOT / "scripts/check.sh").read_text(encoding="utf-8")
        self.assertIn("python3 scripts/verify-deployment-readiness.py", check)
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("deployment-readiness:", makefile)
        check_line = next(line for line in makefile.splitlines() if line.startswith("check:"))
        self.assertIn("deployment-readiness", check_line)
    def test_verifier_rejects_accepted_health_without_provenance(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        health = data["service_evidence_requirements"]["railway"]["control-api"]["health"]
        health["status"] = "ACCEPTED"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT,
                text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("accepted health evidence requires owner and provenance", result.stderr)

    def test_verifier_rejects_accepted_network_without_provenance(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        network = data["service_evidence_requirements"]["railway"]["control-api"]["network"]
        network["status"] = "ACCEPTED"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("accepted network evidence requires owner and provenance", result.stderr)

    def test_verifier_rejects_accepted_secret_evidence_without_provenance(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        secrets = data["service_evidence_requirements"]["railway"]["control-api"]["secrets"]
        secrets["status"] = "ACCEPTED"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("accepted secret evidence requires owner and provenance", result.stderr)

    def test_contract_requires_operational_runbooks_with_pending_exercises(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        expected = {
            "deployment-and-rollback": "docs/runbooks/deployment-and-rollback.md",
            "monitoring-and-incident-ownership": "docs/runbooks/monitoring-and-incident-ownership.md",
            "backup-and-restore-rehearsal": "docs/runbooks/backup-and-restore-rehearsal.md",
            "live-provider-validation": "docs/runbooks/live-provider-validation.md",
        }
        self.assertEqual(set(data["runbook_requirements"]), set(expected))
        for name, path in expected.items():
            record = data["runbook_requirements"][name]
            self.assertEqual(record["path"], path)
            self.assertEqual(record["exercise_status"], "PENDING_EXTERNAL")
            self.assertTrue((ROOT / path).is_file(), name)

    def test_verifier_rejects_missing_or_unapproved_runbook(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        data["runbook_requirements"]["deployment-and-rollback"]["path"] = "docs/runbooks/missing.md"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("runbook path does not match", result.stderr)

    def test_verifier_rejects_accepted_runbook_exercise_without_provenance(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        record = data["runbook_requirements"]["deployment-and-rollback"]
        record["exercise_status"] = "ACCEPTED"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "readiness.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("accepted runbook exercise requires owner and provenance", result.stderr)

    def test_verifier_rejects_malformed_provenance_on_accepted_evidence(self):
        base = json.loads(READINESS.read_text(encoding="utf-8"))
        scenarios = []
        image = json.loads(json.dumps(base))
        image_record = image["service_evidence_requirements"]["railway"]["control-api"]["image"]
        image_record.update({"status": "ACCEPTED", "resolved_image": "example.invalid/control-api@sha256:" + "a" * 64, "evidence_reference": "artifact://image", "source_fingerprint": "bad", "observed_at": "2026-08-18T08:00:00Z"})
        scenarios.append(image)
        monitoring = json.loads(json.dumps(base))
        monitoring_record = monitoring["operational_evidence_requirements"]["monitoring"]["control-plane"]
        monitoring_record.update({"status": "ACCEPTED", "owner": "ops", "evidence_reference": "artifact://monitoring", "source_fingerprint": "a" * 64, "observed_at": "not-a-time"})
        scenarios.append(monitoring)
        recovery = json.loads(json.dumps(base))
        recovery_record = recovery["operational_evidence_requirements"]["recovery"]["postgresql-pitr"]
        recovery_record.update({"status": "ACCEPTED", "measured_rpo_seconds": 5, "measured_rto_seconds": 30, "evidence_reference": "artifact://recovery", "source_fingerprint": "bad", "observed_at": "2026-08-18T08:00:00Z"})
        scenarios.append(recovery)
        runbook = json.loads(json.dumps(base))
        runbook_record = runbook["runbook_requirements"]["deployment-and-rollback"]
        runbook_record.update({"exercise_status": "ACCEPTED", "owner": "ops", "evidence_reference": "artifact://exercise", "source_fingerprint": "a" * 64, "observed_at": "not-a-time"})
        scenarios.append(runbook)
        for index, data in enumerate(scenarios):
            with tempfile.TemporaryDirectory() as td:
                bad = Path(td) / f"readiness-{index}.json"
                bad.write_text(json.dumps(data), encoding="utf-8")
                result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
            self.assertNotEqual(result.returncode, 0, index)
            self.assertIn("valid provenance metadata", result.stderr, index)

    def test_verifier_rejects_health_endpoint_drift_from_service_authority(self):
        for provider, service, port, path in (("railway", "control-api", 9999, "/wrong"), ("hostinger", "openwa-gateway", 9999, "/wrong")):
            data = json.loads(READINESS.read_text(encoding="utf-8"))
            health = data["service_evidence_requirements"][provider][service]["health"]
            health.update({"port": port, "path": path})
            with tempfile.TemporaryDirectory() as td:
                bad = Path(td) / "readiness.json"
                bad.write_text(json.dumps(data), encoding="utf-8")
                result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
            self.assertNotEqual(result.returncode, 0, provider)
            self.assertIn("health endpoint does not match", result.stderr, provider)

    def test_verifier_rejects_bare_or_unbounded_waived_evidence(self):
        scenarios = []
        base = json.loads(READINESS.read_text(encoding="utf-8"))
        service = json.loads(json.dumps(base))
        service["service_evidence_requirements"]["railway"]["control-api"]["health"]["status"] = "WAIVED"
        scenarios.append(service)
        monitoring = json.loads(json.dumps(base))
        monitoring["operational_evidence_requirements"]["monitoring"]["control-plane"]["status"] = "WAIVED"
        scenarios.append(monitoring)
        recovery = json.loads(json.dumps(base))
        recovery["operational_evidence_requirements"]["recovery"]["postgresql-pitr"]["status"] = "WAIVED"
        scenarios.append(recovery)
        runbook = json.loads(json.dumps(base))
        runbook["runbook_requirements"]["deployment-and-rollback"]["exercise_status"] = "WAIVED"
        scenarios.append(runbook)
        for index, data in enumerate(scenarios):
            with tempfile.TemporaryDirectory() as td:
                bad = Path(td) / f"waived-{index}.json"
                bad.write_text(json.dumps(data), encoding="utf-8")
                result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
            self.assertNotEqual(result.returncode, 0, index)
            self.assertIn("formal waiver metadata", result.stderr, index)

    def test_verifier_rejects_duplicate_service_and_secret_class_declarations(self):
        base = json.loads(READINESS.read_text(encoding="utf-8"))
        scenarios = []
        services = json.loads(json.dumps(base))
        services["providers"]["railway"]["services"].append("control-api")
        scenarios.append(services)
        secrets = json.loads(json.dumps(base))
        secret_classes = secrets["service_evidence_requirements"]["railway"]["control-api"]["secrets"]["classes"]
        secret_classes.append(secret_classes[0])
        scenarios.append(secrets)
        for index, data in enumerate(scenarios):
            with tempfile.TemporaryDirectory() as td:
                bad = Path(td) / f"duplicate-{index}.json"
                bad.write_text(json.dumps(data), encoding="utf-8")
                result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
            self.assertNotEqual(result.returncode, 0, index)
            self.assertIn("duplicate", result.stderr.lower(), index)

    def test_verifier_rejects_unapproved_provider_sections(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        data["providers"]["shadow-provider"] = {"services": []}
        data["service_evidence_requirements"]["shadow-provider"] = {}
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "unapproved-provider.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("provider set", result.stderr.lower())

    def test_verifier_fail_closes_malformed_service_and_secret_declarations_without_traceback(self):
        base = json.loads(READINESS.read_text(encoding="utf-8"))
        scenarios = []
        services = json.loads(json.dumps(base))
        services["providers"]["railway"]["services"][0] = {"unexpected": "object"}
        scenarios.append(services)
        secrets = json.loads(json.dumps(base))
        secrets["service_evidence_requirements"]["railway"]["control-api"]["secrets"]["classes"][0] = {"unexpected": "object"}
        scenarios.append(secrets)
        for index, data in enumerate(scenarios):
            with tempfile.TemporaryDirectory() as td:
                bad = Path(td) / f"malformed-{index}.json"
                bad.write_text(json.dumps(data), encoding="utf-8")
                result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
            self.assertNotEqual(result.returncode, 0, index)
            self.assertIn("invalid declaration", result.stderr.lower(), index)
            self.assertNotIn("traceback", result.stderr.lower(), index)

    def test_verifier_rejects_future_dated_accepted_provenance(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        record = data["operational_evidence_requirements"]["monitoring"]["control-plane"]
        record.update({
            "status": "ACCEPTED",
            "owner": "ops",
            "evidence_reference": "artifact://monitoring/future",
            "source_fingerprint": "e" * 64,
            "observed_at": "2099-01-01T00:00:00Z",
        })
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "future.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("valid provenance metadata", result.stderr)

    def test_verifier_rejects_expired_formal_waiver(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        record = data["service_evidence_requirements"]["railway"]["control-api"]["health"]
        record.update({
            "status": "WAIVED",
            "owner": "ops",
            "approver": "release-authority",
            "waiver_reason": "temporary external dependency",
            "evidence_reference": "artifact://waiver/expired",
            "source_fingerprint": "f" * 64,
            "observed_at": "2026-08-10T00:00:00Z",
            "expires_at": "2026-08-11T00:00:00Z",
        })
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "expired-waiver.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("formal waiver metadata", result.stderr)


    def test_verifier_fail_closes_malformed_operational_and_runbook_records_without_traceback(self):
        base = json.loads(READINESS.read_text(encoding="utf-8"))
        scenarios = []
        monitoring = json.loads(json.dumps(base))
        monitoring["operational_evidence_requirements"]["monitoring"]["control-plane"] = "malformed"
        scenarios.append(monitoring)
        recovery = json.loads(json.dumps(base))
        recovery["operational_evidence_requirements"]["recovery"]["postgresql-pitr"] = ["malformed"]
        scenarios.append(recovery)
        runbook = json.loads(json.dumps(base))
        runbook["runbook_requirements"]["deployment-and-rollback"] = None
        scenarios.append(runbook)
        for index, data in enumerate(scenarios):
            with tempfile.TemporaryDirectory() as td:
                bad = Path(td) / f"malformed-operational-{index}.json"
                bad.write_text(json.dumps(data), encoding="utf-8")
                result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
            self.assertNotEqual(result.returncode, 0, index)
            self.assertIn("invalid declaration", result.stderr.lower(), index)
            self.assertNotIn("traceback", result.stderr.lower(), index)


    def test_rg007_closed_requires_all_runbook_exercises_accepted(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        provenance = {"status": "ACCEPTED", "owner": "ops", "evidence_reference": "artifact://monitoring", "source_fingerprint": "a" * 64, "observed_at": "2026-08-18T08:00:00Z"}
        for record in data["operational_evidence_requirements"]["monitoring"].values():
            record.update(provenance)

        gates = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        next(g for g in gates["hard_gates"] if g["id"] == "RG-007")["status"] = "CLOSED"
        with tempfile.TemporaryDirectory() as td:
            contract = Path(td) / "readiness.json"
            release = Path(td) / "release-gates.json"
            contract.write_text(json.dumps(data), encoding="utf-8")
            release.write_text(json.dumps(gates), encoding="utf-8")
            result = subprocess.run(
                ["python3", str(VERIFY_READINESS), "--contract", str(contract), "--release-gates", str(release)],
                cwd=ROOT, text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("rg-007", result.stderr.lower())
        self.assertIn("runbook", result.stderr.lower())

    def test_rg006_rg007_waiver_cannot_bypass_operational_evidence(self):
        gates = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        waiver = {"owner": "ops", "approver": "release-authority", "waiver_reason": "temporary test waiver", "evidence_reference": "artifact://waiver", "source_fingerprint": "a" * 64, "observed_at": "2026-08-18T08:00:00Z", "expires_at": "2099-08-20T08:00:00Z"}
        for gate_id in ("RG-006", "RG-007"):
            tampered = json.loads(json.dumps(gates))
            gate = next(g for g in tampered["hard_gates"] if g["id"] == gate_id)
            gate["status"] = "WAIVED"; gate["waiver"] = waiver
            with tempfile.TemporaryDirectory() as td:
                release = Path(td) / "release-gates.json"
                release.write_text(json.dumps(tampered), encoding="utf-8")
                result = subprocess.run(["python3", str(VERIFY_READINESS), "--release-gates", str(release)], cwd=ROOT, text=True, capture_output=True, check=False)
            self.assertNotEqual(result.returncode, 0, gate_id)
            self.assertIn(gate_id.lower(), result.stderr.lower(), gate_id)

    def test_waived_image_evidence_still_requires_immutable_digest(self):
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        image = data["service_evidence_requirements"]["railway"]["control-api"]["image"]
        image.update({
            "status": "WAIVED", "resolved_image": "example.invalid/control-api:latest",
            "owner": "ops", "approver": "release-authority",
            "waiver_reason": "temporary external registry exception",
            "evidence_reference": "artifact://waiver/image",
            "source_fingerprint": "a" * 64,
            "observed_at": "2026-08-20T00:00:00Z", "expires_at": "2026-08-30T00:00:00Z",
        })
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "waived-mutable-image.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", str(VERIFY_READINESS), "--contract", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("digest-pinned", result.stderr)



    def test_control_api_secret_classes_cover_sensitive_mounted_domains(self):
        railway = json.loads((ROOT / "infrastructure/railway/service-contracts.json").read_text(encoding="utf-8"))
        classes = set(railway["services"]["control-api"]["secret_classes"])
        compose = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        required = {
            "MSISDN_ENCRYPTION_KEY_BASE64_FILE": "msisdn",
            "INBOUND_CONTENT_KEYS_JSON_FILE": "inbound-content",
            "PRIVACY_EVIDENCE_KEYS_JSON_FILE": "privacy",
        }
        for marker, secret_class in required.items():
            self.assertIn(marker, compose)
            self.assertIn(secret_class, classes, f"control-api mounts {marker} but contract omits {secret_class}")

    def test_sensitive_secret_classes_and_public_ingress_tls_evidence_are_complete(self):
        railway = json.loads((ROOT / "infrastructure/railway/service-contracts.json").read_text(encoding="utf-8"))
        expected = {
            "control-api": {"sender-proxy", "media-download", "profiling"},
            "audience-worker": {"profiling"}, "campaign-worker": {"media-download", "profiling"},
            "export-worker": {"profiling"}, "inbound-governance-worker": {"profiling"},
            "metrics-worker": {"profiling"}, "platform-governance-worker": {"profiling"},
        }
        for service, required in expected.items():
            self.assertTrue(required <= set(railway["services"][service]["secret_classes"]), service)
        data = json.loads(READINESS.read_text(encoding="utf-8"))
        network = data["service_evidence_requirements"]["railway"]["control-api"]["network"]
        self.assertIs(network.get("tls_required"), True)
        network.update({"status":"ACCEPTED","evidence_reference":"artifact://network/control-api","source_fingerprint":"a"*64,"observed_at":"2026-08-20T10:00:00Z"})
        with tempfile.TemporaryDirectory() as td:
            bad=Path(td)/"accepted-without-tls.json"; bad.write_text(json.dumps(data),encoding="utf-8")
            result=subprocess.run(["python3",str(VERIFY_READINESS),"--contract",str(bad)],cwd=ROOT,text=True,capture_output=True,check=False)
        self.assertNotEqual(result.returncode,0)
        self.assertIn("tls",result.stderr.lower())

    def test_hostinger_accepted_network_requires_tls_observation(self):
        data=json.loads(READINESS.read_text(encoding='utf-8')); record=data['service_evidence_requirements']['hostinger']['openwa-gateway']['network']
        record.update({'status':'ACCEPTED','owner':'ops','evidence_reference':'artifact://hostinger/network','source_fingerprint':'a'*64,'observed_at':'2026-08-20T10:00:00Z'})
        with tempfile.TemporaryDirectory() as td:
            path=Path(td)/'readiness.json'; path.write_text(json.dumps(data),encoding='utf-8')
            result=subprocess.run(['python3',str(VERIFY_READINESS),'--contract',str(path)],cwd=ROOT,text=True,capture_output=True,check=False)
        self.assertNotEqual(result.returncode,0); self.assertIn('tls',result.stderr.lower())

    def test_readiness_waiver_requires_independent_approver(self):
        for owner, approver in [('same-actor', 'same-actor'), ('Operations', 'operations')]:
            with self.subTest(owner=owner, approver=approver):
                data=json.loads(READINESS.read_text(encoding='utf-8')); record=data['service_evidence_requirements']['railway']['control-api']['health']
                record.update({'status':'WAIVED','owner':owner,'approver':approver,'waiver_reason':'test','evidence_reference':'artifact://waiver/self','source_fingerprint':'a'*64,'observed_at':'2026-08-20T10:00:00Z','expires_at':'2099-08-20T10:00:00Z'})
                with tempfile.TemporaryDirectory() as td:
                    path=Path(td)/'readiness.json'; path.write_text(json.dumps(data),encoding='utf-8')
                    result=subprocess.run(['python3',str(VERIFY_READINESS),'--contract',str(path)],cwd=ROOT,text=True,capture_output=True,check=False)
                self.assertNotEqual(result.returncode,0); self.assertIn('waiver',result.stderr.lower())

    def test_accepted_service_evidence_requires_owner(self):
        data=json.loads(READINESS.read_text(encoding='utf-8')); record=data['service_evidence_requirements']['railway']['control-api']['health']
        record.update({'status':'ACCEPTED','evidence_reference':'artifact://health','source_fingerprint':'a'*64,'observed_at':'2026-08-20T10:00:00Z'})
        with tempfile.TemporaryDirectory() as td:
            path=Path(td)/'readiness.json'; path.write_text(json.dumps(data),encoding='utf-8')
            result=subprocess.run(['python3',str(VERIFY_READINESS),'--contract',str(path)],cwd=ROOT,text=True,capture_output=True,check=False)
        self.assertNotEqual(result.returncode,0); self.assertIn('owner',result.stderr.lower())

    def test_accepted_recovery_requires_targets_and_meets_them(self):
        base=json.loads(READINESS.read_text(encoding='utf-8')); scenarios=[]
        missing=json.loads(json.dumps(base)); r=missing['operational_evidence_requirements']['recovery']['postgresql-pitr']
        r.update({'status':'ACCEPTED','owner':'ops','measured_rpo_seconds':5,'measured_rto_seconds':30,'evidence_reference':'artifact://recovery','source_fingerprint':'a'*64,'observed_at':'2026-08-20T10:00:00Z'}); scenarios.append(missing)
        exceeded=json.loads(json.dumps(base)); r=exceeded['operational_evidence_requirements']['recovery']['postgresql-pitr']
        r.update({'status':'ACCEPTED','owner':'ops','target_rpo_seconds':5,'target_rto_seconds':30,'measured_rpo_seconds':6,'measured_rto_seconds':31,'evidence_reference':'artifact://recovery','source_fingerprint':'a'*64,'observed_at':'2026-08-20T10:00:00Z'}); scenarios.append(exceeded)
        for i,data in enumerate(scenarios):
            with tempfile.TemporaryDirectory() as td:
                path=Path(td)/f'readiness-{i}.json'; path.write_text(json.dumps(data),encoding='utf-8')
                result=subprocess.run(['python3',str(VERIFY_READINESS),'--contract',str(path)],cwd=ROOT,text=True,capture_output=True,check=False)
            self.assertNotEqual(result.returncode,0,i); self.assertTrue('target' in result.stderr.lower() or 'rpo' in result.stderr.lower(),result.stderr)

    def test_verifier_fail_closes_malformed_railway_contract_without_traceback(self):
        import importlib.util
        spec=importlib.util.spec_from_file_location("verify_deployment_readiness", VERIFY_READINESS); module=importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
        data=json.loads(READINESS.read_text(encoding="utf-8")); railway=json.loads((ROOT/"infrastructure/railway/service-contracts.json").read_text(encoding="utf-8")); railway["services"].pop("metrics-worker")
        with tempfile.TemporaryDirectory() as td:
            path=Path(td)/"railway.json"; path.write_text(json.dumps(railway),encoding="utf-8"); old=module.RAILWAY_CONTRACT; module.RAILWAY_CONTRACT=path
            try:
                try: errors=module.validate(data)
                except Exception as exc: self.fail(f"malformed Railway contract raised {type(exc).__name__}: {exc}")
            finally: module.RAILWAY_CONTRACT=old
        self.assertTrue(any("railway" in error.lower() for error in errors), errors)

    def test_nested_service_evidence_objects_fail_closed_without_traceback(self):
        base=json.loads(READINESS.read_text(encoding="utf-8"))
        for field in ("image","health","secrets","network"):
            data=json.loads(json.dumps(base)); data["service_evidence_requirements"]["railway"]["control-api"][field]="bad"
            with tempfile.TemporaryDirectory() as td:
                path=Path(td)/"readiness.json"; path.write_text(json.dumps(data),encoding="utf-8")
                result=subprocess.run([sys.executable,str(VERIFY_READINESS),"--contract",str(path)],cwd=ROOT,text=True,capture_output=True,check=False)
            self.assertNotEqual(result.returncode,0,field); self.assertNotIn("traceback",result.stderr.lower(),field); self.assertIn("invalid declaration",result.stderr.lower(),field)

    def test_nonobject_release_gates_fail_closed_without_traceback(self):
        for value in ([],"x"):
            with tempfile.TemporaryDirectory() as td:
                path=Path(td)/"release.json"; path.write_text(json.dumps(value),encoding="utf-8")
                result=subprocess.run([sys.executable,str(VERIFY_READINESS),"--release-gates",str(path)],cwd=ROOT,text=True,capture_output=True,check=False)
            self.assertNotEqual(result.returncode,0); self.assertNotIn("traceback",result.stderr.lower()); self.assertIn("release gates",result.stderr.lower())

    def test_malformed_hard_gates_member_fails_closed(self):
        for value in ({}, {"hard_gates": "garbage"}, {"hard_gates": {"RG-006": "CLOSED"}}):
            with tempfile.TemporaryDirectory() as td:
                path=Path(td)/"release.json"; path.write_text(json.dumps(value),encoding="utf-8")
                result=subprocess.run([sys.executable,str(VERIFY_READINESS),"--release-gates",str(path)],cwd=ROOT,text=True,capture_output=True,check=False)
            self.assertNotEqual(result.returncode,0,value); self.assertIn("hard_gates",result.stderr.lower(),value)


if __name__ == "__main__":
    unittest.main()
