import json
from pathlib import Path
import plistlib
import re
import tempfile
import unittest
from unittest.mock import Mock, patch

import make_readme_assets as assets
from readme_demo import COPY, MAIN, PLAN, TESTS, WIDGET, dataset


class ReadmeAssetsTests(unittest.TestCase):
    def test_languages_have_the_same_copy_keys(self):
        self.assertEqual(set(COPY["en"]), set(COPY["zh-CN"]))

    def test_english_fixture_contains_no_cjk(self):
        value = json.dumps(dataset("en"), ensure_ascii=False)
        self.assertIsNone(re.search(r"[㐀-鿿]", value))
        self.assertIn("Which framework", value)

    def test_chinese_fixture_contains_chinese(self):
        self.assertIn("网络层的测试", json.dumps(dataset("zh-CN"), ensure_ascii=False))

    def test_fixture_has_no_mail_addresses_or_private_endpoints(self):
        for language in COPY:
            value = json.dumps(dataset(language), ensure_ascii=False)
            self.assertIsNone(re.search(r"[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}", value))
            self.assertNotIn("http://", value)
            self.assertNotIn("https://", value)
            self.assertEqual({m["id"] for m in dataset(language)["machines"]}, {"hub", "laptop", "desktop"})

    def test_scene_keys_exist_in_each_fixture(self):
        for language in COPY:
            data = dataset(language)
            keys = {s["machine_id"] + "/" + s["session_id"] for s in data["sessions"]}
            self.assertTrue({MAIN, PLAN, TESTS, WIDGET}.issubset(keys))
            for key in (MAIN, PLAN, TESTS, WIDGET):
                self.assertTrue(data["items"][key.split("/")[1]])
            self.assertEqual(len(data["interactions"]), 2)

    def test_names_follow_the_readme_language(self):
        self.assertEqual(assets.asset_name("hero", "light", "en", "webp"), "hero-light.webp")
        self.assertEqual(assets.asset_name("hero", "dark", "zh-CN", "webp"), "hero-dark.zh-CN.webp")
        self.assertEqual(assets.asset_name("plan", "light", "zh-CN", "png"), "plan-light.zh-CN.png")

    @patch.object(assets.subprocess, "run")
    def test_fresh_clears_hub_and_pins_language_layout_and_restore_state(self, run):
        for language, apple_language in (("en", "en"), ("zh-CN", "zh-Hans")):
            ctl = Mock(udid="sample-simulator", hub="unused")
            assets.fresh(ctl, "dark", language)
            args = ctl.launch.call_args.kwargs["extra"]
            pairs = dict(zip(args[::2], args[1::2]))
            self.assertIsNone(ctl.hub)
            self.assertEqual(pairs["-AppleLanguages"], f"({apple_language})")
            self.assertEqual(pairs["-dev.openChat"], "")
            self.assertEqual(pairs["-ui.style"], "glass")
            self.assertEqual(pairs["-openChatsFresh"], "YES")
            self.assertEqual(pairs["-openAgentsFresh"], "YES")

    @patch.object(assets.subprocess, "run")
    def test_only_a_temporary_app_copy_receives_the_fixture(self, run):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source = root / "source.app"
            source.mkdir()
            (source / "Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier": "com.example.persona", "CFBundleExecutable": "Persona", "CFBundleSupportedPlatforms": ["iPhoneSimulator"]}))
            (source / "Persona").write_bytes(b"DEBUG openChatsFresh runtimeCLI")
            (source / "Demo.json").write_text("{}")
            ctl = Mock(udid="sample-simulator")
            digest = assets.install_fixture(ctl, source, "en", root / "staging")
            copied = root / "staging/en/source.app/Demo.json"
            self.assertEqual(json.loads(copied.read_text()), dataset("en"))
            self.assertEqual((source / "Demo.json").read_text(), "{}")
            self.assertEqual(len(digest), 64)
            self.assertEqual(ctl.bundle, "com.example.persona")
            self.assertIn("--preserve-metadata=identifier,entitlements", run.call_args_list[0].args[0])

    @patch.object(assets.subprocess, "run")
    def test_debug_marker_may_live_in_xcode_debug_library(self, run):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source = root / "source.app"
            source.mkdir()
            (source / "Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier": "com.example.persona", "CFBundleExecutable": "Persona", "CFBundleSupportedPlatforms": ["iPhoneSimulator"]}))
            (source / "Persona").write_bytes(b"loader stub")
            (source / "Persona.debug.dylib").write_bytes(b"DEBUG openChatsFresh runtimeCLI")
            (source / "Demo.json").write_text("{}")
            digest = assets.install_fixture(Mock(udid="sample-simulator"), source, "en", root / "staging")
            self.assertEqual(len(digest), 64)
            self.assertEqual((source / "Demo.json").read_text(), "{}")

    def test_inbox_capture_retries_the_route_and_requires_visible_controls(self):
        ctl = Mock()
        ctl.call.side_effect = [{"count": 0}, {}, {"element": {}}, {"count": 1}]
        assets.ensure_scene(ctl, {"scene": "inbox"})
        self.assertIn(unittest.mock.call("navigate", coldChats=True, keep=True, scene="inbox"), ctl.call.call_args_list)
        ctl = Mock()
        ctl.call.return_value = {"count": 0}
        with self.assertRaises(SystemExit):
            assets.ensure_scene(ctl, {"scene": "inbox"})

    @patch.object(assets.Path, "is_dir", return_value=True)
    @patch.object(assets, "cmd_hero")
    @patch.object(assets.sys, "argv", ["make_readme_assets.py", "hero", "/tmp/readme-captures"])
    def test_one_hero_command_routes_both_language_directories(self, hero, is_dir):
        assets.main()
        self.assertEqual(hero.call_args_list, [unittest.mock.call("/tmp/readme-captures/en", "en"), unittest.mock.call("/tmp/readme-captures/zh-CN", "zh-CN")])

    @patch.object(assets, "cmd_hero")
    @patch.object(assets.sys, "argv", ["make_readme_assets.py", "hero", "/tmp/readme-captures", "zh-CN"])
    def test_existing_positional_hero_language_is_preserved(self, hero):
        assets.main()
        hero.assert_called_once_with("/tmp/readme-captures", "zh-CN")

    @patch.object(assets, "cmd_svg")
    @patch.object(assets, "cmd_shoot")
    @patch.object(assets, "cmd_hero")
    @patch.object(assets.sys, "argv", ["make_readme_assets.py", "all", "/tmp/readme-captures"])
    def test_all_records_and_encodes_without_touching_svg(self, hero, shoot, svg):
        assets.main()
        self.assertEqual(shoot.call_args.kwargs["languages"], ("en", "zh-CN"))
        self.assertEqual(hero.call_count, 2)
        svg.assert_not_called()

    @patch.object(assets, "hero")
    def test_master_hero_function_keeps_flat_paths_and_suffixes(self, hero):
        assets.cmd_hero("/tmp/readme-captures/en", "en")
        assets.cmd_hero("/tmp/readme-captures/zh-CN", "zh-CN")
        self.assertEqual(hero.call_count, 4)
        self.assertEqual({c.args[1] for c in hero.call_args_list}, {"light", "dark"})
        self.assertEqual({c.args[3] for c in hero.call_args_list}, {"", ".zh-CN"})


if __name__ == "__main__":
    unittest.main()
