package persona.core.designsystem

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.junit.Assert.assertEquals
import org.junit.Test

/** Rich message text moved its literals into tokens (#4228); the values must stay what the inbox drew before. */
class RichTextTokensTest {
    @Test fun keepsTheValuesTheInboxDrewBefore() {
        assertEquals(1.dp, RichTextTokens.codeBorder)
        assertEquals(12.dp, RichTextTokens.codeCopyIcon)
        assertEquals(20.dp, RichTextTokens.listMarkerMinWidth)
        assertEquals(3.dp, RichTextTokens.quoteBar)
        assertEquals(Color(0xFF2E7D32), RichTextTokens.Syntax.string)
        assertEquals(Color(0xFFE65100), RichTextTokens.Syntax.number)
    }

    @Test fun codeTextIsThirteenSpInEveryStyle() {
        for (style in AppStyle.entries) assertEquals(13.sp, Types.of(style).code.fontSize)
    }
}
