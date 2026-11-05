package persona.core.hub

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
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
        val url = base.hubUrl("/api/v1").newBuilder().apply {
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
        val data = try { request("auth", "tokens") } catch (failure: HubFailure) {
            throw when (failure.status) {
                404 -> DeviceCredentialsUnavailable(CoreText.of(Key.DeviceCredentialsUnsupported))
                503 -> DeviceCredentialsUnavailable(CoreText.of(Key.DeviceCredentialsNoDatabase))
                else -> failure
            }
        }
        val rows = when (val value = data["tokens"]) {
            JsonNull -> JsonArray(emptyList())
            is JsonArray -> value
            else -> throw HubFailure(HubError.Protocol)
        }
        val all = rows.map { it.jsonObject }.map { item ->
            PairedDevice(item.getValue("id").jsonPrimitive.content, item.text("name").orEmpty(),
                item.text("kind").orEmpty(), item.text("role").orEmpty(), item.text("last_seen"), item.text("platform"),
                item.text("created_at"), item.text("revoked_at")?.takeIf(String::isNotBlank))
        }
        return PairedDevices(all.filter { it.revokedAt == null }, data.text("current")?.takeIf(String::isNotBlank),
            all.filter { it.revokedAt != null })
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

    /** Tells the hub this device's 设置 › 翻译 (only that field: the rest of its prefs stay as they are). */
    suspend fun saveTranslatePrefs(prefs: HubTranslatePrefs): HubTranslatePrefs? {
        require(preferenceDeviceId.isNotBlank())
        return decode(request("devices", preferenceDeviceId, "prefs", method = "PUT", body = buildJsonObject {
            // Every field, false ones too: the hub merges into what it has, and the codec drops defaults.
            put("translate", buildJsonObject {
                put("enabled", prefs.enabled)
                put("target", prefs.target)
                put("mail", prefs.mail)
            })
        })).translate
    }

    private fun decode(data: JsonObject) = DeviceNotificationPreferences(
        input = data["input"]?.jsonPrimitive?.boolean ?: throw HubFailure(HubError.Protocol),
        done = DoneNotification.entries.firstOrNull { it.wire == data.text("done") } ?: throw HubFailure(HubError.Protocol),
        lockDetails = data["lock_details"]?.jsonPrimitive?.booleanOrNull ?: false,
        longTurnSeconds = data["long_turn_seconds"]?.jsonPrimitive?.intOrNull,
        doneDelaySeconds = data["done_delay_seconds"]?.jsonPrimitive?.intOrNull,
        liveActivity = data["live_activity"]?.jsonPrimitive?.booleanOrNull ?: true,
        translate = (data["translate"] as? JsonObject)?.let {
            runCatching { JsonCodec.decodeFromJsonElement(HubTranslatePrefs.serializer(), it) }.getOrNull()
        },
    )

    private fun JsonObject.text(key: String) = this[key]?.jsonPrimitive?.contentOrNull
}
