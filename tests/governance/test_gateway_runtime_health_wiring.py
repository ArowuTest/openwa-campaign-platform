from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


def service_block(source: str, name: str) -> str:
    match = re.search(
        rf"(?ms)^  {re.escape(name)}:\n(.*?)(?=^  [A-Za-z0-9_-]+:\n|\Z)",
        source,
    )
    if not match:
        raise AssertionError(f"service {name} not found")
    return match.group(1)


class GatewayRuntimeHealthWiringTests(unittest.TestCase):
    def test_control_api_uses_governed_runtime_health_threshold(self):
        source = (ROOT / "cmd/control-api/runtime.go").read_text(encoding="utf-8")
        self.assertIn("PlatformGatewayRuntimeHealthResolver", source)
        self.assertIn("FallbackStaleAfter: cfg.GatewayStaleAfter", source)
        self.assertIn("FallbackGatewayStaleAfter: cfg.GatewayStaleAfter", source)

    def test_governance_worker_uses_same_governed_runtime_health_threshold(self):
        source = (ROOT / "cmd/platform-governance-worker/main.go").read_text(encoding="utf-8")
        self.assertIn('"campaign-platform/internal/platformpolicy"', source)
        self.assertIn("PlatformGatewayRuntimeHealthResolver", source)
        self.assertIn("FallbackStaleAfter: cfg.GatewayStaleAfter", source)
        self.assertIn("FallbackGatewayStaleAfter: cfg.GatewayStaleAfter", source)

    def test_stale_threshold_is_exposed_in_both_compose_profiles(self):
        expected = "GATEWAY_STALE_AFTER_SECONDS: ${GATEWAY_STALE_AFTER_SECONDS:-120}"
        for relative in (
            "infrastructure/compose/compose.yaml",
            "infrastructure/compose/compose.production.yaml",
        ):
            source = (ROOT / relative).read_text(encoding="utf-8")
            self.assertIn(expected, service_block(source, "control-api"), relative)
            self.assertIn(expected, service_block(source, "platform-governance-worker"), relative)

    def test_development_gateway_publishes_runtime_health_to_control_plane(self):
        source = (ROOT / "infrastructure/compose/compose.yaml").read_text(encoding="utf-8")
        control = service_block(source, "control-api")
        gateway = service_block(source, "openwa-gateway")
        expected_secret = (
            "GATEWAY_RUNTIME_SECRET: ${GATEWAY_RUNTIME_SECRET:-"
            "development-gateway-runtime-secret-change-me-32}"
        )
        self.assertIn(expected_secret, control)
        self.assertIn(expected_secret, gateway)
        self.assertIn("CONTROL_API_INTERNAL_URL: http://control-api:8080", gateway)

    def test_stale_threshold_is_documented_for_operators(self):
        source = (ROOT / ".env.example").read_text(encoding="utf-8")
        self.assertIn("GATEWAY_STALE_AFTER_SECONDS=120", source)


class CurrentBootLeaseWiringTests(unittest.TestCase):
    """Static wiring guard only; PostgreSQL behavioral tests remain mandatory."""

    def assert_boot_binding(self, relative, anchor):
        source = (ROOT / relative).read_text(encoding="utf-8")
        start = source.index(anchor)
        # Inspect the first SQL literal in the named production query/function,
        # not comments or unrelated code that might contain the same predicate.
        query = source[start:].split("`", 2)[1]
        compact = re.sub(r"\s+", "", query)
        self.assertIn("coalesce(sn.boot_id,'')<>''", compact)
        self.assertIn("sl.lease_token_hash=sha256(convert_to(sn.boot_id,'UTF8'))", compact)

    def test_candidate_allocation_requires_current_boot(self):
        self.assert_boot_binding("internal/sender/postgres.go", "const postgresAllocationQuery")

    def test_existing_assignment_requires_current_boot(self):
        self.assert_boot_binding("internal/sender/postgres.go", "const postgresAssignedSessionValidationQuery")

    def test_campaign_materialization_requires_current_boot(self):
        self.assert_boot_binding("internal/dispatch/postgres_material.go", "func (l *PostgreSQLMaterialLoader) loadGovernedRoute")

    def test_internal_test_route_requires_current_boot(self):
        self.assert_boot_binding("internal/testmessage/postgres.go", "func (r *PostgreSQLRepository) ValidateTestRoute")


if __name__ == "__main__":
    unittest.main()
