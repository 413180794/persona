package persona.core.contracts.api

import kotlinx.serialization.Serializable

/** A plugin of the hub (`GET /api/v1/plugins`, docs/plugins.md); only `kind == "voice"` means anything to the app. */
@Serializable
data class HubPlugin(
    val id: String = "",
    val name: String = "",
    val kind: String = "",
    /** `starting | running | error | disabled`. */
    val status: String = "",
    val error: String? = null,
) {
    val isRunningVoice: Boolean get() = kind == "voice" && status == "running"
}

/** A voice (音色) a voice plugin offers for one model (`GET /api/v1/plugins/{id}/voices?model=`). */
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

/** A realtime model a voice plugin offers (`GET /api/v1/plugins/{id}/models`). */
@Serializable
data class VoiceModelOption(val id: String = "", val name: String? = null) {
    val label: String get() = name?.takeIf { it.isNotBlank() } ?: id
}

@Serializable
data class VoiceModelOptions(val models: List<VoiceModelOption> = emptyList(), val default: String? = null)

/**
 * What the hub's model configuration says for a voice plugin (the purpose `plugin.<id>`, docs/model-config.md): the
 * effective model, if one is set, and the purpose's status when it is not usable as it stands (`未验证` is usable).
 */
data class VoiceModelChoice(val model: String? = null, val status: String? = null)

/**
 * The paired hub's voice plugins, on its current route with its pinned client and token. Bound to one pairing like
 * [MachineMetricsApi]; calls after the pairing or the route changed fail with a cancellation.
 */
interface VoicePlugins {
    /** The first running voice plugin, or null: a hub without plugins (404) or without a running voice one. */
    suspend fun voicePlugin(): HubPlugin?

    /** The voices of [model] (null: the plugin's default model). */
    suspend fun voices(pluginId: String, model: String?): VoiceOptions

    /** The models the plugin offers; a plugin from before models could be picked (404) has only its own: empty. */
    suspend fun models(pluginId: String): VoiceModelOptions

    /**
     * The model configuration of `plugin.<pluginId>`; null on an older hub without model configuration (404). Any
     * other failure is thrown: a newer hub that cannot be read is not the same as one without configuration.
     */
    suspend fun modelChoice(pluginId: String): VoiceModelChoice?

    /**
     * Opens the plugin's socket (`/api/v1/plugins/{id}/ws`). The listener is called on a transport thread; closing
     * the socket ends the conversation.
     */
    fun connect(pluginId: String, listener: VoiceSocketListener): VoiceSocket
}

/** One open voice conversation's socket: JSON text frames and PCM16 binary frames (docs/plugins.md「语音」). */
interface VoiceSocket {
    fun send(text: String): Boolean
    fun send(bytes: ByteArray, offset: Int = 0, length: Int = bytes.size): Boolean
    fun close(code: Int = 1000, reason: String? = null)
}

interface VoiceSocketListener {
    fun onOpen()
    fun onText(text: String)
    fun onBinary(bytes: ByteArray)
    /** The plugin or the hub closed the socket; 1001 is a plugin restarting (a deploy). */
    fun onClosed(code: Int, reason: String)
    /** The socket failed or was refused; [status] is the HTTP status of a refused upgrade (404: no such plugin). */
    fun onFailure(error: Throwable, status: Int?)
}
