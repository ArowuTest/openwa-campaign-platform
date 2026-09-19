import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


class RetryReconciliationContractTests(unittest.TestCase):
    def test_duplicate_risk_flag_is_only_allowed_for_confirm_not_submitted(self):
        contract = (ROOT / "contracts" / "openapi" / "control-api.yaml").read_text(encoding="utf-8")
        marker = "  /operations/delivery-exceptions/{id}/resolve:"
        start = contract.index(marker)
        end = contract.find("\n  /", start + len(marker))
        section = contract[start:] if end < 0 else contract[start:end]
        confirm_marker = "action: { type: string, enum: [CONFIRM_NOT_SUBMITTED] }"
        before_confirm = section[: section.index(confirm_marker)]
        self.assertNotIn("duplicateRiskAccepted:", before_confirm)
        self.assertIn("required: [action, evidenceRef, reason, duplicateRiskAccepted]", section)
        self.assertIn("duplicateRiskAccepted: { type: boolean, enum: [true]", section)

    def test_metrics_worker_started_log_is_only_on_successful_initial_drain(self):
        source = (ROOT / "cmd" / "metrics-worker" / "main.go").read_text(encoding="utf-8")
        self.assertIn(
            'health.SetReady(true)\n\t\tlogger.Info("metrics reconciliation worker started"',
            source,
        )


if __name__ == "__main__":
    unittest.main()
