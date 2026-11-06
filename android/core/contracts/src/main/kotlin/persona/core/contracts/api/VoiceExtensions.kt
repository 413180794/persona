package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * An extension of the hub (`GET /api/v1/extensions`, docs/extensions.md「hub 的接口」): the voice page looks for a running
 * `kind == "voice"` one; 设置 › 扩展 shows them all. Hubs from before the extension base send only the first five fields.
 */
@Serializable
data class HubExtension(
    val id: String = "",
    val name: String = "",
    /** The first extension point it contributes to (`voice`, `connectors`…). */
    val kind: String = "",
    /** `disabled | needs_setup | incompatible | stopped | starting | running | error`. */
    val status: String = "",
    val error: String? = null,
    val version: String? = null,
    /** `builtin | hub.json | installed`; null from an older hub. */
    val source: String? = null,
    val enabled: Boolean? = null,
    val description: String? = null,
    /** The extension points it contributes to, with the ids. */
    val contributes: Map<String, List<String>>? = null,
    /** What it still needs before it runs, in words. */
    @SerialName("needs_setup") val needsSetup: List<String>? = null,
    val warnings: List<String>? = null,
    val sandbox: ExtensionSandbox? = null,
    /**
     * An extension from a market on a hub with auto-upgrades (docs/extensions.md「自动升级」): its own switch (null: not
     * from a market, or an older hub), its newer version and what happened last with an upgrade.
     */
    @SerialName("auto_upgrade") val autoUpgrade: Boolean? = null,
    val upgrade: ExtensionUpgrade? = null,
    @SerialName("upgrade_note") val upgradeNote: ExtensionUpgradeNote? = null,
    /** An extension with a program on computers: how that program is on each machine it is switched on for. */
    val nodes: List<ExtensionNodeState>? = null,
    /** Its sign-in (manifest `auth`, #4507), as the hub last saw it; null when it does not sign in, or an older hub. */
    val auth: ExtensionAuth? = null,
    /** The command-line programs this version runs with (manifest `tools`, #4507). */
    val tools: List<ExtensionToolView>? = null,
) {
    val isRunningVoice: Boolean get() = kind == "voice" && status == "running"
}

/** A market extension's newer version and where it stands (docs/extensions.md「自动升级」). */
@Serializable
data class ExtensionUpgrade(
    val id: String = "",
    val name: String? = null,
    val market: String = "",
    val from: String = "",
    val to: String = "",
    /** `available | waiting_idle | needs_permission | off | skipped | failed | incompatible`. */
    val state: String = "",
    @SerialName("added_permissions") val addedPermissions: List<ExtensionPermission>? = null,
    val detail: String? = null,
)

/** What happened last with an extension's upgrade: `upgraded | rolled_back | needs_permission | failed`. */
@Serializable
data class ExtensionUpgradeNote(
    val kind: String = "",
    val from: String = "",
    val to: String = "",
    val detail: String? = null,
    val at: String? = null,
)

/** An extension's program on one machine (docs/extensions.md「在电脑上运行的程序」). */
@Serializable
data class ExtensionNodeState(
    @SerialName("machine_id") val machineId: String = "",
    val version: String? = null,
    /** `running | starting | stopped | error | missing | lease_expired | offline | outdated`. */
    val state: String = "",
    val error: String? = null,
)

/** A voice (音色) a voice extension offers for one model (`GET /api/v1/extensions/{id}/voices?model=`). */
@Serializable
data class VoiceOption(
    val id: String = "",
    val name: String = "",
    /** `female`, `male` or absent. */
    val gender: String? = null,
    /** Language codes: `zh`, `en`… */
    val languages: List<String> = emptyList(),
)

@Serializable
data class VoiceOptions(val voices: List<VoiceOption> = emptyList(), val default: String? = null)

/** A realtime model a voice extension offers (`GET /api/v1/extensions/{id}/models`). */
@Serializable
data class VoiceModelOption(val id: String = "", val name: String? = null) {
    val label: String get() = name?.takeIf { it.isNotBlank() } ?: id
}

@Serializable
data class VoiceModelOptions(val models: List<VoiceModelOption> = emptyList(), val default: String? = null)

/**
 * What the hub's model configuration says for a voice extension (the purpose `plugin.<id>`, docs/model-config.md): the
 * effective model, if one is set, and the purpose's status when it is not usable as it stands (`未验证` is usable).
 */
data class VoiceModelChoice(val model: String? = null, val status: String? = null)

/**
 * The paired hub's voice extensions, on its current route with its pinned client and token. Bound to one pairing like
 * [MachineMetricsApi]; calls after the pairing or the route changed fail with a cancellation.
 */
interface VoiceExtensions {
    /** The first running voice extension, or null: a hub without extensions (404) or without a running voice one. */
    suspend fun voiceExtension(): HubExtension?

    /** The voices of [model] (null: the extension's default model). */
    suspend fun voices(extensionId: String, model: String?): VoiceOptions

    /** The models the extension offers; an extension from before models could be picked (404) has only its own: empty. */
    suspend fun models(extensionId: String): VoiceModelOptions

    /**
     * The model configuration of `extension.<extensionId>`; null on an older hub without model configuration (404). Any
     * other failure is thrown: a newer hub that cannot be read is not the same as one without configuration.
     */
    suspend fun modelChoice(extensionId: String): VoiceModelChoice?

    /**
     * Opens the extension's socket (`/api/v1/extensions/{id}/ws`). The listener is called on a transport thread; closing
     * the socket ends the conversation.
     */
    fun connect(extensionId: String, listener: VoiceSocketListener): VoiceSocket
}

/** One open voice conversation's socket: JSON text frames and PCM16 binary frames (docs/extensions.md「语音」). */
interface VoiceSocket {
    fun send(text: String): Boolean
    fun send(bytes: ByteArray, offset: Int = 0, length: Int = bytes.size): Boolean
    fun close(code: Int = 1000, reason: String? = null)
}

interface VoiceSocketListener {
    fun onOpen()
    fun onText(text: String)
    fun onBinary(bytes: ByteArray)
    /** The extension or the hub closed the socket; 1001 is an extension restarting (a deploy). */
    fun onClosed(code: Int, reason: String)
    /** The socket failed or was refused; [status] is the HTTP status of a refused upgrade (404: no such extension). */
    fun onFailure(error: Throwable, status: Int?)
}
