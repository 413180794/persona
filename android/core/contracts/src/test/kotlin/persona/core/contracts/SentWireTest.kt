package persona.core.contracts

import persona.core.contracts.api.SentItem
import persona.core.contracts.api.SentOrigin
import persona.core.contracts.api.SentPage
import persona.core.contracts.api.SentRule
import persona.core.contracts.api.SentWire
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.*
import org.junit.Test

/** The ledger's JSON read the way the web page reads it (web/src/lib/sent.ts), and a page in a hub group's ids. */
class SentWireTest {
    private fun page(json: String) = SentWire.page(JsonCodec.parseToJsonElement(json))

    @Test fun aFieldOfTheWrongTypeIsItsEmptyValue() {
        val row = page("""{"items":[{"id":4,"at":5,"connector":["mail"],"action":null,"text":{"a":1},"summary":true,
            "target":"x","origin":{"machine_id":3,"session_id":"s","extension":null},"message_ref":1,"recalled_at":{}}]}""").items.single()
        assertEquals(SentItem(4), row.copy(origin = SentOrigin()))
        // Each field of an object is read by itself: the one that is a string stays.
        assertEquals(SentOrigin(sessionId = "s"), row.origin)
    }

    @Test fun aRuleIsReadWithItsMatchAndAnythingElseInItsPlaceIsNoRule() {
        val kinds = page("""{"items":[
          {"id":3,"let_through":{"kind":"rule","rule":{"connector":"lark","action":"POST /im/v1/messages","match":{"chat_id":"oc_1","kind":["a","b"]},"decision":"allow"}}},
          {"id":2,"let_through":{"kind":"rule","rule":"x"}},
          {"id":1,"let_through":{"kind":"rule","rule":{"match":[1]}}}]}""").items.map { it.letThrough }
        assertEquals(SentRule("lark", "POST /im/v1/messages", mapOf("chat_id" to JsonPrimitive("oc_1"), "kind" to JsonCodec.parseToJsonElement("""["a","b"]""")), "allow"), kinds[0].rule)
        assertNull(kinds[1].rule)
        // A rule the hub gave is a rule, though nothing in it is readable.
        assertEquals(SentRule(), kinds[2].rule)
    }

    @Test fun anElementThatIsNotARowIsSkippedAndThePageStaysInTheHubsOrder() {
        val rows = page("""{"items":[{"id":9},"x",{"id":2},[],{"id":5}]}""").items
        assertEquals(listOf(9L, 2L, 5L), rows.map { it.id })
    }

    @Test fun aPageInAHubGroupHasTheGroupsMachineIdsOnItsRows() {
        val items = listOf(
            SentItem(2, origin = SentOrigin("mac", "s1")),
            SentItem(1, origin = SentOrigin(sessionId = "s2", extensionId = "examplechat")),
        )
        val page = SentPage(items, next = 1)
        // The primary group keeps the ids as the hub speaks them.
        assertSame(page, page.inGroup(null))
        val other = page.inGroup("g0a1b2c3d")
        assertEquals("mac~g0a1b2c3d", other.items[0].origin.machineId)
        assertEquals("s1", other.items[0].origin.sessionId)
        // A row that names no machine names none in the group either.
        assertEquals("", other.items[1].origin.machineId)
        assertEquals("examplechat", other.items[1].origin.extensionId)
        assertEquals(1L, other.next)
    }
}
