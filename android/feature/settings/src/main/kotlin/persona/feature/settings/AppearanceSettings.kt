package persona.feature.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/**
 * 设置 › 界面风格: three styles, applied at once and remembered on this phone. Light and dark keep following the
 * system. Each option shows a small live preview drawn with the real components of that style.
 */
@Composable
fun AppearanceSettings(modifier: Modifier = Modifier) {
    val store = LocalAppStyleStore.current ?: return
    val current by store.style.collectAsState()
    val glass by store.glass.collectAsState()
    val palette = PersonaTheme.palette
    Column(modifier.fillMaxWidth()) {
        Text("选一种你喜欢的样子，马上生效。深浅色仍然跟随系统。", style = PersonaTheme.type.callout, color = palette.ink2)
        Spacer(Modifier.height(16.dp))
        AppStyle.entries.forEach { style ->
            StyleOption(style, selected = style == current) { store.setStyle(style) }
            Spacer(Modifier.height(12.dp))
        }
        if (current == AppStyle.Glass) {
            JSectionHeader("透明效果")
            JSegmented(GlassPreference.entries.map { JSegment(it.label) }, GlassPreference.entries.indexOf(glass), { store.setGlass(GlassPreference.entries[it]) })
            Spacer(Modifier.height(10.dp))
            Text(
                when (glass) {
                    GlassPreference.Auto -> "Android 13 以上用折射玻璃，更早的系统用模糊玻璃；省电模式下，或者滑动明显掉帧时，自动换成不透明的面板。"
                    GlassPreference.Reduced -> "所有浮层都用不透明的面板，最省电，文字也最清楚。"
                },
                style = PersonaTheme.type.caption, color = palette.ink3,
            )
        }
    }
}

@Composable
private fun StyleOption(style: AppStyle, selected: Boolean, onClick: () -> Unit) {
    val palette = PersonaTheme.palette
    val shape = PersonaTheme.shapes.cardShape
    Row(
        Modifier.fillMaxWidth().clip(shape).background(palette.surface)
            .border(if (selected) 2.dp else 1.dp, if (selected) palette.accent.takeIf { PersonaTheme.style != AppStyle.Brief } ?: palette.ink else palette.line, shape)
            .clickable(role = Role.RadioButton, onClick = onClick).semantics { this.selected = selected }.padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        StylePreview(style)
        Spacer(Modifier.width(16.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(style.label, style = PersonaTheme.type.title, color = palette.ink)
                if (style == AppStyle.Default) { Spacer(Modifier.width(8.dp)); JTag("默认") }
            }
            Spacer(Modifier.height(4.dp))
            Text(style.summary, style = PersonaTheme.type.callout, color = palette.ink2)
        }
        Spacer(Modifier.width(8.dp))
        JRadio(selected, onClick)
    }
}

/** The app's four tabs, as the shell shows them, for the previews' navigation. */
private val PreviewTabs = listOf(
    NavItem("Jarvis", "Jarvis", PhIcons.Sparkle),
    NavItem("Agents", "Agents", PhIcons.SquaresFour),
    NavItem("Inbox", "收件箱", PhIcons.Tray),
    NavItem("Settings", "设置", PhIcons.GearSix),
)

/** The sessions of the previewed space, as the list shows them (a finished, seen one has no state text). */
private val PreviewRows = listOf(
    PreviewRow("补齐网络层的单元测试", Status.NeedsYou, "网络层的测试用哪个框架？", "2 分钟前"),
    PreviewRow("给天气 App 加小组件", Status.Working, "正在跑测试，确认降雨概率的格式", "刚刚"),
    PreviewRow("菜谱网站加搜索", Status.Idle, "可以按食材和用时筛选了", "25 分钟前"),
)

private data class PreviewRow(val title: String, val status: Status, val preview: String, val time: String)

/** What the previewed computer and space count: one session needs you, one works. */
private val PreviewCounts = listOf(Status.NeedsYou to 1, Status.Working to 1)

/**
 * The Agents tab at 0.3 of its size (320dp wide, near a phone's width), drawn with the style's own components
 * (docs/android-design.md 2.4): a computer, one of its spaces and its sessions on the page background (the board's
 * agent strips), and the app's navigation. Glass shows its ambient light; its dock is the opaque panel, there being
 * nothing here to refract.
 */
@Composable
private fun StylePreview(style: AppStyle) {
    // A picture: screen readers skip it (the option says which style it is) and taps reach the option around it,
    // not the headings, rows and tabs drawn in it.
    Box(Modifier.size(96.dp, 132.dp).clip(RoundedCornerShape(14.dp)).clearAndSetSemantics {}) {
        Box(Modifier.requiredSize(320.dp, 440.dp).graphicsLayer { scaleX = 0.3f; scaleY = 0.3f }) {
            PersonaTheme(darkTheme = PersonaTheme.darkTheme, style = style) {
                CompositionLocalProvider(LocalReadingInset provides 0.dp, LocalGlassSource provides null) {
                    val palette = PersonaTheme.palette
                    Box(Modifier.fillMaxSize().background(palette.background).consumeWindowInsets(WindowInsets.navigationBars)) {
                        if (style == AppStyle.Glass) AmbientLight(Ambience.NeedsYou)
                        Column(Modifier.fillMaxSize().padding(horizontal = pagePadding).padding(top = Tokens.Space.l)) {
                            Text("Agents", style = PersonaTheme.type.largeTitle, color = palette.ink, maxLines = 1)
                            AgentTreeHeader(
                                "MacBook", folded = false, onToggle = {}, first = true, icon = PhIcons.Laptop, online = true,
                                detail = "1 个空间，3 个会话", counts = PreviewCounts, onMonitor = {},
                            )
                            AgentSpaceHeader("weather", folded = false, onToggle = {}, first = true, counts = PreviewCounts)
                            PreviewRows.forEachIndexed { index, row ->
                                AgentRow(
                                    row.title, row.status, onClick = {}, depth = 1, position = GroupPosition.of(index, PreviewRows.size),
                                    preview = row.preview, time = row.time, statusText = row.status.label.takeIf { row.status != Status.Idle },
                                )
                            }
                        }
                        AppNavigation(PreviewTabs, "Agents", {}, Modifier.align(Alignment.BottomCenter))
                    }
                }
            }
        }
        Box(Modifier.matchParentSize().pointerInput(Unit) {})
    }
}
