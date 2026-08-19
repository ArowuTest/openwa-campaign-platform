import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
VERIFY = ROOT / "scripts/verify-deployment-topology.py"
TOPOLOGY = ROOT / "config/deployment-topology.json"
HOSTINGER = ROOT / "infrastructure/compose/compose.hostinger-openwa-gateway.yaml"
RAILWAY = ROOT / "infrastructure/railway/service-contracts.json"


class DeploymentTopologyTests(unittest.TestCase):
    def run_verify(self, *args):
        return subprocess.run(
            ["python3", str(VERIFY), *map(str, args)], cwd=ROOT,
            text=True, capture_output=True, check=False,
        )

    def test_split_deployment_contract_passes(self):
        self.assertTrue(VERIFY.is_file(), "split deployment verifier is missing")
        result = self.run_verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Split deployment topology valid", result.stdout)

    def test_contract_declares_three_sibling_transports(self):
        data = json.loads(TOPOLOGY.read_text(encoding="utf-8"))
        self.assertEqual(data["transports"], ["OPENWA/BAILEYS", "OPENWA/WHATSAPP_WEB_JS", "META/CLOUD_API"])
        self.assertFalse(data["meta"]["via_openwa_gateway"])

    def test_railway_contract_has_exact_backend_service_set(self):
        data = json.loads(RAILWAY.read_text(encoding="utf-8"))
        self.assertEqual(set(data["services"]), {
            "control-api", "audience-worker", "campaign-worker", "export-worker",
            "inbound-governance-worker", "metrics-worker", "platform-governance-worker",
        })
        self.assertNotIn("openwa-gateway", data["services"])

    def test_hostinger_manifest_is_gateway_only_and_meta_free(self):
        text = HOSTINGER.read_text(encoding="utf-8")
        self.assertIn("openwa-gateway:", text)
        for forbidden in ("DATABASE_URL", "S3_ACCESS", "S3_SECRET", "META_CLOUD_CREDENTIALS"):
            self.assertNotIn(forbidden, text)
        self.assertNotIn("http://control-api", text)
        self.assertIn("${CONTROL_API_INTERNAL_URL:?", text)

    def test_verifier_rejects_meta_routed_via_openwa(self):
        data = json.loads(TOPOLOGY.read_text(encoding="utf-8"))
        data["meta"]["via_openwa_gateway"] = True
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "topology.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = self.run_verify("--topology", bad)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Meta must remain a direct sibling transport", result.stderr)

    def test_verifier_rejects_hostinger_database_credentials(self):
        text = HOSTINGER.read_text(encoding="utf-8") + "\n# DATABASE_URL_FILE must never appear here\n"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "hostinger.yaml"
            bad.write_text(text, encoding="utf-8")
            result = self.run_verify("--hostinger-compose", bad)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Hostinger gateway manifest must not contain DATABASE_URL", result.stderr)

    def test_verifier_rejects_openwa_gateway_in_railway_contract(self):
        data = json.loads(RAILWAY.read_text(encoding="utf-8"))
        data["services"]["openwa-gateway"] = dict(data["services"]["metrics-worker"])
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "railway.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = self.run_verify("--railway-contract", bad)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("seven backend/control services", result.stderr)

    def test_repository_check_runs_split_topology_verifier(self):
        check = (ROOT / "scripts/check.sh").read_text(encoding="utf-8")
        self.assertIn("python3 scripts/verify-deployment-topology.py", check)

    def test_make_check_includes_split_topology_verifier(self):
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("deployment-topology", makefile)
        check_line = next(line for line in makefile.splitlines() if line.startswith("check:"))
        self.assertIn("deployment-topology", check_line)

    def test_verifier_rejects_railway_port_dockerfile_mismatch(self):
        data = json.loads(RAILWAY.read_text(encoding="utf-8"))
        data["services"]["metrics-worker"]["port"] = 9999
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "railway.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = self.run_verify("--railway-contract", bad)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Dockerfile does not EXPOSE declared port", result.stderr)

    def resolved_env(self):
        env = os.environ.copy()
        env.update({
            "OPENWA_GATEWAY_IMAGE": "example.invalid/openwa@sha256:" + "a" * 64,
            "OPENWA_ENGINE": "BAILEYS",
            "GATEWAY_BIND_ADDRESS": "10.20.30.40",
            "GATEWAY_INTERNAL_URL": "http://10.20.30.40:2785",
            "CONTROL_API_INTERNAL_URL": "https://control.internal.example",
            "CONTROL_API_CALLBACK_URL": "https://control.internal.example/api/v1/internal/gateway/events",
            "CONTROL_API_INBOUND_URL": "https://control.internal.example/api/v1/internal/gateway/inbound",
            "MEDIA_DOWNLOAD_BASE_URL": "https://control.internal.example/api/v1/internal/media",
            "SSRF_ALLOWED_HOSTS": "control.internal.example",
        })
        return env

    def test_resolved_preflight_accepts_private_https_gateway_boundary(self):
        result = subprocess.run(
            ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
            text=True, capture_output=True, check=False, env=self.resolved_env(),
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Resolved Hostinger gateway boundary valid", result.stdout)

    def test_resolved_preflight_rejects_http_control_boundary(self):
        env = self.resolved_env()
        env["CONTROL_API_INTERNAL_URL"] = "http://control.internal.example"
        result = subprocess.run(
            ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
            text=True, capture_output=True, check=False, env=env,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must use HTTPS", result.stderr)

    def test_resolved_preflight_rejects_public_gateway_bind(self):
        env = self.resolved_env()
        env["GATEWAY_BIND_ADDRESS"] = "8.8.8.8"
        result = subprocess.run(
            ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
            text=True, capture_output=True, check=False, env=env,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must not be a globally routable address", result.stderr)

    def test_railway_contract_declares_private_clamav_dependency(self):
        data = json.loads(RAILWAY.read_text(encoding="utf-8"))
        self.assertIn("clamav", data["shared_dependencies"])
        self.assertEqual(data["shared_dependencies"]["clamav"], "private-malware-scanner")

    def test_hostinger_manifest_requires_advertised_gateway_url(self):
        text = HOSTINGER.read_text(encoding="utf-8")
        self.assertIn("GATEWAY_INTERNAL_URL: ${GATEWAY_INTERNAL_URL:?", text)

    def test_resolved_preflight_rejects_docker_internal_gateway_advertisement(self):
        env = self.resolved_env()
        env["GATEWAY_INTERNAL_URL"] = "http://openwa-gateway:2785"
        result = subprocess.run(
            ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
            text=True, capture_output=True, check=False, env=env,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("advertised GATEWAY_INTERNAL_URL", result.stderr)

    def test_public_railway_control_api_requires_network_and_malware_environment(self):
        data = json.loads(RAILWAY.read_text(encoding="utf-8"))
        required = set(data["services"]["control-api"].get("required_environment", []))
        self.assertTrue({"ALLOWED_NETWORK_CIDRS", "TRUSTED_PROXY_CIDRS", "CLAMAV_ADDRESS"} <= required)

    def test_verifier_rejects_public_control_api_without_network_allowlist_contract(self):
        data = json.loads(RAILWAY.read_text(encoding="utf-8"))
        data["services"]["control-api"].pop("required_environment", None)
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "railway.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = self.run_verify("--railway-contract", bad)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("public control-api must require network allowlist", result.stderr)

    def test_production_compose_contains_only_railway_control_plane_services(self):
        text = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        names = set()
        in_services = False
        for line in text.splitlines():
            if line == "services:":
                in_services = True
                continue
            if in_services and line and not line.startswith(" "):
                break
            if in_services and line.startswith("  ") and not line.startswith("    ") and line.endswith(":"):
                names.add(line.strip()[:-1])
        railway = json.loads(RAILWAY.read_text(encoding="utf-8"))
        self.assertEqual(names, set(railway["services"]))

    def test_production_control_plane_has_no_static_openwa_gateway_url(self):
        text = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        self.assertNotIn("OPENWA_GATEWAY_URL:", text)
        self.assertNotIn("\n  openwa-gateway:\n", text)

    def test_production_control_api_requires_network_boundary_environment(self):
        text = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        self.assertIn("ALLOWED_NETWORK_CIDRS: ${ALLOWED_NETWORK_CIDRS:?", text)
        self.assertIn("TRUSTED_PROXY_CIDRS: ${TRUSTED_PROXY_CIDRS:?", text)

    def test_production_verifier_rejects_openwa_gateway_in_control_plane(self):
        compose = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        injected = compose.replace("\nnetworks:\n", "\n  openwa-gateway:\n    <<: *service-defaults\n    image: ${OPENWA_GATEWAY_IMAGE:?digest required}\n    environment:\n      NODE_ENV: production\n\nnetworks:\n")
        verify = ROOT / "scripts/verify-production-compose.py"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "compose.yaml"
            bad.write_text(injected, encoding="utf-8")
            result = subprocess.run(["python3", str(verify), "--compose", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Railway control-plane services", result.stderr)

    def test_railway_media_download_url_is_required_for_cross_provider_https(self):
        text = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        self.assertEqual(text.count("MEDIA_DOWNLOAD_BASE_URL: ${MEDIA_DOWNLOAD_BASE_URL:?"), 2)
        self.assertNotIn("MEDIA_DOWNLOAD_BASE_URL: http://control-api", text)
        railway = json.loads(RAILWAY.read_text(encoding="utf-8"))
        self.assertIn("MEDIA_DOWNLOAD_BASE_URL", railway["services"]["campaign-worker"].get("required_environment", []))

    def test_production_verifier_rejects_docker_internal_media_download_url(self):
        compose = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        good = "MEDIA_DOWNLOAD_BASE_URL: ${MEDIA_DOWNLOAD_BASE_URL:?approved Railway HTTPS media URL is required}"
        injected = compose.replace(good, "MEDIA_DOWNLOAD_BASE_URL: http://control-api:8080/api/v1/internal/media")
        verify = ROOT / "scripts/verify-production-compose.py"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "compose.yaml"
            bad.write_text(injected, encoding="utf-8")
            result = subprocess.run(["python3", str(verify), "--compose", str(bad)], cwd=ROOT, text=True, capture_output=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("MEDIA_DOWNLOAD_BASE_URL", result.stderr)

    def test_resolved_preflight_rejects_docker_local_https_control_host(self):
        env = self.resolved_env()
        env.update({
            "CONTROL_API_INTERNAL_URL": "https://control-api",
            "CONTROL_API_CALLBACK_URL": "https://control-api/api/v1/internal/gateway/events",
            "CONTROL_API_INBOUND_URL": "https://control-api/api/v1/internal/gateway/inbound",
            "MEDIA_DOWNLOAD_BASE_URL": "https://control-api/api/v1/internal/media",
            "SSRF_ALLOWED_HOSTS": "control-api",
        })
        result = subprocess.run(
            ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
            text=True, capture_output=True, check=False, env=env,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Docker-local", result.stderr)

    def test_resolved_preflight_rejects_special_docker_and_loopback_control_hosts(self):
        for host in ("host.docker.internal", "bridge.docker.internal", "127.0.0.1", "169.254.10.20"):
            env = self.resolved_env()
            env.update({
                "CONTROL_API_INTERNAL_URL": f"https://{host}",
                "CONTROL_API_CALLBACK_URL": f"https://{host}/api/v1/internal/gateway/events",
                "CONTROL_API_INBOUND_URL": f"https://{host}/api/v1/internal/gateway/inbound",
                "MEDIA_DOWNLOAD_BASE_URL": f"https://{host}/api/v1/internal/media",
                "SSRF_ALLOWED_HOSTS": host,
            })
            result = subprocess.run(
                ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
                text=True, capture_output=True, check=False, env=env,
            )
            self.assertNotEqual(result.returncode, 0, host)

    def test_resolved_preflight_rejects_special_docker_and_loopback_https_gateway_url(self):
        for host in ("host.docker.internal", "bridge.docker.internal", "127.0.0.1", "169.254.10.20"):
            env = self.resolved_env()
            env["GATEWAY_INTERNAL_URL"] = f"https://{host}:2785"
            result = subprocess.run(
                ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
                text=True, capture_output=True, check=False, env=env,
            )
            self.assertNotEqual(result.returncode, 0, host)

    def test_hostinger_manifest_passes_bind_address_to_gateway_runtime(self):
        text = HOSTINGER.read_text(encoding="utf-8")
        self.assertGreaterEqual(text.count("GATEWAY_BIND_ADDRESS"), 2)