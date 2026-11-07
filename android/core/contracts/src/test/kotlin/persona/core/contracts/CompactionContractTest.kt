package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The user's compaction on the session and the compaction boundary on a transcript item (docs/protocol/protocol.md, #1727). */
class CompactionContractTest {
    @Test fun sessionCarriesTheUsersCompaction() {
        val running = JsonCodec.decodeFromString<Session>(
            """{"machine_id":"m","session_id":"s","assistant":true,
               "compaction":{"state":"running","started_at":"2026-11-04T08:30:00+08:00"}}""",
        ).compaction!!
        assertTrue(running.running)
        assertEquals("2026-11-04T00:30:00Z", (running.startedAt).zhAny())
        assertNull(running.finishedAt)

        val failed = JsonCodec.decodeFromString<Session>(
            """{"machine_id":"m","session_id":"s","compaction":{"state":"failed","error":"Not enough messages to compact",
               "started_at":"2026-11-04T00:30:00Z","finished_at":"2026-11-04T00:31:00Z"}}""",
        ).compaction!!
        assertTrue(failed.failed)
        assertEquals("Not enough messages to compact", (failed.error).zhAny())
        assertEquals("2026-11-04T00:31:00Z", (failed.finishedAt).zhAny())

        // A state this app does not know, and an older hub without the field.
        assertEquals("unknown", (JsonCodec.decodeFromString<Session>(
            """{"session_id":"s","compaction":{"state":"queued","started_at":"2026-11-04T00:30:00Z"}}""").compaction?.state).zhAny())
        assertNull(JsonCodec.decodeFromString<Session>("""{"session_id":"s"}""").compaction)
    }

    @Test fun aNoticeCarriesItsCompactionBoundary() {
        val item = JsonCodec.decodeFromString<Item>(
            """{"id":"9#0","seq":9,"role":"system","kind":"notice","text":"上下文已压缩",
               "compact":{"trigger":"manual","pre_tokens":180902}}""",
        )
        assertEquals(CompactBoundary("manual", 180_902), item.compact)
        assertEquals(CompactBoundary(), JsonCodec.decodeFromString<Item>("""{"id":"1","compact":{}}""").compact)
        assertNull(JsonCodec.decodeFromString<Item>("""{"id":"1","kind":"notice","text":"已中断"}""").compact)
    }
}
