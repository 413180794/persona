package persona.core.hub

import kotlinx.serialization.json.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.MailTranslation
import persona.core.contracts.api.TranslationTransport

/** Settings › Translation's calls to the manager (`server/internal/assistant/inbox_translate.go`), relayed by the paired hub. */
class HttpTranslationTransport internal constructor(
    private val request: suspend (String, String, RequestBody?) -> Response,
) : TranslationTransport {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body -> connection.request(path, method, body) })

    override suspend fun translate(texts: List<String>, target: String): List<String> {
        val out = post("/api/v1/translate", buildJsonObject {
            put("texts", JsonArray(texts.map(::JsonPrimitive)))
            put("target", target)
        })["texts"] as? JsonArray ?: throw HubFailure(HubError.Protocol)
        return out.map { (it as? JsonPrimitive)?.contentOrNull.orEmpty() }
    }

    override suspend fun translateMail(account: String, uid: Long, mailbox: String, target: String): MailTranslation {
        val o = post("/api/v1/translate/mail", buildJsonObject {
            put("account_id", account)
            put("uid", uid)
            put("mailbox", mailbox)
            put("target", target)
        })
        return MailTranslation(
            subject = (o["subject"] as? JsonPrimitive)?.contentOrNull.orEmpty(),
            text = (o["text"] as? JsonPrimitive)?.contentOrNull ?: throw HubFailure(HubError.Protocol),
            clipped = (o["clipped"] as? JsonPrimitive)?.booleanOrNull ?: false,
        )
    }

    private suspend fun post(path: String, body: JsonObject): JsonObject =
        request(path, "POST", body.toString().toRequestBody(json)).use { response ->
            if (!response.isSuccessful) throw HubFailure.from(response)
            val text = response.body?.string().orEmpty()
            try { JsonCodec.parseToJsonElement(text) as? JsonObject } catch (_: Exception) { null }
                ?: throw HubFailure(HubError.Protocol)
        }

    private companion object {
        val json = "application/json; charset=utf-8".toMediaType()
    }
}
