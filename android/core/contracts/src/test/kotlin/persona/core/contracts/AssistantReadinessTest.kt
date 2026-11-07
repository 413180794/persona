package persona.core.contracts

import persona.core.contracts.api.AssistantReadiness
import persona.core.contracts.api.ReadinessStep
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** GET /api/v1/assistant/readiness as docs/protocol/protocol.md "Main chat readiness" writes it, and the step that blocks sending. */
class AssistantReadinessTest {
    private fun read(json: String) = JsonCodec.decodeFromString<AssistantReadiness>(json)

    private val setup = """
        {"version":1,"mode":"setup","ready":false,"next":"model","machine":{"id":"mbp","name":"Laptop"},"launcher":"claude",
         "steps":[{"id":"computer","state":"ok","machine":"mbp"},{"id":"claude","state":"ok","version":"2.1.308"},
          {"id":"model","state":"problem","code":"not_logged_in","machine":"mbp","launcher":"claude","title":"t","detail":"d",
           "action":{"id":"connect_model"},"alt_actions":[{"id":"login_claude"}]},
          {"id":"first_message","state":"todo","code":"never_tried","action":{"id":"test_assistant"}}],
         "suggestions":[{"id":"what_can_you_do","title":"x","local":true}],
         "probe":{"ok":false,"agent":"claude","millis":2140,"error":"e"},"dismissed":false,"checked_at":"2026-11-06T09:00:00Z",
         "future_field":{"a":1}}
    """.trimIndent()

    @Test fun readsTheProtocolExample() {
        val r = read(setup)
        assertEquals("setup", r.mode)
        assertEquals("Laptop", r.machine?.name)
        assertEquals(listOf("computer", "claude", "model", "first_message"), r.steps.map { it.id })
        assertEquals("login_claude", r.step("model")?.altActions?.single()?.id)
        assertEquals(true, r.suggestions.single().local)
        assertEquals(2140L, r.probe?.millis)
    }

    @Test fun theFirstMissingStepBlocks() {
        assertEquals("model", read(setup).blockingStep()?.id)
        val noComputer = read("""{"mode":"setup","next":"computer","machine":null,"steps":[
            {"id":"computer","state":"todo","code":"no_machine","action":{"id":"add_computer"}},
            {"id":"claude","state":"unknown"},{"id":"model","state":"unknown"},{"id":"first_message","state":"unknown"}]}""")
        assertEquals(ReadinessStep.COMPUTER, noComputer.blockingStep()?.id)
        assertNull(noComputer.machine)
    }

    @Test fun nothingBlocksWhenFullOrRateLimitedOrOnlyTheFirstMessageIsLeft() {
        assertNull(read("""{"mode":"full","next":"first_message","steps":[{"id":"first_message","state":"todo"}]}""").blockingStep())
        assertNull(read("""{"mode":"setup","steps":[{"id":"model","state":"problem","code":"rate_limited"}]}""").blockingStep())
        assertNull(read("""{"mode":"chat_only","steps":[{"id":"computer","state":"todo","code":"no_machine"}]}""").blockingStep())
    }

    @Test fun aNewerHubsStepCountsThroughNext() {
        val r = read("""{"mode":"setup","next":"phone","steps":[{"id":"computer","state":"ok"},{"id":"phone","state":"todo","title":"x"}]}""")
        assertEquals("phone", r.blockingStep()?.id)
    }
}
