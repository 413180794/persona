package jarvis.android.voice

import jarvis.android.data.ApiClient
import jarvis.android.data.HubException
import jarvis.android.data.JsonCodec
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject

/** A plugin of the hub (GET /api/v1/plugins, docs/plugins.md). */
@Serializable
data class Plugin(val id: String = "", val name: String = "", val kind: String = "", val status: String = "", val error: String? = null)

/** A voice (音色) a voice plugin offers (GET /api/v1/plugins/{id}/voices). */
@Serializable
data class Voice(val id: String = "", val name: String = "", val gender: String? = null, val languages: List<String> = emptyList())

@Serializable
data class Voices(val voices: List<Voice> = emptyList(), val default: String? = null)

/** A realtime model a voice plugin offers (GET /api/v1/plugins/{id}/models). */
@Serializable
data class VoiceModel(val id: String = "", val name: String? = null) {
    val label: String get() = name?.takeIf { it.isNotBlank() } ?: id
}

@Serializable
data class VoiceModels(val models: List<VoiceModel> = emptyList(), val default: String? = null)

@Serializable private data class Plugins(val plugins: List<Plugin> = emptyList())

/** The hub's voice plugin when one is running; null on a hub without plugins (404) or without a running voice one. */
suspend fun ApiClient.voicePlugin(): Plugin? = try {
    JsonCodec.decodeFromString<Plugins>(raw("GET", "/api/v1/plugins")).plugins.firstOrNull { it.kind == "voice" && it.status == "running" }
} catch (e: HubException) {
    if (e.status == 404) null else throw e
}

suspend fun ApiClient.voices(plugin: String, model: String?): Voices =
    JsonCodec.decodeFromString(raw("GET", "/api/v1/plugins/$plugin/voices", query = model?.let { mapOf("model" to it) } ?: emptyMap()))

/** A plugin from before models could be picked answers 404: then there is only its own. */
suspend fun ApiClient.voiceModels(plugin: String): VoiceModels = try {
    JsonCodec.decodeFromString(raw("GET", "/api/v1/plugins/$plugin/models"))
} catch (e: HubException) {
    if (e.status == 404) VoiceModels() else throw e
}

fun ApiClient.voiceSocketUrl(plugin: String): String = url("/api/v1/plugins/$plugin/ws").toString().replaceFirst("http", "ws")

/** What the plugin says on the voice socket (docs/plugins.md「语音」), its JSON frames decoded. */
sealed interface VoiceEvent {
    data class Ready(val inputRate: Int, val outputRate: Int) : VoiceEvent
    data object Clear : VoiceEvent
    data class Transcript(val id: String, val user: Boolean, val text: String, val final: Boolean) : VoiceEvent
    data class Tool(val id: String, val label: String?, val done: Boolean) : VoiceEvent
    data class Error(val message: String, val fatal: Boolean) : VoiceEvent

    companion object {
        /** null for a frame this app does not know (newer plugins may add some). */
        fun parse(text: String): VoiceEvent? {
            val o = runCatching { JsonCodec.parseToJsonElement(text).jsonObject }.getOrNull() ?: return null
            fun s(k: String) = (o[k] as? JsonPrimitive)?.takeIf { it.isString }?.content
            fun b(k: String) = (o[k] as? JsonPrimitive)?.booleanOrNull ?: false
            fun i(k: String) = (o[k] as? JsonPrimitive)?.intOrNull
            return when (s("type")) {
                "ready" -> Ready(i("input_rate") ?: 16000, i("output_rate") ?: 24000)
                "clear" -> Clear
                "transcript" -> Transcript(s("id") ?: return null, s("role") == "user", s("text").orEmpty(), b("final"))
                "tool" -> Tool(s("id") ?: return null, s("label"), b("done"))
                "error" -> Error(s("message") ?: "出错了", b("fatal"))
                else -> null
            }
        }
    }
}

/** One subtitle line of the conversation. */
data class Line(val id: String, val user: Boolean, val text: String, val final: Boolean)

/** Folds a transcript event into the subtitles: the same id restates its line, a new one is appended. */
fun List<Line>.with(t: VoiceEvent.Transcript): List<Line> {
    val i = indexOfFirst { it.id == t.id }
    val line = Line(t.id, t.user, t.text, t.final)
    return when {
        i >= 0 -> toMutableList().also { it[i] = line }
        t.text.isBlank() -> this
        else -> this + line
    }.filter { it.text.isNotBlank() || !it.final }
}

fun startFrame(voice: String?, model: String? = null): String =
    JsonCodec.encodeToString(JsonObject.serializer(), JsonObject(buildMap {
        put("type", JsonPrimitive("start"))
        if (!model.isNullOrEmpty()) put("model", JsonPrimitive(model))
        if (!voice.isNullOrEmpty()) put("voice", JsonPrimitive(voice))
    }))
