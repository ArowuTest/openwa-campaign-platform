import json
import hashlib
import importlib.util
import subprocess
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class ReleaseReadinessTests(unittest.TestCase):
    def production_evidence_fixture(self, root: Path, *, gate_id="RG-004", candidate="f" * 64, owner="performance", approver="release-authority", kind="capacity-report"):
        evidence_bytes = b'{"result":"independent evidence"}'
        evidence_path = root / gate_id / "artifacts" / "evidence.json"
        evidence_path.parent.mkdir(parents=True, exist_ok=True)
        evidence_path.write_bytes(evidence_bytes)
        manifest = {
            "schema_version": 1,
            "gate_id": gate_id,
            "candidate_fingerprint": candidate,
            "owner": owner,
            "approver": approver,
            "evidence": [{
                "kind": kind,
                "reference": f"{gate_id}/artifacts/evidence.json",
                "source_fingerprint": hashlib.sha256(evidence_bytes).hexdigest(),
                "observed_at": "2026-08-18T17:00:00Z",
            }],
        }
        raw = json.dumps(manifest, sort_keys=True).encode("utf-8")
        artifact = root / gate_id / "capacity.json"
        artifact.parent.mkdir(parents=True, exist_ok=True)
        artifact.write_bytes(raw)
        return artifact, hashlib.sha256(raw).hexdigest()

    def test_deployment_readiness_coupling_subprocess_failure_without_diagnostics_fails_closed(self):
        spec = importlib.util.spec_from_file_location("verify_release_readiness", ROOT / "scripts/verify-release-readiness.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        errors = []
        original = module.subprocess.run
        module.subprocess.run = lambda *args, **kwargs: subprocess.CompletedProcess(args[0], 137, "", "")
        try:
            module.validate_deployment_readiness_coupling(Path("release.json"), Path("readiness.json"), errors)
        finally:
            module.subprocess.run = original
        self.assertTrue(errors)
        self.assertIn("failed", errors[0].lower())

    def test_development_governance_check_passes(self):
        result = subprocess.run(
            ["python3", "scripts/verify-release-readiness.py"],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Governance structure valid", result.stdout)

    def test_production_candidate_fails_with_open_gates(self):
        result = subprocess.run(
            ["python3", "scripts/verify-release-readiness.py", "--production-candidate"],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("Production-candidate gate failed", result.stderr)

    def test_release_gate_ids_are_unique(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text())
        ids = [gate["id"] for gate in data["hard_gates"]]
        self.assertEqual(len(ids), len(set(ids)))



    def test_release_verifier_rejects_missing_or_extra_hard_gate_ids(self):
        base = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        scenarios = []
        missing = json.loads(json.dumps(base))
        missing["hard_gates"] = [g for g in missing["hard_gates"] if g["id"] != "RG-005"]
        scenarios.append(missing)
        extra = json.loads(json.dumps(base))
        extra["hard_gates"].append({"id": "RG-999", "name": "shadow", "criterion": "never", "evidence": "none", "status": "OPEN"})
        scenarios.append(extra)
        for index, data in enumerate(scenarios):
            with tempfile.TemporaryDirectory() as td:
                path = Path(td) / "release-gates.json"
                path.write_text(json.dumps(data), encoding="utf-8")
                result = subprocess.run(["python3", "scripts/verify-release-readiness.py", "--release-gates", str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
            self.assertNotEqual(result.returncode, 0, index)
            self.assertIn("exact hard gate set", result.stderr.lower(), index)

    def test_release_verifier_rejects_closed_gate_without_structured_closure_evidence(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        next(g for g in data["hard_gates"] if g["id"] == "RG-004")["status"] = "CLOSED"
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", "scripts/verify-release-readiness.py", "--release-gates", str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("closure evidence", result.stderr.lower())

    def test_release_verifier_rejects_bare_waived_gate(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        next(g for g in data["hard_gates"] if g["id"] == "RG-005")["status"] = "WAIVED"
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", "scripts/verify-release-readiness.py", "--release-gates", str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("formal waiver metadata", result.stderr.lower())

    def test_rg001_cannot_close_while_must_traceability_is_incomplete(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        gate = next(g for g in data["hard_gates"] if g["id"] == "RG-001")
        gate["status"] = "CLOSED"
        gate["closure_evidence"] = [{"owner": "release", "evidence_reference": "artifact://traceability", "source_fingerprint": "a" * 64, "observed_at": "2026-08-18T17:00:00Z"}]
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", "scripts/verify-release-readiness.py", "--release-gates", str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("rg-001 cannot be closed", result.stderr.lower())

    def test_release_verifier_rejects_required_document_path_escape(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        data["required_documents"].append("../outside.md")
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", "scripts/verify-release-readiness.py", "--release-gates", str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("repo-relative", result.stderr.lower())

    def test_release_verifier_enforces_deployment_readiness_coupling(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        gate = next(g for g in data["hard_gates"] if g["id"] == "RG-006")
        gate["status"] = "CLOSED"
        gate["closure_evidence"] = [{"owner": "release", "evidence_reference": "artifact://dr", "source_fingerprint": "b" * 64, "observed_at": "2026-08-18T17:00:00Z"}]
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", "scripts/verify-release-readiness.py", "--release-gates", str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("rg-006 cannot be closed", result.stderr.lower())
        self.assertIn("recovery evidence", result.stderr.lower())


    def test_release_verifier_accepts_formal_time_bounded_waiver_metadata(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        gate = next(g for g in data["hard_gates"] if g["id"] == "RG-005")
        gate["status"] = "WAIVED"
        now = datetime.now(timezone.utc)
        gate["waiver"] = {
            "owner": "security",
            "approver": "release-authority",
            "waiver_reason": "external penetration test scheduled",
            "evidence_reference": "artifact://waiver/RG-005",
            "source_fingerprint": "c" * 64,
            "observed_at": (now - timedelta(minutes=1)).isoformat().replace("+00:00", "Z"),
            "expires_at": (now + timedelta(days=7)).isoformat().replace("+00:00", "Z"),
        }
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", "scripts/verify-release-readiness.py", "--release-gates", str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_release_verifier_accepts_structured_closure_evidence_for_non_coupled_gate(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        gate = next(g for g in data["hard_gates"] if g["id"] == "RG-004")
        gate["status"] = "CLOSED"
        gate["closure_evidence"] = [{
            "owner": "performance",
            "evidence_reference": "artifact://performance/RG-004",
            "source_fingerprint": "d" * 64,
            "observed_at": "2026-08-18T17:00:00Z",
        }]
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", "scripts/verify-release-readiness.py", "--release-gates", str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_production_candidate_rejects_unbound_closure_evidence(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        gate = next(g for g in data["hard_gates"] if g["id"] == "RG-004")
        gate["status"] = "CLOSED"
        gate["closure_evidence"] = [{
            "owner": "performance", "approver": "release-authority",
            "evidence_reference": "RG-004/capacity.json",
            "source_fingerprint": "d" * 64,
            "observed_at": "2026-08-18T17:00:00Z",
        }]
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", "scripts/verify-release-readiness.py", "--production-candidate", "--release-gates", str(path), "--evidence-root", td, "--candidate-fingerprint", "f" * 64],
                cwd=ROOT, text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("evidence artifact", result.stderr.lower())

    def test_production_candidate_accepts_candidate_and_hash_bound_closure_evidence(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        gate = next(g for g in data["hard_gates"] if g["id"] == "RG-004")
        gate["status"] = "CLOSED"
        candidate = "f" * 64
        with tempfile.TemporaryDirectory() as td:
            evidence_root = Path(td) / "evidence"
            artifact, digest = self.production_evidence_fixture(evidence_root, candidate=candidate)
            gate["closure_evidence"] = [{
                "owner": "performance", "approver": "release-authority",
                "evidence_reference": artifact.relative_to(evidence_root).as_posix(), "source_fingerprint": digest,
                "observed_at": "2026-08-18T17:00:00Z",
            }]
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", "scripts/verify-release-readiness.py", "--production-candidate", "--release-gates", str(path), "--evidence-root", str(evidence_root), "--candidate-fingerprint", candidate],
                cwd=ROOT, text=True, capture_output=True, check=False,
            )
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertNotIn("evidence artifact", result.stderr.lower())
        self.assertNotIn("candidate fingerprint", result.stderr.lower())

    def test_production_candidate_rejects_misbinding_and_self_approval(self):
        scenarios = ["hash", "gate", "candidate", "escape", "self-approval"]
        for scenario in scenarios:
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as td:
                data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
                gate = next(g for g in data["hard_gates"] if g["id"] == "RG-004")
                gate["status"] = "CLOSED"
                candidate = "f" * 64
                owner = "performance"
                approver = "Performance" if scenario == "self-approval" else "release-authority"
                evidence_root = Path(td) / "evidence"
                manifest_gate = "RG-005" if scenario == "gate" else "RG-004"
                manifest_candidate = "e" * 64 if scenario == "candidate" else candidate
                artifact, digest = self.production_evidence_fixture(evidence_root, gate_id=manifest_gate, candidate=manifest_candidate, owner=owner, approver=approver)
                gate["closure_evidence"] = [{
                    "owner": owner, "approver": approver,
                    "evidence_reference": "../outside.json" if scenario == "escape" else artifact.relative_to(evidence_root).as_posix(),
                    "source_fingerprint": "0" * 64 if scenario == "hash" else digest,
                    "observed_at": "2026-08-18T17:00:00Z",
                }]
                path = Path(td) / "release-gates.json"
                path.write_text(json.dumps(data), encoding="utf-8")
                result = subprocess.run(
                    ["python3", "scripts/verify-release-readiness.py", "--production-candidate", "--release-gates", str(path), "--evidence-root", str(evidence_root), "--candidate-fingerprint", candidate],
                    cwd=ROOT, text=True, capture_output=True, check=False,
                )
                self.assertNotEqual(result.returncode, 2, result.stderr)
                self.assertTrue("evidence artifact" in result.stderr.lower() or "independent approver" in result.stderr.lower(), result.stderr)

    def test_production_candidate_rejects_unbound_manifest_evidence_items(self):
        for scenario in ["missing", "hash", "kind", "self-reference"]:
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as td:
                data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
                gate = next(g for g in data["hard_gates"] if g["id"] == "RG-004")
                gate["status"] = "CLOSED"
                candidate = "f" * 64
                evidence_root = Path(td) / "evidence"
                manifest_path, _ = self.production_evidence_fixture(evidence_root, candidate=candidate)
                manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
                item = manifest["evidence"][0]
                if scenario == "missing":
                    item["reference"] = "RG-004/artifacts/missing.json"
                elif scenario == "hash":
                    item["source_fingerprint"] = "0" * 64
                elif scenario == "kind":
                    item["kind"] = "self-attestation"
                else:
                    item["reference"] = "RG-004/capacity.json"
                raw = json.dumps(manifest, sort_keys=True).encode("utf-8")
                manifest_path.write_bytes(raw)
                gate["closure_evidence"] = [{
                    "owner": "performance", "approver": "release-authority",
                    "evidence_reference": manifest_path.relative_to(evidence_root).as_posix(),
                    "source_fingerprint": hashlib.sha256(raw).hexdigest(),
                    "observed_at": "2026-08-18T17:00:00Z",
                }]
                release_path = Path(td) / "release-gates.json"
                release_path.write_text(json.dumps(data), encoding="utf-8")
                result = subprocess.run(
                    ["python3", "scripts/verify-release-readiness.py", "--production-candidate", "--release-gates", str(release_path), "--evidence-root", str(evidence_root), "--candidate-fingerprint", candidate],
                    cwd=ROOT, text=True, capture_output=True, check=False,
                )
                self.assertNotEqual(result.returncode, 2, result.stderr)
                self.assertIn("evidence", result.stderr.lower())

    def test_production_candidate_rejects_manifest_shaped_leaf_evidence(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        gate = next(g for g in data["hard_gates"] if g["id"] == "RG-004")
        gate["status"] = "CLOSED"
        candidate = "f" * 64
        with tempfile.TemporaryDirectory() as td:
            evidence_root = Path(td) / "evidence"
            gate_root = evidence_root / "RG-004"
            gate_root.mkdir(parents=True)
            sibling_manifest = {
                "schema_version": 1,
                "gate_id": "RG-004",
                "candidate_fingerprint": candidate,
                "owner": "performance",
                "approver": "release-authority",
                "evidence": [{
                    "kind": "capacity-report",
                    "reference": "RG-004/sibling-manifest.json",
                    "source_fingerprint": "0" * 64,
                    "observed_at": "2026-08-18T17:00:00Z",
                }],
            }
            sibling_raw = json.dumps(sibling_manifest, sort_keys=True).encode("utf-8")
            sibling_path = gate_root / "sibling-manifest.json"
            sibling_path.write_bytes(sibling_raw)
            outer_manifest = dict(sibling_manifest)
            outer_manifest["evidence"] = [{
                "kind": "capacity-report",
                "reference": "RG-004/sibling-manifest.json",
                "source_fingerprint": hashlib.sha256(sibling_raw).hexdigest(),
                "observed_at": "2026-08-18T17:00:00Z",
            }]
            outer_raw = json.dumps(outer_manifest, sort_keys=True).encode("utf-8")
            outer_path = gate_root / "capacity.json"
            outer_path.write_bytes(outer_raw)
            gate["closure_evidence"] = [{
                "owner": "performance", "approver": "release-authority",
                "evidence_reference": "RG-004/capacity.json",
                "source_fingerprint": hashlib.sha256(outer_raw).hexdigest(),
                "observed_at": "2026-08-18T17:00:00Z",
            }]
            release_path = Path(td) / "release-gates.json"
            release_path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", "scripts/verify-release-readiness.py", "--production-candidate", "--release-gates", str(release_path), "--evidence-root", str(evidence_root), "--candidate-fingerprint", candidate],
                cwd=ROOT, text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 2, result.stderr)
        self.assertIn("manifest", result.stderr.lower())

    def test_production_candidate_accepts_bound_waiver_evidence_for_waivable_gate(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        gate = next(g for g in data["hard_gates"] if g["id"] == "RG-005")
        gate["status"] = "WAIVED"
        candidate = "f" * 64
        now = datetime.now(timezone.utc)
        with tempfile.TemporaryDirectory() as td:
            evidence_root = Path(td) / "evidence"
            manifest_path, digest = self.production_evidence_fixture(evidence_root, gate_id="RG-005", candidate=candidate, owner="security", kind="security-report")
            gate["waiver"] = {
                "owner": "security", "approver": "release-authority", "waiver_reason": "external assessment scheduled",
                "evidence_reference": manifest_path.relative_to(evidence_root).as_posix(), "source_fingerprint": digest,
                "observed_at": (now - timedelta(minutes=1)).isoformat().replace("+00:00", "Z"),
                "expires_at": (now + timedelta(days=7)).isoformat().replace("+00:00", "Z"),
            }
            release_path = Path(td) / "release-gates.json"
            release_path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", "scripts/verify-release-readiness.py", "--production-candidate", "--release-gates", str(release_path), "--evidence-root", str(evidence_root), "--candidate-fingerprint", candidate],
                cwd=ROOT, text=True, capture_output=True, check=False,
            )
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertNotIn("evidence artifact", result.stderr.lower())


    def test_release_verifier_rejects_required_document_set_drift(self):
        base = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        scenarios = []
        missing = json.loads(json.dumps(base))
        missing["required_documents"] = missing["required_documents"][1:]
        scenarios.append(missing)
        extra = json.loads(json.dumps(base))
        extra["required_documents"].append("docs/README.md")
        scenarios.append(extra)
        for index, data in enumerate(scenarios):
            with tempfile.TemporaryDirectory() as td:
                path = Path(td) / "release-gates.json"
                path.write_text(json.dumps(data), encoding="utf-8")
                result = subprocess.run(["python3", "scripts/verify-release-readiness.py", "--release-gates", str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
            self.assertNotEqual(result.returncode, 0, index)
            self.assertIn("exact required governance document set", result.stderr.lower(), index)

    def test_release_verifier_rejects_hard_gate_definition_dridt(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        gate = next(g for g in data["hard_gates"] if g["id"] == "RG-005")
        gate["criterion"] = "Any evidence is acceptable."
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3", "scripts/verify-release-readiness.py", "--release-gates", str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("gate definition", result.stderr.lower())


    def test_production_candidate_rejects_waiver_laundering_of_rg006_rg007(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        waiver = {"owner": "ops", "approver": "release-authority", "waiver_reason": "temporary test waiver", "evidence_reference": "artifact://waiver", "source_fingerprint": "a" * 64, "observed_at": "2026-08-18T08:00:00Z", "expires_at": "2099-08-20T08:00:00Z"}
        for gate in data["hard_gates"]:
            gate["status"] = "WAIVED"
            gate["waiver"] = dict(waiver)
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release-gates.json"
            path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(
                ["python3", "scripts/verify-release-readiness.py", "--production-candidate", "--release-gates", str(path)],
                cwd=ROOT, text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue("rg-006" in result.stderr.lower() or "rg-007" in result.stderr.lower())

    def test_release_waiver_requires_independent_approver(self):
        for owner, approver in [('same-actor', 'same-actor'), ('Release-Authority', 'release-authority')]:
            with self.subTest(owner=owner, approver=approver):
                data=json.loads((ROOT/'config/release-gates.json').read_text(encoding='utf-8'))
                gate=next(g for g in data['hard_gates'] if g['id']=='RG-005'); gate['status']='WAIVED'
                gate['waiver']={'owner':owner,'approver':approver,'waiver_reason':'test','evidence_reference':'artifact://waiver/self','source_fingerprint':'a'*64,'observed_at':'2026-08-20T10:00:00Z','expires_at':'2099-08-20T10:00:00Z'}
                with tempfile.TemporaryDirectory() as td:
                    path=Path(td)/'release.json'; path.write_text(json.dumps(data),encoding='utf-8')
                    result=subprocess.run(['python3','scripts/verify-release-readiness.py','--release-gates',str(path)],cwd=ROOT,text=True,capture_output=True,check=False)
                self.assertNotEqual(result.returncode,0)
                self.assertIn('formal waiver metadata',result.stderr.lower())

    def test_rg001_waiver_cannot_bypass_must_traceability(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text(encoding="utf-8"))
        gate = next(g for g in data["hard_gates"] if g["id"] == "RG-001")
        gate["status"] = "WAIVED"
        gate["waiver"] = {"owner":"release","approver":"independent-authority","waiver_reason":"test","evidence_reference":"artifact://waiver/RG-001","source_fingerprint":"a"*64,"observed_at":"2026-08-20T10:00:00Z","expires_at":"2099-08-20T10:00:00Z"}
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "release.json"; path.write_text(json.dumps(data), encoding="utf-8")
            result = subprocess.run(["python3","scripts/verify-release-readiness.py","--release-gates",str(path)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("rg-001", result.stderr.lower())
        self.assertIn("must", result.stderr.lower())

    def test_rg001_rejects_malformed_traceability_entries(self):
        release=json.loads((ROOT/"config/release-gates.json").read_text(encoding="utf-8"))
        gate=next(g for g in release["hard_gates"] if g["id"]=="RG-001"); gate["status"]="CLOSED"
        gate["closure_evidence"]=[{"owner":"release","evidence_reference":"artifact://rg001","source_fingerprint":"a"*64,"observed_at":"2026-08-20T10:00:00Z"}]
        trace={"requirements":["malformed-row"]}
        with tempfile.TemporaryDirectory() as td:
            rp=Path(td)/"release.json"; tp=Path(td)/"trace.json"; rp.write_text(json.dumps(release),encoding="utf-8"); tp.write_text(json.dumps(trace),encoding="utf-8")
            result=subprocess.run(["python3","scripts/verify-release-readiness.py","--release-gates",str(rp),"--traceability",str(tp)],cwd=ROOT,text=True,capture_output=True,check=False)
        self.assertNotEqual(result.returncode,0); self.assertIn("malformed",result.stderr.lower())


if __name__ == "__main__":
    unittest.main()
