package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

class HttpConnectorsManagement internal constructor(
    private val request: suspend (String, String, RequestBody?) -> Response,
) : ConnectorsManagement {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body ->
        connection.request(path, method, body)
    })

    override suspend fun connectors(all: Boolean): List<HubConnector> {
        val path = if (all) "/api/v1/connectors?all=1" else "/api/v1/connectors"
        return try {
            val body = parse(call(path, "GET")) as? JsonObject ?: return emptyList()
            val list = body["connectors"] as? JsonArray ?: return emptyList()
            decode { JsonCodec.decodeFromJsonElement(ListSerializer(HubConnector.serializer()), list) }
        } catch (_: Exception) {
            emptyList()
        }
    }

    override suspend fun setConnectorEnabled(id: String, enabled: Boolean): HubConnector {
        val body = buildJsonObject { put("enabled", enabled) }
        return decode { JsonCodec.decodeFromJsonElement(HubConnector.serializer(), parse(call("/api/v1/connectors/$id/settings", "PUT", body))) }
    }

    override suspend fun setConnectorFeed(id: String, feed: String, on: Boolean): HubConnector {
        val body = buildJsonObject { put(feed, on) }
        return decode { JsonCodec.decodeFromJsonElement(HubConnector.serializer(), parse(call("/api/v1/connectors/$id/settings", "PUT", body))) }
    }

    override suspend fun deleteConnectorData(id: String): ConnectorDataDeleted =
        read(call("/api/v1/connectors/$id/data", "DELETE"), ConnectorDataDeleted.serializer())

    override suspend fun deleteConnectorArchive(id: String): ConnectorDataDeleted =
        read(call("/api/v1/connectors/$id/archive", "DELETE"), ConnectorDataDeleted.serializer())

    override suspend fun mailAccounts(): List<MailAccount> {
        val body = parse(call("/api/v1/mail/accounts", "GET")) as? JsonObject ?: throw HubFailure(HubError.Protocol)
        val list = body["accounts"] as? JsonArray ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(ListSerializer(MailAccount.serializer()), list) }
    }

    override suspend fun addMailAccount(req: AddMailAccountRequest): MailAccount {
        val payload = JsonCodec.encodeToString(AddMailAccountRequest.serializer(), req)
        val body = parse(callRaw("/api/v1/mail/accounts", "POST", payload)) as? JsonObject ?: throw HubFailure(HubError.Protocol)
        val account = body["account"] ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(MailAccount.serializer(), account) }
    }

    override suspend fun deleteMailAccount(id: String) {
        call("/api/v1/mail/accounts/$id", "DELETE")
    }

    override suspend fun testMailAccount(id: String): MailTestResult =
        read(callRaw("/api/v1/mail/accounts/$id/test", "POST", "{}"), MailTestResult.serializer())

    override suspend fun googleSignInStart(loginHint: String?): GoogleStart {
        val payload = if (loginHint.isNullOrBlank()) "{}" else buildJsonObject { put("login_hint", loginHint) }.toString()
        return read(callRaw("/api/v1/mail/oauth/google/start", "POST", payload), GoogleStart.serializer())
    }

    override suspend fun googleSignInFinish(finish: GoogleFinish): MailAccount {
        val payload = JsonCodec.encodeToString(GoogleFinish.serializer(), finish)
        val body = parse(callRaw("/api/v1/mail/oauth/google/finish", "POST", payload)) as? JsonObject ?: throw HubFailure(HubError.Protocol)
        val account = body["account"] ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(MailAccount.serializer(), account) }
    }

    override suspend fun larkStatus(): LarkStatus =
        read(call("/api/v1/lark/status", "GET"), LarkStatus.serializer())

    override suspend fun larkAccounts(): LarkAccounts? = try {
        read(call("/api/v1/lark/accounts", "GET"), LarkAccounts.serializer())
    } catch (_: Exception) {
        null
    }

    private fun <T> read(body: String, serializer: KSerializer<T>): T =
        decode { JsonCodec.decodeFromJsonElement(serializer, parse(body)) }

    private fun parse(body: String): JsonElement = try {
        JsonCodec.parseToJsonElement(body)
    } catch (error: SerializationException) {
        throw HubFailure(HubError.Protocol, cause = error)
    }

    private inline fun <T> decode(block: () -> T): T = try {
        block()
    } catch (error: IllegalArgumentException) {
        throw HubFailure(HubError.Protocol, cause = error)
    }

    private suspend fun call(path: String, method: String, body: JsonObject? = null): String =
        callRaw(path, method, body?.toString())

    private suspend fun callRaw(path: String, method: String, bodyText: String? = null): String = try {
        val reqBody = bodyText?.toRequestBody(json) ?: if (method == "POST" || method == "PUT") "".toRequestBody(json) else null
        request(path, method, reqBody).use { response ->
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string().orEmpty()
        }
    } catch (failure: HubFailure) {
        throw failure
    }

    private companion object {
        val json = "application/json; charset=utf-8".toMediaType()
    }
}
