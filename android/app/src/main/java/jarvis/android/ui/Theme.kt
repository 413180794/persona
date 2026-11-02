package jarvis.android.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import jarvis.android.R
import jarvis.android.data.AgentState
import jarvis.android.ui.design.DiffColors
import jarvis.android.ui.design.HueColors
import jarvis.android.ui.design.JTypeScale
import jarvis.android.ui.design.ShellColors
import jarvis.android.ui.design.StatusPalette
import jarvis.android.ui.design.StatusTone
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * 设置 › 界面风格 (apple/Shared/Theme.swift `AppStyle`, docs/ios.md「设计语言」): the look of the whole app, switchable at
 * runtime. Each style is a set of fixed color tokens per appearance ([JarvisPalette], light / dark follow the system),
 * mapped onto Material's color scheme so every screen, also those that only read `MaterialTheme.colorScheme`, follows.
 */
enum class AppStyle(val id: String, val title: String, val subtitle: String) {
    /** iOS 26 system look, system blue. The default. */
    GLASS("glass", "液态玻璃", "iOS 原生，简洁"),
    /** Claude's terracotta (the original Jarvis look). */
    CLAUDE("claude", "Claude", "原来的珊瑚橙"),
    /** Claymorphism: soft pastel lavender, puffy surfaces, the rounded face. */
    CLAY("clay", "黏土", "柔和、圆润"),
    /** Floating white islands on a misty teal canvas. */
    ISLAND("island", "Island", "悬浮卡片"),
    /** Feishu: its blue, flat white cards on light gray, hairline borders. */
    LARK("lark", "飞书", "和飞书一致");

    /** 黏土 uses the rounded typeface (iOS `.fontDesign(.rounded)`). */
    val rounded: Boolean get() = this == CLAY

    /** The system-surface styles (iOS draws Liquid Glass there); the others draw their own canvas. */
    val systemSurfaces: Boolean get() = this == GLASS || this == CLAUDE

    /** Corner radius of cards in this style (iOS: glass 20, clay 24, island 26, lark 10). */
    val corner: Dp
        get() = when (this) {
            GLASS, CLAUDE -> 20.dp
            CLAY -> 24.dp
            ISLAND -> 26.dp
            LARK -> 10.dp
        }

    /** Cards float on a soft shadow (黏土, Island); 飞书 draws a hairline instead; the glass styles neither. */
    val cardShadow: Dp get() = when (this) { CLAY -> 3.dp; ISLAND -> 6.dp; else -> 0.dp }

    companion object {
        fun of(id: String?): AppStyle = entries.firstOrNull { it.id == id } ?: GLASS
    }
}

/** The tokens of one style in one appearance (names follow iOS `Theme`). Read them with [JTheme.colors]. */
@Immutable
data class JarvisPalette(
    val style: AppStyle,
    val dark: Boolean,
    /** What can be tapped (links, selected controls, prominent buttons). */
    val accent: Color,
    /** Behind lists (systemGroupedBackground in the glass styles, the style's canvas otherwise). */
    val page: Color,
    /** Cards and grouped rows. */
    val card: Color,
    /** Hairline around cards (飞书's borders; faint elsewhere). */
    val cardStroke: Color,
    val label: Color,
    val secondaryLabel: Color,
    val tertiaryLabel: Color,
    val separator: Color,
    /** Theme.Fill: quaternary / tertiary system fill. */
    val fillWell: Color,
    val fillChip: Color,
    val userBubble: Color,
    val userBubbleText: Color,
    val danger: Color,
    val positive: Color,
    /** Warnings that are not an agent asking for you (with a symbol); [cautionText] for colored words. */
    val caution: Color,
    val cautionText: Color,
    /** What the AI suggests (✦). */
    val ai: Color,
    /** The app icon's coral: Jarvis' own mark only, never something to tap. */
    val brand: Color,
    /** Theme.Fill.pressed: a selected or pressed state. */
    val fillPressed: Color = fillChip,
    /** Supporting text: opaque, ≥ 4.5:1 on every page, card and fill (iOS's translucent secondary is 3.3:1 on gray). */
    val secondaryText: Color = secondaryLabel,
    /** Times and the quietest marks. */
    val tertiaryText: Color = tertiaryLabel,
    /** /loop and other scheduled wake-ups (待唤醒). */
    val loop: Color = accent,
    /** Count badges on rows: white digits on it are ≥ 4.5:1 in both appearances. */
    val countBadge: Color = Color(0xFF636366),
    /** Behind code blocks and terminal snippets, the same in every style. */
    val codeBackground: Color = fillWell,
    /** The surface a floating control sits on (circle buttons, the composer's +, chips over the page). */
    val floating: Color = card,
)

private fun hex(v: Long) = Color(0xFF000000 or v)

/** The palette of [style] in light or [dark] (also the previews in 设置 › 界面风格). Values from apple/Shared/Theme.swift. */
fun paletteOf(style: AppStyle, dark: Boolean): JarvisPalette {
    fun pick(light: Long, darkV: Long) = hex(if (dark) darkV else light)
    val label = if (dark) Color.White else Color.Black
    val accent = when (style) {
        AppStyle.GLASS -> pick(0x007AFF, 0x0A84FF)
        AppStyle.CLAUDE -> pick(0xD4683F, 0xCD6E45)
        AppStyle.CLAY -> pick(0x7A5AF8, 0x8979EB)
        AppStyle.ISLAND -> pick(0x27747F, 0x2A9798)
        AppStyle.LARK -> pick(0x3370FF, 0x4C88FF)
    }
    val page = when (style) {
        AppStyle.GLASS, AppStyle.CLAUDE -> pick(0xF2F2F7, 0x000000)
        AppStyle.CLAY -> pick(0xEFEAF7, 0x0A0510)
        AppStyle.ISLAND -> pick(0xE6F1EF, 0x0A1110)
        AppStyle.LARK -> pick(0xF5F6F7, 0x141414)
    }
    val card = when (style) {
        AppStyle.GLASS, AppStyle.CLAUDE -> pick(0xFFFFFF, 0x1C1C1E)
        AppStyle.CLAY -> pick(0xF8F5FD, 0x2A2533)
        AppStyle.ISLAND -> pick(0xFFFFFF, 0x16201E)
        AppStyle.LARK -> pick(0xFFFFFF, 0x1F1F1F)
    }
    val bubble = when (style) {
        AppStyle.GLASS -> pick(0xDCEAFF, 0x1D3557)
        AppStyle.CLAUDE -> pick(0xB24C25, 0xAF4C28)
        AppStyle.CLAY -> pick(0x6759D3, 0x6756D9)
        AppStyle.ISLAND -> pick(0x14201E, 0xE8F3F1)
        AppStyle.LARK -> pick(0xD1E3FF, 0x133063)
    }
    val bubbleText = when (style) {
        AppStyle.GLASS, AppStyle.LARK -> label
        AppStyle.ISLAND -> pick(0xFFFFFF, 0x0B1211)
        else -> Color.White
    }
    return JarvisPalette(
        style = style,
        dark = dark,
        accent = accent,
        page = page,
        card = card,
        cardStroke = if (style == AppStyle.LARK) pick(0xDEE0E3, 0x373A40) else label.copy(alpha = 0.08f),
        label = label,
        secondaryLabel = if (dark) Color(0x99EBEBF5) else Color(0x993C3C43),
        tertiaryLabel = if (dark) Color(0x4DEBEBF5) else Color(0x4D3C3C43),
        separator = if (dark) Color(0x99545458) else Color(0x4A3C3C43),
        fillWell = if (dark) Color(0x2E767680) else Color(0x14747480),
        fillChip = if (dark) Color(0x3D767680) else Color(0x1F767680),
        userBubble = bubble,
        userBubbleText = bubbleText,
        danger = pick(0xD93A3A, 0xFF5F57),
        positive = pick(0x248A3D, 0x30D158),
        caution = pick(0xF08A00, 0xFF9F0A),
        cautionText = pick(0xB35400, 0xFF9F0A),
        ai = pick(0x5B4FD6, 0x9C94FF),
        brand = pick(0xD4683F, 0xE8835C),
        fillPressed = if (dark) Color(0x52787880) else Color(0x29787880),
        secondaryText = if (dark) hex(0x9A9AA0) else hex(0x636367),
        tertiaryText = if (dark) hex(0x6E6E74) else hex(0x8E8E93),
        loop = pick(0x2E6FD0, 0x5B9BFF),
        countBadge = hex(0x636366),
        codeBackground = pick(0xF3F1EC, 0x1C1D21),
        // Floating controls: the glass styles' frosted white (opaque here: Android blurs nothing behind a view).
        floating = when (style) {
            AppStyle.GLASS, AppStyle.CLAUDE -> pick(0xFFFFFF, 0x1C1C1E)
            else -> card
        },
    )
}

/**
 * The palette as Material roles. Surfaces are opaque (Material draws them over varying backgrounds); the text on
 * [JarvisPalette.accent] is white, which every style's accent carries at ≥ 3.5:1 (docs/ios.md「可读性底线」).
 * primaryContainer is the user's bubble (the chat draws its own messages with it).
 */
internal fun schemeOf(p: JarvisPalette): ColorScheme {
    val dark = p.dark
    val base = if (dark) darkColorScheme() else lightColorScheme()
    val raised = if (p.style.systemSurfaces) (if (dark) hex(0x2C2C2E) else hex(0xE9E9EB)) else p.accent.copy(alpha = 0.12f).compositeOn(p.card)
    return base.copy(
        primary = p.accent,
        onPrimary = Color.White,
        primaryContainer = p.userBubble,
        onPrimaryContainer = p.userBubbleText,
        inversePrimary = p.accent,
        secondary = p.accent,
        onSecondary = Color.White,
        secondaryContainer = raised,
        onSecondaryContainer = p.label,
        tertiary = p.ai,
        onTertiary = Color.White,
        tertiaryContainer = p.ai.copy(alpha = 0.14f).compositeOn(p.card),
        onTertiaryContainer = p.label,
        background = p.page,
        onBackground = p.label,
        surface = p.page,
        onSurface = p.label,
        surfaceVariant = raised,
        // Supporting text everywhere (ListItem, captions): opaque and darker than iOS's translucent secondaryLabel,
        // which is 3.3:1 on the gray grouped page; this keeps ≥ 4.5:1 on every page, card and raised fill.
        onSurfaceVariant = if (dark) hex(0x9A9AA0) else hex(0x636367),
        surfaceTint = Color.Transparent,
        surfaceBright = p.card,
        surfaceDim = p.page,
        surfaceContainerLowest = p.card,
        surfaceContainerLow = p.card,
        surfaceContainer = p.card,
        surfaceContainerHigh = p.card,
        // Material's Card fills with this: the style's card on its page, not a tinted fill.
        surfaceContainerHighest = p.card,
        // Outlined buttons and fields: visible on the page, cards and the raised fill of the interaction cards.
        outline = if (dark) hex(0x5C5C63) else hex(0xC2C2C8),
        outlineVariant = p.cardStroke.compositeOn(p.page),
        error = p.danger,
        onError = Color.White,
        errorContainer = p.danger.copy(alpha = 0.12f).compositeOn(p.card),
        // Words on the pale red: a deeper (lighter in dark) red than danger, which is 3.8:1 there.
        onErrorContainer = if (dark) hex(0xFF8A84) else hex(0xA8231C),
        scrim = Color.Black,
    )
}

/** This translucent color drawn over [bg], as one opaque color. */
private fun Color.compositeOn(bg: Color): Color {
    val a = alpha
    return Color(red * a + bg.red * (1 - a), green * a + bg.green * (1 - a), blue * a + bg.blue * (1 - a), 1f)
}

/**
 * 黏土's rounded face: Nunito (SIL OFL, `assets/licenses/nunito-OFL.txt`), a variable font pinned at the weights the
 * app uses. Latin only; Chinese falls back to the system font, as iOS's rounded design does.
 */
@OptIn(ExperimentalTextApi::class)
private val RoundedFamily: FontFamily by lazy {
    FontFamily(
        listOf(FontWeight.Normal, FontWeight.Medium, FontWeight.SemiBold, FontWeight.Bold).map { w ->
            Font(R.font.nunito_variable, weight = w, variationSettings = FontVariation.Settings(FontVariation.weight(w.weight)))
        },
    )
}

private val LocalPalette = staticCompositionLocalOf { paletteOf(AppStyle.GLASS, false) }
private val LocalType = staticCompositionLocalOf { JTypeScale.of(null) }

/**
 * The picked 界面风格, kept on this phone (SharedPreferences "ui", key `ui.style` like iOS's UserDefaults key; not
 * tied to the hub, so forgetting the hub keeps it). Reads as the default before [init] (unit tests).
 */
object StyleStore {
    private var sp: android.content.SharedPreferences? = null
    private val _style = MutableStateFlow(AppStyle.GLASS)
    val style: StateFlow<AppStyle> = _style.asStateFlow()

    fun init(ctx: android.content.Context) {
        if (sp != null) return
        val p = ctx.applicationContext.getSharedPreferences("ui", android.content.Context.MODE_PRIVATE)
        sp = p
        _style.value = AppStyle.of(p.getString(KEY, null))
    }

    fun set(style: AppStyle) {
        _style.value = style
        sp?.edit()?.putString(KEY, style.id)?.apply()
    }

    const val KEY = "ui.style"
}

/** Material's own shapes on the design system's corners: menus and fields at the card's, sheets at the panel's. */
private val Shapes = androidx.compose.material3.Shapes(
    extraSmall = androidx.compose.foundation.shape.RoundedCornerShape(jarvis.android.ui.design.JRadius.card),
    small = androidx.compose.foundation.shape.RoundedCornerShape(jarvis.android.ui.design.JRadius.control),
    medium = androidx.compose.foundation.shape.RoundedCornerShape(jarvis.android.ui.design.JRadius.card),
    large = androidx.compose.foundation.shape.RoundedCornerShape(jarvis.android.ui.design.JRadius.panel),
)

/** The app theme: the style picked in 设置 (live, [StyleStore.style]) in the system's light or dark appearance. */
@Composable
fun JarvisTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    val ctx = LocalContext.current
    remember { StyleStore.init(ctx) }
    val style by StyleStore.style.collectAsState()
    val palette = remember(style, dark) { paletteOf(style, dark) }
    val scheme = remember(palette) { schemeOf(palette) }
    val scale = remember(style.rounded) { JTypeScale.of(if (style.rounded) RoundedFamily else null) }
    val type = remember(scale) { scale.material() }
    CompositionLocalProvider(LocalPalette provides palette, LocalType provides scale) {
        MaterialTheme(colorScheme = scheme, typography = type, shapes = Shapes, content = content)
    }
}

/** The current tokens: `JTheme.colors.card`, `JTheme.type.body`, `JTheme.style.corner`… */
object JTheme {
    val colors: JarvisPalette
        @Composable @ReadOnlyComposable get() = LocalPalette.current
    val style: AppStyle
        @Composable @ReadOnlyComposable get() = LocalPalette.current.style
    val isDark: Boolean
        @Composable @ReadOnlyComposable get() = LocalPalette.current.dark
    /** The type scale (design/Type.kt): `JTheme.type.heading`, `JTheme.type.captionStrong`… */
    val type: JTypeScale
        @Composable @ReadOnlyComposable get() = LocalType.current
    /** The ChatGPT-style shell's neutral colors. */
    val shell: ShellColors
        @Composable @ReadOnlyComposable get() = ShellColors.of(LocalPalette.current.dark)
    val diff: DiffColors
        @Composable @ReadOnlyComposable get() = DiffColors.of(LocalPalette.current.dark)
    val hue: HueColors
        @Composable @ReadOnlyComposable get() = HueColors.of(LocalPalette.current.dark)
    val brand: jarvis.android.ui.design.BrandColors
        @Composable @ReadOnlyComposable get() = jarvis.android.ui.design.BrandColors.of(LocalPalette.current.dark)
    /** An agent state's dot, words and soft fill. */
    @Composable @ReadOnlyComposable
    fun tone(state: AgentState): StatusTone = StatusPalette.of(state, LocalPalette.current.dark)
    val needsYou: StatusTone
        @Composable @ReadOnlyComposable get() = StatusPalette.needsYou(LocalPalette.current.dark)
}

/** The state's dot in the current appearance (docs/agent-states.md: needs you red, working amber, done-unseen green). */
val AgentState.color: Color
    @Composable @ReadOnlyComposable get() = JTheme.tone(this).dot

/** The dot outside Compose's theme (the widget, notifications): the same table. */
fun AgentState.dotColor(dark: Boolean): Color = StatusPalette.of(this, dark).dot

fun AgentState.label(): String = when (this) {
    AgentState.NeedsYou -> "需要你"
    AgentState.Unread -> "完成未看"
    AgentState.Working -> "工作中"
    AgentState.Idle -> "已看"
    AgentState.Unknown -> "已看"
    AgentState.Offline -> "未运行"
}
