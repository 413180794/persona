package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.json.Json
import org.junit.Assert.*
import org.junit.Test

class RouterTest {
    private val router = Router { _, session ->
        when (session) {
            "assistant" -> ConversationKind.Assistant
            "agent" -> ConversationKind.Agent
            else -> null
        }
    }
    private val assistant = ChatRoute("machine", "assistant", ConversationKind.Assistant)
    private val agent = ChatRoute("machine", "agent", ConversationKind.Agent)

    @Test fun allEntrancesResolveToTheSameTabAndPage() {
        val cases = listOf(
            Triple(RouteInput.DeepLink("jarvis://open?machine=machine&session=assistant"), AppTab.Jarvis, assistant),
            Triple(RouteInput.DeepLink("jarvis://open?machine=machine&session=agent"), AppTab.Agents, agent),
            Triple(RouteInput.Notification("machine/assistant"), AppTab.Jarvis, assistant),
            Triple(RouteInput.Notification("machine/agent"), AppTab.Agents, agent),
            Triple(RouteInput.Share(assistant.copy(kind = null)), AppTab.Jarvis, assistant),
            Triple(RouteInput.Share(agent), AppTab.Agents, agent),
            Triple(RouteInput.Internal(assistant), AppTab.Jarvis, assistant),
            Triple(RouteInput.Internal(agent), AppTab.Agents, agent),
            Triple(RouteInput.Internal(InboxItemRoute("item")), AppTab.Inbox, InboxItemRoute("item")),
            Triple(RouteInput.DeepLink("jarvis://open?target=inbox&item=item"), AppTab.Inbox, InboxItemRoute("item")),
            Triple(RouteInput.DeepLink("jarvis://open?target=lark&chat=chat"), AppTab.Inbox, LarkChatRoute("chat")),
            Triple(RouteInput.Internal(LarkChatRoute("chat")), AppTab.Inbox, LarkChatRoute("chat")),
            Triple(RouteInput.Notification("machine/assistant", requestId = "request"), AppTab.Jarvis, InteractionRoute(assistant, "request")),
            Triple(RouteInput.DeepLink("jarvis://open?target=request&machine=machine&session=agent&request=request"), AppTab.Agents, InteractionRoute(agent, "request")),
            Triple(RouteInput.Internal(ConnectionSettingsRoute), AppTab.Settings, SettingsPageRoute(SettingsPage.Diagnostics)),
            Triple(RouteInput.DeepLink("jarvis://open?target=settings&page=notifications"), AppTab.Settings, SettingsPageRoute(SettingsPage.Notifications)),
            Triple(RouteInput.DeepLink("jarvis://open?machine=machine&session=a%2Fb%2Bc&kind=assistant"), AppTab.Jarvis,
                ChatRoute("machine", "a/b+c", ConversationKind.Assistant)),
        )
        cases.forEach { (input, tab, key) -> assertRoute(router.parse(input), tab, key) }
        AppTab.entries.forEach { tab ->
            assertRoute(router.parse(RouteInput.Internal(tabRoot(tab))), tab, tabRoot(tab))
            assertRoute(router.parseDebug(mapOf("debug.openTab" to tab.name)), tab, tabRoot(tab))
        }
        SettingsPage.entries.forEach { page ->
            assertRoute(router.parse(RouteInput.Internal(SettingsPageRoute(page))), AppTab.Settings, SettingsPageRoute(page))
        }
        val pairLink = "jarvis://pair?url=https%3A%2F%2Fexample.com&token=example"
        val pairing = router.parse(RouteInput.DeepLink(pairLink))
        assertRoute(pairing, AppTab.Settings, PairingRoute)
        assertEquals(pairLink, pairing?.pairingLink)
        assertRoute(router.parseDebug(mapOf("debug.openPairing" to pairLink)), AppTab.Settings, PairingRoute)
        assertRoute(router.parseDebug(mapOf("debug.openPairing" to "true")), AppTab.Settings, PairingRoute)
        assertRoute(router.parseDebug(mapOf("debug.openDiagnostics" to "true")), AppTab.Settings, SettingsPageRoute(SettingsPage.Diagnostics))
        assertRoute(router.parseDebug(mapOf("debug.openSession" to "machine/assistant")), AppTab.Jarvis, assistant)
        assertRoute(router.parseDebug(mapOf("debug.openSession" to "machine/agent")), AppTab.Agents, agent)
        assertRoute(router.parseDebug(mapOf("debug.openRequest" to "request", "debug.session" to "machine/agent")),
            AppTab.Agents, InteractionRoute(agent, "request"))
        assertRoute(router.parseDebug(mapOf("debug.openSession" to "machine/unknown", "debug.conversationKind" to "assistant")),
            AppTab.Jarvis, ChatRoute("machine", "unknown", ConversationKind.Assistant))
        listOf(
            "https://open?machine=machine&session=agent", "jarvis://open/path?machine=machine&session=agent",
            "jarvis://open?machine=machine&session=", "jarvis://open?machine=machine&session=agent&session=assistant",
            "jarvis://open?machine=machine&session=%ZZ", "jarvis://open?machine=machine&session=agent&kind=unknown",
            "jarvis://open?target=settings&page=unknown", "jarvis://pair.example.com", "jarvis://open#fragment",
        ).forEach { assertNull(it, router.parse(RouteInput.DeepLink(it))) }
        assertNull(router.parse(RouteInput.Notification("machine/")))
        assertNull(router.parse(RouteInput.Notification("machine/unknown")))
        assertNull(router.parseDebug(mapOf("debug.openTab" to "inbox", "debug.openDiagnostics" to "true")))
        assertNull(router.parseDebug(mapOf("debug.openRequest" to "request")))
    }

    @Test fun restoredIdentityReusesExistingEntriesAndKeepsSourcesSerializable() {
        val stacks = AppTab.entries.associateWith { mutableListOf(tabRoot(it)) }
        val sources = listOf(RouteSource.InboxItem("item"), RouteSource.LarkConversation("chat"),
            RouteSource.Interaction("machine", "request"))
        val fromInbox = assistant.copy(source = sources.first())
        assertEquals(fromInbox, Json.decodeFromString<ChatRoute>(Json.encodeToString(fromInbox)))
        val old = Json.decodeFromString<ChatRoute>("""{"machineId":"machine","sessionId":"assistant"}""")
        assertNull(old.kind)
        stacks.getValue(AppTab.Agents).add(old)
        router.parse(RouteInput.Internal(fromInbox))!!.restore(stacks)
        assertEquals(listOf(AgentsRoute), stacks.getValue(AppTab.Agents))
        stacks.getValue(AppTab.Jarvis).add(ChatRoute("machine", "other", ConversationKind.Assistant))
        sources.forEach { source ->
            val route = router.parse(RouteInput.Internal(assistant.copy(source = source)))!!
            assertEquals(AppTab.Jarvis, route.restore(stacks))
            route.restore(stacks)
            assertEquals(listOf(AssistantRoute, fromInbox), stacks.getValue(AppTab.Jarvis))
        }
        listOf<NavKey>(InboxItemRoute("item"), LarkChatRoute("chat"), InteractionRoute(agent, "request"),
            SettingsPageRoute(SettingsPage.Diagnostics), PairingRoute).forEach { key ->
            val route = router.parse(RouteInput.Internal(key))!!
            route.restore(stacks)
            route.restore(stacks)
            assertEquals(1, stacks.getValue(route.tab).count { it == key })
            assertEquals(key, stacks.getValue(route.tab).last())
        }
        val interaction = InteractionRoute(fromInbox, "request")
        assertEquals(interaction, Json.decodeFromString<InteractionRoute>(Json.encodeToString(interaction)))
    }

    private fun assertRoute(actual: ResolvedRoute?, tab: AppTab, key: NavKey) {
        assertNotNull(actual)
        assertEquals(tab, actual!!.tab)
        assertEquals(key, actual.key)
    }
}
