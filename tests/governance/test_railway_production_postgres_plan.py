import json
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PLAN = ROOT / "infrastructure/railway/production-postgres-plan-2026-10-02.json"
ADR = ROOT / "docs/decisions/ADR-0007-railway-authoritative-postgres.md"
VERIFY = ROOT / "scripts/verify-railway-production-postgres-plan.py"


class RailwayProductionPostgresPlanTests(unittest.TestCase):
    def load_plan(self) -> dict:
        self.assertTrue(PLAN.is_file(), "production Postgres plan is missing")
        with PLAN.open(encoding="utf-8") as handle:
            value = json.load(handle)
        self.assertIsInstance(value, dict)
        return value

    def test_adr_records_architecture_decision(self):
        self.assertTrue(ADR.is_file(), "ADR-0007 is missing")
        text = ADR.read_text(encoding="utf-8")
        self.assertIn("Railway is the authoritative production PostgreSQL", text)
        self.assertIn("Hostinger must not host the authoritative campaign-platform PostgreSQL", text)
        self.assertIn("Supabase is not part of the V1 production architecture", text)
        self.assertIn("does not close RG-002", text)

    def test_plan_requires_railway_managed_postgres_and_blocks_alternatives(self):
        plan = self.load_plan()
        self.assertEqual(plan["database"]["provider"], "railway-managed-postgresql")
        self.assertEqual(plan["database"]["status"], "PENDING_PROVISIONING")
        self.assertTrue(plan["database"]["ssl_required"])
        self.assertTrue(plan["database"]["pitr_required"])
        self.assertTrue(plan["database"]["backup_required"])
        self.assertTrue(plan["database"]["restore_rehearsal_required"])
        forbidden = set(plan["forbidden_database_targets"])
        self.assertIn("hostinger-authoritative-postgres", forbidden)
        self.assertIn("plain-docker-postgres-without-managed-volume-and-pitr", forbidden)
        self.assertIn("supabase-v1-primary", forbidden)

    def test_plan_has_service_role_bindings_for_all_backend_services(self):
        plan = self.load_plan()
        self.assertEqual(plan["service_role_bindings"], {
            "control-api": "campaign_control_api",
            "audience-worker": "campaign_audience_worker",
            "campaign-worker": "campaign_campaign_worker",
            "export-worker": "campaign_export_worker",
            "inbound-governance-worker": "campaign_inbound_governance_worker",
            "metrics-worker": "campaign_metrics_worker",
            "platform-governance-worker": "campaign_platform_governance_worker",
        })

    def test_plan_contains_no_secret_values_and_no_gate_claim(self):
        plan = self.load_plan()
        raw = json.dumps(plan).lower()
        self.assertNotIn("postgres://", raw)
        self.assertNotIn("postgresql://", raw)
        self.assertNotIn("password=", raw)
        non_claims = " ".join(plan["explicit_non_claims"]).lower()
        self.assertIn("no database has been provisioned", non_claims)
        self.assertIn("no credentials", non_claims)
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
        self.assertIn("Railway production Postgres plan valid", result.stdout)


if __name__ == "__main__":
    unittest.main()
