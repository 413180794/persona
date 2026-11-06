"""Regression tests for the compiler-free English-key migration (#5090)."""
import copy
import json
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts/i18n'))
import apple_english_keys as migration


def entry(en, zh, ja):
    return {'localizations': {'en': migration.unit(en), 'zh-Hans': migration.unit(zh), 'ja': migration.unit(ja)}}


class ArgumentTests(unittest.TestCase):
    def test_positional_reordering_preserves_expression_identity(self):
        key, permutation = migration.key_template('%2$lld unread in %1$lld sessions: %3$@')
        self.assertEqual(key, '%lld unread in %lld sessions: %@')
        self.assertEqual(migration.swift_string(key, ['sessions', 'unread', 'names'], permutation),
                         '"\\(unread) unread in \\(sessions) sessions: \\(names)"')
        zh = migration.unit('%1$lld 个会话共 %2$lld 条未读：%3$@')
        migration.remap(zh, permutation)
        self.assertEqual(zh['stringUnit']['value'], '%2$lld 个会话共 %1$lld 条未读：%3$@')

    def test_named_parameters_follow_names_not_old_order(self):
        self.assertEqual(migration.swift_string('Send ${message} to ${recipient}',
                         [r'\.$recipient', r'\.$message'], {}),
                         '"Send \\(\\.$message) to \\(\\.$recipient)"')

    def test_percent_is_not_an_argument(self):
        self.assertEqual(list(migration.placeholders('%1$lld%% used')), [(0, 6, 1, 'lld')])
        self.assertEqual(migration.swift_string('%lld%% used', ['percent'], {1: 1}), '"\\(percent)% used"')

    def test_unknown_side_effects_fail_before_reordering(self):
        with self.assertRaisesRegex(ValueError, 'purity audit'):
            migration.swift_string('%lld after %lld', ['next()', 'next()'], {1: 2, 2: 1})

    def test_substitutions_are_not_assumed_to_be_uploads(self):
        value = {'localizations': {'en': {'stringUnit': {'value': 'Deleting %#@count@'},
            'substitutions': {'count': {'argNum': 1, 'formatSpecifier': 'lld'}}}}}
        self.assertEqual(migration.canonical('删除 %lld 个', value), 'Deleting %1$lld')

    def test_substitution_keeps_local_argument_and_remaps_other_references(self):
        holder = {'stringUnit': {'value': '%1$@ and %#@count@'}, 'substitutions': {'count': {
            'argNum': 2, 'formatSpecifier': 'lld', 'variations': {'plural': {
                'other': migration.unit('%arg files for %1$@')}}}}}
        migration.remap(holder, {1: 2, 2: 1})
        sub = holder['substitutions']['count']
        self.assertEqual(sub['argNum'], 1)
        self.assertEqual(sub['variations']['plural']['other']['stringUnit']['value'], '%arg files for %2$@')

    def test_multiline_literal_indentation_is_removed_before_splitting(self):
        self.assertEqual(migration.shape('\n    文件 \\(count)\n    ', True), migration.shape('文件 %lld'))

    def test_unknown_ambiguous_type_is_not_guessed(self):
        literal = migration.swift_literals.literals('Text("值 \\(username)")')[0]
        with self.assertRaisesRegex(ValueError, 'Unresolved interpolation type'):
            migration.choose(['值 %@', '值 %lld'], literal, 'source')

    def test_localization_types_and_local_helpers_keep_literal_arguments(self):
        for before in ['String(\n localized:', 'LocalizedStringResource.init(',
                       'IntentDescription(', 'var heading: LocalizedStringKey {',
                       'DisplayRepresentation(title: "Name", subtitle: online ?',
                       '@Parameter(title:', 'title(']:
            self.assertTrue(migration.direct_resource(before, '/Shared/source.swift', 0, {'title'}), before)

    def test_language_names_and_english_doc_reference_have_english_keys(self):
        self.assertEqual(migration.key_template('中文')[0], 'Chinese')
        self.assertEqual(migration.key_template('日本語')[0], 'Japanese')
        self.assertEqual(migration.key_template('See “Outlook 接入方式”')[0], 'See “Outlook Connection”')


class MigrationTests(unittest.TestCase):
    def fixture(self, directory, source):
        root = Path(directory)
        catalog = root / 'apple/Shared/Localization/Localizable.xcstrings'
        shortcuts = root / 'apple/Persona/Intents/AppShortcuts.xcstrings'
        swift = root / 'apple/Persona/View.swift'
        for p in [catalog, shortcuts, swift]:
            p.parent.mkdir(parents=True, exist_ok=True)
        entries = {'打开': entry('Open', '打开', '開く'), '开启': entry('Open', '开启', '有効化'),
                   'Open': entry('Open', '打开英文项', '英語項目を開く'),
                   '未知': entry('Unknown', '未知', '不明'), '结果 %@': entry('Result %@', '结果 %@', '結果：%@')}
        catalog.write_text(json.dumps({'sourceLanguage': 'zh-Hans', 'strings': entries}, ensure_ascii=False))
        shortcuts.write_text(json.dumps({'sourceLanguage': 'zh-Hans', 'strings': {}}))
        (root / 'apple/project.yml').write_text('options:\n  developmentLanguage: zh-Hans\n')
        swift.write_text(source)
        return root, catalog, swift

    def test_collisions_and_nested_literals_preserve_all_existing_translations(self):
        with tempfile.TemporaryDirectory() as directory:
            root, catalog, swift = self.fixture(directory,
                'import SwiftUI\nText("结果 \\(String(localized: "未知"))")\n'
                'Text(flag ? "打开" : "开启")\nString(localized: "Open")\n'
                'var heading: LocalizedStringKey { "打开" }\n')
            original = json.loads(catalog.read_text())['strings']
            mapping = migration.migrate(root)
            migrated = json.loads(catalog.read_text())
            self.assertEqual(migrated['sourceLanguage'], 'en')
            self.assertIn('Open', migrated['strings'])
            for old in ['打开', '开启', 'Open']:
                new, _, _ = mapping[(catalog, old)]
                self.assertEqual(migrated['strings'][new]['localizations'], original[old]['localizations'])
            self.assertNotEqual(mapping[(catalog, '打开')][0], mapping[(catalog, '开启')][0])
            source = swift.read_text()
            self.assertIn('Text("Result \\(String(localized: "Unknown"))")', source)
            self.assertIn('String(localized: "Open")', source)
            self.assertIn('flag ? String(localized:', source)
            self.assertNotIn('LocalizedStringKey { String(', source)

    def test_raw_literal_fails_without_partial_writes(self):
        with tempfile.TemporaryDirectory() as directory:
            root, catalog, swift = self.fixture(directory, 'Text(#"未知"#)\n')
            before = catalog.read_bytes(), swift.read_bytes()
            with self.assertRaisesRegex(ValueError, 'Raw localized literal'):
                migration.migrate(root)
            self.assertEqual(before, (catalog.read_bytes(), swift.read_bytes()))


if __name__ == '__main__':
    unittest.main()
