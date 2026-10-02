import json
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CONTRACT = ROOT / "infrastructure/railway/service-contracts.json"
SHELL = ROOT / "infrastructure/railway/production-services-2026-10-02.json"
PLAN = ROOT / "infrastructure/railway/production-image-plan-2026-10-02.json"
VERIFY = ROOT / "scripts/verify-railway-production-image-plan.py"


class RailwayProductionImagePlanTests(unittest.TestCase):
    def load_json(self, path: Path) -> dict:
        self.assertTrue(path.is_file(), f"missing {path.relative_to(ROOT)}")
        with path.open(encoding="utf-8") as handle:
            data = json.load(handle)
        self.assertIsInstance(data, dict)
        return data

    def test_image_plan_matches_contract_and_shell(self):
        contract = self.load_json(CONTRACT)
        shell = self.load_json(SHELL)
        plan = self.load_json(PLAN)
        self.assertEqual(plan["railway_project"], shell["railway_project"])
        self.assertEqual(plan["environment"], shell["environment"])
        self.assertEqual(set(plan["services"]), set(contract["services"]))
        self.assertNotIn("openwa-gateway", plan["services"])
        for service, declaration in contract["services"].items():
            entry = plan["services"][service]
            self.assertEqual(entry["service_id"], shell["services"][service])
            self.assertEqual(entry["dockerfile"], declaration["dockerfile"])
            self.assertTrue((ROOT / entry["dockerfile"]).is_file())
            self.assertEqual(entry["image_variable"], service.upper().replace("-", "_") + "_IMAGE")
            self.assertEqual(entry["port"], declaration["port"])
            self.assertEqual(entry["health_path"], declaration["health_path"])
            self.assertEqual(entry["public_ingress"], declaration["public_ingress"])
            self.assertEqual(entry["source_commit"], plan["source"]["local_head"])

    def test_image_plan_is_pending_and_contains_no_digests(self):
        plan = self.load_json(PLAN)
        self.assertFalse(plan["source"]["github_remote_configured"])
        self.assertEqual(plan["source"]["push_status"], "NOT_PUSHED_NO_GITHUB_REMOTE")
        for entry in plan["services"].values():
            self.assertIsNone(entry["image_digest"])
            self.assertEqual(entry["build_status"], "PENDING_GITHUB_REMOTE_AND_DIGEST_PINNED_BUILD")
        non_claims = " ".join(plan["explicit_non_claims"]).lower()
        self.assertIn("no image digest", non_claims)
        self.assertIn("no github push", non_claims)
        self.assertIn("no release gate", non_claims)

    def test_cli_verifier_accepts_current_plan(self):
        result = subprocess.run(
            [sys.executable, str(VERIFY)],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Railway production image plan valid", result.stdout)


if __name__ == "__main__":
    unittest.main()
