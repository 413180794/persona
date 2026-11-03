package persona.feature.chat

import persona.core.contracts.Session
import org.junit.Assert.*
import org.junit.Test

class ChatStartingNoteTest {
    @Test fun startingAndFailedStartsAreSaidAboveTheComposer() {
        assertNull(startingNote(null))
        assertNull(startingNote(Session(machineId = "mac", sessionId = "s")))
        assertEquals(StartingNote("正在启动 Claude，消息会在它就绪后发出", failed = false),
            startingNote(Session(machineId = "mac", sessionId = "s", status = "idle", starting = true)))
        assertEquals("正在启动 Codex，消息会在它就绪后发出",
            startingNote(Session(machineId = "mac", sessionId = "s", starting = true, agent = "codex"))?.text)
        // A failure wins over a stale starting flag.
        assertEquals(StartingNote("没有启动起来：claude did not become ready", failed = true),
            startingNote(Session(machineId = "mac", sessionId = "s", status = "offline", starting = true, startError = " claude did not become ready ")))
    }
}
