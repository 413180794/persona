package jarvis.android.chat

import jarvis.android.data.ConnState
import jarvis.android.data.HubException
import jarvis.android.data.ImagePayload
import jarvis.android.data.Item
import jarvis.android.data.SessionKey
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import java.time.Instant

class OutboxTest {
    private val k = SessionKey("m", "s")

    private fun userItem(text: String, ts: String?) = Item(id = "x#0", seq = 1, role = "user", kind = "text", text = text, ts = ts)

    @Test fun arrivedMessagesDropOnlyWhenWrittenAfterThem() {
        val now = System.currentTimeMillis()
        val p = PendingMessage("id1", "继续", createdAt = now)
        // An older 「继续」 in the transcript must not swallow the new one…
        val old = Instant.ofEpochMilli(now - 3_600_000).toString()
        assertEquals(listOf(p), Outbox.dropArrived(listOf(p), listOf(userItem("继续", old))))
        // …the one typed now does (pasted wrappers and whitespace aside).
        val fresh = Instant.ofEpochMilli(now + 2_000).toString()
        assertEquals(emptyList<PendingMessage>(), Outbox.dropArrived(listOf(p), listOf(userItem(" 继续\n", fresh))))
        assertEquals(emptyList<PendingMessage>(), Outbox.dropArrived(listOf(p), listOf(userItem("继续", null))))
    }

    @Test fun failuresMapToPhasesLikeIos() {
        assertEquals(PendingMessage.Phase.WAITING, Outbox.phaseOf(IOException("timeout")))
        assertEquals(PendingMessage.Phase.REFUSED, Outbox.phaseOf(HubException(409, "draft", "input_draft")))
        assertEquals(PendingMessage.Phase.REFUSED, Outbox.phaseOf(HubException(409, "dialog", "dialog_open")))
        val unknown = HubException(502, "maybe", null, buildJsonObject { put("delivered", "unknown") })
        assertEquals(PendingMessage.Phase.UNKNOWN, Outbox.phaseOf(unknown))
        assertEquals(PendingMessage.Phase.FAILED, Outbox.phaseOf(HubException(500, "boom")))
    }

    @Test fun onlyWhatIsGoingOutIsSavedAndItComesBackWaiting() {
        val img = ImagePayload(mime = "image/jpeg", data = "AAAA")
        val all = mapOf(
            k to listOf(
                PendingMessage("a", "one", PendingMessage.Phase.WAITING, createdAt = 5, images = listOf(img)),
                PendingMessage("b", "two", PendingMessage.Phase.SENT),
                PendingMessage("c", "three", PendingMessage.Phase.SENDING, createdAt = 7),
                PendingMessage("d", "four", PendingMessage.Phase.FAILED),
            ),
        )
        val entries = Outbox.entries(all)
        assertEquals(listOf("a", "c"), entries.map { it.clientMsgId })
        val back = Outbox.restore(entries)[k]!!
        assertEquals(listOf("one", "three"), back.map { it.text })
        assertTrue(back.all { it.phase == PendingMessage.Phase.WAITING })
        assertEquals(listOf(img), back[0].images)
    }

    @Test fun refusalFieldsOfferClearAndSend() {
        val p = PendingMessage("a", "x", PendingMessage.Phase.REFUSED, fields = mapOf("code" to "input_draft", "clearable" to "draft"))
        assertTrue(p.canClearDraft)
        assertFalse(p.copy(fields = mapOf("code" to "input_draft")).canClearDraft)
        assertEquals("unknown", (HubException(502, "", null, buildJsonObject { put("delivered", "unknown") }).fields?.get("delivered") as JsonPrimitive).content)
    }
}

class ConnectionLabelTest {
    @Test fun titleWordsFollowIos() {
        assertNull(ConnectionWatch.raw(ConnState.Online, hasNetwork = true, stalled = false, catchingUp = false))
        assertEquals("连接中…", ConnectionWatch.raw(ConnState.Online, hasNetwork = true, stalled = true, catchingUp = false))
        assertEquals("更新中…", ConnectionWatch.raw(ConnState.Online, hasNetwork = true, stalled = false, catchingUp = true))
        assertEquals("等待网络", ConnectionWatch.raw(ConnState.Offline, hasNetwork = false, stalled = false, catchingUp = false))
        assertEquals("连接中…", ConnectionWatch.raw(ConnState.Connecting, hasNetwork = true, stalled = false, catchingUp = false))
        assertEquals("需要重新配对", ConnectionWatch.raw(ConnState.Unauthorized, hasNetwork = true, stalled = false, catchingUp = false))
        assertNull(ConnectionWatch.raw(null, hasNetwork = false, stalled = false, catchingUp = false))
    }
}
