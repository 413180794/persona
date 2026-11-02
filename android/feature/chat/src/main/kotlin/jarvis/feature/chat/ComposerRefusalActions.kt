package jarvis.feature.chat

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import jarvis.core.contracts.api.*
import jarvis.core.designsystem.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

@Composable
internal fun ComposerRefusalActions(
    message: OutboxMessage,
    service: ChatComposerService?,
    delivery: ComposerDelivery?,
    terminal: (() -> Unit)?,
    edit: () -> Unit,
    notice: (String) -> Unit,
) {
    val scope = rememberCoroutineScope()
    var confirm by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    val refusal = delivery?.refusal
    val code = refusal?.code ?: message.error
    fun retry(clear: Boolean) {
        if (service == null || busy) return
        busy = true
        scope.launch {
            try { service.retryRefused(message.clientMessageId, clear) }
            catch (e: CancellationException) { throw e }
            catch (e: Exception) { notice(operationFeedback(e).message) }
            finally { busy = false }
        }
    }
    Column {
        when (code) {
            "input_draft" -> Text(if (refusal?.clearable.isNullOrEmpty())
                "电脑上有未发送的草稿，请先到终端处理。手机输入已保留。" else
                "电脑上有未发送的草稿，可以确认后清空并发送。", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
            "dialog_open" -> Text("电脑上的对话框挡住了输入，请先处理下方交互面板或打开终端。", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
        }
        Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            if (code == "input_draft" && !refusal?.clearable.isNullOrEmpty()) JButton("清空并发送", { confirm = true }, size = JButtonSize.Small, kind = JButtonKind.Secondary, enabled = !busy)
            if (terminal != null) JButton("打开终端", terminal, size = JButtonSize.Small, kind = JButtonKind.Secondary, enabled = !busy)
            JButton("编辑", edit, size = JButtonSize.Small, kind = JButtonKind.Secondary, enabled = !busy)
        }
    }
    if (confirm) JDialog(onDismiss = { confirm = false }, title = "清空电脑草稿并发送？",
        content = {
            Column(Modifier.heightIn(max = 280.dp).verticalScroll(rememberScrollState())) {
                Text("将清空以下电脑草稿，再发送刚才被拒绝的消息：", style = JarvisTheme.type.callout)
                JCodeBlock(refusal?.draft ?: refusal?.clearable.orEmpty(), Modifier.padding(vertical = Tokens.Space.s))
                Text("如果电脑草稿已经变化，会再次拒绝发送。", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
            }
        },
        confirm = "清空并发送" to { confirm = false; retry(true) }, destructive = true)
}
