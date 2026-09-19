import ast
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
GENERATOR = ROOT / "scripts" / "generate-traceability.py"


class TraceabilityGenerationTests(unittest.TestCase):
    def test_generated_outputs_use_explicit_cross_platform_encoding(self):
        tree = ast.parse(GENERATOR.read_text(encoding="utf-8"))
        writes = []
        for node in ast.walk(tree):
            if isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and node.func.attr == "write_text":
                writes.append({kw.arg: kw.value for kw in node.keywords})
        self.assertEqual(len(writes), 2)
        encodings = []
        newlines = []
        for keywords in writes:
            self.assertIn("encoding", keywords)
            self.assertIn("newline", keywords)
            self.assertIsInstance(keywords["encoding"], ast.Constant)
            self.assertIsInstance(keywords["newline"], ast.Constant)
            encodings.append(keywords["encoding"].value)
            newlines.append(keywords["newline"].value)
        self.assertEqual(encodings, ["cp1252", "utf-8"])
        self.assertEqual(newlines, ["\n", "\n"])


if __name__ == "__main__":
    unittest.main()
