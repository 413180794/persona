package jarvis.android.ui.design

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import jarvis.android.data.AgentState
import jarvis.android.ui.JTheme
import jarvis.android.ui.label

// The app's small components (apple/Jarvis/DesignSystem/Components.swift): one way to draw each thing.

/**
 * An agent state's dot (docs/agent-states.md): 需要你 breathes (the one dot that moves; still when animations are off),
 * 工作中 has a still ring around it, 完成未看 is solid green, 已看 a muted gray.
 */
@Composable
fun StatusDot(state: AgentState, size: Dp = JSize.dotL, muted: Boolean = false) {
    val tone = JTheme.tone(if (muted) AgentState.Idle else state)
    val moving = state == AgentState.NeedsYou && !muted && animationsOn()
    val pulse = if (moving) {
        val t = rememberInfiniteTransition(label = "dot")
        t.animateFloat(0f, 1f, infiniteRepeatable(tween(1000, easing = LinearEasing), RepeatMode.Restart), label = "ring").value
    } else 0f
    Box(
        Modifier.size(size).semantics { contentDescription = state.label() }.drawBehind {
            val r = this.size.minDimension / 2
            drawCircle(tone.dot, r)
            if (state == AgentState.Working && !muted) {
                drawCircle(tone.dot.copy(alpha = 0.45f), r * 1.7f, style = Stroke(JSize.lineMedium.toPx()))
            }
            if (moving) {
                val eased = 1 - (1 - pulse) * (1 - pulse)
                drawCircle(tone.dot.copy(alpha = 0.6f * (1 - eased)), r * (1 + 1.2f * eased), style = Stroke(JSize.lineMedium.toPx()))
            }
        },
    )
}

/** False when the system's 「移除动画」 is on (animator duration scale 0): nothing loops then. */
@Composable
fun animationsOn(): Boolean {
    val ctx = LocalContext.current
    return runCatching {
        android.provider.Settings.Global.getFloat(ctx.contentResolver, android.provider.Settings.Global.ANIMATOR_DURATION_SCALE, 1f) > 0f
    }.getOrDefault(true)
}

/** The three looks of a chip (iOS `Chip.Look`). */
sealed interface ChipLook {
    /** Secondary words on a soft gray: a model, a version, a tab, 私聊. */
    data object Neutral : ChipLook
    /** A meaning's words on its soft fill: a state's tone, @我, 推荐 in the AI's color. */
    data class Tint(val color: Color, val text: Color? = null) : ChipLook
    /** Secondary words in a hairline: which agent (Claude / Codex). */
    data object Outline : ChipLook
}

/** The app's one small label (not a control): a fact beside other text. */
@Composable
fun Chip(text: String, modifier: Modifier = Modifier, icon: ImageVector? = null, look: ChipLook = ChipLook.Neutral) {
    val c = JTheme.colors
    val words = when (look) {
        ChipLook.Neutral, ChipLook.Outline -> c.secondaryText
        is ChipLook.Tint -> look.text ?: look.color
    }
    val fill = when (look) {
        ChipLook.Neutral -> c.fillChip
        is ChipLook.Tint -> if (look.text == null) look.color.copy(alpha = 0.12f) else look.color
        ChipLook.Outline -> Color.Transparent
    }
    Row(
        modifier.clip(CircleShape).background(fill)
            .then(if (look == ChipLook.Outline) Modifier.border(JSize.hairline * 2, c.secondaryText.copy(alpha = 0.4f), CircleShape) else Modifier)
            .padding(horizontal = JSpace.s, vertical = JSpace.xxs),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
    ) {
        if (icon != null) Icon(icon, null, Modifier.size(JSize.markXS), tint = words)
        Text(text, style = JTheme.type.labelMedium, color = words, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** A chip in a state's colors. */
@Composable
fun StateChip(state: AgentState, text: String = state.label()) {
    val t = JTheme.tone(state)
    Chip(text, look = ChipLook.Tint(t.tint, t.text))
}

/** A count on a row or a section header: heavy digits on a capsule, 99+ past 99. White on [color]. */
@Composable
fun CountBadge(count: Int, modifier: Modifier = Modifier, color: Color = JTheme.colors.countBadge, text: Color = Color.White) {
    Text(
        if (count > 99) "99+" else "$count",
        style = JTheme.type.labelHeavy, color = text, maxLines = 1, textAlign = TextAlign.Center,
        modifier = modifier.clip(CircleShape).background(color).defaultMinSize(minWidth = JSize.badge)
            .padding(horizontal = JSpace.sm, vertical = JSpace.xxs),
    )
}

/** A count in a state's tone (its words on its soft fill). */
@Composable
fun ToneBadge(count: Int, tone: StatusTone) = CountBadge(count, color = tone.tint, text = tone.text)

/** A centered message for an empty or failed page: a symbol, a title, a line, maybe one button. */
@Composable
fun EmptyState(icon: ImageVector, title: String, message: String, modifier: Modifier = Modifier, action: Pair<String, () -> Unit>? = null) {
    Column(
        modifier.fillMaxWidth().padding(JSpace.xxl),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Box(Modifier.size(JSize.box64).clip(CircleShape).background(JTheme.colors.accent.copy(alpha = 0.1f)), contentAlignment = Alignment.Center) {
            Icon(icon, null, Modifier.size(JSize.well), tint = JTheme.colors.accent)
        }
        Text(title, style = JTheme.type.title, color = JTheme.colors.label, textAlign = TextAlign.Center)
        Text(message, style = JTheme.type.secondary, color = JTheme.colors.secondaryText, textAlign = TextAlign.Center)
        if (action != null) PillButton(action.first, action.second, kind = ButtonKind.Prominent, modifier = Modifier.padding(top = JSpace.xs))
    }
}

/** A name's first letter on a disc (people, chats, senders without a picture). */
@Composable
fun Monogram(name: String, color: Color, size: Dp = JSize.avatar, modifier: Modifier = Modifier) {
    val letter = name.trim().firstOrNull()?.uppercase() ?: "?"
    Box(modifier.size(size).clip(CircleShape).background(color), contentAlignment = Alignment.Center) {
        Text(letter, style = if (size >= JSize.avatar) JTheme.type.bodyStrong else JTheme.type.labelStrong, color = Color.White, maxLines = 1)
    }
}

/** A symbol in a rounded square: [filled] white on the color (settings rows), else the color on its soft tint. */
@Composable
fun IconTile(icon: ImageVector, color: Color, modifier: Modifier = Modifier, size: Dp = JSize.wellS, filled: Boolean = true) {
    Box(
        modifier.size(size).clip(RoundedCornerShape(size * 0.28f)).background(if (filled) color else color.copy(alpha = 0.12f)),
        contentAlignment = Alignment.Center,
    ) { Icon(icon, null, Modifier.size(size * 0.6f), tint = if (filled) Color.White else color) }
}

/** The kinds of [PillButton]. */
enum class ButtonKind {
    /** The one thing to do: the accent, white words. */
    Prominent,
    /** Another thing to do: the accent's words on its soft fill. */
    Tinted,
    /** A quiet choice: the label on the gray fill. */
    Neutral,
    /** Something that can't be undone: red words on a soft red, or red fill when prominent. */
    Destructive,
    DestructiveProminent,
}

/**
 * A capsule button (iOS `.themedButton`): one line always (CLAUDE.md: button words never wrap). [large]: the main
 * action of a panel (50 tall, body semibold); otherwise 40 tall in the secondary size.
 */
@Composable
fun PillButton(
    text: String, onClick: () -> Unit, modifier: Modifier = Modifier, kind: ButtonKind = ButtonKind.Tinted,
    icon: ImageVector? = null, enabled: Boolean = true, large: Boolean = false,
) {
    val c = JTheme.colors
    val (fill, words) = when (kind) {
        ButtonKind.Prominent -> c.accent to Color.White
        ButtonKind.Tinted -> c.accent.copy(alpha = if (c.dark) 0.2f else 0.12f) to c.accent
        ButtonKind.Neutral -> c.fillChip to c.label
        ButtonKind.Destructive -> c.danger.copy(alpha = if (c.dark) 0.2f else 0.12f) to c.danger
        ButtonKind.DestructiveProminent -> c.danger to Color.White
    }
    Row(
        modifier.heightIn(min = if (large) JSize.box50 else JSize.avatar).clip(CircleShape).background(fill)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick).alpha(if (enabled) 1f else 0.45f)
            .padding(horizontal = if (large) JSpace.xl else JSpace.l),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.sm, Alignment.CenterHorizontally),
    ) {
        if (icon != null) Icon(icon, null, Modifier.size(if (large) JSize.markL else JSize.mark), tint = words)
        Text(text, style = if (large) JTheme.type.bodyStrong else JTheme.type.secondaryStrong, color = words, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/**
 * A round floating button (the shell's ☰ and 新对话, a page's back, ⋯): 44 across on the style's floating surface.
 * [dot] marks something waiting behind it.
 */
@Composable
fun CircleButton(
    icon: ImageVector, label: String, onClick: () -> Unit, modifier: Modifier = Modifier,
    enabled: Boolean = true, dot: Boolean = false, tint: Color = JTheme.colors.label, size: Dp = JSize.tapTarget,
) {
    Box(modifier.size(size)) {
        Box(
            Modifier.size(size).floatingCircle().clickable(enabled = enabled, role = Role.Button, onClick = onClick)
                .semantics { contentDescription = label }.alpha(if (enabled) 1f else 0.4f),
            contentAlignment = Alignment.Center,
        ) { Icon(icon, null, Modifier.size(size * 0.5f), tint = tint) }
        if (dot) {
            Box(
                Modifier.align(Alignment.TopEnd).offset(x = -JSpace.xxs, y = JSpace.xxs).size(JSize.dotXL).clip(CircleShape)
                    .background(JTheme.colors.page).padding(JSize.lineMedium).clip(CircleShape).background(JTheme.needsYou.dot),
            )
        }
    }
}

/** A plain icon button without a surface (in a row, a toolbar): 44 to hit, the symbol in [tint]. */
@Composable
fun IconAction(icon: ImageVector, label: String, onClick: () -> Unit, modifier: Modifier = Modifier, tint: Color = JTheme.colors.label, enabled: Boolean = true, size: Dp = JSize.iconS) {
    Box(
        modifier.size(JSize.tapTarget).clip(CircleShape).clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .semantics { contentDescription = label }.alpha(if (enabled) 1f else 0.4f),
        contentAlignment = Alignment.Center,
    ) { Icon(icon, null, Modifier.size(size), tint = tint) }
}

/**
 * A choice among several (a filter, a type): a capsule, the chosen one in the accent's tint with its words and rim.
 * [lead] goes before the words (a state's dot, a symbol); [count] after them.
 */
@Composable
fun ChoiceChip(
    text: String, selected: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier,
    lead: (@Composable () -> Unit)? = null, count: Int? = null,
) {
    val c = JTheme.colors
    val shape = CircleShape
    Row(
        modifier.height(JSize.wellL)
            .then(if (selected) Modifier.clip(shape).background(c.accent.copy(alpha = if (c.dark) 0.22f else 0.12f)).border(JSize.hairline * 2, c.accent.copy(alpha = 0.55f), shape) else Modifier.surface(shape, fill = c.fillChip))
            .clickable(role = Role.Tab, onClick = onClick).padding(horizontal = JSpace.m),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
    ) {
        lead?.invoke()
        Text(text, style = if (selected) JTheme.type.secondaryStrong else JTheme.type.secondaryMedium, color = if (selected) c.accent else c.label, maxLines = 1)
        if (count != null && count > 0) Text("$count", style = JTheme.type.secondary, color = if (selected) c.accent else c.secondaryText, maxLines = 1)
    }
}

/** A section's header in a list: caption semibold in the secondary color, a count, maybe something at the end. */
@Composable
fun SectionHeader(
    text: String, modifier: Modifier = Modifier, count: Int? = null, countColor: Color? = null,
    folded: Boolean? = null, onClick: (() -> Unit)? = null, trailing: @Composable RowScope.() -> Unit = {},
) {
    Row(
        modifier.fillMaxWidth().then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(start = JSpace.page + JSpace.xs, end = JSpace.page, top = JSpace.l, bottom = JSpace.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
    ) {
        Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            Text(text, style = JTheme.type.sectionHeader, color = JTheme.colors.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
            if (count != null) {
                if (countColor != null) CountBadge(count, color = countColor.copy(alpha = 0.14f), text = countColor)
                else Text("$count", style = JTheme.type.sectionHeader, color = JTheme.colors.secondaryText, maxLines = 1)
            }
            if (folded != null) Icon(if (folded) JIcons.disclosure else JIcons.expand, null, Modifier.size(JSize.markM), tint = JTheme.colors.tertiaryText)
        }
        trailing()
    }
}

/** The hairline between rows of a group, from [start] (the words' column) to the edge. */
@Composable
fun RowDivider(start: Dp = JSpace.l) {
    HorizontalDivider(Modifier.padding(start = start), thickness = JSize.hairline, color = JTheme.colors.separator)
}

/** A row in a group card: [lead] (an icon tile, a dot), the title and an optional subtitle, [trailing] (a value, ›). */
@Composable
fun GroupRow(
    title: String, modifier: Modifier = Modifier, subtitle: String? = null, onClick: (() -> Unit)? = null,
    lead: (@Composable () -> Unit)? = null, value: String? = null, chevron: Boolean = onClick != null,
    titleColor: Color = JTheme.colors.label, trailing: (@Composable () -> Unit)? = null,
) {
    Row(
        modifier.fillMaxWidth().heightIn(min = JSize.box50).then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        lead?.invoke()
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(title, style = JTheme.type.body, color = titleColor, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (!subtitle.isNullOrBlank()) Text(subtitle, style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
        if (value != null) Text(value, style = JTheme.type.body, color = JTheme.colors.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = IntrinsicWidthCap))
        trailing?.invoke()
        if (chevron) Icon(JIcons.disclosure, null, Modifier.size(JSize.icon), tint = JTheme.colors.tertiaryText)
    }
}

/** A row's value never takes more than this from its title. */
private val IntrinsicWidthCap = JSize.box140

/** Between two pieces of a meta line: 「需要你 · Claude · tests」. */
const val DOT_SEP = " · "
