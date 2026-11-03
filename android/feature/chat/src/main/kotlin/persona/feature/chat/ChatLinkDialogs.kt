package persona.feature.chat

import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import persona.core.designsystem.JDialog
import persona.core.designsystem.JTextField
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/**
 * Text picked on a page and what to do with it: the first 120 characters of the selection, a line that may stay empty
 * (the message then says a default), 发送. [onSend] closes the dialog when it is done; a failure stays here to retry.
 */
@Composable
internal fun LinkPickDialog(
    title: String,
    selection: ChatLinkContext,
    placeholder: String,
    onDismiss: () -> Unit,
    onSend: suspend (String) -> Unit,
) {
    var instruction by remember(selection.id) { mutableStateOf("") }
    var sending by remember(selection.id) { mutableStateOf(false) }
    var failed by remember(selection.id) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val palette = PersonaTheme.palette
    JDialog(
        onDismiss = { if (!sending) onDismiss() }, title = title,
        confirm = (if (sending) "发送中" else "发送") to {
            if (!sending) {
                sending = true; failed = false
                scope.launch {
                    try { onSend(instruction.trim()) }
                    catch (e: CancellationException) { throw e }
                    catch (_: Exception) { failed = true }
                    finally { sending = false }
                }
            }
        },
        dismiss = "取消" to { if (!sending) onDismiss() },
        confirmEnabled = !sending,
    ) {
        Text(selection.text.take(120), style = PersonaTheme.type.callout, color = palette.ink2, maxLines = 4, overflow = TextOverflow.Ellipsis)
        Spacer(Modifier.height(Tokens.Space.m))
        JTextField(instruction, { instruction = it }, placeholder = placeholder, enabled = !sending, maxLines = 5)
        if (failed) {
            Spacer(Modifier.height(Tokens.Space.s))
            Text("没有发出去，请再试一次", style = PersonaTheme.type.caption, color = palette.danger.text)
        }
    }
}

/**
 * 交给 Persona for text picked on a page with no conversation behind it (the knowledge base, the inbox's pages): the
 * selection as a `<jarvis-context>` and what Persona is to do with it (「看看上面这段。」 when nothing is written) go to
 * [send], the app's main conversation.
 */
@Composable
fun HandToPersonaDialog(selection: ChatLinkContext, onDismiss: () -> Unit, send: suspend (String) -> Unit) =
    LinkPickDialog("交给 Persona", selection, "要 Persona 做什么？", onDismiss) { instruction ->
        send(ChatLinkContext.handToPersonaMessage(selection, instruction))
    }
