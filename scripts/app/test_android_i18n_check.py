import tempfile
import unittest
from pathlib import Path

from android_i18n_check import count, literals, regressions, resource_problems


def write(root, path, text):
    file = root / path
    file.parent.mkdir(parents=True, exist_ok=True)
    file.write_text(text, encoding='utf-8')


def strings(*items):
    return '<resources>\n' + '\n'.join(items) + '\n</resources>\n'


class I18nCheckTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.root = Path(self.dir.name)
        write(self.root, 'feature/demo/build.gradle.kts', '')
        self.module = self.root / 'feature/demo'

    def tearDown(self):
        self.dir.cleanup()

    def resources(self, en, zh, ja):
        write(self.module, 'src/main/res/values/strings.xml', strings(*en))
        write(self.module, 'src/main/res/values-zh/strings.xml', strings(*zh))
        write(self.module, 'src/main/res/values-ja/strings.xml', strings(*ja))
        return resource_problems(self.module, self.root)

    def test_matching_resources_pass(self):
        self.assertEqual([], self.resources(
            ['<string name="demo_hi">Hi %1$s</string>', '<string name="demo_name" translatable="false">日本語</string>',
             '<plurals name="demo_items"><item quantity="one">%d item</item><item quantity="other">%d items</item></plurals>'],
            ['<string name="demo_hi">你好 %1$s</string>', '<plurals name="demo_items"><item quantity="other">%d 项</item></plurals>'],
            ['<string name="demo_hi">%1$s さん</string>', '<plurals name="demo_items"><item quantity="other">%d 件</item></plurals>'],
        ))

    def test_missing_keys_placeholders_plurals_and_prefix_are_reported(self):
        problems = self.resources(
            ['<string name="demo_hi">Hi %1$s</string>', '<string name="other_key">Bye</string>',
             '<plurals name="demo_items"><item quantity="other">%d items</item></plurals>'],
            ['<string name="demo_hi">你好</string>', '<plurals name="demo_items"><item quantity="other">%d 项</item></plurals>'],
            ['<string name="demo_hi">%1$s さん</string>', '<string name="demo_extra">余分</string>',
             '<plurals name="demo_items"><item quantity="other">%d 件</item></plurals>'],
        )
        text = '\n'.join(problems)
        self.assertIn('"other_key" should start with "demo_"', text)
        self.assertIn('"other_key" is missing from values-zh/', text)
        self.assertIn('"demo_hi" has placeholders', text)
        self.assertIn('"demo_extra" in values-ja/ is not in values/', text)
        self.assertIn('plurals "demo_items" in values/ needs', text)

    def test_the_english_fallback_has_no_chinese(self):
        problems = self.resources(['<string name="demo_hi">你好</string>'], ['<string name="demo_hi">你好</string>'],
                                  ['<string name="demo_hi">こんにちは</string>'])
        self.assertIn('values/ is the English fallback', '\n'.join(problems))

    def test_counts_code_literals_not_comments_or_ignored_lines(self):
        write(self.module, 'src/main/kotlin/A.kt', '\n'.join([
            '// 注释里的中文不算',
            '/* 块注释 /* 嵌套 */ 也不算 */',
            'val a = "需要你"',
            'val b = "ok"; val c = "完成"',
            'val d = """多行',
            '文本"""',
            'val e = "需要你" // i18n-ignore: matches the hub\'s text',
            "val f = '中'",
            'val g = "日本語のテキスト"',
        ]))
        write(self.module, 'src/test/kotlin/T.kt', 'val t = "测试里的不算"')
        write(self.module, 'src/main/res/values-zh/strings.xml', strings('<string name="demo_x">中文</string>'))
        self.assertEqual({'feature/demo/src/main/kotlin/A.kt': 4}, count(self.root))

    def test_counts_may_only_go_down(self):
        self.assertEqual([], regressions({'a.kt': 2}, {'a.kt': 3}))
        self.assertEqual(1, len(regressions({'a.kt': 4}, {'a.kt': 3})))
        self.assertEqual(1, len(regressions({'new.kt': 1}, {})))

    def test_escaped_quotes_stay_inside_the_literal(self):
        self.assertEqual([(1, r'"a \" 中"')], literals(r'val x = "a \" 中"'))


if __name__ == '__main__':
    unittest.main()
