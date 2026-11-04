package persona.core.contracts

import org.junit.Assert.*
import org.junit.Test

// docs/hub-update.md「App 里」: the hub broadcasts {"type":"hub_update","update":<GET /api/v1/hub/update>} on every change.
class HubUpdateContractTest {
    @Test fun anUpdateEventDecodes() {
        val event = ServerEvent.decode("""{"type":"hub_update","update":{"version":"1.42.0","build":420,"channel":"release",
            "can_update":true,"state":"downloading","progress":"12/40 MB","busy":[],"auto":false,"auto_notice":true,"confirmed":true}}""")
        val update = (event as HubUpdateEvent).update
        assertEquals("downloading", update.state)
        assertEquals("12/40 MB", update.progress)
        assertTrue(update.working && update.autoNotice)
    }

    @Test fun withoutTheStatusItIsNotOne() {
        assertNull(ServerEvent.decode("""{"type":"hub_update"}"""))
    }

    @Test fun itRoundTrips() {
        val event = HubUpdateEvent(persona.core.contracts.api.HubUpdateStatus(version = "1.0.0", state = "idle"))
        assertEquals(event, ServerEvent.decode(JsonCodec.encodeToString(ServerEvent.serializer(), event)))
    }
}
