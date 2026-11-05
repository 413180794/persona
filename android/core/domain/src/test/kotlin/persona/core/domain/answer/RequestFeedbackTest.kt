package persona.core.domain.answer

import persona.core.domain.zhAny

import persona.core.contracts.Elicitation
import persona.core.contracts.Interaction
import org.junit.Assert.*
import org.junit.Test
import java.time.Instant

class RequestFeedbackTest {
    @Test fun deadlinesAndUrlContinuationFailClosedWithoutOpeningTheExactTarget() {
        val now = Instant.parse("2026-01-01T00:00:00Z")
        assertFalse(RequestExpiry.expired(null, now))
        assertNull(RequestExpiry.label(null, now))
        listOf("invalid", now.toString(), now.minusSeconds(1).toString()).forEach {
            assertTrue(RequestExpiry.expired(it, now))
            assertEquals(RequestExpiry.expiredLabel, RequestExpiry.label(it, now))
        }
        assertFalse(RequestExpiry.expired(now.plusNanos(1).toString(), now))
        assertEquals("即将过期", (RequestExpiry.label(now.plusNanos(1).toString(), now)).zhAny())
        val url = "https://example.com/authorize"
        val request = Interaction("request", kind = "elicitation", elicitation = Elicitation(mode = "url", url = url))
        assertFalse(AnswerBuilder.build(request, AnswerAction.SubmitElicitation).allowed)
        val opened = AnswerDraft(openedElicitationUrl = url)
        assertEquals("accept", (AnswerBuilder.build(request, AnswerAction.SubmitElicitation, opened).resolution?.action).zhAny())
        assertFalse(AnswerBuilder.build(request.copy(elicitation = request.elicitation!!.copy(url = "$url?new=1")),
            AnswerAction.SubmitElicitation, opened).allowed)
        assertFalse(AnswerBuilder.build(request, AnswerAction.SubmitElicitation, opened, AnswerContext(state = "answered")).allowed)
        listOf("javascript:alert(1)", "https:///missing-host").forEach {
            assertNull(ElicitationUrl.target(it))
            assertFalse(AnswerBuilder.build(request.copy(elicitation = request.elicitation!!.copy(url = it)),
                AnswerAction.SubmitElicitation, AnswerDraft(openedElicitationUrl = it)).allowed)
        }
        assertEquals("decline", (AnswerBuilder.build(request, AnswerAction.DeclineElicitation).resolution?.action).zhAny())
        assertEquals("cancel", (AnswerBuilder.build(request, AnswerAction.CancelElicitation).resolution?.action).zhAny())
    }
}
