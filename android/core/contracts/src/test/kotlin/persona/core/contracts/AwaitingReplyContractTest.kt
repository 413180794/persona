package persona.core.contracts

import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.TriageSwitches

/** Session.asks / awaits_reply and the triage's reply_reminder (#5096): optional, and absent means what older hubs did. */
class AwaitingReplyContractTest {
    private fun session(extra: String) = JsonCodec.decodeFromString<Session>(
        """{"machine_id":"m1","session_id":"s1","status":"done","state":"idle"$extra}""")

    @Test fun aSessionSaysWhatItAsksAndThatItAwaitsTheReply() {
        val s = session(""","asks":"要不要推到 beta？","awaits_reply":true""")
        assertEquals("要不要推到 beta？", s.asks)
        assertTrue(s.awaitsReply)
        val older = session("")
        assertNull(older.asks)
        assertFalse(older.awaitsReply)
    }

    @Test fun theReplyReminderIsOnUntilTurnedOffAndAlwaysSent() {
        val read = JsonCodec.decodeFromString<TriageSwitches>("""{"enabled":false}""")
        assertNull(read.replyReminder)
        assertTrue(read.remindsReply)
        assertFalse(JsonCodec.decodeFromString<TriageSwitches>("""{"reply_reminder":false}""").remindsReply)
        // turned back on: sent as true, so the hub does not keep the old off
        val on = JsonCodec.parseToJsonElement(JsonCodec.encodeToString(TriageSwitches.serializer(), read.copy(replyReminder = true)))
        assertTrue(on.jsonObject.getValue("reply_reminder").jsonPrimitive.boolean)
        val off = JsonCodec.parseToJsonElement(JsonCodec.encodeToString(TriageSwitches.serializer(), read.copy(replyReminder = false)))
        assertFalse(off.jsonObject.getValue("reply_reminder").jsonPrimitive.boolean)
    }
}
