package persona.core.designsystem

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

/** Geometry and colours of rich message text (code blocks, list markers, quotes) that the shared scale has no step for. */
object RichTextTokens {
    /** The outline around a code block. */
    val codeBorder = 1.dp
    /** The copy icon in a code block's header. */
    val codeCopyIcon = 12.dp
    /** The narrowest a list's number or bullet column gets, so items line up. */
    val listMarkerMinWidth = 20.dp
    /** The bar down the leading edge of a quote. */
    val quoteBar = 3.dp

    /**
     * The Lark code block's string and number colours, kept fixed (not light / dark or theme aware) so moving them out
     * of the page changed no pixels (#4228). [CodeSyntax.color] already maps these kinds to palette roles; moving the
     * Lark block onto it is a visible change and needs its own issue.
     */
    object Syntax {
        val string = Color(0xFF2E7D32)
        val number = Color(0xFFE65100)
    }
}
