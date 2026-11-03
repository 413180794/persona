package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import persona.core.designsystem.icons.PhIcons

/**
 * Messages typed while the agent works and not taken yet, right above the composer (iOS QueueStrip): [title] says how
 * many and that they go in order, then each one numbered, at most two lines. Tapping one hands its index to [onItem]
 * (撤回编辑 / 拷贝 / 删除). While an edit runs ([busy]) a spinner shows and nothing can be tapped; [enabled] false shows
 * the queue without actions. A long queue scrolls under the title. Glass floats like the composer, brief is a paper
 * card, the board a bordered ticket.
 */
@Composable
fun QueueStrip(
    title: String,
    items: List<String>,
    onItem: (Int) -> Unit,
    modifier: Modifier = Modifier,
    busy: Boolean = false,
    enabled: Boolean = true,
) {
    if (items.isEmpty()) return
    val palette = PersonaTheme.palette
    val style = PersonaTheme.style
    val shape = RoundedCornerShape(when (style) { AppStyle.Glass -> 24.dp; AppStyle.Brief -> 20.dp; AppStyle.Board -> 8.dp })
    val outer = when (style) {
        AppStyle.Glass -> Modifier.padding(horizontal = 10.dp, vertical = 4.dp).floatingSurface(shape)
        AppStyle.Brief -> Modifier.padding(horizontal = 12.dp, vertical = 4.dp).clip(shape).background(palette.surface)
        AppStyle.Board -> Modifier.padding(horizontal = 10.dp, vertical = 4.dp).clip(shape).background(palette.surface).border(1.dp, palette.line, shape)
    }
    val active = enabled && !busy
    Column(modifier.fillMaxWidth().then(outer).padding(top = 10.dp, bottom = 4.dp)) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 14.dp), verticalAlignment = Alignment.CenterVertically) {
            JIcon(PhIcons.Tray, null, size = 16.dp, tint = palette.ink2)
            Spacer(Modifier.width(8.dp))
            Text(title, style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (busy) JSpinner(Modifier.size(14.dp), stroke = 1.8.dp)
        }
        Column(Modifier.fillMaxWidth().heightIn(max = 184.dp).verticalScroll(rememberScrollState()).padding(top = 4.dp)) {
            items.forEachIndexed { index, text ->
                if (index > 0) JDivider(Modifier.padding(start = 50.dp, end = 14.dp))
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 44.dp)
                        .clickable(enabled = active, role = Role.Button, onClickLabel = "排队消息的操作") { onItem(index) }
                        .semantics(mergeDescendants = true) { contentDescription = "第 ${index + 1} 条：$text" }
                        .padding(horizontal = 14.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.Top,
                ) {
                    QueueNumber(index + 1)
                    Spacer(Modifier.width(12.dp))
                    Text(text, style = PersonaTheme.type.callout, color = palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f).padding(top = 1.dp))
                    if (enabled) {
                        Spacer(Modifier.width(8.dp))
                        JIcon(PhIcons.DotsThree, null, size = 18.dp, tint = palette.ink3, modifier = Modifier.padding(top = 2.dp))
                    }
                }
            }
        }
    }
}

/** The place of a queued message: an accent disc in glass, ink on a grey disc in brief, a small square on the board. */
@Composable
private fun QueueNumber(number: Int) {
    val palette = PersonaTheme.palette
    val (fill, ink) = when (PersonaTheme.style) {
        AppStyle.Glass -> palette.accentSoft to palette.accent
        AppStyle.Brief -> palette.surfaceStrong to palette.ink
        AppStyle.Board -> palette.accentSoft to palette.accent
    }
    Box(
        Modifier.size(24.dp).clip(if (PersonaTheme.style == AppStyle.Board) RoundedCornerShape(4.dp) else CircleShape).background(fill)
            .then(if (PersonaTheme.style == AppStyle.Glass) Modifier.border(1.dp, Brush.verticalGradient(listOf(palette.strokeTop, palette.strokeBottom)), CircleShape) else Modifier),
        contentAlignment = Alignment.Center,
    ) { Text("$number", style = PersonaTheme.type.micro.copy(fontWeight = FontWeight.Bold), color = ink, maxLines = 1) }
}
