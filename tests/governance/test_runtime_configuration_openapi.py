import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class RuntimeConfigurationOpenAPIContractTests(unittest.TestCase):
    def test_platform_configuration_create_contract_supports_sender_session_scope(self):
        contract = (ROOT / "contracts/openapi/control-api.yaml").read_text()
        self.assertIn("PlatformConfigurationCreateRequest:", contract)
        self.assertIn("SENDER_SESSION", contract)
        self.assertIn("#/components/schemas/PlatformConfigurationCreateRequest", contract)

    def test_sender_transport_recovery_configuration_is_documented(self):
        contract = (ROOT / "contracts/openapi/control-api.yaml").read_text()
        self.assertIn("SenderTransportRecoveryConfiguration:", contract)
        for field in (
            "reconnectMode", "reconnectMaxAttempts", "reconnectBaseDelayMs",
            "reconnectStabilityResetMs", "watchdogProbeTimeoutMs",
            "watchdogFailureThreshold", "engineTeardownTimeoutMs",
        ):
            self.assertIn(f"        {field}:", contract)
        self.assertIn("SENDER.TRANSPORT_RECOVERY", contract)


if __name__ == "__main__":
    unittest.main()
