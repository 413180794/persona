package persona.feature.chat

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.res.stringResource
import persona.core.contracts.SessionKey
import persona.core.contracts.api.MainConversationSender
import persona.core.contracts.api.PendingMainConversation
import persona.core.designsystem.LocalAppFeedback
import persona.core.navigation.AppTab
import persona.core.navigation.ChatRoute
import persona.core.navigation.ConversationKind
import persona.core.navigation.Navigator
import java.util.UUID
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

/** How long "Hand to Persona" waits for the main conversation to be known before it stops following the message. */
private const val BIND_TIMEOUT_MILLIS = 60_000L

/**
 * "Hand to Persona" from a message (R4, decided: into the main conversation, as everywhere else): queues [text] for the
 * main Persona conversation through the outbox ([operationId] keeps a retry from sending twice), calls [onQueued] (the
 * Persona tab), then [onBound] with the conversation once the outbox knows which it is, so the page can open it.
 * Returns that conversation, or null when it was not known in time.
 */
suspend fun handToPersona(
    sender: MainConversationSender, text: String, operationId: String,
    onQueued: () -> Unit, onBound: (SessionKey) -> Unit,
): SessionKey? {
    if (sender.outbox.value.none { it.clientMessageId == operationId }) sender.sendToMain(text, operationId)
    onQueued()
    val target = withTimeoutOrNull(BIND_TIMEOUT_MILLIS) {
        sender.outbox.map { messages ->
            messages.firstOrNull { it.clientMessageId == operationId }?.session?.takeIf {
                it != PendingMainConversation && it.machineId.isNotBlank() && it.sessionId.isNotBlank()
            }
        }.filterNotNull().first()
    }
    target?.let(onBound)
    return target
}

/**
 * The "Hand to Persona" a chat page offers on its messages: queue the message for the main conversation, switch to the
 * Persona tab and open it there. Null without a [sender] (the page then leaves the action out).
 */
@Composable
fun rememberHandToPersona(sender: MainConversationSender?, navigator: Navigator): ((String) -> Unit)? {
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    val failed = stringResource(R.string.chat_handoff_failed)
    return remember(sender, navigator, scope, feedback, failed) {
        sender?.let { main ->
            { text: String ->
                scope.launch {
                    try {
                        handToPersona(main, text, UUID.randomUUID().toString(),
                            onQueued = { navigator.selectTab(AppTab.Persona) },
                            onBound = { navigator.navigate(ChatRoute(it.machineId, it.sessionId, kind = ConversationKind.Assistant)) })
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        if (feedback?.failure(e) != true) feedback?.notice(failed)
                    }
                }
                Unit
            }
        }
    }
}
