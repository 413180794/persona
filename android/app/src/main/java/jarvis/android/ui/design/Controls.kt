package jarvis.android.ui.design

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.layout.Placeable
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import jarvis.android.ui.JTheme

// Controls and small pieces beyond Components.kt: a letter tile, a row of what fits, the slider, the segmented
// control, the big round control of a call, an error line, a page's first load, a group's footer.

/**
 * A name's first letter in a rounded square on its color's soft tint (iOS `KBAvatar` for people and chats), beside [IconTile] and
 * [Monogram]: the letter in the color, the corners the tile's.
 */
@Composable
fun LetterTile(letter: String, color: Color, modifier: Modifier = Modifier, size: Dp = JSize.wellL) {
    val style = when {
        size >= JSize.avatarXL -> JTheme.type.title
        size >= JSize.wellL -> JTheme.type.secondaryStrong
        else -> JTheme.type.labelStrong
    }
    Box(
        modifier.size(size).clip(RoundedCornerShape(size * 0.28f)).background(color.copy(alpha = 0.12f)),
        contentAlignment = Alignment.Center,
    ) { Text(letter, style = style, color = color, maxLines = 1) }
}

/**
 * A row that keeps only the children that fit whole, in order, and drops the rest (iOS's `ViewThatFits` over fewer and
 * fewer tags): a cut 「#Manage…」 says nothing. Children are centered on the row's line.
 */
@Composable
fun FittingRow(modifier: Modifier = Modifier, spacing: Dp = JSpace.sm, content: @Composable () -> Unit) {
    Layout(content, modifier) { measurables, constraints ->
        val gap = spacing.roundToPx()
        val free = Constraints(maxHeight = constraints.maxHeight)
        val kept = ArrayList<Placeable>()
        var width = 0
        for (m in measurables) {
            val p = m.measure(free)
            val next = width + (if (kept.isEmpty()) 0 else gap) + p.width
            if (constraints.hasBoundedWidth && next > constraints.maxWidth) break
            kept += p
            width = next
        }
        val height = kept.maxOfOrNull { it.height } ?: 0
        layout(width.coerceAtLeast(constraints.minWidth), height) {
            var x = 0
            kept.forEach { p ->
                p.placeRelative(x, (height - p.height) / 2)
                x += p.width + gap
            }
        }
    }
}

/**
 * A slider in iOS's look: a thin capsule track, the accent up to the value, a round white thumb on
 * a soft shadow. Material's slider underneath keeps the dragging, the keys and what TalkBack says.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun JSlider(
    value: Float, onValueChange: (Float) -> Unit, modifier: Modifier = Modifier,
    range: ClosedFloatingPointRange<Float> = 0f..1f, enabled: Boolean = true,
) {
    val c = JTheme.colors
    Slider(
        value = value, onValueChange = onValueChange, modifier = modifier, enabled = enabled, valueRange = range,
        thumb = {
            Box(Modifier.size(JSize.iconL).shadow(JSize.bar, CircleShape, clip = false).background(Color.White, CircleShape))
        },
        track = { state ->
            val span = state.valueRange.endInclusive - state.valueRange.start
            val fraction = if (span > 0f) ((state.value - state.valueRange.start) / span).coerceIn(0f, 1f) else 0f
            Box(Modifier.fillMaxWidth().height(JSize.barWide).clip(CircleShape).background(c.fillPressed)) {
                Box(Modifier.fillMaxWidth(fraction).fillMaxHeight().background(if (enabled) c.accent else c.tertiaryText))
            }
        },
    )
}

/**
 * iOS's segmented picker (文件 · Git, 未暂存 · 已暂存): a capsule track on the gray fill, the chosen segment raised on
 * it in the card's color (a lighter gray in dark). Every title on one line.
 */
@Composable
fun SegmentedControl(titles: List<String>, selected: Int, onSelect: (Int) -> Unit, modifier: Modifier = Modifier) {
    val c = JTheme.colors
    Row(
        modifier.fillMaxWidth().height(JSize.wellL).clip(CircleShape).background(c.fillChip).padding(JSpace.xxs),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        titles.forEachIndexed { i, t ->
            val on = i == selected
            Box(
                Modifier.weight(1f).fillMaxHeight()
                    .then(if (on) Modifier.surface(CircleShape, fill = if (c.dark) c.fillPressed else c.card) else Modifier.clip(CircleShape))
                    .clickable(role = Role.Tab) { if (!on) onSelect(i) }
                    .semantics { this.selected = on },
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    t, style = if (on) JTheme.type.secondaryStrong else JTheme.type.secondaryMedium,
                    color = if (on) c.label else c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(horizontal = JSpace.s),
                )
            }
        }
    }
}

/**
 * A big round control with its word under it (a call's 静音 / 打断 / 挂断): [fill] behind the symbol in [tint]; the
 * word in the caption's size, one line.
 */
@Composable
fun RoundControl(
    icon: ImageVector, label: String, onClick: () -> Unit, modifier: Modifier = Modifier,
    fill: Color = JTheme.colors.fillChip, tint: Color = JTheme.colors.label, enabled: Boolean = true, size: Dp = JSize.box64,
) {
    Column(
        modifier.widthIn(min = size).alpha(if (enabled) 1f else 0.4f),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        Box(
            Modifier.size(size).clip(CircleShape).background(fill)
                .clickable(enabled = enabled, role = Role.Button, onClick = onClick).semantics { contentDescription = label },
            contentAlignment = Alignment.Center,
        ) { Icon(icon, null, Modifier.size(size * 0.42f), tint = tint) }
        Text(label, style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1)
    }
}

/** Something that failed, said in a line (a form, a list): the error triangle and the words in the danger color. */
@Composable
fun ErrorLine(text: String, modifier: Modifier = Modifier, centered: Boolean = false) {
    Row(
        modifier,
        horizontalArrangement = Arrangement.spacedBy(JSpace.sm, if (centered) Alignment.CenterHorizontally else Alignment.Start),
        verticalAlignment = Alignment.Top,
    ) {
        Icon(JIcons.error, null, Modifier.padding(top = JSpace.xxs).size(JSize.markS), tint = JTheme.colors.danger)
        Text(text, style = JTheme.type.caption, color = JTheme.colors.danger, textAlign = if (centered) TextAlign.Center else TextAlign.Start)
    }
}

/** A page's first load: a small spinner in the secondary color, centered, below the header. */
@Composable
fun PageLoading(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth().padding(top = JSpace.xxl + JSpace.s), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(Modifier.size(JSize.icon), strokeWidth = JSize.rule, color = JTheme.colors.secondaryText)
    }
}

/** The words under a group card (KitGroup's footer): caption, secondary, inset. */
@Composable
fun GroupFooter(text: String, modifier: Modifier = Modifier, color: Color = JTheme.colors.secondaryText) {
    Text(
        text, style = JTheme.type.caption, color = color,
        modifier = modifier.fillMaxWidth().padding(start = JSpace.page + JSpace.l, end = JSpace.page + JSpace.l, top = JSpace.sm),
    )
}
