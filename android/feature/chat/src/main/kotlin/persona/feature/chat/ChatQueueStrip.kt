package persona.feature.chat

import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import persona.core.contracts.Session
import persona.core.contracts.api.ChatActions
import persona.core.designsystem.JAction
import persona.core.designsystem.JActionSheet
import persona.core.designsystem.QueueStrip
import persona.core.designsystem.icons.PhIcons
import persona.core.designsystem.operationFeedback
import persona.core.domain.sort.canSend
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** 「排队中 2 条，Claude 忙完会依次处理」: how many wait, and for whom. */
internal fun queueTitle(count: Int, agent: String): String = "排队中 $count 条，$agent 忙完会依次处理"

/** What to put back when one queued message is pulled out: every other index, in order (an empty list is meaningful). */
internal fun queueKeep(size: Int, index: Int): List<Int> = (0 until size).filter { it != index }

/**
 * 排队条 (iOS QueueStrip): what was typed while the agent works and not taken yet, right above the composer. Tapping one
 * offers 撤回编辑 (back into the box, after the draft on a new line), 拷贝 and 删除. Editing goes through the node's
 * session.queue.edit (POST …/queue {keep}): it pulls the whole queue out of the agent and puts the rest back in order,
 * and answers with what was queued, so the pulled-back message is read from that answer. Only for a session the phone
 * may send to.
 */
@Composable
internal fun ChatQueueStrip(
    session: Session?,
    actions: ChatActions?,
    composer: ChatComposerState,
    onNotice: (String) -> Unit,
    refresh: suspend () -> Unit,
) {
    val queued = session?.queued.orEmpty()
    if (session == null || queued.isEmpty()) return
    val scope = rememberCoroutineScope()
    val clipboard = LocalClipboardManager.current
    var busy by remember { mutableStateOf(false) }
    // The row picked, with its words: if the queue moves under the open menu (the agent took one), the menu closes.
    var picked by remember { mutableStateOf<Pair<Int, String>?>(null) }
    LaunchedEffect(queued) { picked?.let { (index, text) -> if (queued.getOrNull(index) != text) picked = null } }
    val enabled = session.canSend && actions != null
    fun edit(index: Int, delete: Boolean) {
        val service = actions ?: return
        if (busy || !session.canSend) return
        busy = true
        scope.launch {
            try {
                val pulled = service.editQueue(session, queueKeep(queued.size, index))
                if (delete) onNotice("已从队列删除")
                else pulled.getOrNull(index)?.let { composer.restoreQueued(it); onNotice("已撤回到输入框，改好再发送") }
                    ?: onNotice("没找到这一条，队列可能已经变了，请核对后再试")
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { onNotice("没改成：${operationFeedback(e).message}") }
            finally {
                // Stay busy until the queue on screen is the new one, so a stale row cannot be picked.
                try { refresh() } catch (e: CancellationException) { throw e } catch (_: Exception) { }
                busy = false
            }
        }
    }
    QueueStrip(queueTitle(queued.size, session.agentLabel), queued.map(::composerMessagePreview),
        onItem = { index -> queued.getOrNull(index)?.let { picked = index to it } }, busy = busy, enabled = enabled)
    val (index, text) = picked ?: return
    JActionSheet(listOf(
        JAction("撤回编辑", PhIcons.ArrowUUpLeft, enabled = enabled && !busy) { edit(index, delete = false) },
        JAction("拷贝", PhIcons.Copy) { clipboard.setText(AnnotatedString(text)); onNotice("已拷贝") },
        JAction("删除", PhIcons.Trash, destructive = true, enabled = enabled && !busy) { edit(index, delete = true) },
    ), onDismiss = { picked = null }, title = composerMessagePreview(text))
}
