import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DEPLOY_PACKET = ROOT / "infrastructure/railway/admin-web-deployment-execution-2026-10-06.json"
UAT_TEMPLATE = ROOT / "evidence/templates/openwa-live-provider-uat-template.json"
VERIFY_DEPLOY = ROOT / "scripts/verify-admin-web-deployment-packet.py"
VERIFY_UAT = ROOT / "scripts/verify-live-provider-uat-evidence.py"


class DeploymentUATPreparationTests(unittest.TestCase):
    def run_python(self, script: Path, *args):
        return subprocess.run(
            [sys.executable, str(script), *map(str, args)],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )

    def test_deployment_packet_is_staged_only_and_pins_accepted_source(self):
        packet = json.loads(DEPLOY_PACKET.read_text(encoding="utf-8"))
        self.assertEqual(packet["schema_version"], 1)
        self.assertEqual(packet["state"], "BLOCKED_UNPUSHED")
        self.assertEqual(
            packet["accepted_source_commit"],
            "daa9f3ddede0ecc5b7d6a52e579fba140a0c5e8d",
        )
        self.assertEqual(
            packet["reviewed_application_commit"],
            "f264237829e1caf4b0bac06c3aacd961885fc7c8",
        )
        self.assertEqual(packet["evidence_followup_commit"], packet["accepted_source_commit"])
        self.assertEqual(
            packet["application_to_source_delta_files"],
            ["evidence/release-gates/champion-hosting-accepted-source-commit-2026-10-06.json"],
        )
        self.assertTrue(packet["production_backend_requires_update"])
        self.assertEqual(packet["expected_post_deploy_source_commit"], packet["accepted_source_commit"])
        self.assertEqual(packet["repo"], "ArowuTest/openwa-campaign-platform")
        self.assertEqual(packet["service_name"], "admin-web")
        self.assertEqual(packet["environment_id"], "546fd711-cf80-4402-89b0-cb3ae5671fa9")
        self.assertEqual(packet["project_id"], "8633a0b9-3b15-4c8d-b6f2-0306b284f4dd")
        self.assertFalse(packet["remote_source_ready"])
        self.assertFalse(packet["explicit_deploy_authorization"])
        self.assertFalse(packet["deploy_committed"])
        self.assertEqual(packet["variable_plan"], {
            "ADMIN_WEB_CONTROL_API_URL": "http://${{control-api.RAILWAY_PRIVATE_DOMAIN}}:${{control-api.PORT}}"
        })
        operations = packet["staged_operations"]
        self.assertEqual([item["action"] for item in operations], [
            "create_service",
            "update_service",
            "set_variables",
            "connect_service_source",
        ])
        self.assertTrue(all(item["staged"] for item in operations))
        self.assertEqual(operations[-1]["commit_sha"], packet["accepted_source_commit"])
        self.assertEqual(packet["commit_operation"]["action"], "accept_deploy")
        self.assertTrue(packet["commit_operation"]["requires_explicit_user_authorization"])

    def test_deployment_packet_verifier_passes_plan_but_ready_mode_fails_until_push(self):
        result = self.run_python(VERIFY_DEPLOY)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Deployment packet valid", result.stdout)

        ready = self.run_python(VERIFY_DEPLOY, "--ready")
        self.assertNotEqual(ready.returncode, 0)
        self.assertIn("remote_source_ready", ready.stderr)
        self.assertIn("explicit_deploy_authorization", ready.stderr)

    def test_repository_check_includes_deployment_packet_validator(self):
        check = (ROOT / "scripts/check.sh").read_text(encoding="utf-8")
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("python3 scripts/verify-admin-web-deployment-packet.py", check)
        self.assertIn("admin-web-deployment-packet", makefile)
        check_line = next(line for line in makefile.splitlines() if line.startswith("check:"))
        self.assertIn("admin-web-deployment-packet", check_line)

    def test_uat_template_contains_no_full_msisdn_or_secret_material(self):
        raw = UAT_TEMPLATE.read_text(encoding="utf-8")
        data = json.loads(raw)
        self.assertEqual(data["schema_version"], 1)
        self.assertFalse(data["release_certification_claimed"])
        self.assertEqual(data["unknown_outcome_exercise"]["status"], "NOT_RUN")
        self.assertNotRegex(raw, r"\+[1-9][0-9]{7,14}")
        for forbidden in ("password", "secret", "qr_payload", "access_token", "private_key"):
            self.assertNotIn(forbidden, raw.lower())

    def test_uat_template_is_not_complete_by_default(self):
        result = self.run_python(VERIFY_UAT, UAT_TEMPLATE)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("CORE_UAT_INCOMPLETE", result.stderr)

    def test_completed_core_uat_passes_without_claiming_release_certification(self):
        data = json.loads(UAT_TEMPLATE.read_text(encoding="utf-8"))
        data["organisation"]["status"] = "ACTIVE"
        data["consent"]["status"] = "APPROVED"
        data["test_recipient"]["status"] = "ACTIVE"
        data["sender_session"].update({
            "status": "READY",
            "pairing_evidence_ref": "evidence://pairing/1",
            "ready_evidence_ref": "evidence://sender-ready/1",
            "qr_persisted": False,
        })
        data["controlled_test"].update({
            "status": "ACCEPTED",
            "evidence_ref": "evidence://controlled-test/1",
        })
        data["routing"].update({
            "reservation_status": "ACTIVE",
            "evidence_ref": "evidence://routing/1",
        })
        data["text_send"].update({
            "provider_accepted": True,
            "sent": True,
            "delivered": True,
            "read": True,
            "evidence_ref": "evidence://text/1",
        })
        data["media_send"].update({
            "provider_accepted": True,
            "sent": True,
            "delivered": True,
            "read": True,
            "evidence_ref": "evidence://media/1",
        })
        data["inbound_reply"].update({
            "received": True,
            "evidence_ref": "evidence://inbound/1",
        })
        data["reconnect"].update({
            "disconnect_observed": True,
            "ready_after_reconnect": True,
            "evidence_ref": "evidence://reconnect/1",
        })
        data["core_uat_complete"] = True

        with tempfile.TemporaryDirectory() as td:
            candidate = Path(td) / "uat.json"
            candidate.write_text(json.dumps(data), encoding="utf-8")
            result = self.run_python(VERIFY_UAT, candidate)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Core live-provider UAT evidence valid", result.stdout)
        self.assertIn("release certification remains open", result.stdout.lower())

    def test_uat_source_commit_must_match_accepted_deploy_commit(self):
        data = json.loads(UAT_TEMPLATE.read_text(encoding="utf-8"))
        data["source_commit"] = "f264237829e1caf4b0bac06c3aacd961885fc7c8"
        with tempfile.TemporaryDirectory() as td:
            candidate = Path(td) / "uat.json"
            candidate.write_text(json.dumps(data), encoding="utf-8")
            result = self.run_python(VERIFY_UAT, candidate)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SOURCE_COMMIT_MISMATCH", result.stderr)

    def test_release_claim_fails_without_unknown_and_operational_evidence(self):
        data = json.loads(UAT_TEMPLATE.read_text(encoding="utf-8"))
        data["release_certification_claimed"] = True
        with tempfile.TemporaryDirectory() as td:
            candidate = Path(td) / "uat.json"
            candidate.write_text(json.dumps(data), encoding="utf-8")
            result = self.run_python(VERIFY_UAT, candidate)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RELEASE_CERTIFICATION_UNSUPPORTED", result.stderr)

    def test_uat_verifier_rejects_full_msisdn_and_secret_like_fields(self):
        data = json.loads(UAT_TEMPLATE.read_text(encoding="utf-8"))
        data["test_recipient"]["masked_msisdn"] = "+2348012345678"
        data["sender_session"]["qr_payload"] = "should-never-be-here"
        with tempfile.TemporaryDirectory() as td:
            candidate = Path(td) / "uat.json"
            candidate.write_text(json.dumps(data), encoding="utf-8")
            result = self.run_python(VERIFY_UAT, candidate)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SENSITIVE_EVIDENCE_FORBIDDEN", result.stderr)


if __name__ == "__main__":
    unittest.main()
