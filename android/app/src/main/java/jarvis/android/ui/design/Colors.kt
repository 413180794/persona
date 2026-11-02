package jarvis.android.ui.design

import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color
import jarvis.android.data.AgentState

/**
 * One agent state's colors (apple/Shared/StatusPalette.swift): [dot] marks it (dots, symbols, filled shapes), [text]
 * is the same hue dark enough for small text on the page, [tint] the soft background behind either.
 */
@Immutable
data class StatusTone(val dot: Color, val text: Color, val tint: Color)

private fun hex(v: Long) = Color(0xFF000000 or v)

/**
 * The agent-state colors (docs/agent-states.md), the same table as iOS, the widget and the notifications: needs you
 * red, working amber, finished-unseen green, seen gray. They do not change with the 界面风格.
 */
object StatusPalette {
    fun needsYou(dark: Boolean) = if (dark) StatusTone(hex(0xFF6369), hex(0xFF6369), hex(0xFF6369).copy(alpha = 0.16f))
    else StatusTone(hex(0xE5484D), hex(0xC62B33), hex(0xE5484D).copy(alpha = 0.12f))

    fun working(dark: Boolean) = if (dark) StatusTone(hex(0xFFB224), hex(0xFFB224), hex(0xFFB224).copy(alpha = 0.16f))
    else StatusTone(hex(0xE8890C), hex(0xA35A00), hex(0xE8890C).copy(alpha = 0.14f))

    fun unread(dark: Boolean) = if (dark) StatusTone(hex(0x4CC38A), hex(0x4CC38A), hex(0x4CC38A).copy(alpha = 0.16f))
    else StatusTone(hex(0x30A46C), hex(0x1D7A4C), hex(0x30A46C).copy(alpha = 0.14f))

    fun idle(dark: Boolean) = if (dark) StatusTone(hex(0x8E8E93).copy(alpha = 0.75f), hex(0x9A9AA0), hex(0x8E8E93).copy(alpha = 0.18f))
    else StatusTone(hex(0x8E8E93).copy(alpha = 0.75f), hex(0x636367), hex(0x8E8E93).copy(alpha = 0.14f))

    fun offline(dark: Boolean) = if (dark) StatusTone(hex(0x8E8E93).copy(alpha = 0.4f), hex(0x9A9AA0), hex(0x8E8E93).copy(alpha = 0.1f))
    else StatusTone(hex(0x8E8E93).copy(alpha = 0.4f), hex(0x636367), hex(0x8E8E93).copy(alpha = 0.08f))

    fun of(state: AgentState, dark: Boolean): StatusTone = when (state) {
        AgentState.NeedsYou -> needsYou(dark)
        AgentState.Working -> working(dark)
        AgentState.Unread -> unread(dark)
        AgentState.Idle, AgentState.Unknown -> idle(dark)
        AgentState.Offline -> offline(dark)
    }
}

/** The ChatGPT-style shell's neutral colors (iOS `Theme.Shell`), the same in every 界面风格. */
@Immutable
data class ShellColors(
    /** The assistant's user bubble: a soft gray with ordinary text. */
    val userBubble: Color,
    /** The 助手 | Agents switch's selected segment. */
    val segment: Color,
    /** The composer's field and its expanded card. */
    val composer: Color,
    val composerStroke: Color,
    /** Send's round button: ink on paper. */
    val ink: Color,
    val inkText: Color,
    /** The drawer behind the page. */
    val drawer: Color,
    /** The 新对话 pill at the drawer's foot. */
    val newChat: Color,
) {
    companion object {
        fun of(dark: Boolean) = if (dark) ShellColors(
            userBubble = hex(0x2F2F2F), segment = hex(0x3A3A3C), composer = hex(0x1F1F21), composerStroke = hex(0x3A3A3C),
            ink = hex(0xF2F2F2), inkText = hex(0x0D0D0D), drawer = hex(0x171717), newChat = hex(0x3B7BF2),
        ) else ShellColors(
            userBubble = hex(0xF2F2F2), segment = hex(0xE6E6E6), composer = hex(0xFFFFFF), composerStroke = hex(0xE3E3E3),
            ink = hex(0x0D0D0D), inkText = hex(0xFFFFFF), drawer = hex(0xF9F9F9), newChat = hex(0x2F6FED),
        )
    }
}

/** Diffs and Git (iOS `Theme.Diff`): their own colors, never the state colors. */
@Immutable
data class DiffColors(val add: Color, val del: Color, val addFill: Color, val delFill: Color, val changed: Color) {
    companion object {
        fun of(dark: Boolean) = if (dark) DiffColors(hex(0x4CC38A), hex(0xFF6369), hex(0x4CC38A).copy(alpha = 0.14f), hex(0xFF6369).copy(alpha = 0.12f), hex(0xE3B341))
        else DiffColors(hex(0x1D7A4C), hex(0xC62B33), hex(0x30A46C).copy(alpha = 0.14f), hex(0xE5484D).copy(alpha = 0.12f), hex(0x8A6A00))
    }
}

/** Categories that only need telling apart (memory types, conversation marks): iOS `Theme.Hue`. */
@Immutable
data class HueColors(val blue: Color, val orange: Color, val green: Color, val purple: Color) {
    val all get() = listOf(orange, purple, blue, green)

    companion object {
        fun of(dark: Boolean) = if (dark) HueColors(hex(0x5B9BFF), hex(0xFF9F0A), hex(0x4CC38A), hex(0x9D8BFF))
        else HueColors(hex(0x2E6FD0), hex(0xB35400), hex(0x1D7A4C), hex(0x7A5AF8))
    }
}

/**
 * Other products' own colors where they mark their product (iOS `Theme.Brand`): Gmail's red and Feishu's blue on a
 * mail account's mark, and Google's sign-in button as its branding guidelines draw it (developers.google.com/identity/
 * branding-guidelines): the words on white with a gray outline in light, on near-black in dark; not the app's accent.
 */
@Immutable
data class BrandColors(val gmail: Color, val feishu: Color, val googleFill: Color, val googleText: Color, val googleStroke: Color) {
    companion object {
        fun of(dark: Boolean) = BrandColors(
            gmail = hex(0xEA4335), feishu = hex(0x3370FF),
            googleFill = if (dark) hex(0x131314) else hex(0xFFFFFF),
            googleText = if (dark) hex(0xE3E3E3) else hex(0x1F1F1F),
            googleStroke = if (dark) hex(0x8E918F) else hex(0x747775),
        )
    }
}
