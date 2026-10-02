import json
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"
SHELL = ROOT / "infrastructure/railway/production-services-2026-10-02.json"
PLAN = ROOT / "infrastructure/railway/production-variable-plan-2026-10-02.json"
VERIFY = ROOT / "scripts/verify-railway-production-variable-plan.py"


class RailwayProductionVariablePlanTests(unittest.TestCase):
    def load_json(self, path: Path) -> dict:
        self.assertTrue(path.is_file(), f"missing {path.relative_to(ROOT)}")
        with path.open(encoding="utf-8") as handle:
            data = json.load(handle)
        self.assertIsInstance(data, dict)
        return data

    def test_variable_plan_matches_service_contract_and_shell(self):
        contract = self.load_json(CONTRACT)
        shell = self.load_json(SHELL)
        plan = self.load_json(PLAN)
        self.assertEqual(plan["railway_project"], shell["railway_project"])
        self.assertEqual(plan["environment"], shell["environment"])
        self.assertEqual(set(plan["services"]), set(contract["services"]))
        self.assertNotIn("openwa-gateway", plan["services"])
        for service, declaration in contract["services"].items():
            planned = plan["services"][service]
            self.assertEqual(planned["service_id"], shell["services"][service])
            self.assertEqual(sorted(planned["required_environment"]), sorted(declaration.get("required_environment", [])))
            self.assertEqual(sorted(planned["secret_classes"]), sorted(declaration["secret_classes"]))
            self.assertEqual(planned["public_ingress"], declaration["public_ingress"])
            self.assertEqual(planned["tls_required"], declaration.get("tls_required", False))
            self.assertEqual(planned["health"], {"port": declaration["port"], "path": declaration["health_path"]})

    def test_variable_plan_contains_no_values_or_secrets(self):
        plan_text = PLAN.read_text(encoding="utf-8").lower()
        for forbidden in ("secret_value", "credential_value", "password", "token", "database_url"):
            self.assertNotIn(forbidden, plan_text)
        plan = self.load_json(PLAN)
        self.assertIn("no secret values", " ".join(plan["explicit_non_claims"]).lower())
        self.assertIn("no railway variables", " ".join(plan["explicit_non_claims"]).lower())
        self.assertIn("no release gate", " ".join(plan["explicit_non_claims"]).lower())

    def test_cli_verifier_accepts_current_plan(self):
        result = subprocess.run(
            [sys.executable, str(VERIFY)],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Railway production variable plan valid", result.stdout)


if __name__ == "__main__":
    unittest.main()
