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

    def test_superagent_grid_order_and_compact_readmes(self):
        expected = ["assistant", "handoff", "extensions", "island", "inbox", "phone-data", "connectors", "clients"]
        for name in ("README.md", "README.zh-CN.md"):
            with self.subTest(readme=name):
                text = (ROOT / name).read_text()
                self.assertLessEqual(len(text.splitlines()), 100)
                self.assertIn("superagent", "\n".join(text.splitlines()[:14]).lower())
                grid = re.search(r"<table>.*?</table>", text, re.S).group()
                self.assertEqual(expected, re.findall(r'icon-([\w-]+)\.svg', grid))
                self.assertEqual(8, grid.count("<td "))
                self.assertLess(text.index("<table>"), text.index("hero-"))
                self.assertNotIn("Persona used to be called Persona", text)
                self.assertNotIn("Persona 原名 Persona", text)
                for target in re.findall(r'(?:src|srcset|href)="([^"]+)"', text):
                    if ":" not in target and not target.startswith("#"):
                        self.assertTrue((ROOT / target).is_file(), target)

    def test_runtime_and_client_badges_are_generated_and_linked(self):
        for label in ASSETS.RUNTIME_BADGES:
            filename = f"badge-{label.lower().replace(' ', '-')}.svg"
            actual = ASSETS.badge(label)
            self.assertEqual(actual, (ROOT / f"docs/assets/readme/{filename}").read_text())
            self.assertEqual(label, ET.fromstring(actual).attrib["aria-label"])
            for name in ("README.md", "README.zh-CN.md"):
                self.assertIn(f"[![{label}](docs/assets/readme/{filename})]", (ROOT / name).read_text())
        for name in ("README.md", "README.zh-CN.md"):
            badge_lines = [line for line in (ROOT / name).read_text().splitlines()
                           if "badge-claude-code.svg" in line or "badge-codex.svg" in line
                           or "badge-herdr.svg" in line or "badge-orca.svg" in line]
            self.assertEqual(1, len(badge_lines), "keep the runtime badges on one GFM source line")
        for label, value in ASSETS.versions():
            self.assertEqual(ASSETS.badge(label, value),
                             (ROOT / f"docs/assets/readme/badge-{label.lower()}.svg").read_text())
        actual = ASSETS.badge("clients", ASSETS.CLIENTS_BADGE)
        self.assertEqual(actual, (ROOT / "docs/assets/readme/badge-clients.svg").read_text())
        ET.fromstring(actual)

    def test_generated_feature_icons_match(self):
        for name in ASSETS.GLYPHS:
            with self.subTest(icon=name):
                actual = ASSETS.icon(name)
                self.assertEqual(actual, (ROOT / f"docs/assets/readme/icon-{name}.svg").read_text())
                ET.fromstring(actual)

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
