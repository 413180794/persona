package persona.feature.chat

import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import persona.core.designsystem.JDialog
import persona.core.designsystem.JTextField
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/**
 * Text picked on a page and what to do with it: the first 120 characters of the selection, a line that may stay empty
 * (the message then says a default), "Send". [onSend] closes the dialog when it is done; a failure stays here to retry.
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
        confirm = stringResource(if (sending) R.string.chat_dialog_sending else R.string.chat_dialog_send) to {
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
        dismiss = stringResource(R.string.chat_dialog_cancel) to { if (!sending) onDismiss() },
        confirmEnabled = !sending,
    ) {
        Text(selection.text.take(120), style = PersonaTheme.type.callout, color = palette.ink2, maxLines = 4, overflow = TextOverflow.Ellipsis)
        Spacer(Modifier.height(Tokens.Space.m))
        JTextField(instruction, { instruction = it }, placeholder = placeholder, enabled = !sending, maxLines = 5)
        if (failed) {
            Spacer(Modifier.height(Tokens.Space.s))
            Text(stringResource(R.string.chat_dialog_send_failed), style = PersonaTheme.type.caption, color = palette.danger.text)
        }
    }
}

/**
 * "Hand to Persona" for text picked on a page with no conversation behind it (the knowledge base, the inbox's pages): the
 * selection as a `<persona-context>` and what Persona is to do with it ("Take a look at the above." when nothing is written) go to
 * [send], the app's main conversation.
 */
@Composable
fun HandToPersonaDialog(selection: ChatLinkContext, onDismiss: () -> Unit, send: suspend (String) -> Unit) {
    val resources = LocalResources.current
    LinkPickDialog(stringResource(R.string.chat_dialog_hand_title), selection, stringResource(R.string.chat_dialog_hand_hint), onDismiss) { instruction ->
        send(ChatLinkContext.handToPersonaMessage(selection, instruction, resources))
    }
}
