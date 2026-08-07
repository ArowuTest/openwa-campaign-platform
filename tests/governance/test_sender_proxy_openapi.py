import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class SenderProxyOpenAPIContractTests(unittest.TestCase):
    def test_sender_proxy_component_schemas_are_defined(self):
        contract = (ROOT / "contracts/openapi/control-api.yaml").read_text()
        self.assertIn("    SenderProxyStatus:\n", contract)
        self.assertIn("    SenderProxyConfigurationRequest:\n", contract)


if __name__ == "__main__":
    unittest.main()
