package persona.feature.assistant

import org.junit.Assert.*
import org.junit.Test

class ComposerFocusRequestsTest {
    @Test fun aRequestIsTakenOnceAndLapsesWhenNobodyTookItInTime() {
        var now = 1_000L
        val focus = ComposerFocusRequests { now }
        assertNull(focus.requests.value)
        focus.request()
        val at = focus.requests.value!!
        assertTrue(focus.take(at))
        assertFalse(focus.take(at))
        assertNull(focus.requests.value)
        // A newer request replaces an older one that was not taken yet.
        focus.request()
        now += 10
        focus.request()
        assertFalse(focus.take(at))
        assertTrue(focus.take(focus.requests.value!!))
        // Not paired yet or the tab never shown: by the time the page asks, the tap is too old to act on.
        focus.request()
        val stale = focus.requests.value!!
        now += 60_000
        assertFalse(focus.take(stale))
        assertNull(focus.requests.value)
    }
}
