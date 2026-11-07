"""Keep extension UI-test environment variables and mock setup aligned."""
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class ExtensionTestNames(unittest.TestCase):
    def test_extension_tests_use_canonical_environment_suffixes(self):
        tokens = re.compile(r"\b(?:TEST_RUNNER_)?PERSONA_([A-Z_]+)\b")
        files = list((ROOT / "apple/PersonaUITests").glob("Extension*Tests.swift"))
        files.append(ROOT / "apple/PersonaUITests/IMessageExtensionTests.swift")
        self.assertTrue(files)
        for path in files:
            for suffix in tokens.findall(path.read_text()):
                self.assertFalse(suffix.startswith("PLUGINS_"), f"noncanonical extension test setting in {path.name}")

    def test_mock_uses_the_same_extension_term(self):
        mock = (ROOT / "scripts/mock/mock_hub.py").read_text()
        self.assertIn('os.environ.get("MOCK_EXTENSIONS"', mock)
        docs = (ROOT / "docs/operations/testing.md").read_text()
        self.assertIn("MOCK_EXTENSIONS=1", docs)
        self.assertIn("TEST_RUNNER_PERSONA_EXTENSIONS_HUB", docs)


if __name__ == "__main__":
    unittest.main()
