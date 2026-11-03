package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.HubPlugin
import persona.core.contracts.api.VoiceModelChoice
import persona.core.contracts.api.VoiceModelOptions
import persona.core.contracts.api.VoiceOptions
import persona.core.contracts.api.VoicePlugins
import persona.core.contracts.api.VoiceSocket
import persona.core.contracts.api.VoiceSocketListener
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import okio.ByteString.Companion.toByteString

/**
 * The hub's voice plugins (docs/plugins.md「语音」) on one route of one pairing: the plugin list, a plugin's voices and
 * models, the model configuration of `plugin.<id>` (docs/model-config.md) and the plugin's socket. Calls after the
 * pairing changed ([isCurrent] false) fail with a cancellation, like [HttpMachineMetricsApi].
 */
class HttpVoicePlugins(
    baseUrl: String,
    private val token: String,
    client: OkHttpClient,
    private val isCurrent: () -> Boolean = { true },
    private val dispatcher: CoroutineDispatcher = Dispatchers.Default,
) : VoicePlugins {
    private val base = baseUrl.toHttpUrl()
    private val http = client.newBuilder().followRedirects(false).followSslRedirects(false)
        .retryOnConnectionFailure(false).build()

    // A conversation can be quiet for minutes, and the plugin speaks whenever the model does: no read timeout, and
    // pings keep the hub's proxy and NATs from dropping the socket in between.
    private val sockets by lazy {
        client.newBuilder().readTimeout(0, TimeUnit.MILLISECONDS).pingInterval(15, TimeUnit.SECONDS).build()
    }

    private fun checkScope() { if (!isCurrent()) throw CancellationException("Pairing changed") }

    private fun plugin(id: String, vararg rest: String): HttpUrl {
        // Plugin ids are a-z0-9_- (docs/plugins.md); anything else must not become a path ("..").
        require(id.matches(PluginId)) { "Invalid plugin id" }
        return base.newBuilder().addPathSegments("api/v1/plugins").addPathSegment(id)
            .apply { rest.forEach(::addPathSegment) }.build()
    }

    /** The body as JSON; null for a 404 when [missing] allows it (an older hub or plugin). */
    private suspend fun read(url: HttpUrl, missing: Boolean): JsonElement? = withContext(dispatcher) {
        checkScope()
        val request = Request.Builder().url(url).header("Authorization", "Bearer $token").build()
        val body = http.newCall(request).awaitResponse().use { response ->
            checkScope()
            if (missing && response.code == 404) return@withContext null
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string() ?: throw HubFailure(HubError.Protocol)
        }
        val json = try { JsonCodec.parseToJsonElement(body) }
        catch (error: SerializationException) { throw HubFailure(HubError.Protocol, cause = error) }
        checkScope()
        json
    }

    /** Decoding errors (SerializationException is one) are the hub's or plugin's protocol, not the network's. */
    private inline fun <T> decode(block: () -> T): T = try { block() }
    catch (error: IllegalArgumentException) { throw HubFailure(HubError.Protocol, cause = error) }

    override suspend fun voicePlugin(): HubPlugin? {
        val json = read(base.newBuilder().addPathSegments("api/v1/plugins").build(), missing = true) ?: return null
        val list = (json as? JsonObject)?.get("plugins") as? JsonArray ?: return null
        return decode { JsonCodec.decodeFromJsonElement(ListSerializer(HubPlugin.serializer()), list) }
            .firstOrNull { it.isRunningVoice }
    }

    override suspend fun voices(pluginId: String, model: String?): VoiceOptions {
        val url = plugin(pluginId, "voices").newBuilder()
            .apply { model?.takeIf { it.isNotBlank() }?.let { addQueryParameter("model", it) } }.build()
        val json = read(url, missing = false)!!
        return decode { JsonCodec.decodeFromJsonElement(VoiceOptions.serializer(), json) }
    }

    override suspend fun models(pluginId: String): VoiceModelOptions {
        val json = read(plugin(pluginId, "models"), missing = true) ?: return VoiceModelOptions()
        return decode { JsonCodec.decodeFromJsonElement(VoiceModelOptions.serializer(), json) }
    }

    override suspend fun modelChoice(pluginId: String): VoiceModelChoice? {
        require(pluginId.matches(PluginId)) { "Invalid plugin id" }
        val url = base.newBuilder().addPathSegments("api/v1/model-config/purposes").addPathSegment("plugin.$pluginId").build()
        val json = read(url, missing = true) ?: return null
        // A newer hub always answers with the purpose; anything else is not "no configuration".
        val purpose = (json as? JsonObject)?.get("purpose") as? JsonObject ?: throw HubFailure(HubError.Protocol)
        val model = ((purpose["effective"] as? JsonObject)?.get("model") as? JsonPrimitive)?.contentOrNull?.trim()
        val status = (purpose["status"] as? JsonPrimitive)?.contentOrNull?.trim()
        return VoiceModelChoice(
            model = model?.takeIf { it.isNotEmpty() },
            // 未验证 means nobody paid to try it, not that it is broken (docs/model-config.md).
            status = status?.takeIf { it.isNotEmpty() && it != Unverified },
        )
    }

    override fun connect(pluginId: String, listener: VoiceSocketListener): VoiceSocket {
        checkScope()
        val request = Request.Builder().url(plugin(pluginId, "ws")).header("Authorization", "Bearer $token").build()
        // One ending reaches the listener: the plugin's close, a failure, or nothing after the page closed it.
        val ended = AtomicBoolean(false)
        val opened = AtomicBoolean(false)
        val socket = sockets.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                opened.set(true)
                if (!ended.get()) listener.onOpen()
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                if (!ended.get()) listener.onText(text)
            }

            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                if (!ended.get()) listener.onBinary(bytes.toByteArray())
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                // Finish the closing handshake; the plugin has said all it will say.
                runCatching { webSocket.close(1000, null) }
                if (ended.compareAndSet(false, true)) listener.onClosed(code, reason)
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                if (ended.compareAndSet(false, true)) listener.onClosed(code, reason)
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                val status = response?.code
                val error = response?.use { HubFailure.from(it) } ?: HubFailure.from(t)
                // OkHttp does not retry an upgrade on a pooled connection that died behind a forwarder (adb reverse,
                // ssh -L): every later attempt failed the same way until the app restarted (docs/android.md).
                if (response == null && !opened.get()) http.connectionPool.evictAll()
                if (ended.compareAndSet(false, true)) listener.onFailure(error, status)
            }
        })
        return object : VoiceSocket {
            override fun send(text: String): Boolean = socket.send(text)
            override fun send(bytes: ByteArray, offset: Int, length: Int): Boolean = socket.send(bytes.toByteString(offset, length))
            override fun close(code: Int, reason: String?) {
                ended.set(true)
                if (!socket.close(code, reason)) socket.cancel()
            }
        }
    }

    private companion object {
        val PluginId = Regex("[A-Za-z0-9_-]{1,64}")
        const val Unverified = "未验证"
    }
}
