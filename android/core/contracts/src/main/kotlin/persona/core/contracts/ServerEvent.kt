@file:OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)

package persona.core.contracts

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.KeepGeneratedSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.descriptors.buildClassSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.*

@Serializable(with = ServerEventSerializer::class)
sealed interface ServerEvent {
    companion object {
        /** Unknown events and invalid required payloads do not enter the event stream. */
        fun decode(text: String): ServerEvent? = runCatching {
            JsonCodec.decodeFromString<ServerEvent>(text).takeUnless { it === UnknownEvent }
        }.getOrNull()
    }
}

@KeepGeneratedSerializer
@Serializable(with = HelloSerializer::class)
data class Hello(
    @SerialName("server_version") val serverVersion: String? = null,
    @SerialName("client_id") val clientId: String? = null,
    val features: List<String> = emptyList(),
    @SerialName("hub_id") val hubId: String? = null,
    val endpoints: List<Endpoint> = emptyList(),
    @SerialName("tls_port") val tlsPort: Int? = null,
    val fp: String? = null,
    /** The hub's display name (docs/plan-hub-mesh.md 2.6). */
    @SerialName("hub_name") val hubName: String? = null,
    /** Every hub of this hub's mesh, itself included; only in the authenticated hello. */
    val mesh: HubMesh? = null,
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = HubMeshSerializer::class)
data class HubMesh(val hubs: List<MeshHub> = emptyList())

/** One hub of a mesh as the hub the client is connected to reports it; trusted because the hello is authenticated. */
@KeepGeneratedSerializer
@Serializable(with = MeshHubSerializer::class)
data class MeshHub(
    @SerialName("hub_id") val hubId: String = "",
    val name: String = "",
    val online: Boolean = false,
    val self: Boolean = false,
    val endpoints: List<String> = emptyList(),
    @SerialName("tls_port") val tlsPort: Int? = null,
    val fp: String? = null,
)

@KeepGeneratedSerializer
@Serializable(with = ItemsEventSerializer::class)
data class ItemsEvent(
    @SerialName("machine_id") val machineId: String,
    @SerialName("session_id") val sessionId: String,
    val items: List<Item> = emptyList(), val history: String? = null,
    @SerialName("from_seq") val fromSeq: Int? = null,
    @SerialName("to_seq") val toSeq: Int? = null,
    val backlog: Boolean = false,
    @SerialName("has_more") val hasMore: Boolean? = null,
    @SerialName("more_after") val moreAfter: Boolean = false,
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = SessionsEventSerializer::class)
data class SessionsEvent(
    @SerialName("machine_id") val machineId: String, val sessions: List<Session> = emptyList(),
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = MachinesEventSerializer::class)
data class MachinesEvent(
    val machines: List<Machine> = emptyList(),
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = RevsEventSerializer::class)
data class RevsEvent(
    @SerialName("machine_id") val machineId: String, val revs: Map<String, Long> = emptyMap(),
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = FleetEventSerializer::class)
data class FleetEvent(
    val fleet: FleetState,
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = InteractionEventSerializer::class)
data class InteractionEvent(
    val interaction: Interaction,
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = InteractionResolvedEventSerializer::class)
data class InteractionResolvedEvent(
    val id: String,
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = EndpointsEventSerializer::class)
data class EndpointsEvent(
    val endpoints: List<Endpoint> = emptyList(),
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = ClientHelloAckSerializer::class)
data class ClientHelloAck(
    val heartbeat: Heartbeat,
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = PongSerializer::class)
data class Pong(
    val id: String,
) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = NotifyEventSerializer::class)
data class NotifyEvent(
    @WireEnum("interaction", "done", "assistant", "unknown") val kind: String,
    @SerialName("machine_id") val machineId: String,
    @SerialName("session_id") val sessionId: String,
    val title: String = "", val subtitle: String = "", val body: String = "",
    @SerialName("interaction_id") val interactionId: String? = null,
) : ServerEvent

/**
 * A plugin changed the badge of its entry on the navigation (`nav_page`, docs/plugins.md「UI 插槽」「角标」): `null`
 * clears it. The app changes the badge without reading the list again.
 */
@KeepGeneratedSerializer
@Serializable(with = PluginUiEventSerializer::class)
data class PluginUiEvent(
    val plugin: String,
    val id: String,
    val badge: persona.core.contracts.api.PluginViewBadge? = null,
) : ServerEvent

data object UnknownEvent : ServerEvent

object ServerEventSerializer : KSerializer<ServerEvent> {
    override val descriptor = buildClassSerialDescriptor("jarvis.core.contracts.ServerEvent")
    override fun deserialize(decoder: Decoder): ServerEvent {
        val input = decoder as? JsonDecoder ?: throw SerializationException("Events require JSON")
        val obj = input.decodeJsonElement() as? JsonObject ?: throw SerializationException("Expected event object")
        val type = (obj["type"] as? JsonPrimitive)?.takeIf { it.isString }?.content
        return when (type) {
            "hello" -> input.json.decodeFromJsonElement<Hello>(obj)
            "items" -> input.json.decodeFromJsonElement<ItemsEvent>(obj)
            "sessions" -> input.json.decodeFromJsonElement<SessionsEvent>(obj).let { event ->
                event.copy(sessions = event.sessions.map { it.copy(machineId = event.machineId) })
            }
            "machines" -> input.json.decodeFromJsonElement<MachinesEvent>(obj)
            "revs" -> input.json.decodeFromJsonElement<RevsEvent>(obj)
            "fleet" -> input.json.decodeFromJsonElement<FleetEvent>(obj)
            "interaction" -> input.json.decodeFromJsonElement<InteractionEvent>(obj)
            "interaction_resolved" -> input.json.decodeFromJsonElement<InteractionResolvedEvent>(obj)
            "endpoints" -> input.json.decodeFromJsonElement<EndpointsEvent>(obj)
            "client_hello_ack" -> input.json.decodeFromJsonElement<ClientHelloAck>(obj)
            "pong" -> input.json.decodeFromJsonElement<Pong>(obj)
            "notify" -> input.json.decodeFromJsonElement<NotifyEvent>(obj)
            "plugin_ui" -> input.json.decodeFromJsonElement<PluginUiEvent>(obj)
            "lock_test" -> input.json.decodeFromJsonElement<LockTestEvent>(obj).also {
                require(it.testId.isNotBlank() && it.dueAt.isNotBlank() && it.sentAt.isNotBlank())
            }
            "delta" -> input.json.decodeFromJsonElement<DeltaEvent>(obj)
            "screen" -> input.json.decodeFromJsonElement<ScreenEvent>(obj)
            "tool_call" -> input.json.decodeFromJsonElement<ToolCallEvent>(obj)
            "hook_event" -> input.json.decodeFromJsonElement<HookEvent>(obj)
            "lark" -> input.json.decodeFromJsonElement<LarkEvent>(obj)
            "inbox" -> input.json.decodeFromJsonElement<InboxEvent>(obj)
            "todos" -> input.json.decodeFromJsonElement<TodosEvent>(obj)
            "assistant_feed" -> input.json.decodeFromJsonElement<AssistantFeedEvent>(obj)
            "task_plan" -> input.json.decodeFromJsonElement<TaskPlanEvent>(obj)
            "terminal" -> input.json.decodeFromJsonElement<TerminalEvent>(obj).also { event ->
                if (!event.streamEnded && (event.epoch == null || event.seq == null || event.frameBytes == null)) {
                    throw SerializationException("Terminal frame requires epoch, seq and base64 data")
                }
            }
            "terminal_control" -> input.json.decodeFromJsonElement<TerminalControlEvent>(obj)
            "error" -> input.json.decodeFromJsonElement<ErrorEvent>(obj)
            else -> UnknownEvent
        }
    }

    override fun serialize(encoder: Encoder, value: ServerEvent) {
        val output = encoder as? JsonEncoder ?: throw SerializationException("Events require JSON")
        val (type, payload) = when (value) {
            is Hello -> "hello" to output.json.encodeToJsonElement(value)
            is ItemsEvent -> "items" to output.json.encodeToJsonElement(value)
            is SessionsEvent -> "sessions" to output.json.encodeToJsonElement(value)
            is MachinesEvent -> "machines" to output.json.encodeToJsonElement(value)
            is RevsEvent -> "revs" to output.json.encodeToJsonElement(value)
            is FleetEvent -> "fleet" to output.json.encodeToJsonElement(value)
            is InteractionEvent -> "interaction" to output.json.encodeToJsonElement(value)
            is InteractionResolvedEvent -> "interaction_resolved" to output.json.encodeToJsonElement(value)
            is EndpointsEvent -> "endpoints" to output.json.encodeToJsonElement(value)
            is ClientHelloAck -> "client_hello_ack" to output.json.encodeToJsonElement(value)
            is Pong -> "pong" to output.json.encodeToJsonElement(value)
            is NotifyEvent -> "notify" to output.json.encodeToJsonElement(value)
            is PluginUiEvent -> "plugin_ui" to output.json.encodeToJsonElement(value)
            is LockTestEvent -> "lock_test" to output.json.encodeToJsonElement(value)
            is DeltaEvent -> "delta" to output.json.encodeToJsonElement(value)
            is ScreenEvent -> "screen" to output.json.encodeToJsonElement(value)
            is ToolCallEvent -> "tool_call" to output.json.encodeToJsonElement(value)
            is HookEvent -> "hook_event" to output.json.encodeToJsonElement(value)
            is LarkEvent -> "lark" to output.json.encodeToJsonElement(value)
            is InboxEvent -> "inbox" to output.json.encodeToJsonElement(value)
            is TodosEvent -> "todos" to output.json.encodeToJsonElement(value)
            is AssistantFeedEvent -> "assistant_feed" to output.json.encodeToJsonElement(value)
            is TaskPlanEvent -> "task_plan" to output.json.encodeToJsonElement(value)
            is TerminalEvent -> "terminal" to output.json.encodeToJsonElement(value)
            is TerminalControlEvent -> "terminal_control" to output.json.encodeToJsonElement(value)
            is ErrorEvent -> "error" to output.json.encodeToJsonElement(value)
            UnknownEvent -> throw SerializationException("Unknown events cannot be sent")
        }
        output.encodeJsonElement(JsonObject(payload.jsonObject + ("type" to JsonPrimitive(type))))
    }
}

object HelloSerializer : WireSerializer<Hello>(Hello.generatedSerializer())
object HubMeshSerializer : WireSerializer<HubMesh>(HubMesh.generatedSerializer())
object MeshHubSerializer : WireSerializer<MeshHub>(MeshHub.generatedSerializer())
object ItemsEventSerializer : WireSerializer<ItemsEvent>(ItemsEvent.generatedSerializer())
object SessionsEventSerializer : WireSerializer<SessionsEvent>(SessionsEvent.generatedSerializer())
object MachinesEventSerializer : WireSerializer<MachinesEvent>(MachinesEvent.generatedSerializer())
object RevsEventSerializer : WireSerializer<RevsEvent>(RevsEvent.generatedSerializer())
object FleetEventSerializer : WireSerializer<FleetEvent>(FleetEvent.generatedSerializer())
object InteractionEventSerializer : WireSerializer<InteractionEvent>(InteractionEvent.generatedSerializer())
object InteractionResolvedEventSerializer : WireSerializer<InteractionResolvedEvent>(InteractionResolvedEvent.generatedSerializer())
object EndpointsEventSerializer : WireSerializer<EndpointsEvent>(EndpointsEvent.generatedSerializer())
object ClientHelloAckSerializer : WireSerializer<ClientHelloAck>(ClientHelloAck.generatedSerializer())
object PongSerializer : WireSerializer<Pong>(Pong.generatedSerializer())
object NotifyEventSerializer : WireSerializer<NotifyEvent>(NotifyEvent.generatedSerializer())
object PluginUiEventSerializer : WireSerializer<PluginUiEvent>(PluginUiEvent.generatedSerializer())
