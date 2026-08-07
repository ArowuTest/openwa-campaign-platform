import re
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class StackStartupContractTests(unittest.TestCase):
    def test_root_dockerignore_excludes_generated_context(self):
        dockerignore = (ROOT / ".dockerignore").read_text()
        for pattern in (".git", "**/node_modules", "**/.next", "**/dist", "**/*.tsbuildinfo"):
            self.assertIn(pattern, dockerignore)

    def test_node_security_exception_is_structurally_valid(self):
        result = subprocess.run(
            [sys.executable, ROOT / "scripts/verify-node-security.py", "--structural-only"],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("no production exceptions", result.stdout)

    def test_first_party_node_images_use_locked_installs(self):
        for app, dockerfile_name in (
            ("apps/admin-web", "admin-web.Dockerfile"),
            ("services/openwa-gateway", "openwa-gateway.Dockerfile"),
        ):
            self.assertTrue((ROOT / app / "package-lock.json").is_file(), app)
            dockerfile = (ROOT / "infrastructure/docker" / dockerfile_name).read_text()
            self.assertIn("package-lock.json", dockerfile)
            self.assertIn("npm ci", dockerfile)
            self.assertNotIn("npm install --no-audit --no-fund", dockerfile)

    def test_admin_runtime_binds_all_interfaces(self):
        dockerfile = (ROOT / "infrastructure/docker/admin-web.Dockerfile").read_text()
        self.assertIn("ENV HOSTNAME=0.0.0.0", dockerfile)

    def test_control_api_receives_required_keyrings(self):
        compose = (ROOT / "infrastructure/compose/compose.yaml").read_text()
        control = compose.split("  control-api:", 1)[1].split("  audience-worker:", 1)[0]
        for name in (
            "IDENTITY_SECRET_KEY_BASE64",
            "INBOUND_CONTENT_KEYS_JSON",
            "PRIVACY_EVIDENCE_KEYS_JSON",
            "SENDER_PROXY_KEYS_JSON",
        ):
            self.assertIn(name, control)
        self.assertIn("SENDER_PROXY_KEYS_JSON: ${SENDER_PROXY_KEYS_JSON:-}", control)
        self.assertNotIn("ZGV2ZWxvcG1lbnQtcHJveHkta2V5", control)

    def test_production_control_api_mounts_sender_proxy_keyring_as_secret(self):
        compose = (ROOT / "infrastructure/compose/compose.production.yaml").read_text()
        control = compose.split("  control-api:", 1)[1].split("  audience-worker:", 1)[0]
        self.assertIn("SENDER_PROXY_KEYS_JSON_FILE: /run/secrets/sender_proxy_keys", control)
        self.assertIn("- sender_proxy_keys", control)
        self.assertIn('sender_proxy_keys: { file: "${SENDER_PROXY_KEYS_FILE:?required}" }', compose)

    def test_edge_exposes_control_health_and_readiness(self):
        nginx = (ROOT / "infrastructure/nginx/default.conf").read_text()
        self.assertIn("location = /healthz", nginx)
        self.assertIn("location = /readyz", nginx)
        self.assertIn("http://control-api:8080/healthz", nginx)
        self.assertIn("http://control-api:8080/readyz", nginx)

    def test_export_worker_has_privacy_keyring_and_healthcheck(self):
        compose = (ROOT / "infrastructure/compose/compose.yaml").read_text()
        export = compose.split("  export-worker:", 1)[1].split("  inbound-governance-worker:", 1)[0]
        self.assertIn("PRIVACY_EVIDENCE_KEYS_JSON", export)
        self.assertIn("healthcheck:", export)
        self.assertIn("/readyz", export)


    def test_openwa_worker_is_single_governed_transport_service(self):
        development = (ROOT / "infrastructure/compose/compose.yaml").read_text()
        production = (ROOT / "infrastructure/compose/compose.production.yaml").read_text()
        gateway_dockerfile = (ROOT / "infrastructure/docker/openwa-gateway.Dockerfile").read_text()
        provider = (ROOT / "services/openwa-gateway/src/provider/openwa.provider.ts").read_text()
        security = (ROOT / "config/node-security-exceptions.json").read_text()
        verifier = (ROOT / "scripts/verify-node-security.py").read_text()

        for compose in (development, production):
            self.assertNotIn("  openwa-upstream:", compose)
            self.assertNotIn("openwa-upstream-data", compose)
            self.assertNotIn("OPENWA_UPSTREAM_URL", compose)
            self.assertNotIn("OPENWA_UPSTREAM_API_KEY", compose)

        for forbidden in ("dashboard:ci", "dashboard:build", "/app/dashboard/dist"):
            self.assertNotIn(forbidden, gateway_dockerfile)
        self.assertNotIn("OPENWA_UPSTREAM_URL", provider)
        self.assertNotIn("OPENWA_UPSTREAM_API_KEY", provider)
        self.assertNotIn("SEC-EXC-001", security)
        self.assertNotIn("openwa-dashboard", verifier)
        self.assertNotIn("SEC-EXC-001", verifier)

    def test_openwa_recovery_bootstrap_is_configurable_in_both_compose_profiles(self):
        required = (
            "OPENWA_RECONNECT_BASE_DELAY_MS",
            "OPENWA_RECONNECT_MAX_ATTEMPTS",
            "OPENWA_RECONNECT_STABILITY_RESET_MS",
            "OPENWA_WATCHDOG_INTERVAL_MS",
            "OPENWA_WATCHDOG_PROBE_TIMEOUT_MS",
            "OPENWA_WATCHDOG_FAILURE_THRESHOLD",
            "OPENWA_ENGINE_TEARDOWN_TIMEOUT_MS",
        )
        for relative in (
            "infrastructure/compose/compose.yaml",
            "infrastructure/compose/compose.production.yaml",
        ):
            compose = (ROOT / relative).read_text()
            gateway = compose.split("  openwa-gateway:", 1)[1]
            for name in required:
                self.assertIn(name, gateway, f"{name} missing from {relative}")
            self.assertIn(
                "OPENWA_RECONNECT_BASE_DELAY_MS: ${OPENWA_RECONNECT_BASE_DELAY_MS:-}",
                gateway,
                f"Baileys native reconnect base must remain the fallback in {relative}",
            )

    def test_openwa_media_fetch_allows_only_the_control_plane_internal_host(self):
        for relative in (
            "infrastructure/compose/compose.yaml",
            "infrastructure/compose/compose.production.yaml",
        ):
            compose = (ROOT / relative).read_text()
            match = re.search(
                r"(?ms)^  openwa-gateway:\s*$\n(.*?)(?=^  [a-z0-9][a-z0-9_-]*:\s*$|^networks:|\Z)",
                compose,
            )
            self.assertIsNotNone(match, relative)
            self.assertIn("SSRF_ALLOWED_HOSTS: control-api", match.group(1), relative)


if __name__ == "__main__":
    unittest.main()
