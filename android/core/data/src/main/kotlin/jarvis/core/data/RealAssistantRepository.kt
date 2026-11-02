package jarvis.core.data

import jarvis.core.contracts.JsonCodec
import jarvis.core.contracts.Session
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.*
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.*

class RealAssistantRepository(
    private val connection: InboxAssistantConnection,
    private val sessions: SessionsRepository,
    scope: CoroutineScope,
    private val snapshots: ListSnapshotStore? = null,
    private val preferences: HubPreferences? = null,
) : AssistantConversations {
    private val current = MutableStateFlow(AssistantSnapshot())
    override val assistant = current.asStateFlow()
    private val currentPins = MutableStateFlow<Set<SessionKey>>(emptySet())
    override val pinned = currentPins.asStateFlow()
    private val lock = Mutex()
    private var restoredScope: String? = null
    private suspend fun restore(transport: InboxAssistantTransport) {
        if (restoredScope == transport.scope) return
        current.value = AssistantSnapshot()
        currentPins.value = preferences?.preferences?.value?.pins?.sessions?.mapNotNull(::sessionPath)?.toSet().orEmpty()
        quietly { snapshots.restore<JsonObject>("${transport.scope}:assistant")?.let { apply(it) } }
        restoredScope = transport.scope
    }
    private fun apply(data: JsonObject) {
        val machine = data.string("machine_id")
        val main = data.string("main")
        val conversations = data.array("conversations").map { JsonCodec.decodeFromJsonElement<Session>(it) }
        current.value = AssistantSnapshot(
            if (machine.isNotEmpty() && main.isNotEmpty()) SessionKey(machine, main) else null, conversations)
    }

    init {
        if (preferences != null) scope.launch {
            preferences.preferences.collect { state ->
                currentPins.value = state.pins.sessions.mapNotNull(::sessionPath).toSet()
            }
        }
        scope.launch {
            connection.current.collectLatest { transport ->
                if (transport == null) lock.withLock {
                    restoredScope = null
                    current.value = AssistantSnapshot()
                    currentPins.value = emptySet()
                }
                if (transport != null) {
                    try { refresh() } catch (e: CancellationException) { throw e } catch (_: Exception) { }
                }
            }
        }
        scope.launch {
            sessions.sessions.collect { updates ->
                if (connection.current.value != null) current.update { snapshot ->
                    val byKey = updates.associateBy { it.key }
                    snapshot.copy(conversations = (snapshot.conversations.map { byKey[it.key] ?: it } +
                        updates.filter { it.assistant == true }).distinctBy { it.key })
                }
            }
        }
    }

    override suspend fun refresh() = lock.withLock {
        val transport = requireNotNull(connection.current.value) { "请先配对 hub" }
        restore(transport)
        val data = transport.assistant()
        val pins = if (preferences != null) preferences.preferences.value.pins.sessions.mapNotNull(::sessionPath).toSet()
            else transport.pins().array("sessions").mapNotNull { (it as? JsonPrimitive)?.contentOrNull?.let(::sessionPath) }.toSet()
        if (connection.current.value !== transport) return@withLock
        apply(data)
        snapshots.save("${transport.scope}:assistant", data)
        currentPins.value = pins
    }

    override suspend fun startConversation(): SessionKey = lock.withLock {
        val transport = requireNotNull(connection.current.value) { "请先配对 hub" }
        // Refresh the main pointer before deciding whether this is the first conversation.
        val config = transport.assistant()
        val isMain = config.string("main").isEmpty()
        val result = transport.createConversation(isMain)
        val session = JsonCodec.decodeFromJsonElement<Session>(result.getValue("session"))
        check(connection.current.value === transport) { "连接已切换" }
        current.update {
            it.copy(mainSession = if (isMain) session.key else it.mainSession,
                conversations = (listOf(session) + it.conversations).distinctBy { conversation -> conversation.key })
        }
        snapshots.save("${transport.scope}:assistant", buildJsonObject {
            put("machine_id", current.value.mainSession?.machineId ?: session.machineId)
            put("main", current.value.mainSession?.sessionId.orEmpty())
            put("conversations", JsonArray(current.value.conversations.map { JsonCodec.encodeToJsonElement(it) }))
        })
        session.key
    }
}

internal fun JsonObject.string(key: String): String = (this[key] as? JsonPrimitive)?.contentOrNull.orEmpty()
internal fun JsonObject.optional(key: String): String? = string(key).takeIf { it.isNotEmpty() }
internal fun JsonObject.array(key: String): JsonArray = this[key] as? JsonArray ?: JsonArray(emptyList())
internal fun JsonObject.obj(key: String): JsonObject? = this[key] as? JsonObject
internal fun JsonObject.flag(key: String): Boolean = (this[key] as? JsonPrimitive)?.booleanOrNull == true
internal fun JsonObject.number(key: String): Long? = (this[key] as? JsonPrimitive)?.longOrNull

private fun sessionPath(path: String): SessionKey? = path.split('/', limit = 2).let {
    if (it.size == 2) SessionKey(it[0], it[1]) else null
}
