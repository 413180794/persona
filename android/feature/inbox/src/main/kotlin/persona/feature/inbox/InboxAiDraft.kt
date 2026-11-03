package persona.feature.inbox

import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.ChatRoute
import persona.core.navigation.Navigator
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch

/**
 * What AI 草拟 / AI 改 asks the hub for (the old app's wording): the user's request, then 「在这版上改：<当前草稿>」
 * when there is a draft to revise; either may be missing.
 */
fun draftInstruction(request: String, draft: String): String = listOfNotNull(
    request.trim().takeIf { it.isNotEmpty() },
    draft.trim().takeIf { it.isNotEmpty() }?.let { "在这版上改：$it" },
).joinToString("\n")

/** Replies whose last send has an unclear result are checked, never sent again: no confirmation for that. */
internal val ReplyComposerState.checksResult: Boolean
    get() = delivery in setOf(ReplyDelivery.Unknown, ReplyDelivery.NeedsConfirmation)

/**
 * A request and AI 草拟 (no draft yet) or AI 改 (a draft to revise). The hub's text replaces the reply as an AI draft
 * (sending it asks first); a failure says why and keeps what was there.
 */
@Composable
internal fun InboxAiDraftRow(repository: InboxRepository, itemId: String, composer: ReplyComposer, modifier: Modifier = Modifier) {
    val state by composer.state.collectAsStateWithLifecycle()
    var request by rememberSaveable(itemId) { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val revising = state.draft.text.isNotBlank()
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            JTextField(request, { request = it }, Modifier.weight(1f), placeholder = if (revising) "让 AI 改：…" else "要怎么回（可以不填）",
                singleLine = true, enabled = !busy && state.canEdit)
            JButton(if (revising) "AI 改" else "AI 草拟", {
                busy = true
                error = null
                val instruction = draftInstruction(request, state.draft.text)
                scope.launch {
                    try {
                        val text = repository.draftReply(itemId, instruction)
                        if (composer.replaceWithAi(text)) request = "" else error = "回复正在发送或待核对，稍后再改"
                    } catch (e: CancellationException) { throw e }
                    catch (e: Exception) { error = "没草拟出来：${failureReason(e)}" }
                    finally { busy = false }
                }
            }, kind = JButtonKind.Secondary, size = JButtonSize.Medium, icon = PhIcons.Sparkle,
                enabled = state.canEdit, loading = busy, loadingText = "草拟中…")
        }
        error?.let { JNotice(it, status = Status.NeedsYou) }
    }
}

/**
 * The reply to an item, AI-assisted (the old app's detail, the AI 分身 draft's 改一改): why the AI 分身 drafted it and
 * its conversation, the reply itself, and AI 草拟 / AI 改. Sending an AI draft asks first ([SendAsYouDialog]); the
 * user's own words go as typed. Feishu items reply in their chat, mail items to the newest message (POST …/reply).
 */
@Composable
internal fun InboxDraftSheet(
    repository: InboxRepository,
    record: InboxRecord,
    composer: ReplyComposer,
    navigator: Navigator?,
    onDismiss: () -> Unit,
) {
    val state by composer.state.collectAsStateWithLifecycle()
    var confirming by remember { mutableStateOf(false) }
    val palette = PersonaTheme.palette
    val agent = record.agent
    val dismiss by rememberUpdatedState(onDismiss)
    // Close once this sheet's reply has gone (not because an earlier one had when it opened).
    LaunchedEffect(composer) { composer.state.map { it.sent }.distinctUntilChanged().drop(1).collect { if (it) dismiss() } }
    JSheet(onDismiss = onDismiss, title = if (record.isLark) "回复 ${record.item.title}" else "回复邮件「${record.item.title}」",
        dismissible = !state.sending, scrollable = true) {
        // The sheet is opaque: the composer must not sample (and show) the page behind it as glass.
        CompositionLocalProvider(LocalGlassSource provides null) {
        Column(Modifier.fillMaxWidth().imePadding(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            if (agent != null && agent.state == "drafted" && agent.basis.isNotBlank()) {
                Text("AI 分身的依据：${agent.basis}", style = PersonaTheme.type.callout, color = palette.ink2)
            }
            if (agent?.machineId != null && agent.sessionId != null) {
                JButton("查看处理过程", { onDismiss(); navigator?.navigate(ChatRoute(agent.machineId, agent.sessionId)) },
                    kind = JButtonKind.Secondary, size = JButtonSize.Small, icon = PhIcons.ListChecks)
            }
            ReplyComposerField(state, composer::update, onSend = {
                if (!state.checksResult && state.draft.ai) confirming = true else composer.send()
            }, placeholder = "自己写，或者让 AI 草拟")
            InboxAiDraftRow(repository, record.item.id, composer)
        }
        }
    }
    if (confirming) SendAsYouDialog(sendConfirmation(record, state.draft.text),
        onSend = { confirming = false; composer.send() }, onDismiss = { confirming = false }, enabled = !state.sending)
}
