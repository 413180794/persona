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
import androidx.compose.foundation.Image
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Dp
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import persona.core.contracts.api.PersonaInfo
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
        ExtensionThemeOptions()
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
    Row(
        Modifier.option(selected, onClick).padding(14.dp),
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
        // The card is the radio button; this one only shows the state, so a screen reader stops once.
        JRadio(selected, null)
    }
}

/** A selectable option card: the whole card is the radio button, outlined in the accent (ink in 晨报) when chosen. */
@Composable
private fun Modifier.option(selected: Boolean, onClick: () -> Unit): Modifier {
    val palette = PersonaTheme.palette
    val shape = PersonaTheme.shapes.cardShape
    return fillMaxWidth().clip(shape).background(palette.surface)
        .border(if (selected) 2.dp else 1.dp, if (selected) palette.accent.takeIf { PersonaTheme.style != AppStyle.Brief } ?: palette.ink else palette.line, shape)
        .clickable(role = Role.RadioButton, onClick = onClick).semantics(mergeDescendants = true) { this.selected = selected }
}

/** The app's four tabs, as the shell shows them, for the previews' navigation. */
private val PreviewTabs = listOf(
    NavItem("Persona", "Persona", PhIcons.Sparkle),
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

/**
 * 设置 › 外观 › 形象 (docs/persona.md): the built-in faces, the default first, then the hub's packages (a package is
 * downloaded when chosen and kept on the phone), applied at once and remembered on this phone, with the chosen one
 * in each state under them. Demo mode has only the default.
 */
@Composable
fun PersonaSettings(demo: Boolean, modifier: Modifier = Modifier) {
    val appearance = LocalAppStyleStore.current ?: return
    val packs = LocalPersonaStore.current?.takeUnless { demo }
    val chosenId by appearance.personaId.collectAsState()
    val offered by (packs?.offered ?: remember { MutableStateFlow(emptyList()) }).collectAsState()
    val downloading by (packs?.downloading ?: remember { MutableStateFlow(emptySet()) }).collectAsState()
    val error by (packs?.error ?: remember { MutableStateFlow(null) }).collectAsState()
    val scope = rememberCoroutineScope()
    // Shown at once on tap: the choice may wait for a refresh already fetching thumbnails.
    var asked by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(packs) { packs?.refresh() }
    val chosen = if (demo) PersonaDesign.Default.id else chosenId
    val pack = offered.firstOrNull { it.id == chosen }?.takeIf { packs?.onPhone(it) == true }
    val design = PersonaDesign.fromId(chosen)
    val palette = PersonaTheme.palette
    Column(modifier.fillMaxWidth()) {
        Text(if (demo) "体验模式只有默认形象。" else "顶栏和对话列表里 Persona 的名字和脸。颜色跟着界面风格走，不另外改 App 的颜色。形象包放在 hub 的 ~/.persona/personas 里，选中时下载到手机。",
            style = PersonaTheme.type.callout, color = palette.ink2)
        Spacer(Modifier.height(Tokens.Space.l))
        (if (demo) listOf(PersonaDesign.Default) else PersonaDesign.entries).forEach { d ->
            PersonaOption(d.title, d.summary, selected = d.id == chosen, tag = "默认".takeIf { d == PersonaDesign.Default },
                onClick = { if (!demo) packs?.choose(d) ?: appearance.setFace(d) }) {
                PersonaFace(PersonaState.Idle, size = Tokens.Space.xxxl * 2, design = d, pack = null)
            }
            Spacer(Modifier.height(Tokens.Space.m))
        }
        if (packs != null) offered.forEach { p ->
            val busy = p.id in downloading || p.id == asked
            PersonaOption(p.manifest.name, p.manifest.about ?: p.manifest.subtitle ?: if (packs.onPhone(p)) "hub 上的形象" else "选中后下载", selected = p.id == chosen,
                onClick = { if (!busy) scope.launch { asked = p.id; try { packs.choose(p) } finally { if (asked == p.id) asked = null } } }) {
                Box(contentAlignment = Alignment.Center) {
                    PackAvatar(packs, p, Tokens.Space.xxxl * 2)
                    if (busy) JSpinner(Modifier.size(Tokens.Space.xxl))
                }
            }
            Spacer(Modifier.height(Tokens.Space.m))
        }
        error?.let { JNotice(it, status = Status.NeedsYou); Spacer(Modifier.height(Tokens.Space.m)) }
        JSectionHeader("${pack?.manifest?.name ?: design.title}的五种状态")
        Row(
            Modifier.fillMaxWidth().clip(PersonaTheme.shapes.cardShape).background(palette.surface).padding(vertical = Tokens.Space.m)
                .semantics(mergeDescendants = true) {},
        ) {
            PersonaState.entries.forEach { state ->
                Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally) {
                    PersonaFace(state, size = Tokens.Space.xxxl + Tokens.Space.s, design = design, pack = pack)
                    Spacer(Modifier.height(Tokens.Space.xs))
                    Text(state.label, style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 1)
                }
            }
        }
    }
}

/** A package's still face: from the package once on the phone, else the thumbnail the store fetched for this card. */
@Composable
private fun PackAvatar(store: PersonaStore, p: PersonaInfo, size: Dp) {
    val pictures by store.pictures.collectAsState()
    val pixels = with(LocalDensity.current) { size.roundToPx() }
    val image by produceState<ImageBitmap?>(null, p.id, p.rev, pictures) { value = store.avatar(p, pixels)?.asImageBitmap() }
    Box(Modifier.size(size).clip(CircleShape).background(PersonaTheme.palette.surfaceStrong)) {
        image?.let { Image(it, contentDescription = null, Modifier.matchParentSize()) }
    }
}

@Composable
private fun PersonaOption(
    title: String, summary: String, selected: Boolean, onClick: () -> Unit, tag: String? = null, face: @Composable () -> Unit,
) {
    val palette = PersonaTheme.palette
    Row(
        Modifier.option(selected, onClick).padding(Tokens.Space.m),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        face()
        Spacer(Modifier.width(Tokens.Space.l))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(title, style = PersonaTheme.type.title, color = palette.ink)
                tag?.let { Spacer(Modifier.width(Tokens.Space.s)); JTag(it) }
            }
            Spacer(Modifier.height(Tokens.Space.xs))
            Text(summary, style = PersonaTheme.type.callout, color = palette.ink2)
        }
        Spacer(Modifier.width(Tokens.Space.s))
        JRadio(selected, null)
    }
}
