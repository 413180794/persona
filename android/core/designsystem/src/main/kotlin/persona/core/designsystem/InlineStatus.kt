package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import persona.core.designsystem.icons.PhIcons

/** What an [JInlineStatus] says: something under way, something to look at, or something that went wrong. */
enum class InlineTone { Busy, Caution, Failed }

/**
 * A short status set inside a card or above the composer, up to three lines: a spinner while [tone] is busy, a mark
 * otherwise, on a soft tint (the chat's 正在启动, a machine's 重新部署 line). [action] sits at the end, for one small
 * button that acts on it.
 */
@Composable
fun JInlineStatus(
    text: String,
    modifier: Modifier = Modifier,
    tone: InlineTone = InlineTone.Busy,
    action: (@Composable () -> Unit)? = null,
) {
    val palette = PersonaTheme.palette
    val (fill, ink) = when (tone) {
        InlineTone.Busy -> palette.surfaceStrong to palette.ink2
        InlineTone.Caution -> palette.needsYou.tint to palette.cautionText
        InlineTone.Failed -> palette.danger.tint to palette.danger.text
    }
    Row(
        modifier.fillMaxWidth().semantics(mergeDescendants = true) {}.clip(PersonaTheme.shapes.controlShape).background(fill)
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        when (tone) {
            InlineTone.Busy -> JSpinner(Modifier.size(14.dp), color = ink, stroke = 1.8.dp)
            InlineTone.Caution -> JIcon(PhIcons.Warning, null, size = 16.dp, tint = palette.caution)
            InlineTone.Failed -> JIcon(PhIcons.WarningCircle, null, size = 16.dp, tint = palette.danger.dot)
        }
        Text(text, Modifier.weight(1f), style = PersonaTheme.type.caption, color = ink, maxLines = 3, overflow = TextOverflow.Ellipsis)
        action?.invoke()
    }
}
