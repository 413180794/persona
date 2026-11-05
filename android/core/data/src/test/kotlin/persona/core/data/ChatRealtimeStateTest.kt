package persona.core.data

import persona.core.contracts.DeltaEvent
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import org.junit.Test
import org.junit.Assert.assertEquals

class ChatRealtimeStateTest {
    private val key = SessionKey("m1", "s1")

    @Test
    fun `a streamed reply the hub cut matches its transcript item once`() {
        val state = ChatRealtimeState(key)
        val user = Item("u1#0", 1, role = "user", kind = "text", text = "写长一点")
        val whole = "x".repeat(30_000)
        state.delta(DeltaEvent("m1", "s1", whole), listOf(user))
        state.delta(DeltaEvent("m1", "s1", ""), listOf(user))
        val cut = Item("a1#0", 2, role = "assistant", kind = "text",
            text = whole.take(24 * 1024) + "\n…（已截断，共 30000 字节）", fullSize = 30_000)

        val rows = state.items(listOf(user, cut)).filter { it.role == "assistant" && it.kind == "text" }

        assertEquals(listOf(cut.text), rows.map { it.text })
    }
}
