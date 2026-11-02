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

// The pieces the 设置 sub-pages and 机器 share: rounded groups of rows on the page, like Android's own settings.
// Colors come from the app's MaterialTheme (kitColors), so these pages follow light / dark and dynamic color.

/** The roles these pages color with, read from the current MaterialTheme. */
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
        val s = MaterialTheme.colorScheme
        val dark = s.background.luminance() < 0.5f
        return KitColors(
            dark = dark,
            page = s.surface,
            card = s.surfaceContainer,
            label = s.onSurface,
            secondaryLabel = s.onSurfaceVariant,
            tertiaryLabel = s.onSurfaceVariant.copy(alpha = 0.6f),
            separator = s.outlineVariant.copy(alpha = 0.6f),
            fillChip = s.surfaceContainerHighest,
            accent = s.primary,
            danger = s.error,
            positive = if (dark) Color(0xFF30D158) else Color(0xFF248A3D),
            caution = if (dark) Color(0xFFFF9F0A) else Color(0xFFC93400),
        )
    }

/** Corner of groups and cards. */
val KitCorner: Dp = 16.dp

/** A card on the page: the grouped rows, the machine cards, the charts. */
@Composable
fun Modifier.kitCard(padding: Dp = 0.dp, shape: Shape = RoundedCornerShape(KitCorner)): Modifier =
    background(kitColors.card, shape).padding(padding)

/** A sub-page: a top bar with back and a lazy column of [KitGroup]s. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KitPage(
    title: String,
    onBack: () -> Unit,
    actions: @Composable RowScope.() -> Unit = {},
    content: LazyListScope.() -> Unit,
) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = { jarvis.android.ui.PaneBackButton(onBack) },
                actions = actions,
            )
        },
    ) { pad ->
        LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = PaddingValues(top = pad.calculateTopPadding() + 4.dp, bottom = pad.calculateBottomPadding() + 32.dp),
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
    separatorStart: Dp = 16.dp,
    content: KitGroupScope.() -> Unit,
) {
    val c = kitColors
    val scope = KitGroupScope().apply(content)
    Column(modifier.fillMaxWidth().padding(top = if (header != null) 20.dp else 0.dp)) {
        if (header != null) {
            Text(header, style = MaterialTheme.typography.labelLarge, color = c.accent, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = 32.dp, end = 32.dp, bottom = 6.dp))
        }
        Column(Modifier.padding(horizontal = 16.dp).fillMaxWidth().kitCard()) {
            scope.rows.forEachIndexed { i, r ->
                if (i > 0) Box(Modifier.padding(start = separatorStart).fillMaxWidth().height(0.5.dp).background(c.separator))
                r()
            }
        }
        if (footer != null) {
            Text(footer, fontSize = 13.sp, lineHeight = 18.sp, color = c.secondaryLabel,
                modifier = Modifier.padding(start = 32.dp, end = 32.dp, top = 6.dp))
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
) {
    val c = kitColors
    Row(
        Modifier.fillMaxWidth().let { if (onClick != null) it.clickable(onClick = onClick) else it }
            .heightIn(min = 52.dp).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (icon != null) Icon(icon, null, tint = c.accent, modifier = Modifier.size(22.dp))
        Column(Modifier.weight(1f)) {
            Text(title, fontSize = 16.sp, color = titleColor.takeOrElse { c.label }, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!subtitle.isNullOrEmpty()) Text(subtitle, fontSize = 13.sp, lineHeight = 18.sp, color = c.secondaryLabel)
        }
        if (!value.isNullOrEmpty()) {
            Text(value, fontSize = 14.sp, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.widthIn(max = 200.dp), textAlign = TextAlign.End)
        }
        if (onClick != null) Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = c.tertiaryLabel, modifier = Modifier.size(20.dp))
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
            .heightIn(min = 52.dp)
            .padding(horizontal = 16.dp, vertical = 10.dp)
            .alpha(if (enabled) 1f else 0.45f),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (icon != null) Icon(icon, null, tint = iconTint.takeOrElse { c.accent }, modifier = Modifier.size(22.dp))
        Column(Modifier.weight(1f)) {
            Text(title, fontSize = 16.sp, color = c.label)
            if (!subtitle.isNullOrEmpty()) Text(subtitle, fontSize = 13.sp, lineHeight = 18.sp, color = c.secondaryLabel)
        }
        Switch(checked = checked, onCheckedChange = null, enabled = enabled)
    }
}

/** One option of an inline picker: a check mark when [selected]. */
@Composable
fun KitChoiceRow(title: String, selected: Boolean, onClick: () -> Unit, detail: String? = null) {
    val c = kitColors
    Row(
        Modifier.fillMaxWidth()
            .semantics { this.selected = selected; role = Role.RadioButton }
            .clickable(onClick = onClick)
            .heightIn(min = 52.dp)
            .padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(title, fontSize = 16.sp, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!detail.isNullOrEmpty()) Text(detail, fontSize = 13.sp, color = c.secondaryLabel, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (selected) Icon(Icons.Default.Check, "已选", tint = c.accent, modifier = Modifier.size(20.dp))
    }
}

/** A warning inside a group: a symbol, a bold title and a line of explanation. */
@Composable
fun KitNoticeRow(title: String, message: String? = null, tint: Color = Color.Unspecified, icon: ImageVector = Icons.Default.Warning) {
    val c = kitColors
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(icon, null, tint = tint.takeOrElse { c.caution }, modifier = Modifier.size(22.dp))
        Column(Modifier.weight(1f)) {
            Text(title, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = c.label)
            if (!message.isNullOrEmpty()) Text(message, fontSize = 13.sp, lineHeight = 18.sp, color = c.secondaryLabel)
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
            .heightIn(min = 52.dp).padding(horizontal = 16.dp, vertical = 10.dp).alpha(if (enabled) 1f else 0.4f),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (icon != null) Icon(icon, null, tint = color, modifier = Modifier.size(22.dp))
        Text(title, fontSize = 16.sp, color = color, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        trailing?.invoke()
    }
}

/** A label and its value (版本, 内核…). */
@Composable
fun KitValueRow(label: String, value: String, mono: Boolean = false) {
    val c = kitColors
    Row(
        Modifier.fillMaxWidth().heightIn(min = 44.dp).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(label, fontSize = 16.sp, color = c.label, maxLines = 1)
        Text(value, fontSize = if (mono) 14.sp else 16.sp, color = c.secondaryLabel, modifier = Modifier.weight(1f), textAlign = TextAlign.End,
            fontFamily = if (mono) FontFamily.Monospace else null)
    }
}

/** A small label nobody taps: neutral grey, or tinted with [color]. */
@Composable
fun KitChip(text: String, color: Color = Color.Unspecified, icon: ImageVector? = null) {
    val c = kitColors
    val tinted = color != Color.Unspecified
    Row(
        Modifier.background(if (tinted) color.copy(alpha = 0.14f) else c.fillChip, CircleShape).padding(horizontal = 8.dp, vertical = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        if (icon != null) Icon(icon, null, tint = if (tinted) color else c.secondaryLabel, modifier = Modifier.size(10.dp))
        Text(text, fontSize = 12.sp, color = if (tinted) color else c.secondaryLabel, maxLines = 1)
    }
}

/** Nothing to show yet: a symbol, a line and why. */
@Composable
fun KitEmpty(icon: ImageVector, title: String, message: String?, modifier: Modifier = Modifier) {
    val c = kitColors
    Column(modifier.fillMaxWidth().padding(horizontal = 40.dp), horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Icon(icon, null, tint = c.tertiaryLabel, modifier = Modifier.size(44.dp))
        Text(title, fontSize = 17.sp, fontWeight = FontWeight.SemiBold, color = c.label, textAlign = TextAlign.Center)
        if (!message.isNullOrEmpty()) Text(message, fontSize = 15.sp, color = c.secondaryLabel, textAlign = TextAlign.Center)
    }
}

/** A single-line field in a settings group; spacing stays in the shared kit. */
@Composable
fun KitTextField(label: String, value: String, onChange: (String) -> Unit, placeholder: String = "", enabled: Boolean = true) {
    OutlinedTextField(
        value = value, onValueChange = onChange, singleLine = true, enabled = enabled,
        label = { Text(label, maxLines = 1) },
        placeholder = { Text(placeholder, maxLines = 1, color = kitColors.tertiaryLabel) },
        modifier = Modifier.fillMaxWidth().padding(16.dp),
    )
}

data class KitOption(val id: String, val title: String)

/** A picker row keeps the current value visible even when its source is no longer in the options. */
@Composable
fun KitPickerRow(title: String, value: String, options: List<KitOption>, onChange: (String) -> Unit, enabled: Boolean = true) {
    var open by remember { mutableStateOf(false) }
    Box {
        KitRow(title, onClick = if (enabled) ({ open = true }) else null,
            value = options.firstOrNull { it.id == value }?.title ?: value)
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            options.forEach { option ->
                DropdownMenuItem(
                    text = { Text(option.title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    trailingIcon = { if (option.id == value) Icon(Icons.Default.Check, "已选", tint = kitColors.accent) },
                    onClick = { open = false; onChange(option.id) },
                )
            }
        }
    }
}

@Composable
fun KitLoadingRow() {
    Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
}

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
