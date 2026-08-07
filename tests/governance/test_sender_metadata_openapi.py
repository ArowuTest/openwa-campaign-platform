import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class SenderMetadataOpenAPIContractTests(unittest.TestCase):
    def test_sender_metadata_route_and_write_only_recovery_reference_are_defined(self):
        contract = (ROOT / "contracts/openapi/control-api.yaml").read_text()
        self.assertIn("  /sender-sessions/{id}/metadata:\n", contract)
        self.assertIn("    SenderSessionRegistrationRequest:\n", contract)
        self.assertIn("    SenderOperationalMetadataRequest:\n", contract)
        self.assertGreaterEqual(contract.count("recoveryReference: { type: string, minLength: 3, maxLength: 500, writeOnly: true"), 2)

    def test_sender_health_contract_reports_governed_policy_evidence(self):
        contract = (ROOT / "contracts/openapi/control-api.yaml").read_text()
        self.assertIn("    SenderHealthAssessment:\n", contract)
        for field in ("policySource", "policyConfigurationId", "policyScopeType", "policyScopeId", "policyVersion"):
            self.assertIn(f"        {field}:", contract)
        for field in ("recentOutcomeCount", "recentFailureCount", "recentFailureRateBps", "recentDisconnectCount"):
            self.assertIn(f"        {field}:", contract)
        self.assertIn("policyScopeType: { type: string, enum: [PLATFORM, GATEWAY_POOL, SENDER_POOL, SENDER_SESSION] }", contract)


if __name__ == "__main__":
    unittest.main()
