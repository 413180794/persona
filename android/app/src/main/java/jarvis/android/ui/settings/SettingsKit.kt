package jarvis.android.ui.settings

import android.content.Context
import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.graphics.takeOrElse
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.HubException
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChipLook
import jarvis.android.ui.design.EmptyState
import jarvis.android.ui.design.IconTile
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.PageLoading
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.design.surface
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.navigationBars

// The pieces the 设置 sub-pages and 机器 share: rounded groups of rows on the page, like iOS's inset grouped lists.
// Everything comes from the design system (ui/design, JTheme): the style's cards, the type scale, the spacing.

/** The roles these pages color with (the current style's tokens). */
@Immutable
data class KitColors(
    val dark: Boolean,
    val page: Color,
    val card: Color,
    val label: Color,
    val secondaryLabel: Color,
    val tertiaryLabel: Color,
    val separator: Color,
    val fillChip: Color,
    val accent: Color,
    val danger: Color,
    val positive: Color,
    val caution: Color,
)

val kitColors: KitColors
    @Composable @ReadOnlyComposable
    get() {
        val p = JTheme.colors
        return KitColors(
            dark = p.dark, page = p.page, card = p.card, label = p.label, secondaryLabel = p.secondaryText,
            tertiaryLabel = p.tertiaryText, separator = p.separator, fillChip = p.fillChip, accent = p.accent,
            danger = p.danger, positive = p.positive, caution = p.cautionText,
        )
    }

/** Corner of groups and cards: the style's. */
val KitCorner: Dp @Composable @ReadOnlyComposable get() = JTheme.style.corner

/** A card on the page: the grouped rows, the machine cards, the charts. */
@Composable
fun Modifier.kitCard(padding: Dp = 0.dp, shape: Shape = RoundedCornerShape(KitCorner)): Modifier =
    surface(shape).padding(padding)

/** A sub-page: the round back button and the title, then a lazy column of [KitGroup]s. */
@Composable
fun KitPage(
    title: String,
    /** Null: no way back (a step that must be done, like choosing an engine right after pairing). */
    onBack: (() -> Unit)?,
    actions: @Composable RowScope.() -> Unit = {},
    content: LazyListScope.() -> Unit,
) {
    Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
        PageHeader(title, onBack, actions = actions)
        LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = PaddingValues(top = JSpace.xs, bottom = JSpace.xxl + WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()),
            content = content,
        )
    }
}

/** Rows of one [KitGroup]: each [row] gets a hairline above it except the first. */
class KitGroupScope {
    internal val rows = mutableListOf<@Composable () -> Unit>()
    fun row(content: @Composable () -> Unit) { rows += content }
}

/** One group: optional [header] / [footer] outside the card, rows inside with separators. */
@Composable
fun KitGroup(
    modifier: Modifier = Modifier,
    header: String? = null,
    footer: String? = null,
    separatorStart: Dp = JSpace.l,
    content: KitGroupScope.() -> Unit,
) {
    val c = kitColors
    val scope = KitGroupScope().apply(content)
    Column(modifier.fillMaxWidth().padding(top = if (header != null) JSpace.s else JSpace.l)) {
        if (header != null) SectionHeader(header)
        Column(Modifier.padding(horizontal = JSpace.page).fillMaxWidth().kitCard()) {
            scope.rows.forEachIndexed { i, r ->
                if (i > 0) RowDivider(separatorStart)
                r()
            }
        }
        if (footer != null) {
            Text(footer, style = JTheme.type.caption, color = c.secondaryLabel,
                modifier = Modifier.padding(start = JSpace.page + JSpace.l, end = JSpace.page + JSpace.l, top = JSpace.sm))
        }
    }
}

/** A navigation row: icon, title, an optional value on the right and a chevron when it opens something. */
@Composable
fun KitRow(
    title: String,
    onClick: (() -> Unit)?,
    icon: ImageVector? = null,
    value: String? = null,
    titleColor: Color = Color.Unspecified,
    subtitle: String? = null,
    iconColor: Color = Color.Unspecified,
) {
    val c = kitColors
    Row(
        Modifier.fillMaxWidth().let { if (onClick != null) it.clickable(onClick = onClick) else it }
            .heightIn(min = JSize.box50).padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        if (icon != null) IconTile(icon, iconColor.takeOrElse { c.accent })
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(title, style = JTheme.type.body, color = titleColor.takeOrElse { c.label }, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!subtitle.isNullOrEmpty()) Text(subtitle, style = JTheme.type.caption, color = c.secondaryLabel)
        }
        if (!value.isNullOrEmpty()) {
            Text(value, style = JTheme.type.body, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.widthIn(max = JSize.box200), textAlign = TextAlign.End)
        }
        if (onClick != null) Icon(JIcons.disclosure, null, tint = c.tertiaryLabel, modifier = Modifier.size(JSize.iconXS))
    }
}

/** A switch row; the whole row toggles. */
@Composable
fun KitSwitchRow(
    title: String,
    checked: Boolean,
    onChange: (Boolean) -> Unit,
    subtitle: String? = null,
    icon: ImageVector? = null,
    iconTint: Color = Color.Unspecified,
    enabled: Boolean = true,
) {
    val c = kitColors
    Row(
        Modifier.fillMaxWidth()
            .clickable(enabled = enabled, role = Role.Switch) { onChange(!checked) }
            .heightIn(min = JSize.box50)
            .padding(horizontal = JSpace.l, vertical = JSpace.s)
            .alpha(if (enabled) 1f else 0.45f),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        if (icon != null) IconTile(icon, iconTint.takeOrElse { c.accent })
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(title, style = JTheme.type.body, color = c.label)
            if (!subtitle.isNullOrEmpty()) Text(subtitle, style = JTheme.type.caption, color = c.secondaryLabel)
        }
        Switch(checked = checked, onCheckedChange = null, enabled = enabled, colors = jSwitchColors())
    }
}

/** The switches' colors: the accent when on, a soft gray track when off (iOS's). */
@Composable
fun jSwitchColors() = androidx.compose.material3.SwitchDefaults.colors(
    checkedThumbColor = Color.White, checkedTrackColor = JTheme.colors.positive, checkedBorderColor = Color.Transparent,
    uncheckedThumbColor = Color.White, uncheckedTrackColor = JTheme.colors.fillPressed, uncheckedBorderColor = Color.Transparent,
)

/** One option of an inline picker: a check mark when [selected]. */
@Composable
fun KitChoiceRow(title: String, selected: Boolean, onClick: () -> Unit, detail: String? = null) {
    val c = kitColors
    Row(
        Modifier.fillMaxWidth()
            .semantics { this.selected = selected; role = Role.RadioButton }
            .clickable(onClick = onClick)
            .heightIn(min = JSize.box50)
            .padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(title, style = JTheme.type.body, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!detail.isNullOrEmpty()) Text(detail, style = JTheme.type.caption, color = c.secondaryLabel, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (selected) Icon(JIcons.done, "已选", tint = c.accent, modifier = Modifier.size(JSize.iconXS))
    }
}

/** A warning inside a group: a symbol, a bold title and a line of explanation. */
@Composable
fun KitNoticeRow(title: String, message: String? = null, tint: Color = Color.Unspecified, icon: ImageVector = JIcons.error) {
    val c = kitColors
    Row(
        Modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m),
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Icon(icon, null, tint = tint.takeOrElse { c.caution }, modifier = Modifier.size(JSize.iconS))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(title, style = JTheme.type.heading, color = c.label)
            if (!message.isNullOrEmpty()) Text(message, style = JTheme.type.caption, color = c.secondaryLabel)
        }
    }
}

/** A button-like row: accent text, or red when [destructive]. */
@Composable
fun KitActionRow(
    title: String,
    onClick: () -> Unit,
    icon: ImageVector? = null,
    destructive: Boolean = false,
    enabled: Boolean = true,
    trailing: (@Composable () -> Unit)? = null,
) {
    val c = kitColors
    val color = if (destructive) c.danger else c.accent
    Row(
        Modifier.fillMaxWidth().clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .heightIn(min = JSize.box50).padding(horizontal = JSpace.l, vertical = JSpace.s).alpha(if (enabled) 1f else 0.4f),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        if (icon != null) Icon(icon, null, tint = color, modifier = Modifier.size(JSize.iconS))
        Text(title, style = JTheme.type.body, color = color, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        trailing?.invoke()
    }
}

/** A label and its value (版本, 内核…). */
@Composable
fun KitValueRow(label: String, value: String, mono: Boolean = false) {
    val c = kitColors
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.tapTarget).padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Text(label, style = JTheme.type.body, color = c.label, maxLines = 1)
        Text(value, style = if (mono) JTheme.type.code else JTheme.type.body, color = c.secondaryLabel, modifier = Modifier.weight(1f), textAlign = TextAlign.End)
    }
}

/** A small label nobody taps: neutral grey, or tinted with [color]. */
@Composable
fun KitChip(text: String, color: Color = Color.Unspecified, icon: ImageVector? = null) {
    val tinted = color != Color.Unspecified
    Chip(text, icon = icon, look = if (tinted) ChipLook.Tint(color) else ChipLook.Neutral)
}

/** Nothing to show yet: a symbol, a line and why. */
@Composable
fun KitEmpty(icon: ImageVector, title: String, message: String?, modifier: Modifier = Modifier) {
    EmptyState(icon, title, message.orEmpty(), modifier)
}

/** A single-line field in a settings group: its label above, the filled field under it. */
@Composable
fun KitTextField(label: String, value: String, onChange: (String) -> Unit, placeholder: String = "", enabled: Boolean = true) {
    Column(Modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        Text(label, style = JTheme.type.caption, color = kitColors.secondaryLabel, maxLines = 1)
        JTextField(value, onChange, placeholder, enabled = enabled, singleLine = true)
    }
}

data class KitOption(val id: String, val title: String)

/** A picker row: the value at the end, a menu of [options] with the current one checked; a value no longer offered still shows. */
@Composable
fun KitPickerRow(title: String, value: String, options: List<KitOption>, onChange: (String) -> Unit, enabled: Boolean = true) {
    var open by remember { mutableStateOf(false) }
    Box {
        KitRow(title, onClick = if (enabled) ({ open = true }) else null, value = options.firstOrNull { it.id == value }?.title ?: value)
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            options.forEach { option ->
                DropdownMenuItem(
                    text = { Text(option.title, style = JTheme.type.body, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    trailingIcon = { if (option.id == value) Icon(JIcons.done, "已选", tint = kitColors.accent) },
                    onClick = { open = false; onChange(option.id) },
                )
            }
        }
    }
}

/** Loading inside a group. */
@Composable
fun KitLoadingRow() = PageLoading(Modifier.fillMaxWidth().padding(JSpace.xxl))

/** A short confirmation or error at the bottom of the screen. */
fun kitToast(ctx: Context, text: String) {
    Toast.makeText(ctx.applicationContext, text, Toast.LENGTH_SHORT).show()
}

/** What to tell the user about a failed call. */
val Throwable.kitMessage: String
    get() = when (this) {
        is HubException -> message.ifBlank { "hub 返回 $status" }
        is java.io.IOException -> "连不上 hub"
        else -> message ?: javaClass.simpleName
    }
