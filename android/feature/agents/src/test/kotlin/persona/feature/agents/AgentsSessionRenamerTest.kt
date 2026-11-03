package persona.feature.agents

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test

@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class AgentsSessionRenamerTest {
    private val original = Session("machine", "session", title = "旧会话", paneId = "pane", slotName = "旧标签")

    @Test fun allConsumersSeeTheNameBeforeTheReplyAndRollbackTogether() = runTest {
        val api = FakeOperations()
        val repository = FakeSessions(original)
        val wiring = wiring(api)
        val renamer = AgentsSessionRenamer(wiring, repository)
        var list = emptyList<Session>()
        var space = emptyList<Session>()
        var chat = emptyList<Session>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { renamer.project(repository.sessions).collect { list = it } }
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { renamer.project(repository.sessions).collect { space = it } }
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { renamer.project(repository.sessions).collect { chat = it } }
        val reply = CompletableDeferred<Unit>()
        api.onRename = { reply.await() }
        val saving = async { renamer.rename(original, " 新会话\n", "旧标签") }
        runCurrent()
        assertFalse(saving.isCompleted)
        assertEquals(Triple(original.key, "新会话", null), api.calls.single())
        assertEquals(listOf("新会话", "新会话", "新会话"), listOf(list, space, chat).map { it.single().displayTitle })
        reply.complete(Unit)
        assertTrue(saving.await())
        assertEquals(1, repository.refreshes)
        assertEquals("新会话", chat.single().displayTitle)

        // A second field edit must retain the acknowledged title until the hub catches up.
        val failure = CompletableDeferred<Unit>()
        api.onRename = { failure.await(); throw AgentOperationFailure(false, IllegalStateException("rejected")) }
        val failed = async { runCatching { renamer.rename(chat.single(), "", " 新标签 ") } }
        runCurrent()
        assertEquals("新会话", list.single().displayTitle)
        assertEquals("新标签", chat.single().slotName)
        assertEquals(Triple(original.key, null, "新标签"), api.calls.last())
        failure.complete(Unit)
        assertTrue(failed.await().isFailure)
        runCurrent()
        assertEquals("旧标签", chat.single().slotName)
        assertEquals("新会话", chat.single().displayTitle)
        assertEquals(list, space)
        assertEquals(space, chat)

        repository.sessions.value = listOf(original.copy(title = "新会话"))
        runCurrent()
        repository.sessions.value = listOf(original.copy(title = "电脑上又改了"))
        runCurrent()
        assertEquals("电脑上又改了", chat.single().displayTitle)
    }

    @Test fun noChangesAndMissingPaneDoNotSendAndPairingChangesHideOldEdits() = runTest {
        val api = FakeOperations()
        val repository = FakeSessions(original)
        val wiring = wiring(api)
        val renamer = AgentsSessionRenamer(wiring, repository)
        var visible = emptyList<Session>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { renamer.project(repository.sessions).collect { visible = it } }
        assertFalse(renamer.rename(original, " 旧会话 ", " 旧标签 "))
        assertFalse(renamer.rename(original, " \n", ""))
        assertFalse(renamer.rename(original.copy(paneId = null, tabId = "tab"), null, "新标签"))
        assertTrue(api.calls.isEmpty())
        assertEquals(0, repository.refreshes)
        assertTrue(renamer.rename(original, "新会话", null))
        runCurrent()
        assertEquals("新会话", visible.single().displayTitle)
        val other = FakeOperations("other-pairing")
        wiring.state.value = binding(other)
        runCurrent()
        assertEquals("旧会话", visible.single().displayTitle)
        assertTrue(renamer.rename(original, null, "新标签"))
        runCurrent()
        assertEquals("旧会话", visible.single().displayTitle)
        assertEquals("新标签", visible.single().slotName)
        assertEquals(Triple(original.key, null, "新标签"), other.calls.single())
    }

    @Test fun uncertainRenameOnlyVerifiesUntilTheHubConfirmsIt() = runTest {
        val api = FakeOperations()
        val repository = FakeSessions(original)
        val renamer = AgentsSessionRenamer(wiring(api), repository)
        api.onRename = { throw AgentOperationFailure(true, IllegalStateException("unknown")) }
        assertTrue(runCatching { renamer.rename(original, "新会话", null) }.isFailure)
        assertTrue(runCatching { renamer.rename(original, "新会话", null) }.isFailure)
        assertEquals(1, api.calls.size)
        repository.sessions.value = listOf(original.copy(title = "新会话"))
        assertTrue(renamer.rename(original, "新会话", null))
        assertEquals(1, api.calls.size)
    }

    @Test fun acknowledgedNamesReconcileWithoutAnOpenScreenAndRefreshFailureDoesNotUndoSuccess() = runTest {
        val api = FakeOperations()
        val repository = FakeSessions(original)
        val renamer = AgentsSessionRenamer(wiring(api), repository)
        repository.onRefresh = { repository.sessions.value = listOf(original.copy(title = "新会话")) }
        assertTrue(renamer.rename(original, "新会话", null))
        repository.sessions.value = listOf(original.copy(title = "电脑上又改了"))
        assertEquals("电脑上又改了", renamer.project(repository.sessions).first().single().title)
        repository.onRefresh = { error("Refresh unavailable") }
        assertTrue(renamer.rename(repository.sessions.value.single(), "再改一次", null))
        assertEquals("再改一次", renamer.project(repository.sessions).first().single().title)
    }

    private fun wiring(api: FakeOperations) = object : AgentsWiring {
        override val state = MutableStateFlow(binding(api))
    }

    private fun binding(api: FakeOperations) = AgentsWiringState(api.scope, AgentsOperationsBindings(api,
        object : AssistantConversations {
            override val assistant = MutableStateFlow(AssistantSnapshot())
            override val pinned = MutableStateFlow(emptySet<SessionKey>())
            override suspend fun startConversation(): SessionKey = error("unused")
            override suspend fun refresh() = Unit
        }))

    private class FakeSessions(session: Session) : SessionsRepository {
        override val sessions = MutableStateFlow(listOf(session))
        override val machines = MutableStateFlow(emptyList<Machine>())
        var refreshes = 0
        var onRefresh: suspend () -> Unit = {}
        override suspend fun refresh() { refreshes++; onRefresh() }
    }

    private class FakeOperations(override val scope: String = "pairing") : AgentOperations {
        val calls = mutableListOf<Triple<SessionKey, String?, String?>>()
        var onRename: suspend () -> Unit = {}
        override suspend fun pins() = AgentOperationPreferences()
        override suspend fun mutes() = AgentOperationPreferences()
        override suspend fun savePins(value: AgentOperationPreferences) = Unit
        override suspend fun saveMutes(value: AgentOperationPreferences) = Unit
        override suspend fun renameSession(key: SessionKey, title: String?, tabName: String?) {
            calls += Triple(key, title, tabName)
            onRename()
        }
        override suspend fun renameSpace(machineId: String, workspaceId: String, name: String) = Unit
        override suspend fun closeSession(key: SessionKey) = Unit
    }
}
