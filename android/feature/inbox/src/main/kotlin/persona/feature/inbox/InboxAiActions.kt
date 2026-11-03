package persona.feature.inbox

import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.tooling.preview.Preview
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.DeliveryState
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

@Composable
internal fun InboxAiActions(repository: InboxRepository, itemId: String, context: String? = null,
    navigator: Navigator? = LocalInboxNavigator.current,
    modifier: Modifier = Modifier,
    onHandoffQueued: (() -> Unit)? = null,
    primaryAction: JAction? = null,
) {
    var loaded by remember(repository, itemId) { mutableStateOf<String?>(null) }
    LaunchedEffect(repository, itemId) {
        try { if (context == null) loaded = repository.personaContext(itemId) }
        catch (e: CancellationException) { throw e }
        catch (_: Exception) { }
    }
    val actions = listOf(InboxAiAction.Summarize to "总结", InboxAiAction.DraftReply to "拟回复", InboxAiAction.Handoff to "交给 Jarvis")
    val buttons = actions.map { (action, label) ->
        key(action) {
            rememberInboxAiButton(repository, itemId, action, label, context ?: loaded, navigator,
                onQueued = onHandoffQueued.takeIf { action == InboxAiAction.Handoff })
        }
    }
    if (primaryAction == null) InboxActionBar(buttons, modifier)
    else Column(modifier) {
        DecisionButtons(primary = primaryAction,
            secondary = JAction(buttons[2].label, PhIcons.Sparkle, enabled = buttons[2].enabled, onClick = buttons[2].onClick),
            more = buttons.take(2).map { JAction(it.label, PhIcons.Sparkle, enabled = it.enabled, onClick = it.onClick) })
        InboxActionProgress(buttons)
    }
}

internal data class InboxAiButtonState(
    val label: String,
    val enabled: Boolean = true,
    val pending: PendingStatus? = null,
    val failed: Boolean = false,
    val onClick: () -> Unit = {},
)

@Composable
internal fun rememberInboxAiButton(repository: InboxRepository, itemId: String, action: InboxAiAction, label: String,
    context: String?, navigator: Navigator?,
    onQueued: (() -> Unit)? = null,
): InboxAiButtonState {
    val scope = rememberCoroutineScope()
    val snapshot by repository.snapshot.collectAsStateWithLifecycle()
    val outbox by repository.personaOutbox.collectAsStateWithLifecycle()
    var saved by rememberSaveable(repository, snapshot.contentSource?.scope, itemId, action) { mutableStateOf<String?>(null) }
    var queued by rememberSaveable(repository, snapshot.contentSource?.scope, itemId, action) { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var failed by remember { mutableStateOf(false) }
    val request = saved?.let { JsonCodec.decodeFromString<InboxPersonaRequest>(it) }
    val delivery = outbox.firstOrNull { it.clientMessageId == request?.operationId }?.state
    val pending = when {
        busy -> PendingStatus.Sending
        failed -> PendingStatus.Refused
        queued -> when (delivery) {
            DeliveryState.Sending, DeliveryState.Queued -> PendingStatus.Sending
            DeliveryState.Unknown -> PendingStatus.Unconfirmed
            DeliveryState.Verifying -> PendingStatus.Verifying
            DeliveryState.NeedsConfirmation -> PendingStatus.NeedsReview
            DeliveryState.Refused, DeliveryState.Rejected -> PendingStatus.Refused
            DeliveryState.Sent -> null
            else -> PendingStatus.Waiting
        }
        else -> null
    }
    return InboxAiButtonState(label, !busy, pending, failed, onClick = {
        if (queued) {
            navigator?.selectTab(AppTab.Jarvis)
        } else {
            val operation = request ?: InboxPersonaRequest(itemId, action, context = context).also {
                saved = JsonCodec.encodeToString(InboxPersonaRequest.serializer(), it)
            }
            busy = true
            failed = false
            scope.launch {
                try {
                    sendInboxToPersona(repository, operation, navigator)
                    queued = true
                    onQueued?.invoke()
                } catch (e: CancellationException) { throw e }
                catch (_: Exception) { failed = true }
                finally { busy = false }
            }
        }
    })
}

@Composable
private fun InboxActionBar(actions: List<InboxAiButtonState>, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            actions.forEach { action ->
                JChip(action.label, icon = PhIcons.Sparkle,
                    onClick = action.onClick.takeIf { action.enabled },
                    status = if (action.pending != null) Status.Working else null)
            }
        }
        InboxActionProgress(actions)
    }
}

@Composable
private fun InboxActionProgress(actions: List<InboxAiButtonState>) {
    val pending = actions.filter { it.pending != null }
    if (pending.isNotEmpty()) JNotice(
        pending.joinToString("\n") { if (it.failed) "${it.label}请求保存失败，请重试" else "${it.label}：${it.pending?.label}" },
        status = if (pending.any { it.failed }) Status.NeedsYou else Status.Working,
    )
}

@Preview(showBackground = true, widthDp = 320)
@Preview(showBackground = true, widthDp = 320, fontScale = 1.5f)
@Composable
private fun InboxActionBarPreview() {
    PersonaTheme(dynamicColor = false) {
        InboxActionBar(listOf(InboxAiButtonState("总结", pending = PendingStatus.Sending),
            InboxAiButtonState("拟回复", pending = PendingStatus.Waiting),
            InboxAiButtonState("交给 Jarvis", pending = PendingStatus.NeedsReview)), Modifier.padding(Tokens.Space.m))
    }
}
