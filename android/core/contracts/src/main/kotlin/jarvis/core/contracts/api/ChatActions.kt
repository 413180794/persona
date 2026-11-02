package jarvis.core.contracts.api

import jarvis.core.contracts.Session

sealed interface ChatAction {
    data object Interrupt : ChatAction
    data class Rename(val title: String) : ChatAction
    data class Mute(val muted: Boolean) : ChatAction
    data class Modes(val permissionMode: String? = null, val effort: String? = null) : ChatAction
    data object Close : ChatAction
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
}
