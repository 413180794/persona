package persona.core.data

import persona.core.contracts.CoreText
import persona.core.contracts.CoreText.Key
import persona.core.contracts.JsonCodec
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
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
    /** The manager the page shows (docs/assistant/managers.md step 4): each keeps its own conversations, cached apart. */
    private fun manager(): String? = AssistantManagerChoice.current.value.manager
    private fun cacheKey(transport: InboxAssistantTransport, manager: String? = manager()) =
        "${transport.scope}:assistant" + (manager?.let { "@$it" } ?: "")
    private suspend fun restore(transport: InboxAssistantTransport) {
        val key = cacheKey(transport)
        if (restoredScope == key) return
        current.value = AssistantSnapshot()
        currentPins.value = preferences?.preferences?.value?.pins?.sessions?.mapNotNull(::sessionPath)?.toSet().orEmpty()
        quietly { snapshots.restore<JsonObject>(key)?.let { apply(it) } }
        restoredScope = key
    }
    private fun apply(data: JsonObject, manager: String? = manager()) {
        val machine = data.string("machine_id")
        val main = data.string("main")
        val conversations = data.array("conversations").map { JsonCodec.decodeFromJsonElement<Session>(it) }
        // Their own requests (feed, stop…) go to this manager whichever the page shows later (HubHttpClient: one
        // conversation's routes carry no pick of the page). Only a picked manager is noted: the default's need none (the
        // hub finds a conversation's manager itself), and a default noted before the roster read would be a placeholder.
        (conversations.map { it.sessionId } + main).filter { it.isNotEmpty() }
            .forEach { AssistantManagerChoice.noteItem(listOf("assistant", "conversations", it), manager) }
        current.value = AssistantSnapshot(
            if (machine.isNotEmpty() && main.isNotEmpty()) SessionKey(machine, main) else null, conversations, manager)
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
                // Another manager's conversations are its page's, not this one's.
                val choice = AssistantManagerChoice.current.value
                if (connection.current.value != null) current.update { snapshot ->
                    val byKey = updates.associateBy { it.key }
                    snapshot.copy(conversations = (snapshot.conversations.map { byKey[it.key] ?: it } +
                        updates.filter { it.assistant == true && choice.owns(it.manager) }).distinctBy { it.key })
                }
            }
        }
        // Another manager on the page: its own conversations (its cache at once, then the hub's).
        scope.launch {
            AssistantManagerChoice.current.map { it.manager }.distinctUntilChanged().drop(1).collect {
                if (connection.current.value != null) try { refresh() } catch (e: CancellationException) { throw e } catch (_: Exception) { }
            }
        }
    }

    override suspend fun refresh() = lock.withLock {
        val transport = requireNotNull(connection.current.value) { CoreText.of(Key.PairHubFirst).shown }
        restore(transport)
        val manager = manager()
        val data = transport.assistantOf(manager)
        val pins = if (preferences != null) preferences.preferences.value.pins.sessions.mapNotNull(::sessionPath).toSet()
            else transport.pins().array("sessions").mapNotNull { (it as? JsonPrimitive)?.contentOrNull?.let(::sessionPath) }.toSet()
        if (connection.current.value !== transport || manager() != manager) return@withLock
        apply(data, manager)
        snapshots.save(cacheKey(transport, manager), data)
        currentPins.value = pins
    }

    override suspend fun startConversation(): SessionKey = start(null)

    override suspend fun startConversation(launcher: String): SessionKey = start(launcher)

    override suspend fun mainConversationOf(manager: String): SessionKey = lock.withLock {
        val transport = requireNotNull(connection.current.value) { CoreText.of(Key.PairHubFirst).shown }
        // Asked of that manager alone (`?manager=`), whatever the page shows meanwhile: never another one's.
        val config = transport.assistantOf(manager)
        val machine = config.string("machine_id")
        val main = config.string("main")
        if (machine.isNotEmpty() && main.isNotEmpty()) return@withLock SessionKey(machine, main)
        val session = JsonCodec.decodeFromJsonElement<Session>(transport.createConversationOf(manager, true).getValue("session"))
        check(connection.current.value === transport) { CoreText.of(Key.ConnectionSwitched).shown }
        // Still on its page: the new main conversation shows at once.
        if (manager() == manager) current.update {
            it.copy(mainSession = session.key, conversations = (listOf(session) + it.conversations).distinctBy { c -> c.key })
        }
        session.key
    }

    private suspend fun start(launcher: String?): SessionKey = lock.withLock {
        val transport = requireNotNull(connection.current.value) { CoreText.of(Key.PairHubFirst).shown }
        // The manager the page shows as this starts: both requests go to it even if the page moves on meanwhile.
        val manager = manager()
        // Refresh the main pointer before deciding whether this is the first conversation.
        val config = transport.assistantOf(manager)
        val isMain = config.string("main").isEmpty()
        val result = transport.createConversationOf(manager, isMain, launcher)
        val session = JsonCodec.decodeFromJsonElement<Session>(result.getValue("session"))
        check(connection.current.value === transport) { CoreText.of(Key.ConnectionSwitched).shown }
        if (manager() != manager) return@withLock session.key
        current.update {
            it.copy(mainSession = if (isMain) session.key else it.mainSession,
                conversations = (listOf(session) + it.conversations).distinctBy { conversation -> conversation.key })
        }
        snapshots.save(cacheKey(transport, manager), buildJsonObject {
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
