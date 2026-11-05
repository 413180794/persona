package persona.feature.chat

import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.domain.ports.PortLink

class PortPresenceTest {
    @Test fun listedPortsAreLiveAndOthersAreAskedOnceAWhile() {
        val scope = TestScope()
        var clock = 0L
        val asked = mutableListOf<Int>()
        val presence = PortPresence(scope, { port -> asked += port; (port == 3000) to "后台".takeIf { port == 3000 } }, { clock })
        val listed = mapOf(5173 to "落地页")
        assertEquals(PortPresence.State.Live("落地页"), presence.state(5173, listed))
        presence.ask(listOf(5173, 3000, 4000), listed)
        scope.runCurrent()
        assertEquals(listOf(3000, 4000), asked) // the listed one is never asked
        assertEquals(PortPresence.State.Live("后台"), presence.state(3000, listed))
        assertEquals(PortPresence.State.Gone, presence.state(4000, listed))
        clock = 20_000; presence.ask(listOf(3000, 4000), listed); scope.runCurrent()
        assertEquals(listOf(3000, 4000), asked) // both still fresh
        clock = 40_000; presence.ask(listOf(3000, 4000), listed); scope.runCurrent()
        assertEquals(listOf(3000, 4000, 4000), asked) // a gone one is asked again sooner
    }

    @Test fun noAnswerLeavesItUnknownAndAFailedOpenMarksItGone() {
        val scope = TestScope()
        val presence = PortPresence(scope, { error("old hub") })
        presence.ask(listOf(8000), emptyMap()); scope.runCurrent()
        assertNull(presence.state(8000, emptyMap()))
        presence.noteGone(8000)
        assertEquals(PortPresence.State.Gone, presence.state(8000, emptyMap()))
    }

    @Test fun aBadgeSaysPortPathAndState() {
        assertEquals(":5173 落地页", portBadgeLabel(PortLink(5173, "/", false), PortPresence.State.Live("落地页"), "已停止"))
        assertEquals(":8000/docs 已停止", portBadgeLabel(PortLink(8000, "/docs", false), PortPresence.State.Gone, "已停止"))
        assertEquals(":3000/a/very/long/path/to/so…", portBadgeLabel(PortLink(3000, "/a/very/long/path/to/somewhere", false), null, "已停止"))
    }
}
