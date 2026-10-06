import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
VERIFY = ROOT / "scripts/verify-admin-web-hosting.py"
CONTRACT = ROOT / "infrastructure/railway/admin-web-service-contract.json"
OVERLAY = ROOT / "infrastructure/compose/compose.production.admin-web.yaml"
TOPOLOGY = ROOT / "config/deployment-topology.json"
DOCKERFILE = ROOT / "infrastructure/docker/admin-web.Dockerfile"


class AdminWebHostingTests(unittest.TestCase):
    def run_verify(self, *args, env=None):
        return subprocess.run(
            [sys.executable, str(VERIFY), *map(str, args)],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
            env=env,
        )

    def test_hosting_contract_passes(self):
        result = self.run_verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Admin-web hosting contract valid", result.stdout)

    def test_frontend_contract_is_separate_from_seven_backend_services(self):
        backend = json.loads((ROOT / "infrastructure/railway/service-contracts.json").read_text(encoding="utf-8"))
        frontend = json.loads(CONTRACT.read_text(encoding="utf-8"))
        self.assertEqual(len(backend["services"]), 7)
        self.assertNotIn("admin-web", backend["services"])
        self.assertEqual(frontend["service"]["name"], "admin-web")
        self.assertTrue(frontend["service"]["public_ingress"])
        self.assertEqual(frontend["service"]["secret_classes"], [])
        self.assertEqual(frontend["service"]["required_environment"], ["ADMIN_WEB_CONTROL_API_URL"])
        self.assertEqual(
            frontend["service"]["recommended_environment_values"]["ADMIN_WEB_CONTROL_API_URL"],
            "http://${{control-api.RAILWAY_PRIVATE_DOMAIN}}:${{control-api.PORT}}",
        )

    def test_topology_declares_frontend_without_moving_openwa_gateway(self):
        data = json.loads(TOPOLOGY.read_text(encoding="utf-8"))
        self.assertEqual(data["railway_frontend"]["service"], "admin-web")
        self.assertEqual(data["railway_frontend"]["control_api_upstream_env"], "ADMIN_WEB_CONTROL_API_URL")
        self.assertEqual(
            data["railway_frontend"]["control_api_upstream_reference"],
            "http://${{control-api.RAILWAY_PRIVATE_DOMAIN}}:${{control-api.PORT}}",
        )
        self.assertTrue(data["railway_frontend"]["public_ingress"])
        self.assertFalse(data["railway"]["openwa_gateway_allowed"])
        self.assertEqual(data["hostinger"]["services"], ["openwa-gateway"])

    def test_admin_web_image_uses_supported_node_and_non_root_runtime(self):
        text = DOCKERFILE.read_text(encoding="utf-8")
        self.assertIn("FROM node:22.19-alpine", text)
        self.assertIn("ENV NODE_ENV=production", text)
        self.assertIn("USER campaign", text)
        self.assertIn("EXPOSE 3000", text)

    def test_production_overlay_has_no_secrets_and_requires_private_upstream(self):
        text = OVERLAY.read_text(encoding="utf-8")
        self.assertIn("ADMIN_WEB_IMAGE", text)
        self.assertIn("ADMIN_WEB_CONTROL_API_URL", text)
        self.assertIn("/healthz", text)
        self.assertIn("read_only: true", text)
        self.assertIn("no-new-privileges:true", text)
        self.assertNotIn("CONTROL_API_INTERNAL_URL", text)
        self.assertNotIn("_SECRET", text)
        self.assertNotIn("_PASSWORD", text)
        self.assertNotIn("_TOKEN", text)

    def test_browser_api_is_forced_same_origin(self):
        api = (ROOT / "apps/admin-web/lib/api.ts").read_text(encoding="utf-8")
        contract = json.loads(CONTRACT.read_text(encoding="utf-8"))
        self.assertIn("browserAPIBase = '/api'", api)
        self.assertNotIn("NEXT_PUBLIC_CONTROL_API_BASE", api)
        self.assertIn("NEXT_PUBLIC_CONTROL_API_BASE", contract["forbidden_environment"])

    def test_local_compose_uses_frontend_specific_control_upstream(self):
        text = (ROOT / "infrastructure/compose/compose.yaml").read_text(encoding="utf-8")
        admin = text.split("  admin-web:", 1)[1].split("\n  openwa-gateway:", 1)[0]
        self.assertIn("ADMIN_WEB_CONTROL_API_URL: http://control-api:8080", admin)
        self.assertNotIn("CONTROL_API_INTERNAL_URL", admin)

    def test_repository_check_includes_admin_web_hosting_verifier(self):
        check = (ROOT / "scripts/check.sh").read_text(encoding="utf-8")
        self.assertIn("python3 scripts/verify-admin-web-hosting.py", check)
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("admin-web-hosting", makefile)
        check_line = next(line for line in makefile.splitlines() if line.startswith("check:"))
        self.assertIn("admin-web-hosting", check_line)

    def test_resolved_preflight_accepts_digest_and_railway_private_control_url(self):
        env = os.environ.copy()
        env.update({
            "ADMIN_WEB_IMAGE": "registry.example/admin-web@sha256:" + "a" * 64,
            "ADMIN_WEB_CONTROL_API_URL": "http://control-api.railway.internal:8080",
        })
        result = self.run_verify("--resolved", env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Resolved admin-web hosting boundary valid", result.stdout)

    def test_resolved_preflight_rejects_public_or_credentialed_control_url(self):
        cases = [
            "https://control.example.com",
            "http://user:pass@control-api.railway.internal:8080",
            "http://control-api.railway.internal:8080/api",
        ]
        for raw in cases:
            env = os.environ.copy()
            env.update({
                "ADMIN_WEB_IMAGE": "registry.example/admin-web@sha256:" + "b" * 64,
                "ADMIN_WEB_CONTROL_API_URL": raw,
            })
            result = self.run_verify("--resolved", env=env)
            self.assertNotEqual(result.returncode, 0, raw)
            self.assertIn("ADMIN_WEB_CONTROL_API_URL", result.stderr, raw)

    def test_deployment_ready_preflight_requires_matching_backend_and_fresh_external_evidence(self):
        env = os.environ.copy()
        env.update({
            "ADMIN_WEB_IMAGE": "registry.example/admin-web@sha256:" + "c" * 64,
            "ADMIN_WEB_CONTROL_API_URL": "http://control-api.railway.internal:8080",
            "ACCEPTED_SOURCE_COMMIT": "a" * 40,
            "CONTROL_API_DEPLOYED_COMMIT": "b" * 40,
            "TRUSTED_PROXY_CURRENT_RECONFIRMED": "true",
            "ACTIONABLE_STAGED_CHANGES_ABSENT": "true",
            "DEPLOY_AUTHORIZED": "true",
        })
        result = self.run_verify("--deployment-ready", env=env)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("CONTROL_API_DEPLOYED_COMMIT", result.stderr)

        env["CONTROL_API_DEPLOYED_COMMIT"] = env["ACCEPTED_SOURCE_COMMIT"]
        env["TRUSTED_PROXY_CURRENT_RECONFIRMED"] = "false"
        result = self.run_verify("--deployment-ready", env=env)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("TRUSTED_PROXY_CURRENT_RECONFIRMED", result.stderr)

    def test_deployment_ready_preflight_passes_only_with_all_external_preconditions(self):
        env = os.environ.copy()
        commit = "d" * 40
        env.update({
            "ADMIN_WEB_IMAGE": "registry.example/admin-web@sha256:" + "e" * 64,
            "ADMIN_WEB_CONTROL_API_URL": "http://control-api.railway.internal:8080",
            "ACCEPTED_SOURCE_COMMIT": commit,
            "CONTROL_API_DEPLOYED_COMMIT": commit,
            "TRUSTED_PROXY_CURRENT_RECONFIRMED": "true",
            "ACTIONABLE_STAGED_CHANGES_ABSENT": "true",
            "DEPLOY_AUTHORIZED": "true",
        })
        result = self.run_verify("--deployment-ready", env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Admin-web deployment preconditions valid", result.stdout)

    def test_runbook_requires_deployment_ready_preflight_and_backend_alignment(self):
        text = (ROOT / "docs/runbooks/deployment-and-rollback.md").read_text(encoding="utf-8")
        self.assertIn("verify-admin-web-hosting.py --deployment-ready", text)
        self.assertIn("Do not create or deploy admin-web until control-api reports the same accepted commit", text)
        self.assertIn("validated `X-Real-IP`", text)

    def test_hosting_plan_is_fail_closed_against_deployment_overclaims(self):
        plan_path = ROOT / "infrastructure/railway/production-admin-web-plan-2026-10-05.json"
        data = json.loads(plan_path.read_text(encoding="utf-8"))
        self.assertFalse(data["railway_observation"]["admin_web_present"])
        self.assertIsNone(data["railway_observation"]["staged_changes"])
        self.assertEqual(data["railway_observation"]["control_api"]["latest_deployment"]["status"], "SUCCESS")
        self.assertEqual(
            data["service"]["recommended_environment_values"]["ADMIN_WEB_CONTROL_API_URL"],
            "http://${{control-api.RAILWAY_PRIVATE_DOMAIN}}:${{control-api.PORT}}",
        )
        self.assertFalse(data["local_candidate"]["committed"])
        self.assertFalse(data["local_candidate"]["pushed"])
        self.assertIsNone(data["service"]["service_id"])
        self.assertIsNone(data["service"]["image_digest"])
        self.assertEqual(
            data["deployment_preconditions"],
            {
                "accepted_source_committed": False,
                "accepted_source_pushed": False,
                "control_api_source_matches_accepted_commit": False,
                "trusted_proxy_current_reconfirmed": False,
                "actionable_staged_changes_absent": True,
                "explicit_deploy_authorization": False,
            },
        )
        self.assertTrue(data["railway_observation"]["control_api"]["trusted_proxy_covers_railway_private_ula"])
        self.assertFalse(data["railway_observation"]["control_api"]["trusted_proxy_value_disclosed"])
        self.assertEqual(data["local_candidate"]["branch"], "work/champion-hosting-integration-20261005")
        self.assertTrue(data["local_candidate"]["full_operator_console"])
        self.assertEqual(data["local_candidate"]["static_page_routes"], 14)
        self.assertEqual(data["local_candidate"]["runtime_routes"], ["/api/[...path]", "/healthz"])
        self.assertFalse(data["merge_provenance"]["parent_worktrees_modified"])
        self.assertEqual(
            data["merge_provenance"]["champion_parent"]["candidate_fingerprint_sha256"],
            "4d17a10cd130201a5a7473202c689d8ac3fc845dd5f3546ceb31f0a1629d7c8b",
        )
        self.assertEqual(
            data["merge_provenance"]["hosting_parent"]["candidate_fingerprint_sha256"],
            "2be3e40b1b529101afb5c51735398df4948d14416c2f7b90af6e84eebbb4bb24",
        )

        for mutate, expected in (
            (lambda candidate: candidate["service"].__setitem__("service_id", "00000000-0000-0000-0000-000000000001"), "service_id"),
            (lambda candidate: candidate["local_candidate"].__setitem__("committed", True), "committed"),
            (lambda candidate: candidate["railway_observation"].__setitem__("admin_web_present", True), "admin-web exists"),
            (lambda candidate: candidate["railway_observation"]["control_api"].__setitem__("trusted_proxy_covers_railway_private_ula", False), "trusts railway private ula"),
            (lambda candidate: candidate["merge_provenance"]["champion_parent"].__setitem__("candidate_fingerprint_sha256", "0" * 64), "fingerprint drift"),
        ):
            candidate = json.loads(json.dumps(data))
            mutate(candidate)
            with tempfile.TemporaryDirectory() as td:
                bad = Path(td) / "plan.json"
                bad.write_text(json.dumps(candidate), encoding="utf-8")
                result = self.run_verify("--plan", bad)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(expected.lower(), result.stderr.lower())

    def test_verifier_rejects_frontend_secret_classes(self):
        data = json.loads(CONTRACT.read_text(encoding="utf-8"))
        data["service"]["secret_classes"] = ["identity"]
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "admin-web.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = self.run_verify("--contract", bad)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must not receive secret classes", result.stderr)


if __name__ == "__main__":
    unittest.main()
