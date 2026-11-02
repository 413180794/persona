package jarvis.core.hub

import jarvis.core.contracts.JsonCodec
import jarvis.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.*
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

/** Supply the paired TLS/local-network-policy client and recreate this adapter when pairing changes. */
class HttpDeviceSettings(
    baseUrl: String,
    private val token: String,
    private val preferenceDeviceId: String,
    client: OkHttpClient,
    private val isCurrent: () -> Boolean = { true },
) : DeviceSettings {
    private val base = baseUrl.toHttpUrl()
    private val http = client.newBuilder().followRedirects(false).followSslRedirects(false)
        .retryOnConnectionFailure(false).build()

    private suspend fun request(vararg path: String, method: String = "GET", body: JsonObject? = null): JsonObject {
        if (!isCurrent()) throw CancellationException("Pairing changed")
        val url = base.newBuilder().encodedPath("/api/v1").query(null).fragment(null).apply {
            path.forEach(::addPathSegment)
        }.build()
        val request = Request.Builder().url(url).header("Authorization", "Bearer $token")
            .method(method, body?.toString()?.toRequestBody("application/json".toMediaType())).build()
        return http.newCall(request).awaitResponse().use { response ->
            if (!isCurrent()) throw CancellationException("Pairing changed")
            if (!response.isSuccessful) throw HubFailure.from(response)
            val text = response.body?.string().orEmpty()
            if (text.isBlank()) JsonObject(emptyMap()) else JsonCodec.parseToJsonElement(text).jsonObject
        }
    }

    override suspend fun devices(): PairedDevices {
        val data = request("auth", "tokens")
        val rows = when (val value = data["tokens"]) {
            JsonNull -> JsonArray(emptyList())
            is JsonArray -> value
            else -> throw HubFailure(HubError.Protocol)
        }
        return PairedDevices(rows.map { it.jsonObject }.filter { it.text("revoked_at").isNullOrBlank() }.map { item ->
            PairedDevice(item.getValue("id").jsonPrimitive.content, item.text("name").orEmpty(),
                item.text("kind").orEmpty(), item.text("role").orEmpty(), item.text("last_seen"), item.text("platform"))
        }, data.text("current")?.takeIf(String::isNotBlank))
    }

    override suspend fun revoke(credentialId: String) {
        require(credentialId.isNotBlank())
        request("auth", "tokens", credentialId, method = "DELETE")
    }

    override suspend fun preferences(): DeviceNotificationPreferences {
        require(preferenceDeviceId.isNotBlank())
        return decode(request("devices", preferenceDeviceId, "prefs"))
    }

    override suspend fun savePreferences(preferences: DeviceNotificationPreferences): DeviceNotificationPreferences {
        require(preferenceDeviceId.isNotBlank())
        // Leave island and future server fields untouched.
        return decode(request("devices", preferenceDeviceId, "prefs", method = "PUT", body = buildJsonObject {
            put("input", preferences.input)
            put("done", preferences.done.wire)
            put("lock_details", preferences.lockDetails)
            put("live_activity", preferences.liveActivity)
        }))
    }

    private fun decode(data: JsonObject) = DeviceNotificationPreferences(
        input = data["input"]?.jsonPrimitive?.boolean ?: throw HubFailure(HubError.Protocol),
        done = DoneNotification.entries.firstOrNull { it.wire == data.text("done") } ?: throw HubFailure(HubError.Protocol),
        lockDetails = data["lock_details"]?.jsonPrimitive?.booleanOrNull ?: false,
        longTurnSeconds = data["long_turn_seconds"]?.jsonPrimitive?.intOrNull,
        doneDelaySeconds = data["done_delay_seconds"]?.jsonPrimitive?.intOrNull,
        liveActivity = data["live_activity"]?.jsonPrimitive?.booleanOrNull ?: true,
    )

    private fun JsonObject.text(key: String) = this[key]?.jsonPrimitive?.contentOrNull
}
