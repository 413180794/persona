package persona.core.domain.tasks

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.JsonCodec
import persona.core.contracts.Todo

class DeliveryTest {
    private fun todo(json: String) = JsonCodec.decodeFromString(Todo.serializer(), json)

    @Test
    fun anOlderHubSendsNoDelivery() {
        assertNull(todo("""{"id":"todo_1","title":"修登录"}""").hold)
    }

    @Test
    fun anOpenTaskHeldShortOfLiveSaysWhere() {
        val t = todo("""{"id":"todo_1","title":"修登录","delivery":{"state":"pr","gate":" 等你批准合并 ","unblock":"user",
            "at":"2026-11-06T02:00:00Z","something_new":1}}""")
        assertEquals(TodoHold(HoldStep.Pr, "等你批准合并", HoldUnblock.User), t.hold)
        assertEquals(TodoHold(HoldStep.Merged, null, null), todo("""{"id":"t","delivery":{"state":"merged","gate":""}}""").hold)
    }

    @Test
    fun liveNothingToShipDoneOrUnknownSayNothing() {
        for (state in listOf("live", "none", "shipped")) {
            assertNull(todo("""{"id":"t","delivery":{"state":"$state"}}""").hold)
        }
        assertNull(todo("""{"id":"t","done_at":"2026-11-06T02:00:00Z","delivery":{"state":"pr"}}""").hold)
    }
}
