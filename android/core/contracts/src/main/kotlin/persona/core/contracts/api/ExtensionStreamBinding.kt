package persona.core.contracts.api

/** Authenticated page ownership is local routing data, never extension-controlled JSON. */
data class ExtensionStreamBinding(
    val connection: HubConnection,
    val owner: String,
    val group: String,
    val manager: String? = null,
    /** Existing authenticated interaction resolver; the hub routes each ID to its persisted owning manager. */
    val approvals: ChatTransport? = null,
)
