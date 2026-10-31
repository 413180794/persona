package jarvis.android.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
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

val chatColors: ChatColors
    @Composable @ReadOnlyComposable get() {
        val s = MaterialTheme.colorScheme
        val dark = isSystemInDarkTheme()
        return ChatColors(
            label = s.onSurface,
            secondaryLabel = s.onSurfaceVariant,
            tertiaryLabel = s.onSurfaceVariant.copy(alpha = 0.6f),
            accent = s.primary,
            danger = s.error,
            positive = Color(if (dark) 0xFF4ADE80 else 0xFF16A34A),
            working = jarvis.android.data.AgentState.Working.color(),
            ai = Color(if (dark) 0xFFC4B5FD else 0xFF7C3AED),
            page = s.surface,
            card = s.surfaceContainerLow,
            fillWell = s.surfaceContainerHigh,
            fillChip = s.surfaceContainerHighest,
            codeBackground = s.surfaceContainerHighest.copy(alpha = if (dark) 0.7f else 0.8f),
            cardStroke = s.outlineVariant.copy(alpha = 0.6f),
            separator = s.outlineVariant.copy(alpha = 0.5f),
            diffAdd = Color(if (dark) 0xFF4ADE80 else 0xFF15803D),
            diffAddFill = Color(if (dark) 0x2622C55E else 0x1F22C55E),
            diffDel = Color(if (dark) 0xFFF87171 else 0xFFB91C1C),
            diffDelFill = Color(if (dark) 0x26EF4444 else 0x1AEF4444),
            userBubble = s.primary,
            userBubbleText = s.onPrimary,
        )
    }

