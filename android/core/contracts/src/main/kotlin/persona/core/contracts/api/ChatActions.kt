package persona.core.contracts.api

import persona.core.contracts.Session
import persona.core.contracts.SessionInfo

sealed interface ChatAction {
    data object Interrupt : ChatAction
    data class Rename(val title: String) : ChatAction
    data class Mute(val muted: Boolean) : ChatAction
    data class Modes(val permissionMode: String? = null, val effort: String? = null) : ChatAction
    data object Close : ChatAction
    /** Join a Codex app-server thread live (POST …/attach); only for a session whose capabilities offer it. */
    data object Attach : ChatAction
}

sealed interface ChatActionResult {
    data object Done : ChatActionResult
    data object Unknown : ChatActionResult
    data class Failed(val reason: String, val cause: Throwable? = null) : ChatActionResult
}

/** Scope one instance to the paired hub. Unknown operations must be verified before another of the same kind. */
interface ChatActions {
    suspend fun isMuted(session: Session): Boolean
    suspend fun execute(session: Session, action: ChatAction): ChatActionResult
    suspend fun verify(session: Session, action: ChatAction): ChatActionResult

    /**
     * Pull everything queued while the agent works back out and re-queue the indexes in [keep], in order
     * (POST …/queue, the node's session.queue.edit); returns what was queued before the edit. Not idempotent and
     * not deduplicated by the hub: a failure is reported, never retried.
     */
    suspend fun editQueue(session: Session, keep: List<Int>): List<String> =
        throw UnsupportedOperationException("排队消息管理尚未接入")

    /** What the node reads from the session's transcript (GET …/info). */
    suspend fun info(session: Session): SessionInfo = throw UnsupportedOperationException("会话信息尚未接入")
}
