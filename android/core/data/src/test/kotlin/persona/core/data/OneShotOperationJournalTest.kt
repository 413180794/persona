package persona.core.data

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class OneShotOperationJournalTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test fun interruptedOperationsOnlyVerifyAfterRestartAndClosedScopeCannotRecreateFiles() = runTest {
        val scope = HubScope("hub", "pairing")
        val directory = temporary.newFolder("operations")
        val transport = Transport()
        val session = Session("machine", "session", status = "offline", cwd = "/workspace", launcher = "agent")
        var writes = 0
        val recovery = object : ChatRecoveryService {
            override suspend fun resume(session: Session): ChatRecoveryResult {
                writes++
                throw CancellationException("Process interrupted after dispatch")
            }
            override suspend fun reconnect(machineId: String): ChatActionResult {
                writes++
                return ChatActionResult.Unknown
            }
        }
        val operations = object : AgentOperations {
            override val scope = "hub/pairing"
            override suspend fun pins() = AgentOperationPreferences()
            override suspend fun mutes() = AgentOperationPreferences()
            override suspend fun savePins(value: AgentOperationPreferences) = Unit
            override suspend fun saveMutes(value: AgentOperationPreferences) = Unit
            override suspend fun renameSession(key: SessionKey, title: String?, tabName: String?) = Unit
            override suspend fun renameSpace(machineId: String, workspaceId: String, name: String) = Unit
            override suspend fun closeSession(key: SessionKey) {
                writes++
                throw AgentOperationFailure(true, IllegalStateException("Response lost"))
            }
        }
        val original = OneShotOperationJournal(directory, scope)
        val first = PersistentChatRecoveryService(recovery, original, transport)
        assertTrue(runCatching { first.resume(session) }.exceptionOrNull() is CancellationException)
        assertEquals(ChatActionResult.Unknown, first.reconnect(session.machineId))
        assertTrue(runCatching { PersistentAgentOperations(operations, original, transport).closeSession(session.key) }.isFailure)
        val operationId = original.claim(OneShotOperationKind.Resume, session.machineId, session.sessionId).operation.id
        original.close()

        val restored = OneShotOperationJournal(directory, scope)
        assertEquals(operationId, restored.claim(OneShotOperationKind.Resume, session.machineId, session.sessionId).operation.id)
        val restarted = PersistentChatRecoveryService(recovery, restored, transport)
        val close = PersistentAgentOperations(operations, restored, transport)
        assertEquals(ChatRecoveryResult.Unknown, restarted.resume(session))
        assertEquals(ChatActionResult.Unknown, restarted.reconnect(session.machineId))
        assertTrue((runCatching { close.closeSession(session.key) }.exceptionOrNull() as AgentOperationFailure).uncertain)
        assertEquals(3, writes)

        transport.sessionList = listOf(session.copy(status = "idle"))
        transport.machineList = listOf(Machine(id = session.machineId, online = true))
        assertEquals(ChatRecoveryResult.Restored(session.key), restarted.resume(session))
        assertEquals(ChatActionResult.Done, restarted.reconnect(session.machineId))
        transport.sessionList = emptyList()
        close.closeSession(session.key)
        assertEquals(3, writes)

        val other = OneShotOperationJournal(File(temporary.root, "other"), HubScope("hub", "other-pairing"))
        assertTrue(other.claim(OneShotOperationKind.Resume, session.machineId, session.sessionId).firstAttempt)
        other.close()
        restored.close()
        directory.deleteRecursively()
        assertTrue(runCatching { restored.claim(OneShotOperationKind.Close, session.machineId, session.sessionId) }.isFailure)
        assertFalse(directory.exists())
    }

    private class Transport : ChatTransport {
        var machineList = emptyList<Machine>()
        var sessionList = emptyList<Session>()
        override suspend fun machines() = machineList
        override suspend fun sessions() = sessionList
        override suspend fun items(session: SessionKey, query: ItemsQuery): ItemsResponse = error("Unused")
        override suspend fun interactions() = emptyList<Interaction>()
        override suspend fun resolve(id: String, resolution: Resolution) = error("Unused")
        override suspend fun image(session: SessionKey, itemId: String): ByteArray = error("Unused")
        override suspend fun fullItem(session: SessionKey, itemId: String): Item = error("Unused")
    }
}
