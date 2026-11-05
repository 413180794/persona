package persona.core.designsystem

import android.content.Context
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.graphics.Color

/** One status, three uses: the dot or holder, text in that colour, and a soft tint behind it. */
@Immutable
data class StatusColors(val dot: Color, val text: Color, val tint: Color, val onDot: Color = Color.White)

/**
 * Semantic colours of one style in one mode. Components read these; pages should not pick raw colours.
 * [surface] may be translucent (glass); [surfaceOpaque] is the same surface without transparency, for
 * places that cannot show what is behind them (Material dialogs, the solid glass fallback).
 */
@Immutable
data class PersonaPalette(
    val background: Color,
    val surface: Color,
    val surfaceOpaque: Color,
    val surfaceStrong: Color,
    val sunken: Color,
    val line: Color,
    val strokeTop: Color,
    val strokeBottom: Color,
    val ink: Color,
    val ink2: Color,
    val ink3: Color,
    val action: Color,
    val onAction: Color,
    val accent: Color,
    val accentSoft: Color,
    val marker: Color,
    val onMarker: Color,
    val needsYou: StatusColors,
    val working: StatusColors,
    val done: StatusColors,
    val idle: StatusColors,
    val offline: StatusColors,
    val danger: StatusColors,
    val positive: Color,
    val caution: Color,
    val cautionText: Color,
    val diffAdd: Color,
    val diffDel: Color,
    val terminalBackground: Color,
    val terminalText: Color,
    val scrim: Color,
    val dark: Boolean,
    val ai: Color = accent,
    val positiveRole: StatusColors? = null,
    val cautionRole: StatusColors? = null,
    val customRoles: Boolean = false,
    /** The user's own messages: iOS 液态玻璃's pale blue (ColorSpec.blue), the same in every style (#2643). */
    val userBubble: Color = if (dark) Tokens.Semantic.Glass.Dark.userBubble else Tokens.Semantic.Glass.Light.userBubble,
    /** The words on [userBubble] when it is a deep colour of its own (a 主题色 like 珊瑚橙); null: the page's ink. */
    val userBubbleText: Color? = null,
    /** Where an agent was opened (#3132, iOS `Theme.sourceFeishu` / `sourceTerminal`): a Feishu group's topic, a terminal pane. */
    val sourceFeishu: StatusColors = if (dark) StatusColors(Color(0xFF82AAFF), Color(0xFF82AAFF), Color(0x2982AAFF))
        else StatusColors(Color(0xFF1D5FD6), Color(0xFF1D5FD6), Color(0x1A1D5FD6)),
    val sourceTerminal: StatusColors = if (dark) StatusColors(Color(0xFFBEA3FF), Color(0xFFBEA3FF), Color(0x29BEA3FF))
        else StatusColors(Color(0xFF6A46C2), Color(0xFF6A46C2), Color(0x1A6A46C2)),
) {
    fun source(kind: SourceKind): StatusColors = when (kind) {
        SourceKind.Feishu -> sourceFeishu
        SourceKind.Terminal -> sourceTerminal
    }

    fun status(status: Status): StatusColors = when (status) {
        Status.NeedsYou -> needsYou
        Status.Working -> working
        Status.Unread -> done
        Status.Idle -> idle
        Status.Offline -> offline
        Status.Unknown, Status.Muted -> idle
        Status.Waiting -> working.copy(dot = working.dot.copy(alpha = 0.55f))
    }
}

internal object Palettes {
    // 玻璃: night blue with three status lights; panels are frosted white.
    val GlassDark = PersonaPalette(
        background = Color(0xFF060A14), surface = Color(0x14FFFFFF), surfaceOpaque = Color(0xFF151C2C),
        surfaceStrong = Color(0x24FFFFFF), sunken = Color(0x47000000), line = Color(0x17FFFFFF),
        strokeTop = Color(0x40FFFFFF), strokeBottom = Color(0x0DFFFFFF),
        ink = Color(0xFFF4F7FC), ink2 = Color(0xFFAAB4C9), ink3 = Color(0xFF7A859C),
        action = Color(0xFFF4F7FC), onAction = Color(0xFF0A0F1C), accent = Color(0xFF7DB2FF), accentSoft = Color(0x295A95FF),
        marker = Color(0x295A95FF), onMarker = Color(0xFFF4F7FC),
        needsYou = StatusColors(Color(0xFFFFB547), Color(0xFFFFC56E), Color(0x2EFFB547), Color(0xFF2B1A00)),
        working = StatusColors(Color(0xFF5A95FF), Color(0xFF8DB6FF), Color(0x2E5A95FF)),
        done = StatusColors(Color(0xFF3CD9AE), Color(0xFF6FE6C5), Color(0x2E3CD9AE), Color(0xFF032A20)),
        idle = StatusColors(Color(0xFF7A859C), Color(0xFFAAB4C9), Color(0x1F7A859C)),
        offline = StatusColors(Color(0xFF5C667C), Color(0xFF8C96AB), Color(0x1A5C667C)),
        danger = StatusColors(Color(0xFFFF6B6B), Color(0xFFFF8F8F), Color(0x2EFF6B6B)),
        positive = Color(0xFF3CD9AE), caution = Color(0xFFFFB547), cautionText = Color(0xFFFFC56E),
        diffAdd = Color(0xFF3CD9AE), diffDel = Color(0xFFFF7B7B),
        terminalBackground = Color(0xFF05070D), terminalText = Color(0xFFE6E3DA), scrim = Color(0x99000000), dark = true,
    )
    val GlassLight = PersonaPalette(
        background = Color(0xFFE9EEF6), surface = Color(0x9EFFFFFF), surfaceOpaque = Color(0xFFF7F9FC),
        surfaceStrong = Color(0xD1FFFFFF), sunken = Color(0x0F0B1222), line = Color(0x170B1222),
        strokeTop = Color(0xFFFFFFFF), strokeBottom = Color(0x80FFFFFF),
        ink = Color(0xFF0B1222), ink2 = Color(0xFF4D586E), ink3 = Color(0xFF7E899E),
        action = Color(0xFF0B1222), onAction = Color(0xFFFFFFFF), accent = Color(0xFF2F6FE8), accentSoft = Color(0x1A2F6FE8),
        marker = Color(0x1A2F6FE8), onMarker = Color(0xFF0B1222),
        needsYou = StatusColors(Color(0xFFE69100), Color(0xFFA35F00), Color(0x24E69100), Color(0xFF2B1A00)),
        working = StatusColors(Color(0xFF2F6FE8), Color(0xFF1F5BD0), Color(0x1F2F6FE8)),
        done = StatusColors(Color(0xFF12A579), Color(0xFF0B7F5D), Color(0x1F12A579)),
        idle = StatusColors(Color(0xFF8590A5), Color(0xFF5B667B), Color(0x1A8590A5)),
        offline = StatusColors(Color(0xFFA0A9BA), Color(0xFF6E788C), Color(0x14A0A9BA)),
        danger = StatusColors(Color(0xFFE5484D), Color(0xFFC62F36), Color(0x1FE5484D)),
        positive = Color(0xFF12A579), caution = Color(0xFFE69100), cautionText = Color(0xFFA35F00),
        diffAdd = Color(0xFF0E9A6E), diffDel = Color(0xFFD4383E),
        terminalBackground = Color(0xFF0D121D), terminalText = Color(0xFFE6E3DA), scrim = Color(0x590B1222), dark = false,
    )

    // 晨报: paper and ink; the highlighter is the only colour and only marks advice and what needs you.
    val BriefLight = PersonaPalette(
        background = Color(0xFFFFFFFF), surface = Color(0xFFF4F5F7), surfaceOpaque = Color(0xFFF4F5F7),
        surfaceStrong = Color(0xFFE9ECF0), sunken = Color(0xFFF4F5F7), line = Color(0xFFE6E8EC),
        strokeTop = Color(0xFFE6E8EC), strokeBottom = Color(0xFFE6E8EC),
        ink = Color(0xFF141922), ink2 = Color(0xFF5C6573), ink3 = Color(0xFF8A92A0),
        action = Color(0xFF141922), onAction = Color(0xFFFFFFFF), accent = Color(0xFF141922), accentSoft = Color(0xFFE9ECF0),
        marker = Color(0xFFFFE15A), onMarker = Color(0xFF141922),
        needsYou = StatusColors(Color(0xFFF2C200), Color(0xFF141922), Color(0xFFFFE15A), Color(0xFF141922)),
        working = StatusColors(Color(0xFF141922), Color(0xFF141922), Color(0xFFE9ECF0)),
        done = StatusColors(Color(0xFF1E8E5A), Color(0xFF1E8E5A), Color(0xFFE5F3EB)),
        idle = StatusColors(Color(0xFFB2B8C2), Color(0xFF5C6573), Color(0xFFF4F5F7)),
        offline = StatusColors(Color(0xFFC7CCD4), Color(0xFF8A92A0), Color(0xFFF4F5F7)),
        danger = StatusColors(Color(0xFFCC3A2E), Color(0xFFB8322A), Color(0xFFFBEAE8)),
        positive = Color(0xFF1E8E5A), caution = Color(0xFFE0A800), cautionText = Color(0xFF8A6400),
        diffAdd = Color(0xFF1E8E5A), diffDel = Color(0xFFCC3A2E),
        terminalBackground = Color(0xFF141922), terminalText = Color(0xFFE9E6DF), scrim = Color(0x66141922), dark = false,
        // paper and ink: a source tag is ink on a grey slip, like any other tag
        sourceFeishu = StatusColors(Color(0xFF5C6573), Color(0xFF5C6573), Color(0xFFE9ECF0)),
        sourceTerminal = StatusColors(Color(0xFF5C6573), Color(0xFF5C6573), Color(0xFFE9ECF0)),
    )
    val BriefDark = PersonaPalette(
        background = Color(0xFF0E1117), surface = Color(0xFF181C24), surfaceOpaque = Color(0xFF181C24),
        surfaceStrong = Color(0xFF242934), sunken = Color(0xFF0E1117), line = Color(0xFF232833),
        strokeTop = Color(0xFF232833), strokeBottom = Color(0xFF232833),
        ink = Color(0xFFECEFF4), ink2 = Color(0xFF9CA4B3), ink3 = Color(0xFF6F7787),
        action = Color(0xFFECEFF4), onAction = Color(0xFF0E1117), accent = Color(0xFFECEFF4), accentSoft = Color(0xFF242934),
        marker = Color(0xFFE9C846), onMarker = Color(0xFF15130A),
        needsYou = StatusColors(Color(0xFFE9C846), Color(0xFFF2D876), Color(0xFF3A3315), Color(0xFF15130A)),
        working = StatusColors(Color(0xFFECEFF4), Color(0xFFECEFF4), Color(0xFF242934), Color(0xFF0E1117)),
        done = StatusColors(Color(0xFF4CC38A), Color(0xFF6FD3A2), Color(0xFF15291F), Color(0xFF07170F)),
        idle = StatusColors(Color(0xFF4A5160), Color(0xFF9CA4B3), Color(0xFF181C24)),
        offline = StatusColors(Color(0xFF3A404C), Color(0xFF6F7787), Color(0xFF181C24)),
        danger = StatusColors(Color(0xFFFF6B5E), Color(0xFFFF8A80), Color(0xFF3A1A17)),
        positive = Color(0xFF4CC38A), caution = Color(0xFFE9C846), cautionText = Color(0xFFF2D876),
        diffAdd = Color(0xFF4CC38A), diffDel = Color(0xFFFF7A6E),
        terminalBackground = Color(0xFF07090D), terminalText = Color(0xFFE9E6DF), scrim = Color(0x99000000), dark = true,
        sourceFeishu = StatusColors(Color(0xFF9CA4B3), Color(0xFF9CA4B3), Color(0xFF242934)),
        sourceTerminal = StatusColors(Color(0xFF9CA4B3), Color(0xFF9CA4B3), Color(0xFF242934)),
    )

    // 看板: a grey board with white strips; the strip holder colour is the state.
    val BoardLight = PersonaPalette(
        background = Color(0xFFE6E9ED), surface = Color(0xFFFFFFFF), surfaceOpaque = Color(0xFFFFFFFF),
        surfaceStrong = Color(0xFFF3F5F7), sunken = Color(0xFFEEF1F4), line = Color(0xFFD5DAE0),
        strokeTop = Color(0xFFD5DAE0), strokeBottom = Color(0xFFD5DAE0),
        ink = Color(0xFF0E141B), ink2 = Color(0xFF526070), ink3 = Color(0xFF8792A1),
        action = Color(0xFF0E141B), onAction = Color(0xFFFFFFFF), accent = Color(0xFF2563EB), accentSoft = Color(0xFFE6EEFF),
        marker = Color(0xFFE6EEFF), onMarker = Color(0xFF0E141B),
        needsYou = StatusColors(Color(0xFFF5A000), Color(0xFF8A5A00), Color(0xFFFFF3DA), Color(0xFF2B1D00)),
        working = StatusColors(Color(0xFF2563EB), Color(0xFF1D4FC4), Color(0xFFE6EEFF)),
        done = StatusColors(Color(0xFF15A34A), Color(0xFF0F7E39), Color(0xFFE2F6E9)),
        idle = StatusColors(Color(0xFFA2ACB9), Color(0xFF526070), Color(0xFFEEF1F4)),
        offline = StatusColors(Color(0xFFC4CBD4), Color(0xFF8792A1), Color(0xFFEEF1F4)),
        danger = StatusColors(Color(0xFFDC2626), Color(0xFFB91C1C), Color(0xFFFDE8E8)),
        positive = Color(0xFF15A34A), caution = Color(0xFFF5A000), cautionText = Color(0xFF8A5A00),
        diffAdd = Color(0xFF15A34A), diffDel = Color(0xFFDC2626),
        terminalBackground = Color(0xFF0E141B), terminalText = Color(0xFFE6E3DA), scrim = Color(0x660E141B), dark = false,
    )
    val BoardDark = PersonaPalette(
        background = Color(0xFF0E1114), surface = Color(0xFF1A1E23), surfaceOpaque = Color(0xFF1A1E23),
        surfaceStrong = Color(0xFF22272E), sunken = Color(0xFF13161A), line = Color(0xFF2A3037),
        strokeTop = Color(0xFF2A3037), strokeBottom = Color(0xFF2A3037),
        ink = Color(0xFFE8ECF1), ink2 = Color(0xFF98A3B2), ink3 = Color(0xFF677283),
        action = Color(0xFFE8ECF1), onAction = Color(0xFF0E1114), accent = Color(0xFF4C8DFF), accentSoft = Color(0xFF15284D),
        marker = Color(0xFF15284D), onMarker = Color(0xFFE8ECF1),
        needsYou = StatusColors(Color(0xFFFFB224), Color(0xFFFFC75E), Color(0xFF3A2B0C), Color(0xFF2B1D00)),
        working = StatusColors(Color(0xFF4C8DFF), Color(0xFF7FAEFF), Color(0xFF15284D)),
        done = StatusColors(Color(0xFF35C46C), Color(0xFF5ED68C), Color(0xFF103020), Color(0xFF04210F)),
        idle = StatusColors(Color(0xFF5E6976), Color(0xFF98A3B2), Color(0xFF22272E)),
        offline = StatusColors(Color(0xFF454D58), Color(0xFF677283), Color(0xFF1A1E23)),
        danger = StatusColors(Color(0xFFFF5C5C), Color(0xFFFF8585), Color(0xFF3A1717)),
        positive = Color(0xFF35C46C), caution = Color(0xFFFFB224), cautionText = Color(0xFFFFC75E),
        diffAdd = Color(0xFF35C46C), diffDel = Color(0xFFFF6B6B),
        terminalBackground = Color(0xFF07090B), terminalText = Color(0xFFE6E3DA), scrim = Color(0x99000000), dark = true,
    )

    fun of(style: AppStyle, dark: Boolean): PersonaPalette = when (style) {
        AppStyle.Glass -> if (dark) GlassDark else GlassLight
        AppStyle.Brief -> if (dark) BriefDark else BriefLight
        AppStyle.Board -> if (dark) BoardDark else BoardLight
    }
}

@Immutable
data class PersonaColors(
    val brand: Color,
    val danger: Color,
    val positive: Color,
    val caution: Color,
    val cautionText: Color,
    val ai: Color,
    val loop: Color,
    val countBadge: Color,
    val onCountBadge: Color,
    val codeBackground: Color,
    val terminalBackground: Color,
    val terminalText: Color,
)

/** The older colour set, now derived from the active style so existing call sites follow it. */
internal fun PersonaPalette.legacy() = PersonaColors(
    brand = accent, danger = danger.dot, positive = positive, caution = caution, cautionText = cautionText,
    ai = ai, loop = working.dot, countBadge = needsYou.dot, onCountBadge = needsYou.onDot,
    codeBackground = sunken, terminalBackground = terminalBackground, terminalText = terminalText,
)

/**
 * A role colour for something that is not an agent's state: good, needs a look, broken, or plain. Extensions' views
 * (docs/extensions.md「声明式 UI」) name these four and never a colour.
 */
enum class Tone { Neutral, Positive, Caution, Danger }

/** Where an agent was opened, each on its own tint ([PersonaPalette.source]): a Feishu group's topic or a terminal pane. */
enum class SourceKind { Feishu, Terminal }

/** [tone]'s colours: positive is the done green, caution the needs-you amber, danger red, neutral the idle grey. */
fun PersonaPalette.tone(tone: Tone): StatusColors = when (tone) {
    Tone.Positive -> positiveRole ?: done
    Tone.Caution -> cautionRole ?: needsYou
    Tone.Danger -> danger
    Tone.Neutral -> idle
}

/**
 * An agent's state (docs/agent-states.md). Pages show [localizedLabel] (or [labelRes] outside composition); [label]
 * stays the Simplified Chinese name for the code outside composition that still reads it.
 */
enum class Status(val label: String) {
    NeedsYou("需要你"), Working("工作中"), Unread("完成未看"), Idle("已看"),
    Offline("未运行"), Unknown("未知"), Waiting("待唤醒"), Muted("已屏蔽");

    @get:StringRes val labelRes: Int get() = when (this) {
        NeedsYou -> R.string.designsystem_status_needs_you
        Working -> R.string.designsystem_status_working
        Unread -> R.string.designsystem_status_unread
        Idle -> R.string.designsystem_status_idle
        Offline -> R.string.designsystem_status_offline
        Unknown -> R.string.designsystem_status_unknown
        Waiting -> R.string.designsystem_status_waiting
        Muted -> R.string.designsystem_status_muted
    }

    /** The name in the page's language. */
    val localizedLabel: String @Composable get() = stringResource(labelRes)

    /** The name in [context]'s language, for notifications and other code outside composition. */
    fun label(context: Context): String = context.getString(labelRes)
}

@Immutable
data class StatusTone(val dot: Color, val text: Color, val tint: Color)

/** Status colours of the active style. Prefer [PersonaTheme.palette] in new code. */
fun statusTone(status: Status, darkTheme: Boolean): StatusTone =
    Palettes.of(ActiveStyle.value, darkTheme).status(status).let { StatusTone(it.dot, it.text, it.tint) }

/** A diff's words that changed in a line (UI version 5, #3136): the add / delete colour, deeper than the line's own tint. */
val PersonaPalette.diffAddWord: Color get() = diffAdd.copy(alpha = 0.30f)
val PersonaPalette.diffDelWord: Color get() = diffDel.copy(alpha = 0.30f)
