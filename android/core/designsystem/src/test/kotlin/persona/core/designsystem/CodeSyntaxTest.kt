package persona.core.designsystem

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.designsystem.CodeSyntax.Kind

class CodeSyntaxTest {
    private fun runs(code: String, language: String) = CodeSyntax.spans(code, language).map { code.substring(it.start, it.end) to it.kind }

    @Test fun colorsKeywordsStringsNumbersAndComments() {
        assertEquals(listOf("val" to Kind.Keyword, "42" to Kind.Number, "// the answer" to Kind.Comment),
            runs("val n = 42 // the answer", "kotlin"))
        assertEquals(listOf("return" to Kind.Keyword, "\"a \\\" b\"" to Kind.String), runs("return \"a \\\" b\"", "ts"))
        assertEquals(listOf("/* x\ny */" to Kind.Comment, "fun" to Kind.Keyword), runs("/* x\ny */ fun f()", "kotlin"))
    }

    @Test fun hashIsACommentOnlyWhereTheLanguageSaysSo() {
        assertEquals(listOf("# list" to Kind.Comment), runs("# list\nls -la", "bash"))
        assertTrue(runs("#include <stdio.h>", "c").none { it.second == Kind.Comment })
        assertEquals(listOf("select" to Kind.Keyword, "-- all" to Kind.Comment), runs("select -- all", "sql"))
    }

    @Test fun slashesAreACommentOnlyInTheCFamily() {
        assertTrue(runs("curl https://example.com/x", "bash").none { it.second == Kind.Comment })
        assertTrue(runs("url: https://host/path", "yaml").none { it.second == Kind.Comment })
        assertTrue(runs("a // b", "python").none { it.second == Kind.Comment })
        assertTrue(runs("x /* y */", "python").none { it.second == Kind.Comment })
    }

    @Test fun plainTextFencesStayUncoloured() {
        assertEquals(emptyList<Pair<String, Kind>>(), runs("don't do it in the end", "text"))
        assertEquals(emptyList<Pair<String, Kind>>(), runs("-- not a comment", "diff"))
        assertEquals(emptyList<Pair<String, Kind>>(), runs("return 1", "plaintext"))
    }

    @Test fun anUnclosedStringStopsAtTheLineEnd() {
        assertEquals(listOf("\"open" to Kind.String, "val" to Kind.Keyword), runs("\"open\nval", "kotlin"))
    }

    @Test fun theRubberBandPastTheThreshold() {
        assertEquals(-50f, SwipeReply.offset(-50f, 100f))
        assertEquals(-125f, SwipeReply.offset(-200f, 100f)) // 100 + (100 × 0.25)
        assertTrue(SwipeReply.armed(-100f, 100f))
        assertTrue(!SwipeReply.armed(-99f, 100f))
    }
}
