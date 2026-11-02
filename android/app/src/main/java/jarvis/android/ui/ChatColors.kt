package jarvis.android.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

/**
 * The chat page's semantic colors, derived from the Material scheme (so dynamic color and dark mode follow) plus the
 * few fixed hues iOS uses: diff green / red, the working green, the AI violet.
 */
@Immutable
data class ChatColors(
    val label: Color,
    val secondaryLabel: Color,
    val tertiaryLabel: Color,
    val accent: Color,
    val danger: Color,
    val positive: Color,
    val working: Color,
    val ai: Color,
    val page: Color,
    val card: Color,
    val fillWell: Color,
    val fillChip: Color,
    val codeBackground: Color,
    val cardStroke: Color,
    val separator: Color,
    val diffAdd: Color,
    val diffAddFill: Color,
    val diffDel: Color,
    val diffDelFill: Color,
    val userBubble: Color,
    val userBubbleText: Color,
)

/**
 * The Jarvis conversation in the shell (docs/android.md「外壳」, iOS `\.shellChat`): neutral bubbles like ChatGPT's,
 * whatever the style; the Agents chats keep the style's own.
 */
val LocalShellChat = staticCompositionLocalOf { false }

val chatColors: ChatColors
    @Composable @ReadOnlyComposable get() {
        val s = MaterialTheme.colorScheme
        val p = JTheme.colors
        val dark = p.dark
        return ChatColors(
            label = s.onSurface,
            secondaryLabel = s.onSurfaceVariant,
            tertiaryLabel = s.onSurfaceVariant.copy(alpha = 0.6f),
            accent = s.primary,
            danger = s.error,
            positive = p.positive,
            working = jarvis.android.data.AgentState.Working.color,
            ai = p.ai,
            page = p.page,
            card = p.card,
            fillWell = p.fillWell,
            fillChip = p.fillChip,
            codeBackground = p.codeBackground,
            cardStroke = p.cardStroke,
            separator = p.separator,
            diffAdd = JTheme.diff.add,
            diffAddFill = JTheme.diff.addFill,
            diffDel = JTheme.diff.del,
            diffDelFill = JTheme.diff.delFill,
            // The style's own bubble (液态玻璃's pale blue with dark words, Claude's coral…), as on iOS.
            userBubble = if (LocalShellChat.current) JTheme.shell.userBubble else p.userBubble,
            userBubbleText = if (LocalShellChat.current) p.label else p.userBubbleText,
        )
    }

