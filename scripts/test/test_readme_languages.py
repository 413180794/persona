"""The bilingual README has reciprocal switches and reproducible localized SVGs."""
from pathlib import Path
import importlib.util
import re
import unittest
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/assets/make_readme_assets.py"
SPEC = importlib.util.spec_from_file_location("readme_assets", SCRIPT)
ASSETS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ASSETS)
SVG = "{http://www.w3.org/2000/svg}"


class ReadmeLanguageTests(unittest.TestCase):
    def test_readmes_switch_languages_at_the_top(self):
        en = (ROOT / "README.md").read_text()
        zh = (ROOT / "README.zh-CN.md").read_text()
        self.assertIn("[简体中文](README.zh-CN.md)", "\n".join(en.splitlines()[:12]))
        self.assertIn("[English](README.md)", "\n".join(zh.splitlines()[:12]))
        self.assertIn("banner-light.svg", en)
        self.assertIn("banner-light.zh-CN.svg", zh)
        self.assertEqual(len(re.findall(r"^#{1,6} ", en, re.M)),
                         len(re.findall(r"^#{1,6} ", zh, re.M)))

    def test_generated_svg_text_and_committed_assets_match(self):
        for lang, suffix in ASSETS.LANGS.items():
            for theme in ASSETS.THEMES:
                for name, draw in (("banner", ASSETS.banner), ("architecture", ASSETS.architecture)):
                    with self.subTest(lang=lang, theme=theme, image=name):
                        actual = draw(theme, lang)
                        committed = ROOT / f"docs/assets/readme/{name}-{theme}{suffix}.svg"
                        self.assertEqual(actual, committed.read_text(), "regenerate the README SVG assets")
                        root = ET.fromstring(actual)
                        self.assertEqual(lang, root.attrib["lang"])
                        text = " ".join("".join(node.itertext()) for tag in ("title", "text")
                                        for node in root.iter(SVG + tag))
                        if lang == "en":
                            self.assertNotRegex(text, r"[一-鿿]")
                        else:
                            self.assertRegex(text, r"[一-鿿]")


if __name__ == "__main__":
    unittest.main()
