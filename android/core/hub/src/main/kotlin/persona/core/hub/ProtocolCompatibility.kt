package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.ProtocolCompatibility
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okio.Buffer

/** Only repeatable JSON message bodies carry generated markup; uploads and opaque extension arguments stay intact. */
internal fun compatibleRequestBody(path: String, body: RequestBody?): RequestBody? {
    if (body == null || body.isOneShot() || body.isDuplex() || body.contentType()?.subtype != "json" ||
        !Regex("(?:/messages|/queue|/fork|/rewind|/sessions|/assistant/(?:send|conversations))/?$").containsMatchIn(path)) return body
    val text = Buffer().also(body::writeTo).readUtf8()
    val obj = runCatching { JsonCodec.parseToJsonElement(text) as? JsonObject }.getOrNull() ?: return body
    val mapped = obj.toMutableMap()
    for (field in listOf("text", "prompt")) {
        val value = obj[field] as? JsonPrimitive ?: continue
        if (value.isString) mapped[field] = JsonPrimitive(ProtocolCompatibility.messageForHub(value.content))
    }
    return JsonObject(mapped).toString().toRequestBody(body.contentType())
}

internal fun compatibleLookCurrent(current: persona.core.contracts.api.LookCurrent) = current.copy(
    id = ProtocolCompatibility.canonicalBuiltin(current.id),
    effective = ProtocolCompatibility.canonicalBuiltin(current.effective)!!,
)
