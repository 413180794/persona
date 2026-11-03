package persona.feature.terminal

import android.content.ClipData
import android.content.ClipboardManager
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/**
 * 选择文本: the terminal (the rows kept while driving it, then the screen) as plain text over the whole screen. Rows
 * stay rows (monospaced, never folded, scrolled both ways) so columns and frames line up as on screen; a long press
 * selects any part, 全部拷贝 copies everything. iOS selects on the grid itself; Android borrows a text page, as the old
 * Android app did.
 */
@Composable
internal fun TerminalSelectText(text: String, onDismiss: () -> Unit) {
    val context = LocalContext.current
    val feedback = LocalAppFeedback.current
    val palette = PersonaTheme.palette
    JFullScreen(onDismiss) {
        JPage("选择文本", onBack = onDismiss, actions = {
            JTextButton("全部拷贝", {
                context.getSystemService(ClipboardManager::class.java)?.setPrimaryClip(ClipData.newPlainText("终端", text))
                feedback?.notice("已拷贝")
                onDismiss()
            }, enabled = text.isNotEmpty())
        }) { padding ->
            if (text.isEmpty()) Box(Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                JEmptyState("终端里还没有文字", "等终端显示内容后再来选择。", icon = PhIcons.TerminalWindow)
            } else SelectionContainer(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).horizontalScroll(rememberScrollState())) {
                Text(text, style = PersonaTheme.type.code, color = palette.ink, softWrap = false,
                    modifier = Modifier.padding(start = pagePadding, end = pagePadding,
                        top = padding.calculateTopPadding() + Tokens.Space.s, bottom = padding.calculateBottomPadding() + Tokens.Space.l))
            }
        }
    }
}
