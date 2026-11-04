"""无需 Android SDK 的源码改名兼容契约检查；不替代 Gradle 或升级实测。"""
import re
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ANDROID_NAME = '{http://schemas.android.com/apk/res/android}name'
ANDROID_TARGET = '{http://schemas.android.com/apk/res/android}targetActivity'


class PersonaIdentifiersTest(unittest.TestCase):
    def test_old_go_commands_share_the_new_source(self):
        for old, new in (('jarvisd', 'personad'), ('jarvisd-wechat', 'personad-wechat')):
            legacy = ROOT / 'server/cmd' / old
            target = ROOT / 'server/cmd' / new
            self.assertTrue(legacy.is_symlink())
            self.assertEqual(target.resolve(), legacy.resolve())
        self.assertTrue((ROOT / 'server/go.mod').read_text().startswith('module github.com/nwverse/persona/server\n'))
        for path in (ROOT / 'server').rglob('*.go'):
            for module in re.findall(r'"(github\.com/[^/]+/[^/]+/server)/internal/', path.read_text()):
                self.assertEqual('github.com/nwverse/persona/server', module)

    def test_application_id_property_keeps_legacy_fallback(self):
        source = (ROOT / 'android/app/build.gradle.kts').read_text()
        self.assertRegex(source, r'providers\.gradleProperty\("personaApplicationId"\)\s*'
                         r'\.orElse\(providers\.gradleProperty\("jarvisApplicationId"\)\)\s*'
                         r'\.getOrElse\("com\.example\.jarvis"\)')
        self.assertIn('namespace = "persona.android"', source)

    def test_launcher_and_old_activity_components_are_preserved(self):
        manifest = ET.parse(ROOT / 'android/app/src/main/AndroidManifest.xml').getroot()
        app = manifest.find('application')
        self.assertEqual('.PersonaApp', app.get(ANDROID_NAME))
        aliases = {item.get(ANDROID_NAME): item.get(ANDROID_TARGET) for item in app.findall('activity-alias')}
        self.assertEqual('.MainActivity', aliases['jarvis.android.MainActivity'])
        self.assertEqual('.ShareActivity', aliases['jarvis.android.ShareActivity'])
        launchers = [component.get(ANDROID_NAME) for component in app if any(
            category.get(ANDROID_NAME) == 'android.intent.category.LAUNCHER'
            for category in component.findall('intent-filter/category'))]
        self.assertEqual(['jarvis.android.MainActivity'], launchers)
        helper = (ROOT / 'android/app/src/main/java/persona/android/ActivityComponents.kt').read_text()
        self.assertIn('Intent().setClassName(context, "jarvis.android.MainActivity")', helper)
        for relative in ('android/app/src/main/java/persona/android/ShareActivity.kt',
                         'android/app/src/main/java/persona/android/platform/LockScreenTests.kt',
                         'android/app/src/debug/java/persona/android/debug/DebugEntryActivity.kt'):
            caller = (ROOT / relative).read_text()
            self.assertIn('mainActivityIntent(', caller)
            self.assertNotIn('MainActivity::class.java', caller)
        self.assertEqual({('jarvis', 'pair'), ('jarvis', 'open'), ('persona', 'pair'), ('persona', 'open')}, {
            (data.get('{http://schemas.android.com/apk/res/android}scheme'),
             data.get('{http://schemas.android.com/apk/res/android}host'))
            for data in app.findall('activity/intent-filter/data')
            if data.get('{http://schemas.android.com/apk/res/android}scheme')
        })

    def test_debug_metadata_is_queried_on_the_canonical_target_not_the_alias(self):
        source = (ROOT / 'android/app/src/main/java/persona/android/MainActivity.kt').read_text()
        self.assertIn('getActivityInfo(ComponentName(this, MainActivity::class.java), PackageManager.GET_META_DATA)', source)
        self.assertNotIn('getActivityInfo(componentName,', source)
        self.assertIn('getString("jarvis.launchAdapter")', source)
        debug = ET.parse(ROOT / 'android/app/src/debug/AndroidManifest.xml').getroot().find('application')
        target = next(item for item in debug.findall('activity') if item.get(ANDROID_NAME) == 'persona.android.MainActivity')
        metadata = target.find('meta-data')
        self.assertEqual('jarvis.launchAdapter', metadata.get(ANDROID_NAME))
        self.assertEqual('persona.android.debug.DebugLaunchAdapter', metadata.get('{http://schemas.android.com/apk/res/android}value'))
        main = ET.parse(ROOT / 'android/app/src/main/AndroidManifest.xml').getroot()
        self.assertFalse(any(item.get(ANDROID_NAME) == 'jarvis.launchAdapter' for item in main.iter('meta-data')))
        adapter = (ROOT / 'android/app/src/debug/java/persona/android/debug/DebugLaunchAdapter.kt').read_text()
        self.assertIn('class DebugLaunchAdapter : LaunchAdapter', adapter)

    def test_pairing_wrappers_parse_both_link_schemes_before_permission_and_networking(self):
        helper = (ROOT / 'android/app/src/main/java/persona/android/platform/PairingLinks.kt').read_text()
        self.assertIn('if (PairingInput.isLink(url)) PairingInput.parse(url) else null', helper)
        pairing_input = (ROOT / 'android/feature/settings/src/main/kotlin/persona/feature/settings/PairingInput.kt').read_text()
        self.assertIn('fun isLink(value: String): Boolean = Pairing.isLink(value)', pairing_input)
        self.assertIn('if (isLink(address)) parse(address)', pairing_input)
        pairing = (ROOT / 'android/core/domain/src/main/kotlin/persona/core/domain/Pairing.kt').read_text()
        self.assertIn('val schemes = listOf("persona", "jarvis")', pairing)
        self.assertIn('schemes.any { value.startsWith("$it://") }', pairing)
        self.assertIn('PairingInput.parse(url)', helper)
        platform = (ROOT / 'android/app/src/main/java/persona/android/platform/LocalNetworkPermission.kt').read_text()
        self.assertEqual(2, platform.count('val parsed = request.parsedPairingLink() ?: request'))
        self.assertEqual(2, platform.count('permission.ensureAllowed(parsed)'))
        self.assertEqual(2, platform.count('permission.missingFor(parsed)'))
        self.assertNotIn('startsWith("jarvis:")', platform)
        wired = (ROOT / 'android/app/src/main/java/persona/android/di/PairingModule.kt').read_text()
        self.assertIn('val parsed = request.parsedPairingLink()', wired)
        self.assertIn('HubPairingClient.addresses(parsed)', wired)
        self.assertNotIn('startsWith("jarvis:")', wired)

    def test_only_one_foreground_service_identity_is_registered(self):
        manifest = ET.parse(ROOT / 'android/platform/notify/src/main/AndroidManifest.xml').getroot()
        app = manifest.find('application')
        self.assertEqual(['jarvis.platform.notify.HubService'], [item.get(ANDROID_NAME) for item in app.findall('service')])
        receivers = {item.get(ANDROID_NAME) for item in app.findall('receiver')}
        self.assertTrue({'jarvis.platform.notify.ActionReceiver', 'persona.platform.notify.ActionReceiver'} <= receivers)
        source = (ROOT / 'android/platform/notify/src/main/kotlin/persona/platform/notify/HubService.kt').read_text()
        self.assertEqual(2, source.count('Intent(context, jarvis.platform.notify.HubService::class.java)'))

    def test_legacy_workers_and_receivers_delegate_to_new_implementations(self):
        wrappers = {
            'android/platform/notify/src/main/kotlin/jarvis/platform/notify/LegacyComponents.kt':
                ('HubService', 'ActionReceiver', 'BootReceiver', 'ConnectionWatchdog'),
            'android/platform/phonetools/src/main/kotlin/jarvis/platform/phonetools/LegacyPhoneNotificationWorker.kt':
                ('PhoneNotificationWorker',),
            'android/app/src/main/java/jarvis/android/platform/LegacyShareRetryWorker.kt': ('ShareRetryWorker',),
        }
        for relative, names in wrappers.items():
            source = (ROOT / relative).read_text()
            for name in names:
                self.assertRegex(source, rf'class {name}(?:\([^\n]*\))?\s*:\s*persona\.[\w.]*{name}\(')
                package = re.search(r'^package (\S+)', source, re.M)[1].replace('jarvis.', 'persona.', 1)
                implementations = list((ROOT / 'android').rglob('*.kt'))
                self.assertTrue(any(f'package {package}\n' in path.read_text()
                                    and re.search(rf'open class {name}\b', path.read_text())
                                    for path in implementations))

    def test_saved_route_type_names_and_tab_keys_stay_stable(self):
        source = (ROOT / 'android/core/navigation/src/main/kotlin/persona/core/navigation/Routes.kt').read_text()
        for name in ('InboxItem', 'LarkConversation', 'Interaction'):
            self.assertIn(f'@SerialName("jarvis.core.navigation.RouteSource.{name}")', source)
        self.assertIn('enum class AppTab { Jarvis, Agents, Inbox, Settings }', source)
        self.assertIn('@SerialName("jarvis.core.navigation.ChatRoute")', source)


if __name__ == '__main__':
    unittest.main()
