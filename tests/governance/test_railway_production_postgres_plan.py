import json
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PLAN = ROOT / "infrastructure/railway/production-postgres-plan-2026-10-02.json"
EVIDENCE = ROOT / "evidence/release-gates/railway-production-postgres-2026-10-02.json"
ADR = ROOT / "docs/decisions/ADR-0007-railway-authoritative-postgres.md"
VERIFY = ROOT / "scripts/verify-railway-production-postgres-plan.py"


class RailwayProductionPostgresPlanTests(unittest.TestCase):
    def load_json(self, path: Path) -> dict:
        self.assertTrue(path.is_file(), f"missing {path.relative_to(ROOT)}")
        with path.open(encoding="utf-8") as handle:
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

    def test_plan_records_provisioned_railway_managed_postgres(self):
        plan = self.load_json(PLAN)
        database = plan["database"]
        self.assertEqual(database["provider"], "railway-managed-postgresql")
        self.assertEqual(database["status"], "PROVISIONED_PITR_ENABLED_PENDING_ROLE_MIGRATION_RESTORE_EVIDENCE")
        self.assertEqual(database["service_id"], "cc26de87-8b13-408c-b750-42e35ebb5f52")
        self.assertEqual(database["name"], "Postgres")
        self.assertGreaterEqual(database["engine_major_version"], database["minimum_major_version"])
        self.assertIn("postgres-ssl", database["image"])
        self.assertEqual(database["deployment_status"], "SUCCESS")
        self.assertTrue(database["ssl_required"])
        self.assertTrue(database["pitr_required"])
        self.assertTrue(database["backup_required"])
        self.assertTrue(database["restore_rehearsal_required"])
        forbidden = set(plan["forbidden_database_targets"])
        self.assertIn("hostinger-authoritative-postgres", forbidden)
        self.assertIn("plain-docker-postgres-without-managed-volume-and-pitr", forbidden)
        self.assertIn("supabase-v1-primary", forbidden)

    def test_plan_records_private_network_and_no_public_exposure(self):
        plan = self.load_json(PLAN)
        database = plan["database"]
        self.assertEqual(database["volume"]["mount_path"], "/var/lib/postgresql/data")
        self.assertEqual(database["volume"]["region"], "ams")
        self.assertEqual(database["private_network"]["hostname"], "postgres.railway.internal")
        self.assertEqual(database["private_network"]["state"], "ready")
        self.assertEqual(database["private_network"]["sync_status"], "ACTIVE")
        self.assertEqual(database["public_exposure"], {"domains": [], "tcp_proxies": []})

    def test_plan_records_pitr_enabled_with_live_probe_pending_ssh_key(self):
        plan = self.load_json(PLAN)
        pitr = plan["database"]["pitr"]
        self.assertTrue(pitr["enabled"])
        self.assertTrue(pitr["bucket_wired"])
        self.assertEqual(pitr["bucket"]["name"], "Postgres-PITR")
        self.assertEqual(pitr["bucket"]["region"], "ams")
        self.assertEqual(pitr["live_probe"]["coverage_status"], "PENDING_RAILWAY_SSH_KEY")
        self.assertEqual(pitr["live_probe"]["archiver_status"], "PENDING_RAILWAY_SSH_KEY")

    def test_plan_has_service_role_bindings_for_all_backend_services(self):
        plan = self.load_json(PLAN)
        self.assertEqual(plan["service_role_bindings"], {
            "control-api": "campaign_control_api",
            "audience-worker": "campaign_audience_worker",
            "campaign-worker": "campaign_campaign_worker",
            "export-worker": "campaign_export_worker",
            "inbound-governance-worker": "campaign_inbound_governance_worker",
            "metrics-worker": "campaign_metrics_worker",
            "platform-governance-worker": "campaign_platform_governance_worker",
        })

    def test_plan_and_evidence_contain_no_secret_values_and_no_gate_claim(self):
        plan = self.load_json(PLAN)
        evidence = self.load_json(EVIDENCE)
        raw = json.dumps({"plan": plan, "evidence": evidence}).lower()
        self.assertNotIn("postgres://", raw)
        self.assertNotIn("postgresql://", raw)
        self.assertNotIn("password=", raw)
        non_claims = " ".join(plan["explicit_non_claims"]).lower()
        self.assertIn("no application database_url", non_claims)
        self.assertIn("no service-specific login roles", non_claims)
        self.assertIn("no production migration", non_claims)
        self.assertIn("no backup restore rehearsal", non_claims)
        self.assertIn("no release gate", non_claims)

    def test_evidence_matches_plan(self):
        plan = self.load_json(PLAN)
        evidence = self.load_json(EVIDENCE)
        database = plan["database"]
        self.assertEqual(evidence["service"]["id"], database["service_id"])
        self.assertEqual(evidence["volume"], database["volume"])
        self.assertEqual(evidence["private_network"], database["private_network"])
        self.assertEqual(evidence["public_exposure"], database["public_exposure"])
        self.assertEqual(evidence["pitr"], database["pitr"])

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
