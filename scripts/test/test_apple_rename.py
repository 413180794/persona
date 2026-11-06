import importlib.util
import json
from pathlib import Path
import plistlib
import re
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
APPLE = ROOT / "apple"
PROJECT = (APPLE / "project.yml").read_text()
TARGETS = PROJECT.split("targets:\n", 1)[1].split("schemes:\n", 1)[0]
SCHEMES = PROJECT.split("schemes:\n", 1)[1]


def blocks(text):
    matches = list(re.finditer(r"^  (\w+):\n", text, re.MULTILINE))
    return {m.group(1): text[m.end():matches[i + 1].start() if i + 1 < len(matches) else len(text)]
            for i, m in enumerate(matches)}


class AppleRenameTests(unittest.TestCase):
    def test_new_sources_do_not_recreate_retired_directories(self):
        for legacy in ("Jarvis", "JarvisWatch", "JarvisWidgets", "JarvisNotify", "JarvisTests", "JarvisMacTests", "JarvisUITests"):
            with self.subTest(directory=legacy):
                self.assertFalse((APPLE / legacy).exists(), "new files must use the canonical Persona source directory")
        self.assertFalse((APPLE / "PersonaLink/Sources/JarvisLink").exists())
        # Persona Link lives beside the other Apple products (#883); a top-level mac/ is easy to take for the Mac app.
        # Tracked files only: a checkout that built it before still has the ignored mac/build/ lying around.
        tracked = subprocess.run(["git", "ls-files", "--", "mac"], cwd=ROOT, capture_output=True, text=True, check=True).stdout
        self.assertEqual(tracked, "", "the menu bar app is apple/PersonaLink/")
        self.assertTrue((APPLE / "Jarvis.xcodeproj").is_symlink())

    def test_targets_keep_original_product_modules(self):
        targets = blocks(TARGETS)
        expected = {
            "Persona": "Jarvis", "PersonaMac": "Jarvis", "PersonaWatch": "Jarvis",
            "PersonaVision": "Jarvis",
            "PersonaWidgets": "JarvisWidgets", "PersonaNotify": "JarvisNotify",
            "PersonaMacNotify": "JarvisNotify",
        }
        # The share extension (#4586) is new, so it has no Jarvis module to keep.
        self.assertEqual(set(targets), {*expected, "PersonaShare", "PersonaVisionShare", "PersonaTests", "PersonaMacTests", "PersonaUITests"})
        for target, module in expected.items():
            with self.subTest(target=target):
                self.assertIn("PRODUCT_MODULE_NAME: " + module + "\n", targets[target])
                self.assertIn("PRODUCT_NAME: Persona", targets[target])
        self.assertIn("TEST_TARGET_NAME: Persona\n", targets["PersonaUITests"])
        self.assertIn('Persona.app/Persona"', targets["PersonaTests"])
        self.assertIn('Persona.app/Contents/MacOS/Persona"', targets["PersonaMacTests"])

    def test_vision_target_keeps_canonical_source_and_scheme(self):
        target = blocks(TARGETS)["PersonaVision"]
        self.assertIn("platform: visionOS\n", target)
        self.assertIn("- path: Vision\n", target)
        self.assertIn("PRODUCT_NAME: Persona\n", target)
        self.assertTrue((APPLE / "Vision").is_dir())
        self.assertIn("PersonaVision: all\n", blocks(SCHEMES)["PersonaVision"])

    def test_legacy_schemes_build_the_same_targets(self):
        schemes = blocks(SCHEMES)
        for suffix in ("", "Mac", "Watch"):
            legacy = schemes["Jarvis" + suffix].split("  # Legacy", 1)[0].strip()
            current = schemes["Persona" + suffix].split("  # Legacy", 1)[0].strip()
            self.assertEqual(legacy, current)
        alias = APPLE / "Jarvis.xcodeproj"
        self.assertTrue(alias.is_symlink())
        self.assertEqual(alias.readlink(), Path("Persona.xcodeproj"))

    def test_intent_control_and_activity_types_keep_system_identity(self):
        expected = {
            "Persona/Intents/AskPersonaIntent.swift": ["AskJarvisIntent"],
            "Persona/Intents/PersonaStatusIntent.swift": ["JarvisStatusIntent"],
            "Persona/Intents/PersonaShortcuts.swift": ["JarvisShortcuts"],
            "PersonaWidgets/PersonaControls.swift": ["AskJarvisControl", "JarvisAgentsControl"],
            "LiveActivity/PersonaControlIntents.swift": ["OpenJarvisAssistantIntent", "OpenJarvisAgentsIntent"],
            "LiveActivity/PersonaActivityAttributes.swift": ["JarvisActivityAttributes"],
        }
        for path, names in expected.items():
            source = (APPLE / path).read_text()
            for name in names:
                with self.subTest(path=path, name=name):
                    self.assertRegex(source, r"\bstruct " + name + r"\s*:")
        controls = (APPLE / "PersonaWidgets/PersonaControls.swift").read_text()
        self.assertIn('kind: "jarvis.control.ask"', controls)
        self.assertIn('kind: "jarvis.control.agents"', controls)
        info = plistlib.loads((APPLE / "PersonaNotify/Info.plist").read_bytes())
        self.assertEqual(info["NSExtension"]["NSExtensionPrincipalClass"], "$(PRODUCT_MODULE_NAME).NotificationService")
        self.assertIn("JarvisPushKeychainGroup", info)
        self.assertNotIn("PersonaPushKeychainGroup:", PROJECT)
        self.assertNotIn("PersonaPushRelay:", PROJECT)
        self.assertNotIn("PersonaAppGroup:", PROJECT)
        for relative in ("Persona/Resources/Info.plist", "Mac/Resources/Info.plist"):
            info = plistlib.loads((APPLE / relative).read_bytes())
            schemes = info["CFBundleURLTypes"][0]["CFBundleURLSchemes"]
            self.assertIn("jarvis", schemes)
            self.assertIn("persona", schemes)

    def test_avatar_names_do_not_collide_with_persona_model(self):
        source = (APPLE / "Persona/Views/Assistant/AssistantAvatar.swift").read_text()
        self.assertIn("struct AssistantAvatar: View", source)
        self.assertIn("enum AssistantAvatarGallery", source)
        self.assertNotIn("struct PersonaAvatar", source)
        self.assertTrue((APPLE / "Persona/Persona/Persona.swift").is_file())

    def test_baseline_paths_resolve_without_changing_thresholds(self):
        baseline = json.loads((APPLE / "PersonaTests/TokenBaseline.json").read_text())
        self.assertTrue(baseline)
        for path in baseline:
            with self.subTest(path=path):
                self.assertFalse(path.startswith("Jarvis"))
                self.assertTrue((APPLE / path).is_file())

    def test_launch_and_ctl_compatibility_entries(self):
        launch = (APPLE / "PersonaUITests/LaunchLayout.swift").read_text()
        self.assertIn("func launchPersona()", launch)
        self.assertIn("func launchJarvis() { launchPersona() }", launch)
        self.assertIn("func launchPinned()", (APPLE / "PersonaUITests/UITestStyle.swift").read_text())
        alias = ROOT / "scripts/app/personactl.py"
        self.assertTrue(alias.is_symlink())
        self.assertEqual(alias.readlink(), Path("jarvisctl.py"))

    def test_menu_bar_package_keeps_installed_process_name(self):
        package = (APPLE / "PersonaLink/Package.swift").read_text()
        self.assertIn('name: "PersonaLink"', package)
        self.assertIn('path: "Sources/PersonaLink"', package)
        info = plistlib.loads((APPLE / "PersonaLink/Info.plist").read_bytes())
        self.assertEqual(info["CFBundleExecutable"], "JarvisLink")
        script = (ROOT / "scripts/app/mac-link.sh").read_text()
        self.assertIn("-scheme PersonaLink", script)
        self.assertIn('cp "$bin" "$APP/Contents/MacOS/JarvisLink"', script)
        self.assertIn("pkill -x JarvisLink", script)

    def test_entitlement_checker_accepts_new_and_legacy_products(self):
        spec = importlib.util.spec_from_file_location("entitlements", ROOT / "scripts/release/entitlements.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.assertEqual(Path(module.PROJECT), APPLE / "Persona.xcodeproj")
        for name in ("Persona.app", "Jarvis.app"):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                app = Path(directory) / name
                app.mkdir()
                self.assertEqual(Path(module.app_bundle(directory)), app)
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(SystemExit):
                module.app_bundle(directory)
            for name in ("Persona.app", "Jarvis.app"):
                (Path(directory) / name).mkdir()
            with self.assertRaises(SystemExit):
                module.app_bundle(directory)


if __name__ == "__main__":
    unittest.main()
