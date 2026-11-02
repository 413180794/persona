package jarvis.feature.chat

import jarvis.core.contracts.Session
import jarvis.core.contracts.api.ConnectionState
import org.junit.Assert.*
import org.junit.Test
import jarvis.core.domain.sort.SessionRenamer
import jarvis.core.domain.sort.SessionRename
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.*

class ChatChromeTest {
    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    @Test fun renameSendsOnlyChangesGuardsDuplicateSavesAndReportsSuccessOrFailure() = runTest {
        val session = Session("machine", "session", title = "旧会话", paneId = "pane", slotName = "旧标签")
        val names = MutableStateFlow(listOf(session))
        val calls = mutableListOf<SessionRename>()
        val reply = CompletableDeferred<Unit>()
        var fail = false
        val renamer = object : SessionRenamer {
            override fun project(source: Flow<List<Session>>) = names
            override suspend fun rename(session: Session, title: String?, tabName: String?): Boolean {
                val edit = SessionRename(title, tabName)
                calls += edit
                names.value = listOf(edit.apply(session))
                try {
                    if (fail) error("Rejected")
                    reply.await()
                } catch (error: Exception) {
                    names.value = listOf(session)
                    throw error
                }
                return true
            }
        }
        val state = ChatChromeState(null, backgroundScope, {}, {}, renamer)
        state.rename(session, " 旧会话 ", " 旧标签 ")
        runCurrent()
        assertTrue(calls.isEmpty())
        state.rename(session, " 新会话 ", "")
        state.rename(session, "不能重复", "")
        runCurrent()
        assertTrue(state.busy)
        assertEquals(listOf(SessionRename(title = "新会话")), calls)
        assertEquals("新会话", names.value.single().displayTitle)
        reply.complete(Unit)
        runCurrent()
        assertFalse(state.busy)
        assertEquals("已重命名", state.notice)
        fail = true
        state.rename(session, "", " 新标签 ")
        runCurrent()
        assertEquals(SessionRename(tabName = "新标签"), calls.last())
        assertEquals(session, names.value.single())
        assertFalse(state.busy)
        assertNotNull(state.notice)
        assertNotEquals("已重命名", state.notice)
    }

    @Test fun companionsStayInTheSameMachineAndSpaceAndCommandsQuoteShellInput() {
        val current = Session(machineId = "machine", sessionId = "current", spaceId = "space", cwd = "/workspace/project")
        val peer = current.copy(sessionId = "peer")
        assertEquals(listOf(peer), spaceCompanions(current, listOf(current, peer,
            peer.copy(machineId = "other"), peer.copy(spaceId = "other"), peer.copy(assistant = true))))
        assertTrue(spaceCompanions(current.copy(spaceId = null), listOf(peer.copy(spaceId = null))).isEmpty())
        assertEquals("cd -- '/workspace/project' && codex resume 'session'", resumeCommand(current.copy(agent = "codex", sessionId = "session")))
        assertEquals("cd -- '/workspace/it'\"'\"'s' && claude --resume 'session'\"'\"'id'",
            resumeCommand(current.copy(cwd = "/workspace/it's", sessionId = "session'id")))
        assertEquals("配对已失效", connectionTitle(ConnectionState.Revoked))
        assertNull(connectionTitle(ConnectionState.Online))
    }
}
