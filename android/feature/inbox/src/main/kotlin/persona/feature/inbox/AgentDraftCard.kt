package persona.feature.inbox

import persona.core.designsystem.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.navigation.ChatRoute
import persona.core.navigation.Navigator
import kotlinx.coroutines.CoroutineScope

@Composable
internal fun AgentDraftCard(record: InboxRecord, composers: ReplyComposers, navigator: Navigator,
    onEditingChange: (Boolean) -> Unit = {},
) {
    val agent = record.agent ?: return
    if (agent.state == "running" || agent.state == "queued") {
        StatusLine(StatusLineState(waiting = agent.state == "queued", working = agent.state == "running",
            waitingDetail = "AI 分身等待处理", workDetail = agent.task))
        return
    }
    if (agent.state == "failed") {
        StatusText("AI 分身处理失败，你可以直接回复", isError = true)
        return
    }
    if (agent.state != "drafted" || agent.draft.isBlank()) return
    val composer = composers.get(ReplyTarget.Inbox(record.item.id), agent.draft)
    val state by composer.state.collectAsStateWithLifecycle()
    var editing by rememberSaveable(record.item.id) { mutableStateOf(false) }
    LaunchedEffect(state.sent) { if (state.sent) editing = false }
    val editingChanged by rememberUpdatedState(onEditingChange)
    SideEffect { editingChanged(editing) }
    DisposableEffect(Unit) { onDispose { editingChanged(false) } }
    DraftPreview(
        text = if (state.sent) "已发送" else state.draft.text.ifBlank { if (state.loading) "正在读取草稿…" else "草稿为空" },
        actions = {
            JButton("改一改", { editing = true }, kind = JButtonKind.Secondary, size = JButtonSize.Small, enabled = state.canEdit && !state.sent)
            JButton(state.actionLabel, composer::send, size = JButtonSize.Small,
                enabled = !state.loading && !state.sending && state.draft.text.isNotBlank())
        })
    ReplyStatus(state)
    if (editing) JSheet(onDismiss = { editing = false }, title = "修改回复") {
        Column(Modifier.fillMaxWidth().imePadding().verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            if (agent.basis.isNotBlank()) Text(agent.basis, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
            if (agent.machineId != null && agent.sessionId != null) {
                JButton("查看处理过程", { editing = false; navigator.navigate(ChatRoute(agent.machineId, agent.sessionId)) }, kind = JButtonKind.Secondary)
            }
            ReplyComposerField(state, composer::update, composer::send, "回复内容")
        }
    }
}

class ReplyComposers(private val repository: InboxRepository, private val scope: CoroutineScope) {
    private val composers = mutableMapOf<ReplyTarget, ReplyComposer>()
    @Synchronized fun get(target: ReplyTarget, initialText: String = "", replyRepository: InboxRepository = repository): ReplyComposer =
        composers.getOrPut(target) { ReplyComposer(replyRepository, target, scope, initialText) }
}

@Composable
internal fun rememberReplyComposers(repository: InboxRepository): ReplyComposers {
    val paired by repository.replyComposers.collectAsStateWithLifecycle()
    paired?.let { return it }
    if (repository is InboxExperienceRepository) return repository.composers
    val scope = rememberCoroutineScope()
    return remember(repository, scope) { ReplyComposers(repository, scope) }
}
