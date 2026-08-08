import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CONTRACT = (ROOT / "contracts/openapi/control-api.yaml").read_text()


def path_block(path: str) -> str:
    marker = f"  {path}:\n"
    start = CONTRACT.index(marker)
    end = CONTRACT.find("\n  /", start + len(marker))
    if end < 0:
        end = len(CONTRACT)
    return CONTRACT[start:end]


class Task4OpenAPIContractTests(unittest.TestCase):
    def test_all_accumulating_event_histories_document_cursor_continuation(self):
        paths = (
            "/admin/gateway-pools/{id}/events",
            "/admin/audience-import-mappings/{id}/events",
            "/admin/reporting-privacy-policies/{id}/events",
            "/admin/configurations/{id}/events",
            "/admin/maintenance-windows/{id}/events",
            "/privacy/cases/{id}/events",
            "/privacy/legal-holds/{id}/events",
            "/admin/provider-capabilities/{id}/events",
            "/admin/retention-policies/{id}/events",
            "/operations/alert-policies/{id}/events",
        )
        for path in paths:
            with self.subTest(path=path):
                block = path_block(path)
                self.assertIn("name: limit", block)
                self.assertIn("name: cursor", block)
                self.assertIn("nextCursor", block)

    def test_new_top_level_inventories_document_cursor_continuation(self):
        paths = (
            "/admin/inbound-retention-policies",
            "/admin/opt-out-policies",
            "/admin/provider-capabilities",
            "/admin/sender-pacing-policies",
            "/admin/users",
            "/organisations/{id}/policies",
            "/consent-reviews",
            "/admin/test-recipients",
            "/sender-nodes",
            "/sender-pools",
            "/commercial-records",
            "/admin/audience-import-mappings",
            "/privacy/legal-holds",
            "/admin/reporting-privacy-policies",
            "/admin/configurations",
            "/admin/maintenance-windows",
            "/admin/gateway-pools",
            "/admin/retention-policies",
            "/operations/alert-policies",
        )
        for path in paths:
            with self.subTest(path=path):
                block = path_block(path)
                self.assertIn("name: limit", block)
                self.assertIn("name: cursor", block)
                self.assertIn("nextCursor", block)
                self.assertIn("hasMore", block)

    def test_gateway_runtime_resource_health_is_container_scoped_and_documented(self):
        self.assertIn("GatewayRuntimeResourceHealth:", CONTRACT)
        for field in (
            "scope", "filesystemPath", "diskTotalBytes", "diskFreeBytes",
            "diskAvailableBytes", "inodesTotal", "inodesFree", "processId",
            "processUptimeSeconds", "openFileDescriptorCount", "networkRxBytes",
            "networkTxBytes", "networkInterfaceCount",
        ):
            self.assertIn(f"        {field}:", CONTRACT)
        self.assertIn("enum: [CONTAINER]", CONTRACT)
    def test_gateway_runtime_health_policy_and_dashboard_evidence_are_documented(self):
        self.assertIn("GatewayRuntimeHealthConfiguration:", CONTRACT)
        self.assertIn("GATEWAY.RUNTIME_HEALTH", CONTRACT)
        for field in (
            "gatewayStaleAfterSeconds", "gatewayRuntimeHealthSource",
            "gatewayRuntimeHealthConfigurationId", "gatewayRuntimeHealthConfigurationVersion",
        ):
            self.assertIn(field, CONTRACT)


if __name__ == "__main__":
    unittest.main()
