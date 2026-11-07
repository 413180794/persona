package persona.core.navigation

import org.junit.Assert.*
import org.junit.Test

class ProtocolCompatibilityTest {
    @Test fun baselineAndCurrentSchemesResolveToTheSameCanonicalTabAndChat() {
        val router = Router { _, _ -> ConversationKind.Assistant }
        val current = router.parse(RouteInput.DeepLink("persona://open?machine=test-machine&session=test-session"))
        val old = router.parse(RouteInput.DeepLink("jarvis://open?machine=test-machine&session=test-session"))
        assertEquals(current?.key, old?.key)
        assertEquals(current?.tab, old?.tab)
        assertEquals(AppTab.Persona, old?.tab)
        assertEquals(AssistantRoute, router.parse(RouteInput.DeepLink("jarvis://open?target=tab&tab=jarvis"))?.key)
        assertEquals(PairingRoute, router.parse(RouteInput.DeepLink("jarvis://pair?url=http%3A%2F%2F127.0.0.1&token=test-token"))?.key)
        assertNull(router.parse(RouteInput.DeepLink("other://open?target=tab&tab=persona")))
        assertNull(router.parse(RouteInput.DeepLink("jarvis://open?target=tab&tab=persona&tab=agents")))
    }

    @Test fun oldHubBookmarksStayInAppAndRetainMessageLocation() {
        val hubs = listOf("https://hub.example.com/prefix")
        assertEquals(HubLinks.page("https://hub.example.com/prefix/persona/test-chat?at=message", hubs),
            HubLinks.page("https://hub.example.com/prefix/j/test-chat?at=message", hubs))
        assertNull(HubLinks.page("https://other.example.com/prefix/j/test-chat", hubs))
    }
}
