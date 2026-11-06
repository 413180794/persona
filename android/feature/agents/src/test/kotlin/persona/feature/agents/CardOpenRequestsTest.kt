package persona.feature.agents

import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.navigation.RequestRef

/** The Agents page opens a notification's card once, and not long after the tap (#4808). */
class CardOpenRequestsTest {
    private var now = 1_000L
    private val requests = CardOpenRequests { now }
    private val card = RequestRef("machine", "script")

    @Test fun takenOnce() {
        requests.request(card)
        val asked = requests.requests.value!!
        assertTrue(requests.take(asked))
        assertFalse(requests.take(asked))
        assertNull(requests.requests.value)
    }

    @Test fun aNewerTapReplacesTheOlder() {
        requests.request(card)
        val older = requests.requests.value!!
        now += 1
        requests.request(RequestRef("machine", "other"))
        assertFalse(requests.take(older))
        assertTrue(requests.take(requests.requests.value!!))
    }

    @Test fun lapses() {
        requests.request(card)
        now += 15_001
        assertFalse(requests.take(requests.requests.value!!))
        assertNull(requests.requests.value)
    }
}
