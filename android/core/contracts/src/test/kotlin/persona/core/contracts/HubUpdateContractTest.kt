package persona.core.contracts

import org.junit.Assert.*
import org.junit.Test

// docs/operations/hub-update.md "In the app": the hub broadcasts {"type":"hub_update","update":<GET /api/v1/hub/update>} on every change.
class HubUpdateContractTest {
    @Test fun anUpdateEventDecodes() {
        val event = ServerEvent.decode("""{"type":"hub_update","update":{"version":"1.42.0","build":420,"channel":"release",
            "can_update":true,"state":"downloading","progress":"12/40 MB","busy":[],"auto":false,"auto_notice":true,"confirmed":true}}""")
        val update = (event as HubUpdateEvent).update
        assertEquals("downloading", (update.state).zhAny())
        assertEquals("12/40 MB", (update.progress).zhAny())
        assertTrue(update.working && update.autoNotice)
    }

    @Test fun withoutTheStatusItIsNotOne() {
        assertNull(ServerEvent.decode("""{"type":"hub_update"}"""))
    }

    @Test fun itRoundTrips() {
        val event = HubUpdateEvent(persona.core.contracts.api.HubUpdateStatus(version = "1.0.0", state = "idle"))
        assertEquals(event, ServerEvent.decode(JsonCodec.encodeToString(ServerEvent.serializer(), event)))
    }

    @Test fun helloDecodesUpdateCompatFields() {
        val event = ServerEvent.decode("""{"type":"hello","features":["x"],"channel":"release","build":202611021500,"protocol":1,"min_app_build":95}""")
        val hello = event as Hello
        assertEquals("release", (hello.channel).zhAny())
        assertEquals(202611021500L, hello.build)
        assertEquals(1, hello.protocol)
        assertEquals(95, hello.minAppBuild)
    }
}
