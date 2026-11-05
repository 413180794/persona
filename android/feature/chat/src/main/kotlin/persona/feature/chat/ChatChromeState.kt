package persona.feature.chat

import androidx.compose.ui.platform.LocalResources
import android.content.res.Resources
import androidx.compose.runtime.*
import persona.core.designsystem.operationFeedback
import persona.core.domain.FailureAction
import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import persona.core.domain.sort.SessionRename
import persona.core.domain.sort.SessionRenamer

internal fun spaceCompanions(session: Session?, sessions: List<Session>): List<Session> {
    if (session == null) return emptyList()
    val space = session.spaceId?.takeIf(String::isNotBlank) ?: session.workspaceId?.takeIf(String::isNotBlank)
    return sessions.filter { other ->
        other.machineId == session.machineId && other.key != session.key && !other.assistant &&
            if (space != null) (other.spaceId?.takeIf(String::isNotBlank) ?: other.workspaceId) == space
            else !session.spaceName.isNullOrBlank() && other.spaceName == session.spaceName
    }.sortedWith(compareBy<Session> { it.agentState.priority }.thenBy { it.slotIndex ?: Int.MAX_VALUE }.thenBy { it.sessionId })
}

internal fun connectionTitle(res: Resources, state: ConnectionState?): String? = when (state) {
    null, ConnectionState.Online -> null
    ConnectionState.Connecting -> res.getString(R.string.chat_connection_connecting)
    ConnectionState.Updating -> res.getString(R.string.chat_connection_updating)
    ConnectionState.WaitingForNetwork -> res.getString(R.string.chat_connection_waiting_network)
    ConnectionState.NeedsLocalNetworkPermission -> res.getString(R.string.chat_connection_local_network)
    ConnectionState.CertificateMismatch -> res.getString(R.string.chat_connection_certificate)
    ConnectionState.Revoked -> res.getString(R.string.chat_connection_revoked)
}

/** The header's word for a connection that is not online, in the page's language. */
@Composable
internal fun connectionTitle(state: ConnectionState?): String? = connectionTitle(LocalResources.current, state)

internal class ChatChromeState(
    private val res: Resources,
    private val actions: ChatActions?, private val scope: CoroutineScope,
    private val refresh: suspend () -> Unit, private val onClosed: () -> Unit,
    private val renamer: SessionRenamer? = null,
) {
    var busy by mutableStateOf(false); private set
    var muted by mutableStateOf<Boolean?>(null); private set
    var notice by mutableStateOf<String?>(null)
    var pending by mutableStateOf<ChatAction?>(null); private set
    val available get() = actions != null && !busy && pending == null
    val canRename get() = renamer != null && !busy && pending == null

    fun rename(session: Session, title: String?, tabName: String?) {
        val service = renamer ?: return
        val edit = SessionRename.changes(session, title, tabName)
        if (!canRename || !edit.changed) return
        busy = true
        scope.launch {
            try {
                if (service.rename(session, edit.title, edit.tabName)) notice = res.getString(R.string.chat_action_renamed)
            } catch (error: CancellationException) { throw error }
            catch (error: Exception) { notice = operationFeedback(error).message }
            finally { busy = false }
        }
    }

    /** 会话信息 (GET …/info); fails when the actions are not wired, which the sheet says. */
    suspend fun info(session: Session): SessionInfo =
        actions?.info(session) ?: throw UnsupportedOperationException(res.getString(R.string.chat_action_info_unavailable))

    suspend fun loadMute(session: Session) {
        try { muted = actions?.isMuted(session) }
        catch (error: CancellationException) { throw error }
        catch (_: Exception) { muted = null }
    }

    fun run(session: Session, action: ChatAction, verify: Boolean = false) {
        val service = actions ?: return
        if (busy || (!verify && pending != null)) return
        busy = true
        scope.launch {
            try {
                when (val result = if (verify) service.verify(session, action) else service.execute(session, action)) {
                    ChatActionResult.Done -> {
                        pending = null
                        notice = res.getString(when (action) {
                            ChatAction.Interrupt -> R.string.chat_action_interrupt_sent
                            is ChatAction.Rename -> R.string.chat_action_renamed
                            is ChatAction.Mute -> if (action.muted) R.string.chat_action_muted else R.string.chat_action_unmuted
                            is ChatAction.Modes -> R.string.chat_action_modes_updated
                            ChatAction.Close -> R.string.chat_action_closed
                            ChatAction.Attach -> R.string.chat_action_attached
                            ChatAction.Delete -> R.string.chat_action_deleted
                        })
                        if (action is ChatAction.Mute) muted = action.muted
                        if (action == ChatAction.Close || action == ChatAction.Delete) onClosed()
                        else try { refresh() } catch (error: CancellationException) { throw error }
                        catch (_: Exception) { notice = res.getString(R.string.chat_action_done_refresh_later) }
                    }
                    ChatActionResult.Unknown -> { pending = action; notice = res.getString(R.string.chat_action_unknown) }
                    is ChatActionResult.Failed -> {
                        val feedback = result.cause?.let { operationFeedback(it) }
                        pending = action.takeIf { feedback?.action == FailureAction.Verify }
                        notice = feedback?.message ?: result.reason
                    }
                }
            } catch (error: CancellationException) { throw error }
            catch (error: Exception) {
                val feedback = operationFeedback(error)
                pending = action.takeIf { feedback.action == FailureAction.Verify }
                notice = feedback.message
            }
            finally { busy = false }
        }
    }
}
