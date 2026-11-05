package persona.feature.agents

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.launch
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*

@OptIn(ExperimentalCoroutinesApi::class)
class HerdrNoticeTest {
    private val off = HerdrState(false, false, true)
    private val stopped = Machine("desk", "Office", online = true)

    @Test fun onlyOnlineClassicHerdrMachinesKnownToBeStoppedOfferStart() {
        assertTrue(stopped.herdrStopped(off))
        assertTrue(stopped.copy(engine = "herdr-both").herdrStopped(off))
        assertFalse(stopped.copy(online = false).herdrStopped(off))
        assertFalse(stopped.herdrStopped(off.copy(known = false)))
        assertFalse(stopped.herdrStopped(null))
        assertFalse(stopped.herdrStopped(off.copy(running = true)))
        for (engine in listOf("herdr-core", "orca", "paseo", "future"))
            assertFalse(engine, stopped.copy(engine = engine).herdrStopped(off))
    }

    private class Repository(machine: Machine) : SessionsRepository {
        override val machines = MutableStateFlow(listOf(machine))
        override val sessions = MutableStateFlow<List<Session>>(emptyList())
        override suspend fun refresh() = Unit
        override suspend fun applyMachine(machine: Machine) { machines.value = machines.value.map { if (it.id == machine.id) machine else it } }
    }

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: kotlinx.serialization.json.JsonObject) = true
    }

    private class Api : MachineManagement {
        val calls = mutableListOf<String>()
        val answer = CompletableDeferred<HerdrState>()
        var status: suspend () -> HerdrState = { HerdrState(false, false, true) }
        override suspend fun herdr(machineId: String) = status()
        override suspend fun startHerdr(machineId: String): HerdrState { calls += machineId; return answer.await() }
        override suspend fun launchers(machineId: String) = error("unused")
        override suspend fun launcherQuota(machineId: String, name: String, refresh: Boolean) = error("unused")
        override suspend fun reconnect(machineId: String) = error("unused")
        override suspend fun deploy(machineId: String) = error("unused")
        override suspend fun setEngine(machineId: String, engine: String) = error("unused")
        override suspend fun claudeSettings(machineId: String) = error("unused")
        override suspend fun setClaudeSetting(machineId: String, key: String, value: kotlinx.serialization.json.JsonElement) = error("unused")
    }

    @Test fun startIsExplicitDeduplicatedAndUsesTheConfirmedResult() = runTest {
        val api = Api()
        val repository = Repository(stopped)
        val notices = mutableListOf<String>()
        val starter = HerdrStarter(api, repository, Connection(), backgroundScope, notices::add)
        runCurrent()
        assertTrue(api.calls.isEmpty())
        starter.refresh()
        starter.start("desk"); starter.start("desk")
        assertEquals(setOf("desk"), starter.starting.value)
        runCurrent()
        assertEquals(listOf("desk"), api.calls)
        repository.machines.value = listOf(stopped.copy(name = "Renamed", nodeVersion = "new"))
        api.answer.complete(HerdrState(false, true, true))
        runCurrent()
        assertTrue(starter.starting.value.isEmpty())
        assertTrue(starter.states.value.getValue("desk").running)
        assertEquals("new", repository.machines.value.single().nodeVersion)
        assertEquals(listOf("Renamed 的 herdr 已启动"), notices)
        starter.start("desk"); runCurrent()
        assertEquals(1, api.calls.size)
    }

    @Test fun failureKeepsTheNoticeAndDoesNotRetryOnItsOwn() = runTest {
        val api = Api()
        val repository = Repository(stopped)
        val notices = mutableListOf<String>()
        val starter = HerdrStarter(api, repository, Connection(), backgroundScope, notices::add)
        starter.refresh()
        starter.start("desk"); runCurrent()
        api.answer.completeExceptionally(IllegalStateException("engine_missing")); runCurrent()
        assertTrue(stopped.herdrStopped(starter.states.value["desk"]))
        assertTrue(starter.starting.value.isEmpty())
        assertEquals(1, api.calls.size)
        assertTrue(notices.single().contains("engine_missing"))
    }

    @Test fun disconnectedUnknownAndGoneMachinesCannotStart() = runTest {
        val api = Api()
        val repository = Repository(stopped)
        val connection = Connection()
        val starter = HerdrStarter(api, repository, connection, backgroundScope) { fail(it) }
        connection.state.value = ConnectionState.WaitingForNetwork
        starter.start("desk")
        connection.state.value = ConnectionState.Online
        api.status = { off.copy(known = false) }; starter.refresh()
        starter.start("desk"); starter.start("gone"); runCurrent()
        assertTrue(api.calls.isEmpty())
        assertTrue(starter.starting.value.isEmpty())
    }

    @Test fun anUnconfirmedReplyDoesNotClaimSuccess() = runTest {
        val api = Api()
        val repository = Repository(stopped)
        val notices = mutableListOf<String>()
        val starter = HerdrStarter(api, repository, Connection(), backgroundScope, notices::add)
        starter.refresh()
        starter.start("desk"); runCurrent()
        api.answer.complete(HerdrState(true, false, true)); runCurrent()
        assertTrue(stopped.herdrStopped(starter.states.value["desk"]))
        assertEquals(listOf("未确认 herdr 已启动，请刷新后查看"), notices)
    }

    @Test fun cancellationClearsPendingWithoutClaimingAResult() = runTest {
        val api = Api()
        val repository = Repository(stopped)
        val notices = mutableListOf<String>()
        val starter = HerdrStarter(api, repository, Connection(), backgroundScope, notices::add)
        starter.refresh()
        starter.start("desk"); runCurrent()
        api.answer.cancel(); runCurrent()
        assertTrue(starter.starting.value.isEmpty())
        assertTrue(stopped.herdrStopped(starter.states.value["desk"]))
        assertTrue(notices.isEmpty())
    }

    @Test fun anOldStatusReadCannotUndoAConfirmedStart() = runTest {
        val api = Api()
        val repository = Repository(stopped)
        val starter = HerdrStarter(api, repository, Connection(), backgroundScope) { }
        starter.refresh()
        val stale = CompletableDeferred<HerdrState>()
        api.status = { stale.await() }
        val refreshing = backgroundScope.launch { starter.refresh() }
        runCurrent()
        starter.start("desk"); runCurrent()
        api.answer.complete(HerdrState(false, true, true)); runCurrent()
        stale.complete(off); refreshing.join()
        assertTrue(starter.states.value.getValue("desk").running)
    }

    @Test fun anUnavailableStatusIsUnknownAndCannotOfferStart() = runTest {
        val api = Api()
        val starter = HerdrStarter(api, Repository(stopped), Connection(), backgroundScope) { fail(it) }
        starter.refresh()
        assertTrue(starter.states.value.getValue("desk").known)
        api.status = { throw IllegalStateException("old hub") }
        starter.refresh()
        assertTrue(starter.states.value.isEmpty())
        starter.start("desk"); runCurrent()
        assertTrue(api.calls.isEmpty())
    }
}
