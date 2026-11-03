package persona.feature.chat

import persona.core.contracts.api.ChatRecoveryService

/** The draft owner supplies a user-facing reason and clears it only after a successful save. */
data class ChatStatusBindings(
    val recovery: ChatRecoveryService? = null,
    val draftSaveError: String? = null,
)
