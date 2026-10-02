import json
import json
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"
SHELL = ROOT / "infrastructure/railway/production-services-2026-10-02.json"
EVIDENCE = ROOT / "evidence/release-gates/railway-production-shell-2026-10-02.json"
README = ROOT / "infrastructure/railway/README.md"
OPERATIONS_DOC = ROOT / "docs/operations/railway-production-shell-2026-10-02.md"
VERIFY = ROOT / "scripts/verify-railway-production-shell.py"


class RailwayProductionShellTests(unittest.TestCase):
    def load_json(self, path: Path) -> dict:
        self.assertTrue(path.is_file(), f"missing {path.relative_to(ROOT)}")
        with path.open(encoding="utf-8") as handle:
            data = json.load(handle)
        self.assertIsInstance(data, dict)
        return data

    def test_production_shell_matches_authoritative_service_contract(self):
        contract = self.load_json(CONTRACT)
        shell = self.load_json(SHELL)

        expected_services = set(contract["services"])
        self.assertEqual(set(shell["services"]), expected_services)
        self.assertNotIn("openwa-gateway", shell["services"])
        self.assertIn("openwa-gateway", contract.get("forbidden_services", []))

        for service, service_id in shell["services"].items():
            self.assertRegex(service_id, r"^[0-9a-f-]{36}$")
            self.assertIn("dockerfile", contract["services"][service])
            self.assertIn("health_path", contract["services"][service])
            self.assertIn("secret_classes", contract["services"][service])

    def test_production_shell_has_project_environment_and_no_deployment_claim(self):
        shell = self.load_json(SHELL)
        self.assertEqual(shell["schema_version"], 1)
        self.assertEqual(shell["railway_project"]["name"], "openwa-prod")
        self.assertRegex(shell["railway_project"]["id"], r"^[0-9a-f-]{36}$")
        self.assertEqual(shell["environment"]["name"], "production")
        self.assertRegex(shell["environment"]["id"], r"^[0-9a-f-]{36}$")

        notes = " ".join(shell.get("notes", []))
        notes_lower = notes.lower()
        self.assertIn("Empty service shell only", notes)
        self.assertIn("no source deployment", notes_lower)
        self.assertIn("openwa-gateway remains forbidden", notes)

    def test_supporting_evidence_does_not_claim_release_gate_closure(self):
        evidence = self.load_json(EVIDENCE)
        self.assertEqual(evidence["schema_version"], 1)
        self.assertEqual(evidence["evidence_type"], "supporting-railway-production-shell")
        self.assertIn("does not close RG-002", evidence["scope"])
        self.assertIn("no production secrets", evidence["scope"])
        self.assertEqual(set(evidence["contract_alignment"]["expected_backend_services"]), set(self.load_json(CONTRACT)["services"]))
        self.assertEqual(evidence["contract_alignment"]["forbidden_service_absent"], "openwa-gateway")

    def test_documentation_preserves_boundary_and_non_claims(self):
        for path in (README, OPERATIONS_DOC):
            self.assertTrue(path.is_file(), f"missing {path.relative_to(ROOT)}")
            text = path.read_text(encoding="utf-8")
            self.assertIn("openwa-gateway", text)
            self.assertIn("supporting", text.lower())
            self.assertIn("not", text.lower())
            self.assertIn("release", text.lower())

    def test_cli_verifier_accepts_current_shell(self):
        result = subprocess.run(
            [sys.executable, str(VERIFY)],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Railway production shell evidence valid", result.stdout)


if __name__ == "__main__":
    unittest.main()
