package jarvis.android.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
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
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * 设置 › 界面风格 (ios/Shared/Theme.swift `AppStyle`, docs/ios.md「设计语言」): the look of the whole app, switchable at
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
)

private fun hex(v: Long) = Color(0xFF000000 or v)

/** The palette of [style] in light or [dark] (also the previews in 设置 › 界面风格). Values from ios/Shared/Theme.swift. */
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

private fun typography(rounded: Boolean): Typography {
    val t = Typography()
    if (!rounded) return t
    val f = RoundedFamily
    return t.copy(
        displayLarge = t.displayLarge.copy(fontFamily = f), displayMedium = t.displayMedium.copy(fontFamily = f),
        displaySmall = t.displaySmall.copy(fontFamily = f), headlineLarge = t.headlineLarge.copy(fontFamily = f),
        headlineMedium = t.headlineMedium.copy(fontFamily = f), headlineSmall = t.headlineSmall.copy(fontFamily = f),
        titleLarge = t.titleLarge.copy(fontFamily = f), titleMedium = t.titleMedium.copy(fontFamily = f),
        titleSmall = t.titleSmall.copy(fontFamily = f), bodyLarge = t.bodyLarge.copy(fontFamily = f),
        bodyMedium = t.bodyMedium.copy(fontFamily = f), bodySmall = t.bodySmall.copy(fontFamily = f),
        labelLarge = t.labelLarge.copy(fontFamily = f), labelMedium = t.labelMedium.copy(fontFamily = f),
        labelSmall = t.labelSmall.copy(fontFamily = f),
    )
}

private val LocalPalette = staticCompositionLocalOf { paletteOf(AppStyle.GLASS, false) }

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

/** The app theme: the style picked in 设置 (live, [StyleStore.style]) in the system's light or dark appearance. */
@Composable
fun JarvisTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    val ctx = LocalContext.current
    remember { StyleStore.init(ctx) }
    val style by StyleStore.style.collectAsState()
    val palette = remember(style, dark) { paletteOf(style, dark) }
    val scheme = remember(palette) { schemeOf(palette) }
    val type = remember(style.rounded) { typography(style.rounded) }
    CompositionLocalProvider(LocalPalette provides palette) {
        MaterialTheme(colorScheme = scheme, typography = type, content = content)
    }
}

/** The current tokens: `JTheme.colors.card`, `JTheme.style.corner`… */
object JTheme {
    val colors: JarvisPalette
        @Composable @ReadOnlyComposable get() = LocalPalette.current
    val style: AppStyle
        @Composable @ReadOnlyComposable get() = LocalPalette.current.style
    val isDark: Boolean
        @Composable @ReadOnlyComposable get() = LocalPalette.current.dark
}

/** The state colours the iOS app uses (docs/agent-states.md): needs you red-orange, done-unseen blue, working green. */
fun AgentState.color(): Color = when (this) {
    AgentState.NeedsYou -> Color(0xFFFF6B3D)
    AgentState.Unread -> Color(0xFF3B82F6)
    AgentState.Working -> Color(0xFF22C55E)
    AgentState.Idle -> Color(0xFF9CA3AF)
    AgentState.Unknown -> Color(0xFF9CA3AF)
    AgentState.Offline -> Color(0xFFD1D5DB)
}

fun AgentState.label(): String = when (this) {
    AgentState.NeedsYou -> "需要你"
    AgentState.Unread -> "完成未看"
    AgentState.Working -> "工作中"
    AgentState.Idle -> "已看"
    AgentState.Unknown -> "已看"
    AgentState.Offline -> "未运行"
}
