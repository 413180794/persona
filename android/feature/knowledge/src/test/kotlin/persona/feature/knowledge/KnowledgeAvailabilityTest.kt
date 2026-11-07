package persona.feature.knowledge

import java.io.IOException
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.api.HubCodedFailure

class KnowledgeAvailabilityTest {
    @Test fun businessFailuresAreNotNetworkFailures() {
        assertEquals(R.string.knowledge_disabled_text, loadMessage(ResponseFailure("kb_disabled")))
        assertEquals(R.string.knowledge_opening, loadMessage(ResponseFailure("kb_unavailable")))
        assertEquals(R.string.knowledge_unreadable, loadMessage(ResponseFailure(null)))
        assertEquals(R.string.knowledge_unreachable, loadMessage(IOException("connection refused")))
        assertEquals(R.string.knowledge_disabled_text,
            loadMessage(IOException("wrapped", ResponseFailure("kb_disabled"))))
    }
}

private class ResponseFailure(override val code: String?) : IOException(), HubCodedFailure {
    override val status = 503
}
