import json
import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class ReleaseReadinessTests(unittest.TestCase):
    def test_development_governance_check_passes(self):
        result = subprocess.run(
            ["python3", "scripts/verify-release-readiness.py"],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Governance structure valid", result.stdout)

    def test_production_candidate_fails_with_open_gates(self):
        result = subprocess.run(
            ["python3", "scripts/verify-release-readiness.py", "--production-candidate"],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("Production-candidate gate failed", result.stderr)

    def test_release_gate_ids_are_unique(self):
        data = json.loads((ROOT / "config/release-gates.json").read_text())
        ids = [gate["id"] for gate in data["hard_gates"]]
        self.assertEqual(len(ids), len(set(ids)))


if __name__ == "__main__":
    unittest.main()
