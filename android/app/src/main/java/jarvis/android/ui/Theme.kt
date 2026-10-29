package jarvis.android.ui

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import jarvis.android.data.AgentState

private val Brand = Color(0xFF2B5CE6)

@Composable
fun JarvisTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val ctx = LocalContext.current
    val scheme = when {
        Build.VERSION.SDK_INT >= 31 -> if (dark) dynamicDarkColorScheme(ctx) else dynamicLightColorScheme(ctx)
        dark -> darkColorScheme(primary = Color(0xFF9DB4FF))
        else -> lightColorScheme(primary = Brand)
    }
    MaterialTheme(colorScheme = scheme, content = content)
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
