package persona.android.di

import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.feature.agents.AgentsDraftSource
import persona.feature.agents.AgentsOperationsBindings
import persona.feature.agents.AgentsWiringState
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test

/** Hub groups in the app layer (docs/android.md「多 hub」): ids between a group and the merged lists, and routing. */
class GroupRoutingTest {
    private val primary = GroupNames(null)
    private val other = GroupNames("g0a1b2c3d")

    @Test fun thePrimaryGroupPassesEverythingThroughUnchanged() {
        val machine = Machine("macbook", "")
        val session = Session("macbook", "s1")
        val event = ItemsEvent("macbook", "s1")
        assertSame(machine, primary.machine(machine))
        assertSame(session, primary.session(session))
        assertSame(event, primary.event(event))
        val snapshot = HubPreferencesSnapshot(HubPreferenceSet(listOf("macbook/w1"), listOf("macbook/s1")))
        assertSame(snapshot, primary.preferences(snapshot))
        assertTrue(primary.owns("macbook"))
        assertFalse(primary.owns("macbook~g0a1b2c3d"))
    }

    @Test fun anotherGroupsIdsCarryItsSuffixUpAndLoseItDown() {
        assertEquals(Machine("macbook~g0a1b2c3d", "macbook"), other.machine(Machine("macbook", "")))
        assertEquals("macbook~g0a1b2c3d", other.session(Session("macbook", "s1")).key.machineId)
        val interaction = other.interaction(Interaction("i1", "macbook", "s1"))
        assertEquals(SessionKey("macbook~g0a1b2c3d", "s1"), interaction.sessionKey)
        // Two hubs can use the same interaction id: other groups' ids carry the suffix, and answering strips it.
        assertEquals("i1~g0a1b2c3d", interaction.id)
        assertEquals("i1~g0a1b2c3d", (other.event(InteractionResolvedEvent("i1")) as InteractionResolvedEvent).id)
        assertEquals("i1~g0a1b2c3d", (other.event(NotifyEvent("interaction", "m", "s", interactionId = "i1")) as NotifyEvent).interactionId)
        assertEquals("i1", GroupKeys.raw(interaction.id))
        val events: List<ServerEvent> = listOf(ItemsEvent("m", "s"), SessionsEvent("m", listOf(Session("m", "s"))),
            MachinesEvent(listOf(Machine("m"))), RevsEvent("m"), InteractionEvent(Interaction("i", "m", "s")),
            NotifyEvent("done", "m", "s"), DeltaEvent("m", "s"), ScreenEvent("m", "s"), HookEvent("m", "s"),
            TerminalEvent("m", "s"), TerminalControlEvent("m", "s", "active"), ErrorEvent("m", "s"))
        events.map(other::event).forEach { translated -> assertTrue("$translated", translated.toString().contains("m~g0a1b2c3d")) }
        // Events without machine ids are left alone.
        val lark = LarkEvent(listOf("f"))
        assertSame(lark, other.event(lark))
        val preferences = other.preferences(HubPreferencesSnapshot(
            HubPreferenceSet(listOf("m/w"), listOf("m/s"), setOf("m/s")), seen = mapOf("m/s" to "v")))
        assertEquals(listOf("m~g0a1b2c3d/w"), preferences.pins.spaces)
        assertEquals(setOf("m~g0a1b2c3d/s"), preferences.pins.allowed)
        assertEquals(mapOf("m~g0a1b2c3d/s" to "v"), preferences.seen)
        assertEquals(SessionKey("m", "s"), other.raw(SessionKey("m~g0a1b2c3d", "s")))
    }

    @Test fun aGroupsDraftsAreStoredWithItsPlainKeys() = runBlocking<Unit> {
        val stored = mutableListOf<DraftKey>()
        val scope = HubScope("hub", "gen")
        val repository = object : DraftRepository {
            override fun observe(key: DraftKey) = flowOf(DraftSnapshot(scope, key, "x"))
            override suspend fun read(key: DraftKey) = DraftSnapshot(scope, key).also { stored += key }
            override suspend fun save(key: DraftKey, text: String, expectedVersion: Long) = DraftSnapshot(scope, key, text).also { stored += key }
        }
        val merged = SessionKey("m~g0a1b2c3d", "s")
        val wrapped = repository.inGroup(other)
        val read = wrapped.read(DraftKey.Chat(merged))
        assertEquals(DraftKey.Chat(SessionKey("m", "s")), stored.single())
        assertEquals(DraftKey.Chat(merged), read.key)
        assertEquals(DraftKey.Chat(merged), wrapped.observe(DraftKey.Chat(merged)).first().key)
        // Keys that are not a chat's pass through.
        assertEquals(DraftKey.Assistant(), wrapped.save(DraftKey.Assistant(), "t", 0).key)
        assertSame(repository, repository.inGroup(primary))
    }

    @Test fun creatingAnAgentReachesTheGroupWithItsPlainMachineAndComesBackSuffixed() = runBlocking<Unit> {
        var requested: AgentStartRequest? = null
        val service = object : AgentCreationService {
            override val scope = "hub/gen"
            override suspend fun launchers(machineId: String) = listOf(AgentLauncher(machineId))
            override suspend fun directories(machineId: String, path: String) = AgentDirectories(machineId)
            override suspend fun start(request: AgentStartRequest, operationId: String): AgentStartResult {
                requested = request
                return AgentStartResult.Created(Session(request.machineId, "new"))
            }
            override suspend fun operation(operationId: String) = AgentStartResult.Pending
        }
        val wrapped = service.inGroup(other)
        assertEquals("m", wrapped.launchers("m~g0a1b2c3d").single().name)
        val created = wrapped.start(AgentStartRequest("m~g0a1b2c3d", "/", "claude"), "op") as AgentStartResult.Created
        assertEquals("m", requested!!.machineId)
        assertEquals(SessionKey("m~g0a1b2c3d", "new"), created.session.key)
    }

    @Test fun mergedPinsWriteOnlyTheGroupsWhosePartChanged() = runBlocking<Unit> {
        class Hub(var pins: AgentOperationPreferences, val reachable: Boolean = true) : AgentOperations {
            val saved = mutableListOf<AgentOperationPreferences>()
            override val scope = "s"
            override suspend fun pins(): AgentOperationPreferences { check(reachable); return pins }
            override suspend fun mutes() = AgentOperationPreferences()
            override suspend fun savePins(value: AgentOperationPreferences) { check(reachable); saved += value; pins = value }
            override suspend fun saveMutes(value: AgentOperationPreferences) = Unit
            override suspend fun renameSession(key: SessionKey, title: String?, tabName: String?) = Unit
            override suspend fun renameSpace(machineId: String, workspaceId: String, name: String) = Unit
            override suspend fun closeSession(key: SessionKey) = Unit
        }
        // Each group's operations already speak merged ids (the per-group wiring translates).
        val home = Hub(AgentOperationPreferences(sessions = listOf("a/1")))
        val away = Hub(AgentOperationPreferences(sessions = listOf("b~g0a1b2c3d/2")), reachable = false)
        val assistant = object : AssistantConversations {
            override val assistant = MutableStateFlow(AssistantSnapshot())
            override val pinned = MutableStateFlow(emptySet<SessionKey>())
            override suspend fun refresh() = Unit
            override suspend fun startConversation() = SessionKey("a", "main")
        }
        fun state(operations: AgentOperations) = AgentsWiringState("s", AgentsOperationsBindings(operations, assistant),
            drafts = AgentsDraftSource { emptyFlow() })
        val merged = mergedAgentsState(listOf(primary, other), listOf(state(home), state(away)), 0)
        val operations = requireNotNull(merged.operations).operations
        // The unreachable group contributes nothing it does not know and is never overwritten with nothing.
        val pins = operations.pins()
        assertEquals(listOf("a/1"), pins.sessions)
        operations.savePins(pins.copy(sessions = pins.sessions + "a/2"))
        assertEquals(listOf(AgentOperationPreferences(sessions = listOf("a/1", "a/2"))), home.saved)
        assertTrue(away.saved.isEmpty())
        // Pinning in the group that could not be read fails instead of replacing its list.
        val failed = runCatching { operations.savePins(pins.copy(sessions = pins.sessions + "a/2" + "b~g0a1b2c3d/9")) }
        assertTrue(failed.isFailure)
        assertTrue(away.saved.isEmpty())
        assertEquals("s+s", merged.scope)
    }

    @Test fun fleetsAddUpAndTheMostUrgentPhaseWins() {
        val one = FleetState(phase = "working", working = 2, items = listOf(FleetItem("a")))
        assertSame(one, mergeFleets(listOf(one), one))
        val merged = mergeFleets(listOf(one, FleetState(phase = "needsYou", needsYou = 1, unread = 3, items = listOf(FleetItem("b")))), one)
        assertEquals("needsYou", merged.phase)
        assertEquals(1, merged.needsYou)
        assertEquals(2, merged.working)
        assertEquals(3, merged.unread)
        assertEquals(listOf("a", "b"), merged.items.map { it.id })
    }
}
