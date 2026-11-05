"""The Apple apps' String Catalogs are complete in en and ja, and no Chinese UI text bypasses them (#2457).

Runs `scripts/i18n/apple_l10n.py check` on the repository, and checks the helpers it relies on: the Swift literal
lexer and the normalization that matches a Swift literal to a catalog key. See docs/ios.md「界面语言」.
"""
from pathlib import Path
import subprocess
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts" / "i18n"))

import apple_l10n  # noqa: E402
import swift_literals  # noqa: E402


class CatalogsTest(unittest.TestCase):
    def test_repository_is_complete(self):
        r = subprocess.run([sys.executable, str(ROOT / "scripts/i18n/apple_l10n.py"), "check"],
                           capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stdout[-4000:] + r.stderr[-2000:])


class LiteralsTest(unittest.TestCase):
    def test_interpolations_with_strings_inside(self):
        src = 'let a = "共 \\(n) 个，\\(names.joined(separator: "、"))" // 注释里的 "中文" 不算\n/* "块注释" */ let b = #"原样 \\(x)"#'
        texts = [l.text for l in swift_literals.literals(src)]
        self.assertIn('共 \\(n) 个，\\(names.joined(separator: "、"))', texts)
        self.assertIn("、", texts)  # the one inside the interpolation, found on its own
        self.assertIn("原样 \\(x)", texts)
        self.assertNotIn("中文", texts)
        self.assertNotIn("块注释", texts)

    def test_only_the_literals_own_text_counts(self):
        lit = swift_literals.literals('let a = "\\(name ?? "未知")"')[0]
        self.assertFalse(lit.has_cjk)


class NormalizeTest(unittest.TestCase):
    def test_a_literal_matches_its_catalog_key(self):
        n = apple_l10n.normalize
        self.assertEqual(n('共 \\(items.count) 个', literal=True), n("共 %lld 个"))
        self.assertEqual(n('\\(a)：\\(b(c(d)))', literal=True), n("%1$@：%2$@"))
        self.assertEqual(n('正在下载 \\(p)%', literal=True), n("正在下载 %lld%%"))
        self.assertEqual(n('新建待办 \\(\\.$todoTitle)', literal=True), n("新建待办 ${todoTitle}"))

    def test_multi_line_literals_lose_their_indentation(self):
        self.assertEqual(apple_l10n.normalize("\n    第一行\n    第二行\n    ", literal=True), "第一行\n第二行")

    def test_ignore_markers(self):
        src = 'a("中") // i18n-ignore\n// i18n-ignore\nb("文")\nc("字")\n'
        self.assertEqual(apple_l10n.ignored_lines(src), {1, 3})
        self.assertIsNone(apple_l10n.ignored_lines("// i18n-ignore-file: demo data\nlet x = \"中\""))

    def test_placeholders(self):
        self.assertEqual(apple_l10n.specs("%1$@ 有 %2$lld 个 %%"), {1: "@", 2: "lld"})


if __name__ == "__main__":
    unittest.main()
