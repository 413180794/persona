package jarvis.feature.agents

import jarvis.core.contracts.Session
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.*
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import org.junit.Assert.*
import org.junit.Test

class AgentOperationsTest {
    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    @Test fun spaceRenameUpdatesMembersAndPreferenceKeysBeforeReplyAndRestoresOnFailure() = runTest {
        val api = FakeOperations()
        api.pins = AgentOperationPreferences(spaces = listOf("machine/旧空间", "other/保留"))
        api.mutes = AgentOperationPreferences(spaces = listOf("machine/旧空间"), allowed = setOf("machine/session"))
        val operations = AgentOperationsState(api)
        operations.refresh()
        val first = Session("machine", "session", workspaceId = "workspace", spaceId = "space", spaceName = "旧空间")
        val second = first.copy(sessionId = "second")
        val untouched = first.copy(machineId = "other")
        val reply = CompletableDeferred<Unit>()
        api.onSpaceRename = { reply.await(); throw AgentOperationFailure(false, IllegalStateException("rejected")) }
        val saving = async { runCatching { operations.renameSpace(first, " 新空间\n") } }
        runCurrent()
        assertEquals(listOf("新空间", "新空间", "旧空间"), operations.state.value.project(listOf(first, second, untouched)).map { it.spaceName })
        assertEquals(listOf("machine/新空间", "other/保留"), operations.state.value.pins?.spaces)
        assertEquals(listOf("machine/新空间"), operations.state.value.mutes?.spaces)
        assertEquals(setOf("machine/session"), operations.state.value.mutes?.allowed)
        assertTrue(operations.state.value.busy)
        reply.complete(Unit)
        assertTrue(saving.await().isFailure)
        assertEquals(listOf(first, second, untouched), operations.state.value.project(listOf(first, second, untouched)))
        assertEquals(api.pins, operations.state.value.pins)
        assertEquals(api.mutes, operations.state.value.mutes)
        api.onSpaceRename = { fail("No-op must not call the API") }
        operations.renameSpace(first, " ")
        operations.renameSpace(first, " 旧空间 ")
    }

    @Test fun preferencesPreserveRemoteEntriesAndRollbackFailedSpaceRenameAndClose() = runTest {
        val api = FakeOperations()
        val operations = AgentOperationsState(api)
        val session = Session(machineId = "machine", sessionId = "session", title = "旧会话",
            workspaceId = "workspace", spaceId = "space", spaceName = "旧空间", status = "working")
        api.pins = AgentOperationPreferences(spaces = listOf("machine/旧空间"), sessions = listOf("other/session"))
        api.mutes = AgentOperationPreferences(spaces = listOf("machine/旧空间"), allowed = setOf("other/allowed"))
        operations.refresh()
        operations.preference(false, false, session.key.path, "machine/旧空间")
        assertEquals(setOf("other/allowed", session.key.path), api.mutes.allowed)
        operations.preference(true, false, session.key.path)
        assertEquals(listOf("other/session", session.key.path), api.pins.sessions)
        val before = operations.state.value
        api.fail = true
        try { operations.renameSpace(session, "新空间"); fail("Rename must fail") } catch (_: AgentOperationFailure) { }
        assertEquals(before, operations.state.value)
        assertEquals(listOf(session), operations.state.value.project(listOf(session)))
        try { operations.session(session, AgentSessionEdit(closed = true)); fail("Close must fail") } catch (_: AgentOperationFailure) { }
        assertEquals(before, operations.state.value)
        api.uncertain = true
        try { operations.session(session, AgentSessionEdit(closed = true)); fail("Close must be uncertain") } catch (_: AgentOperationFailure) { }
        assertTrue("close:${session.key.path}" in operations.state.value.uncertain)
        assertEquals(listOf(session), operations.state.value.project(listOf(session)))
        try { operations.session(session, AgentSessionEdit(closed = true)); fail("Duplicate close must be blocked") } catch (_: IllegalStateException) { }
        assertEquals(listOf(session.key), operations.reconcile(listOf(session.copy(status = "offline", state = "offline"))))
        assertTrue(operations.state.value.uncertain.isEmpty())
        api.fail = false
        operations.renameSpace(session, "新空间")
        assertEquals("workspace", api.renamedWorkspace)
        assertEquals(listOf("machine/新空间"), operations.state.value.pins?.spaces)
        assertEquals(listOf("machine/新空间"), operations.state.value.mutes?.spaces)
        assertEquals("新空间", operations.state.value.project(listOf(session)).single().spaceName)
        assertEquals("codex resume 'a'\\''b'", session.copy(agent = "codex", sessionId = "a'b").recoveryCommand())
    }

    private class FakeOperations : AgentOperations {
        override val scope = "scope"
        var pins = AgentOperationPreferences()
        var mutes = AgentOperationPreferences()
        var fail = false
        var uncertain = false
        var renamedWorkspace: String? = null
        var onSpaceRename: suspend () -> Unit = {}
        private fun checkFailure() { if (fail) throw AgentOperationFailure(uncertain, IllegalStateException()) }
        override suspend fun pins() = pins
        override suspend fun mutes() = mutes
        override suspend fun savePins(value: AgentOperationPreferences) { checkFailure(); pins = value }
        override suspend fun saveMutes(value: AgentOperationPreferences) { checkFailure(); mutes = value }
        override suspend fun renameSession(key: SessionKey, title: String?, tabName: String?) { checkFailure() }
        override suspend fun renameSpace(machineId: String, workspaceId: String, name: String) { checkFailure(); renamedWorkspace = workspaceId; onSpaceRename() }
        override suspend fun closeSession(key: SessionKey) { checkFailure() }
    }
}
