import json
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"
SHELL = ROOT / "infrastructure/railway/production-services-2026-10-02.json"
EVIDENCE = ROOT / "evidence/release-gates/railway-service-config-2026-10-03.json"
VERIFY = ROOT / "scripts/verify-railway-service-config.py"


class RailwayServiceConfigTests(unittest.TestCase):
    def load_json(self, path: Path) -> dict:
        self.assertTrue(path.is_file(), f"missing {path.relative_to(ROOT)}")
        with path.open(encoding="utf-8") as handle:
            data = json.load(handle)
        self.assertIsInstance(data, dict)
        return data

    def test_service_config_matches_contract_and_shell(self):
        contract = self.load_json(CONTRACT)
        shell = self.load_json(SHELL)
        evidence = self.load_json(EVIDENCE)
        self.assertEqual(evidence["railway_project"], shell["railway_project"])
        self.assertEqual(evidence["environment"], shell["environment"])
        self.assertEqual(set(evidence["services"]), set(contract["services"]))
        self.assertNotIn("openwa-gateway", evidence["services"])
        for service, declaration in contract["services"].items():
            entry = evidence["services"][service]
            self.assertEqual(entry["service_id"], shell["services"][service])
            self.assertEqual(entry["builder"], "DOCKERFILE")
            self.assertEqual(entry["build_environment"], "V3")
            self.assertEqual(entry["dockerfile_path"], declaration["dockerfile"])
            self.assertTrue((ROOT / entry["dockerfile_path"]).is_file())
            self.assertEqual(entry["runtime"], "V2")
            self.assertEqual(entry["healthcheck_path"], declaration["health_path"])
            self.assertEqual(entry["healthcheck_timeout_seconds"], 300)
            self.assertEqual(entry["region"], "ams")
            self.assertEqual(entry["replicas"], 1)

    def test_service_config_contains_no_variables_or_deploy_claims(self):
        evidence = self.load_json(EVIDENCE)
        for entry in evidence["services"].values():
            self.assertEqual(entry["variable_names"], [])
            self.assertIsNone(entry["latest_deployment"])
        non_claims = " ".join(evidence["explicit_non_claims"]).lower()
        self.assertIn("no application service", non_claims)
        self.assertIn("no backend service has production database_url", non_claims)
        self.assertIn("no service-specific login", non_claims)
        self.assertIn("no release gate", non_claims)
        self.assertIn("openwa-gateway", non_claims)

    def test_cli_verifier_accepts_current_service_config(self):
        result = subprocess.run(
            [sys.executable, str(VERIFY)],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Railway service runtime config evidence valid", result.stdout)


if __name__ == "__main__":
    unittest.main()
