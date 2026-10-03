import json
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / "evidence/release-gates/railway-db-identity-variables-2026-10-03.json"
VERIFY = ROOT / "scripts/verify-railway-db-identity-variables.py"

EXPECTED = {
    "control-api": "campaign_control_api",
    "audience-worker": "campaign_audience_worker",
    "campaign-worker": "campaign_campaign_worker",
    "export-worker": "campaign_export_worker",
    "inbound-governance-worker": "campaign_inbound_governance_worker",
    "metrics-worker": "campaign_metrics_worker",
    "platform-governance-worker": "campaign_platform_governance_worker",
}
REQUIRED = {"APP_ENV", "DATABASE_DRIVER", "DATABASE_EXPECTED_ROLE"}


class RailwayDBIdentityVariableTests(unittest.TestCase):
    def load_evidence(self) -> dict:
        self.assertTrue(EVIDENCE.is_file(), f"missing {EVIDENCE.relative_to(ROOT)}")
        with EVIDENCE.open(encoding="utf-8") as handle:
            data = json.load(handle)
        self.assertIsInstance(data, dict)
        return data

    def test_records_non_secret_identity_variables_only(self):
        data = self.load_evidence()
        self.assertEqual(data["evidence_type"], "railway-db-identity-variable-wiring")
        self.assertTrue(data["values_redacted"])
        self.assertTrue(data["skip_deploys"])
        self.assertEqual(set(data["variable_names_verified"]), REQUIRED)
        for service, expected_role in EXPECTED.items():
            entry = data["services"][service]
            self.assertEqual(entry["expected_role"], expected_role)
            self.assertTrue(REQUIRED.issubset(set(entry["variables_present"])))
            self.assertFalse(entry["database_url_present"])
            self.assertEqual(entry["sealed_variable_names"], [])
            self.assertIsNone(entry["latest_deployment"])
            self.assertNotIn("DATABASE_URL", entry["variables_present"])
            self.assertNotIn("DATABASE_URL_FILE", entry["variables_present"])

    def test_non_claims_keep_secret_and_deploy_work_open(self):
        data = self.load_evidence()
        text = " ".join(data["explicit_non_claims"]).lower()
        self.assertIn("no database_url secret", text)
        self.assertIn("no rotatable service login role", text)
        self.assertIn("no service-specific database password", text)
        self.assertIn("no app service", text)
        self.assertIn("no release gate", text)

    def test_cli_verifier_accepts_current_evidence(self):
        result = subprocess.run(
            [sys.executable, str(VERIFY)],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Railway DB identity variable evidence valid", result.stdout)


if __name__ == "__main__":
    unittest.main()
