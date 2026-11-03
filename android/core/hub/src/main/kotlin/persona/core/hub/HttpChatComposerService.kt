package persona.core.hub

import persona.core.contracts.HubScope
import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.withContext
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.*
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.asRequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import java.io.File

/** Decorates the existing transport's readiness gate without changing the outbox payload format. */
class HttpChatComposerService internal constructor(
    private val hubScope: HubScope,
    private val transport: MessageTransport,
    private val request: suspend (String, RequestBody, Map<String, String>) -> Response,
    private val onRetryDelivery: suspend (String, ComposerDelivery) -> Unit = { _, _ -> },
    private val onPairingExpired: () -> Unit = {},
) : ChatComposerService, MessageTransport by transport {
    constructor(
        hubScope: HubScope, transport: MessageTransport, connection: OkHttpHubConnection, token: String,
        onRetryDelivery: suspend (String, ComposerDelivery) -> Unit,
        onPairingExpired: () -> Unit,
        isCurrent: () -> Boolean = { true },
    ) :
        this(hubScope, transport, { path, body, headers ->
            if (!isCurrent()) throw CancellationException("Pairing changed")
            val route = connection.route.value ?: throw HubFailure(HubError.NoNetwork)
            val url = route.endpoint.baseUrl().resolve(path) ?: throw HubFailure(HubError.Protocol)
            connection.http.client.newCall(Request.Builder().url(url).header("Authorization", "Bearer $token")
                .apply { headers.forEach { (name, value) -> header(name, value) } }.post(body).build()).awaitResponse().also {
                    if (!isCurrent()) {
                        it.close()
                        throw CancellationException("Pairing changed")
                    }
                    if (connection.route.value !== route) {
                        it.close()
                        throw HubFailure(HubError.Transport, "Route changed before receipt")
                    }
                }
        }, onRetryDelivery, onPairingExpired)

    override val deliveries = MutableStateFlow<Map<String, ComposerDelivery>>(emptyMap())
    private val refused = mutableMapOf<String, MessageRequest>()
    private val sending = Mutex()

    override suspend fun send(request: MessageRequest): MessageTransportResult = sending.withLock {
        sendLocked(request)
    }

    private suspend fun sendLocked(message: MessageRequest): MessageTransportResult {
        require(message.scope == hubScope)
        if (!readyToSend.value) return MessageTransportResult.NotSent
        val body = buildJsonObject {
            put("text", message.text)
            put("client_msg_id", message.clientMessageId)
            message.clearDraft?.let { put("clear_draft", it) }
        }.toString().toRequestBody(jsonType)
        return try {
            request(api("sessions", message.session.machineId, message.session.sessionId, "messages"), body, message.headers).use { response ->
                if (response.isSuccessful) {
                    val fields = runCatching { JsonCodec.parseToJsonElement(response.peekBody(64 * 1024).string()).jsonObject }.getOrNull()
                    if (fields?.get("state")?.jsonPrimitive?.contentOrNull in setOf("pending", "unknown")) {
                        record(message, ComposerDelivery(DeliveryState.Unknown))
                        return@use MessageTransportResult.NoReceipt
                    }
                    refused.remove(message.clientMessageId)
                    record(message, ComposerDelivery(DeliveryState.Sent))
                    MessageTransportResult.Accepted
                } else {
                    val failure = HubFailure.from(response)
                    if (failure.kind == HubError.PairingExpired) onPairingExpired()
                    val fields = runCatching { JsonCodec.parseToJsonElement(response.peekBody(64 * 1024).string()).jsonObject }.getOrNull()
                    val refusal = if (failure.delivered == "none" && failure.code in setOf("input_draft", "dialog_open")) {
                        refused[message.clientMessageId] = message
                        ComposerRefusal(message.session, message.clientMessageId, failure.code!!,
                            fields?.get("draft")?.jsonPrimitive?.contentOrNull,
                            fields?.get("clearable")?.jsonPrimitive?.contentOrNull)
                    } else null
                    val state = when {
                        failure.delivered == "yes" -> DeliveryState.Sent
                        failure.delivered == "none" -> DeliveryState.Refused
                        else -> DeliveryState.Unknown
                    }
                    record(message, ComposerDelivery(state, refusal))
                    MessageTransportResult.HubError(failure.code, failure.delivered, failure.message, failure.retryable)
                }
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) {
            val kind = HubFailure.from(error).kind
            if (kind in setOf(HubError.NoNetwork, HubError.AddressNotFound, HubError.ConnectionRefused, HubError.Unreachable)) {
                MessageTransportResult.NotSent
            } else {
                record(message, ComposerDelivery(DeliveryState.Unknown))
                MessageTransportResult.NoReceipt
            }
        }
    }

    override suspend fun retryRefused(clientMessageId: String, clearDraft: Boolean): ComposerDelivery = sending.withLock {
        val original = refused[clientMessageId] ?: throw ComposerOperationException("请先查看终端，确认消息没有发出")
        val refusal = deliveries.value[clientMessageId]?.refusal
            ?: throw ComposerOperationException("发送结果待确认，请先查看终端")
        val clearable = if (clearDraft) refusal.clearable?.takeIf { it.isNotEmpty() }
            ?: throw ComposerOperationException("这份电脑草稿需要在终端里处理") else null
        if (!readyToSend.value) throw ComposerOperationException("连接恢复后再试，输入已保留")
        onRetryDelivery(clientMessageId, ComposerDelivery(DeliveryState.Sending))
        record(original, ComposerDelivery(DeliveryState.Sending))
        val result = try { sendLocked(original.copy(clearDraft = clearable)) }
        catch (cancelled: CancellationException) {
            val unknown = ComposerDelivery(DeliveryState.Unknown)
            record(original, unknown)
            withContext(NonCancellable) { onRetryDelivery(clientMessageId, unknown) }
            throw cancelled
        }
        if (result == MessageTransportResult.NotSent) record(original, ComposerDelivery(DeliveryState.Refused, refusal))
        deliveries.value.getValue(clientMessageId).also { onRetryDelivery(clientMessageId, it) }
    }

    override suspend fun upload(session: SessionKey, name: String, file: File): String {
        if (!readyToSend.value) throw ComposerOperationException("连接恢复后重试上传，输入已保留")
        if (file.length() > 200L * 1024 * 1024) throw ComposerOperationException("文件不能超过 200 MB")
        val url = path("machines", session.machineId, "files").addQueryParameter("name", name).build()
        return request(url.encodedPath + "?" + url.encodedQuery,
            file.asRequestBody("application/octet-stream".toMediaType()), emptyMap()).use { response ->
            if (!response.isSuccessful) {
                val failure = HubFailure.from(response)
                if (failure.kind == HubError.PairingExpired) onPairingExpired()
                throw ComposerOperationException(when {
                    response.code == 404 -> "hub 不支持上传，请更新 hub"
                    failure.code == "node_outdated" -> "这台电脑不支持上传，请更新电脑上的服务"
                    response.code == 413 -> "文件过大，请选择较小的文件"
                    else -> "上传失败，请重试；输入已保留"
                })
            }
            JsonCodec.parseToJsonElement(response.body!!.string()).jsonObject["path"]!!.jsonPrimitive.content.also {
                require(it.startsWith('/') && !it.contains('\n') && !it.contains('\r'))
            }
        }
    }

    override suspend fun interrupt(session: SessionKey) {
        if (!readyToSend.value) throw ComposerOperationException("连接恢复后再中断")
        request(api("sessions", session.machineId, session.sessionId, "interrupt"), "{}".toRequestBody(jsonType), emptyMap()).use {
            if (!it.isSuccessful) {
                if (it.code in setOf(401, 403)) onPairingExpired()
                throw ComposerOperationException("中断未能确认，请查看终端")
            }
        }
    }

    private fun record(request: MessageRequest, delivery: ComposerDelivery) {
        deliveries.update { it + (request.clientMessageId to delivery) }
    }

    private fun path(vararg segments: String) = HttpUrl.Builder().scheme("https").host("example.com")
        .addPathSegments("api/v1").apply { segments.forEach(::addPathSegment) }
    private fun api(vararg segments: String) = path(*segments).build().encodedPath
    private val jsonType = "application/json; charset=utf-8".toMediaType()
}
