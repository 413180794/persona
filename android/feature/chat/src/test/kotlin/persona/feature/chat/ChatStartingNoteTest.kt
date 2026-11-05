package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Session
import org.junit.Assert.*
import org.junit.Test

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ChatStartingNoteTest {
    private val res get() = ApplicationProvider.getApplicationContext<Context>().resources
    @Test fun startingAndFailedStartsAreSaidAboveTheComposer() {
        assertNull(startingNote(res, null))
        assertNull(startingNote(res, Session(machineId = "mac", sessionId = "s")))
        assertEquals(StartingNote("正在启动 Claude，消息会在它就绪后发出", failed = false),
            startingNote(res, Session(machineId = "mac", sessionId = "s", status = "idle", starting = true)))
        assertEquals("正在启动 Codex，消息会在它就绪后发出",
            startingNote(res, Session(machineId = "mac", sessionId = "s", starting = true, agent = "codex"))?.text)
        // A failure wins over a stale starting flag.
        assertEquals(StartingNote("没有启动起来：claude did not become ready", failed = true),
            startingNote(res, Session(machineId = "mac", sessionId = "s", status = "offline", starting = true, startError = " claude did not become ready ")))
    }
}
