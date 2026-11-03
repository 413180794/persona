package persona.android.navigation

import androidx.navigation3.runtime.NavKey
import persona.core.navigation.*
import org.junit.Assert.assertEquals
import org.junit.Test

class AppNavigatorTest {
    @Test fun confirmedCloseRemovesAllMatchingDestinationsAcrossTabs() {
        val target = persona.core.contracts.SessionKey("machine", "closed")
        val chat = ChatRoute(target.machineId, target.sessionId)
        val other = ChatRoute("other-machine", target.sessionId)
        stacks.getValue(AppTab.Agents).addAll(listOf(chat, TerminalRoute(target.machineId, target.sessionId)))
        stacks.getValue(AppTab.Jarvis).add(chat.copy(kind = ConversationKind.Assistant))
        stacks.getValue(AppTab.Inbox).addAll(listOf(InteractionRoute(chat, "request"), other))
        navigator.sessionClosed(target)
        assertEquals(listOf(AgentsRoute), stacks.getValue(AppTab.Agents))
        assertEquals(listOf(AssistantRoute), stacks.getValue(AppTab.Jarvis))
        assertEquals(listOf(InboxRoute, other), stacks.getValue(AppTab.Inbox))
        navigator.sessionClosed(target)
        navigator.back()
        assertEquals(listOf(AgentsRoute), stacks.getValue(AppTab.Agents))
    }

    private val stacks = mapOf<AppTab, MutableList<NavKey>>(
        AppTab.Jarvis to mutableListOf(AssistantRoute),
        AppTab.Agents to mutableListOf(AgentsRoute),
        AppTab.Inbox to mutableListOf(InboxRoute),
        AppTab.Settings to mutableListOf(SettingsRoute),
    )
    private var selected = AppTab.Agents
    private val navigator = AppNavigator(stacks, { selected }, { selected = it })

    @Test fun switchingTabsKeepsEachStackAndBackNeverRemovesTheRoot() {
        val chat = ChatRoute("machine", "session")
        navigator.navigate(chat)
        navigator.selectTab(AppTab.Inbox)
        navigator.back()
        assertEquals(listOf(InboxRoute), stacks.getValue(AppTab.Inbox))
        navigator.selectTab(AppTab.Agents)
        assertEquals(listOf(AgentsRoute, chat), stacks.getValue(AppTab.Agents))
        navigator.back()
        navigator.back()
        assertEquals(listOf(AgentsRoute), stacks.getValue(AppTab.Agents))
    }

    @Test fun notificationOpensAgentsFromAnotherTabAndDoesNotDuplicateTheChat() {
        navigator.selectTab(AppTab.Settings)
        val chat = ChatRoute("machine", "session")
        navigator.navigate(chat)
        navigator.navigate(chat)
        assertEquals(AppTab.Agents, selected)
        assertEquals(listOf(AgentsRoute, chat), stacks.getValue(AppTab.Agents))
        navigator.navigate(InboxRoute)
        assertEquals(AppTab.Inbox, selected)
        assertEquals(listOf(SettingsRoute), stacks.getValue(AppTab.Settings))
    }

    @Test fun assistantChatsStayInPersonaAndLateMetadataRepairsRestoredChats() {
        var assistantKnown = false
        val assistantChat = ChatRoute("machine", "assistant")
        val router = AppNavigator(stacks, { selected }, { selected = it }, chatTab = {
            if (assistantKnown && it == assistantChat) AppTab.Jarvis else null
        })
        router.selectTab(AppTab.Jarvis)
        router.navigate(assistantChat)
        assertEquals(AppTab.Jarvis, selected)
        assertEquals(listOf(AgentsRoute), stacks.getValue(AppTab.Agents))
        router.back()

        // Simulate an old restored stack before repositories have loaded metadata.
        stacks.getValue(AppTab.Agents).add(assistantChat)
        router.selectTab(AppTab.Agents)
        assistantKnown = true
        router.restoreChatTabs()
        assertEquals(AppTab.Jarvis, selected)
        assertEquals(listOf(AssistantRoute, assistantChat), stacks.getValue(AppTab.Jarvis))
        assertEquals(listOf(AgentsRoute), stacks.getValue(AppTab.Agents))
        router.selectTab(AppTab.Settings)
        router.navigate(assistantChat)
        assertEquals(AppTab.Jarvis, selected)
        assertEquals(listOf(AssistantRoute, assistantChat), stacks.getValue(AppTab.Jarvis))
    }

    @Test fun restoredStackKeepsOwnershipAndSettingsGearOpensTheSettingsTab() {
        val chat = ChatRoute("machine", "assistant")
        stacks.getValue(AppTab.Jarvis).add(chat)
        navigator.navigate(chat)
        assertEquals(AppTab.Jarvis, selected)
        assertEquals(listOf(AssistantRoute, chat), stacks.getValue(AppTab.Jarvis))
        navigator.selectTab(AppTab.Agents)
        navigator.navigate(ConnectionSettingsRoute)
        assertEquals(AppTab.Settings, selected)
        assertEquals(listOf(SettingsRoute), stacks.getValue(AppTab.Settings))
        assertEquals(listOf(AgentsRoute), stacks.getValue(AppTab.Agents))
    }

    @Test fun embeddedAssistantConversationsStayInThePersonaHome() {
        val assistantChat = ChatRoute("machine", "assistant")
        val opened = mutableListOf<Pair<ChatRoute, Boolean>>()
        val router = AppNavigator(stacks, { selected }, { selected = it }, chatTab = {
            if (it.sessionId == "assistant") AppTab.Jarvis else AppTab.Agents
        }, openAssistant = { chat, select ->
            (chat.sessionId == "assistant").also { if (it) { opened += chat to select; if (select) selected = AppTab.Jarvis } }
        })
        router.navigate(assistantChat)
        assertEquals(listOf(assistantChat to true), opened)
        assertEquals(AppTab.Jarvis, selected)
        assertEquals(listOf(AssistantRoute), stacks.getValue(AppTab.Jarvis))

        // A page restored before metadata arrived becomes a home request, switching only if it was on screen.
        stacks.getValue(AppTab.Agents).add(assistantChat)
        router.selectTab(AppTab.Inbox)
        router.restoreChatTabs()
        assertEquals(assistantChat to false, opened.last())
        assertEquals(AppTab.Inbox, selected)
        assertEquals(listOf(AgentsRoute), stacks.getValue(AppTab.Agents))

        val agentChat = ChatRoute("machine", "agent")
        router.navigate(agentChat)
        assertEquals(listOf(AgentsRoute, agentChat), stacks.getValue(AppTab.Agents))
    }
}
