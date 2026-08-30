import importlib.util
import json
import os
import subprocess
import sys
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
        self.assertEqual(data["hostinger"]["service_contracts"], {
            "openwa-gateway": {"port": 2785, "health_path": "/healthz"}
        })

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
        self.assertIn("${GATEWAY_CAPACITY:?", text)

    def test_verifier_rejects_meta_routed_via_openwa(self):
        data = json.loads(TOPOLOGY.read_text(encoding="utf-8"))
        data["meta"]["via_openwa_gateway"] = True
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "topology.json"
            bad.write_text(json.dumps(data), encoding="utf-8")
            result = self.run_verify("--topology", bad)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Meta must remain a direct sibling transport", result.stderr)

    def test_hostinger_manifest_pins_production_node_environment(self):
        text = HOSTINGER.read_text(encoding="utf-8")
        self.assertIn("NODE_ENV: production", text)

    def test_verifier_rejects_hostinger_without_production_node_environment(self):
        text = HOSTINGER.read_text(encoding="utf-8").replace("NODE_ENV: production", "NODE_ENV: development", 1)
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "hostinger-node-env.yaml"
            bad.write_text(text, encoding="utf-8")
            result = self.run_verify("--hostinger-compose", bad)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("NODE_ENV: production", result.stderr)

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
            "GATEWAY_CAPACITY": "10",
            "OPENWA_MAX_CONCURRENT_SESSIONS": "10",
            "GATEWAY_INTERNAL_URL": "https://gateway.private.example",
            "GATEWAY_RUNTIME_ALLOWED_HOSTS": "gateway.private.example",
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

    def test_resolved_preflight_rejects_missing_or_mismatched_gateway_capacity(self):
        scenarios = []
        missing = self.resolved_env()
        missing.pop("GATEWAY_CAPACITY")
        scenarios.append(missing)
        mismatched = self.resolved_env()
        mismatched["GATEWAY_CAPACITY"] = "9"
        scenarios.append(mismatched)
        for env in scenarios:
            result = subprocess.run(["python3", str(VERIFY), "--resolved"], cwd=ROOT, text=True, capture_output=True, check=False, env=env)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("capacity", result.stderr.lower())

    def test_resolved_preflight_rejects_gateway_url_credentials_and_non_authority_components(self):
        for raw in [
            "https://deploy:secret@gateway.private.example",
            "https://gateway.private.example/runtime",
            "https://gateway.private.example?token=secret",
            "https://gateway.private.example#fragment",
            "https://gateway.private.example:65536",
        ]:
            env = self.resolved_env()
            env["GATEWAY_INTERNAL_URL"] = raw
            result = subprocess.run(["python3", str(VERIFY), "--resolved"], cwd=ROOT, text=True, capture_output=True, check=False, env=env)
            self.assertNotEqual(result.returncode, 0, raw)
            self.assertIn("GATEWAY_INTERNAL_URL", result.stderr, raw)

    def test_resolved_preflight_rejects_gateway_control_hostname_alias(self):
        env = self.resolved_env()
        env["GATEWAY_INTERNAL_URL"] = "https://control.internal.example:2785"
        env["GATEWAY_RUNTIME_ALLOWED_HOSTS"] = "gateway.private.example,control.internal.example"
        result = subprocess.run(
            ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
            text=True, capture_output=True, check=False, env=env,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("control hostname", result.stderr.lower())

    def test_resolved_preflight_rejects_http_control_boundary(self):
        env = self.resolved_env()
        env["CONTROL_API_INTERNAL_URL"] = "http://control.internal.example"
        result = subprocess.run(
            ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
            text=True, capture_output=True, check=False, env=env,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must use HTTPS", result.stderr)

    def test_resolved_preflight_rejects_non_authority_control_origin(self):
        for raw in [
            "https://deploy:secret@control.internal.example",
            "https://control.internal.example/api",
            "https://control.internal.example?token=secret",
            "https://control.internal.example#fragment",
            "https://control.internal.example:0",
            "https://control.internal.example:65536",
        ]:
            env = self.resolved_env()
            env["CONTROL_API_INTERNAL_URL"] = raw
            result = subprocess.run(
                ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
                text=True, capture_output=True, check=False, env=env,
            )
            self.assertNotEqual(result.returncode, 0, raw)
            self.assertIn("CONTROL_API_INTERNAL_URL", result.stderr, raw)

    def test_resolved_preflight_rejects_credentials_fragments_and_invalid_ports_in_control_endpoints(self):
        for key in ("CONTROL_API_CALLBACK_URL", "CONTROL_API_INBOUND_URL", "MEDIA_DOWNLOAD_BASE_URL"):
            valid = self.resolved_env()[key]
            for raw in [
                valid.replace("https://", "https://deploy:secret@", 1),
                valid + "#fragment",
                valid.replace("control.internal.example", "control.internal.example:0", 1),
            ]:
                env = self.resolved_env()
                env[key] = raw
                result = subprocess.run(
                    ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
                    text=True, capture_output=True, check=False, env=env,
                )
                self.assertNotEqual(result.returncode, 0, f"{key}={raw}")
                self.assertIn(key, result.stderr, f"{key}={raw}")

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

    def test_resolved_preflight_rejects_private_literal_control_hosts(self):
        for host in ("10.20.30.50", "172.16.0.50", "192.168.1.50", "fd00::1", "::ffff:10.20.30.50", "010.020.030.050", "0x0a.0x14.0x1e.0x28", "167772161", "10.1"):
            env = self.resolved_env()
            bracketed = f"[{host}]" if ":" in host else host
            env.update({
                "CONTROL_API_INTERNAL_URL": f"https://{bracketed}",
                "CONTROL_API_CALLBACK_URL": f"https://{bracketed}/api/v1/internal/gateway/events",
                "CONTROL_API_INBOUND_URL": f"https://{bracketed}/api/v1/internal/gateway/inbound",
                "MEDIA_DOWNLOAD_BASE_URL": f"https://{bracketed}/api/v1/internal/media",
                "SSRF_ALLOWED_HOSTS": host,
            })
            result = subprocess.run(
                ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
                text=True, capture_output=True, check=False, env=env,
            )
            self.assertNotEqual(result.returncode, 0, host)
            self.assertIn("CONTROL_API_INTERNAL_URL", result.stderr, host)

    def test_resolved_preflight_rejects_ip_like_gateway_allowlist_entries(self):
        for host in ("10.20.30.50", "fd00::1", "010.020.030.050", "0x0a.0x14.0x1e.0x28", "167772161", "10.1"):
            env = self.resolved_env()
            env["GATEWAY_RUNTIME_ALLOWED_HOSTS"] = f"gateway.private.example,{host}"
            result = subprocess.run(
                ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
                text=True, capture_output=True, check=False, env=env,
            )
            self.assertNotEqual(result.returncode, 0, host)
            self.assertIn("GATEWAY_RUNTIME_ALLOWED_HOSTS", result.stderr, host)

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

    def test_production_verifier_rejects_healthcheck_drift_from_railway_contract(self):
        compose = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        ready = "http://127.0.0.1:8080/readyz"
        drifted = compose.replace(ready, "http://127.0.0.1:8080/healthz", 1)
        verify = ROOT / "scripts/verify-production-compose.py"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "compose.yaml"
            bad.write_text(drifted, encoding="utf-8")
            result = subprocess.run(
                ["python3", str(verify), "--compose", str(bad)],
                cwd=ROOT, text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("healthcheck", result.stderr.lower())
        self.assertIn("service-contract", result.stderr.lower())

    def test_production_verifier_ignores_decoy_loopback_url_before_healthcheck(self):
        compose = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        health = '      test: ["CMD", "/control-api", "healthcheck", "http://127.0.0.1:8080/readyz"]'
        drifted = compose.replace(health, '      test: ["CMD", "/control-api", "healthcheck", "http://127.0.0.1:8080/healthz"]', 1)
        prefix, control_and_rest = drifted.split("  control-api:\n", 1)
        control_and_rest = control_and_rest.replace(
            "    environment:\n",
            "    environment:\n      HEALTHCHECK_DECOY_URL: http://127.0.0.1:8080/readyz\n",
            1,
        )
        drifted = prefix + "  control-api:\n" + control_and_rest
        self.assertNotEqual(drifted, compose)
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "compose-decoy-health.yaml"
            bad.write_text(drifted, encoding="utf-8")
            result = subprocess.run(
                ["python3", str(ROOT / "scripts/verify-production-compose.py"), "--compose", str(bad)],
                cwd=ROOT, text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("healthcheck", result.stderr.lower())

    def test_production_verifier_rejects_decoy_url_inside_healthcheck_directive(self):
        compose = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        health = '      test: ["CMD", "/control-api", "healthcheck", "http://127.0.0.1:8080/readyz"]'
        drifted = compose.replace(
            health,
            '      test: ["CMD-SHELL", "echo http://127.0.0.1:8080/readyz >/dev/null && /control-api healthcheck http://127.0.0.1:8080/healthz"]',
            1,
        )
        self.assertNotEqual(drifted, compose)
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "compose-intra-healthcheck-decoy.yaml"
            bad.write_text(drifted, encoding="utf-8")
            result = subprocess.run(
                ["python3", str(ROOT / "scripts/verify-production-compose.py"), "--compose", str(bad)],
                cwd=ROOT, text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("healthcheck", result.stderr.lower())


    def test_verifier_rejects_hostinger_public_port_binding_even_when_runtime_bind_env_is_private(self):
        text = HOSTINGER.read_text(encoding="utf-8")
        approved = '      - "${GATEWAY_BIND_ADDRESS:?approved private bind address required}:2785:2785"'
        drifted = text.replace(approved, '      - "0.0.0.0:2785:2785"', 1)
        self.assertNotEqual(drifted, text)
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "hostinger-public-port.yaml"
            bad.write_text(drifted, encoding="utf-8")
            result = self.run_verify("--hostinger-compose", bad)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("private", result.stderr.lower())
        self.assertIn("port", result.stderr.lower())


    def test_production_verifier_rejects_published_port_for_private_railway_service(self):
        compose = (ROOT / "infrastructure/compose/compose.production.yaml").read_text(encoding="utf-8")
        anchor = "    image: ${AUDIENCE_WORKER_IMAGE:?AUDIENCE_WORKER_IMAGE must be digest-pinned}"
        drifted = compose.replace(anchor, anchor + '\n    ports:\n      - "8091:8091"', 1)
        self.assertNotEqual(drifted, compose)
        verify = ROOT / "scripts/verify-production-compose.py"
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "compose-public-worker.yaml"
            bad.write_text(drifted, encoding="utf-8")
            result = subprocess.run(
                ["python3", str(verify), "--compose", str(bad)],
                cwd=ROOT, text=True, capture_output=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("public ingress", result.stderr.lower())

    def test_resolved_preflight_rejects_runtime_registration_url_override(self):
        env = self.resolved_env()
        env["GATEWAY_RUNTIME_REGISTRATION_URL"] = "http://127.0.0.1:9999/runtime"
        result = subprocess.run(
            ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
            text=True, capture_output=True, check=False, env=env,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("GATEWAY_RUNTIME_REGISTRATION_URL", result.stderr)

    def test_resolved_preflight_rejects_loopback_and_link_local_gateway_bind(self):
        for bind in ("127.0.0.1", "169.254.10.20"):
            env = self.resolved_env()
            env["GATEWAY_BIND_ADDRESS"] = bind
            env["GATEWAY_INTERNAL_URL"] = "https://gateway.private.example:2785"
            result = subprocess.run(
                ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
                text=True, capture_output=True, check=False, env=env,
            )
            self.assertNotEqual(result.returncode, 0, bind)
            self.assertIn("GATEWAY_BIND_ADDRESS", result.stderr)

    def test_hostinger_manifest_measures_openwa_session_data_filesystem(self):
        text = HOSTINGER.read_text(encoding="utf-8")
        required = "GATEWAY_RESOURCE_FILESYSTEM_PATH: /app/session-data"
        self.assertIn(required, text)
        drifted = text.replace(required, "GATEWAY_RESOURCE_FILESYSTEM_PATH: /data", 1)
        with tempfile.TemporaryDirectory() as td:
            bad = Path(td) / "hostinger-resource-path.yaml"
            bad.write_text(drifted, encoding="utf-8")
            result = self.run_verify("--hostinger-compose", bad)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("resource filesystem", result.stderr.lower())

    def test_strict_release_security_runs_resolved_hostinger_boundary_preflight(self):
        script = (ROOT / "scripts/release-security-check.sh").read_text(encoding="utf-8")
        self.assertIn(
            "python3 scripts/verify-deployment-topology.py --resolved",
            script,
            "strict release security can skip resolved Hostinger private-bind validation",
        )


    def test_resolved_preflight_rejects_ipv4_mapped_special_control_hosts(self):
        for host in ("::ffff:127.0.0.1", "::ffff:169.254.169.254", "::ffff:0.0.0.0"):
            env = self.resolved_env(); bracketed = f"[{host}]"
            env.update({"CONTROL_API_INTERNAL_URL": f"https://{bracketed}", "CONTROL_API_CALLBACK_URL": f"https://{bracketed}/api/v1/internal/gateway/events", "CONTROL_API_INBOUND_URL": f"https://{bracketed}/api/v1/internal/gateway/inbound", "MEDIA_DOWNLOAD_BASE_URL": f"https://{bracketed}/api/v1/internal/media", "SSRF_ALLOWED_HOSTS": host})
            result = subprocess.run(["python3", str(VERIFY), "--resolved"], cwd=ROOT, text=True, capture_output=True, check=False, env=env)
            self.assertNotEqual(result.returncode, 0, host)


    def test_resolved_preflight_rejects_unapproved_gateway_dns_and_http(self):
        for raw in (
            "https://gateway.attacker.example:2785",
            "http://10.20.30.40:2785",
        ):
            env = self.resolved_env(); env["GATEWAY_INTERNAL_URL"] = raw
            result = subprocess.run(
                ["python3", str(VERIFY), "--resolved"], cwd=ROOT,
                text=True, capture_output=True, check=False, env=env,
            )
            self.assertNotEqual(result.returncode, 0, raw)
            self.assertIn("GATEWAY_INTERNAL_URL", result.stderr)


    def test_strict_sbom_lockfiles_follow_dependency_bearing_manifests(self):
        spec = importlib.util.spec_from_file_location("generate_sbom", ROOT / "scripts/generate-sbom.py")
        module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); aggregator = root / "package.json"; child = root / "child/package.json"
            child.parent.mkdir(parents=True)
            aggregator.write_text(json.dumps({"name":"root","workspaces":["child"]}), encoding="utf-8")
            child.write_text(json.dumps({"name":"child","dependencies":{"x":"1.0.0"}}), encoding="utf-8")
            required = module.required_npm_lockfiles([aggregator, child])
            self.assertEqual(required, [child.with_name("package-lock.json")])

    def test_release_sbom_includes_hostinger_gateway_image_manifest(self):
        source = (ROOT / "scripts/generate-sbom.py").read_text(encoding="utf-8")
        self.assertIn("compose.hostinger-openwa-gateway.yaml", source, "release SBOM omits Hostinger OpenWA gateway image")

    def test_resolved_preflight_rejects_public_https_gateway_advertisement(self):
        env = self.resolved_env(); env["GATEWAY_INTERNAL_URL"] = "https://8.8.8.8:2785"
        result = subprocess.run(["python3", str(VERIFY), "--resolved"], cwd=ROOT, text=True, capture_output=True, check=False, env=env)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("GATEWAY_INTERNAL_URL", result.stderr)

    def test_resolved_preflight_rejects_trailing_dot_reserved_gateway_hosts(self):
        for host in ("localhost.","host.docker.internal."):
            env=self.resolved_env(); env["GATEWAY_INTERNAL_URL"]="https://"+host; env["GATEWAY_RUNTIME_ALLOWED_HOSTS"]=host
            result=subprocess.run([sys.executable,str(VERIFY),"--resolved"],cwd=ROOT,text=True,capture_output=True,check=False,env=env)
            self.assertNotEqual(result.returncode,0,host); self.assertIn("GATEWAY",result.stderr)

    def test_resolved_preflight_rejects_global_literal_in_gateway_allowlist(self):
        for host in ("8.8.8.8", "2001:4860:4860::8888"):
            env = self.resolved_env()
            env["GATEWAY_RUNTIME_ALLOWED_HOSTS"] = "gateway.private.example," + host
            result = subprocess.run([sys.executable, str(VERIFY), "--resolved"], cwd=ROOT, text=True, capture_output=True, check=False, env=env)
            self.assertNotEqual(result.returncode, 0, host)
            self.assertIn("GATEWAY_RUNTIME_ALLOWED_HOSTS", result.stderr)
