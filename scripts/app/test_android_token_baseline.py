import unittest
from android_token_baseline import count, regressions


class TokenBaselineTest(unittest.TestCase):
    def test_ignores_comments_strings_and_resolves_aliases(self):
        counts = count('''
            import jarvis.core.designsystem.Tokens.Space as Spacing
            import androidx.compose.ui.graphics.Color as Tint
            // Color.White 19.sp 99.dp
            /* 88.dp /* Color.Red */ 55.sp */
            val text = "Color.Black 12.sp 21.dp"
            val raw = """99.dp 22.sp"""
            val a = Tint(0xFF123456)
            val b = 14.sp
            val c = 8.dp
            val d = Spacing.m
            val e = JarvisTypography.body
            val f = MaterialTheme.colorScheme.primary
        ''')
        self.assertEqual({k: {'hardcoded': 1, 'token': 1} for k in ('color', 'font', 'dp')}, counts)

    def test_new_file_or_category_cannot_spend_another_files_reduction(self):
        baseline = {'files': {'a.kt': count('1.dp 2.dp')}}
        current = {'files': {'a.kt': count('1.dp'), 'b.kt': count('2.dp')}}
        self.assertEqual(1, len(regressions(current, baseline)))
        current = {'files': {'a.kt': count('Color.Red')}}
        self.assertEqual(1, len(regressions(current, baseline)))
        self.assertEqual([], regressions({'files': {'a.kt': count('Tokens.Space.m')}}, baseline))


if __name__ == '__main__':
    unittest.main()
