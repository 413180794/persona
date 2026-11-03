package persona.android.navigation

import androidx.navigation3.runtime.NavKey
import persona.core.contracts.SessionKey
import persona.core.navigation.*
import persona.feature.assistant.NeedsYouLine
import org.junit.Assert.assertEquals
import org.junit.Test

/** The Persona home's 现在要你处理的事 opens a request in its conversation and an inbox item in the inbox. */
class NeedsYouLinksTest {
    private val stacks = mapOf<AppTab, MutableList<NavKey>>(
        AppTab.Jarvis to mutableListOf(AssistantRoute), AppTab.Agents to mutableListOf(AgentsRoute),
        AppTab.Inbox to mutableListOf(InboxRoute), AppTab.Settings to mutableListOf(SettingsRoute),
    )

    @Test fun aRequestOpensItsConversationOnAgents() {
        val line = NeedsYouLine.Agent(SessionKey("m", "s"), "r1", "plan", null, "迁移", "计划等你批准")
        assertEquals(AppTab.Agents, needsYouRoute(line).open(stacks))
        assertEquals(ChatRoute("m", "s", ConversationKind.Agent), stacks.getValue(AppTab.Agents).last())
        assertEquals(listOf<NavKey>(AssistantRoute), stacks.getValue(AppTab.Jarvis))
    }

    @Test fun anInboxItemOpensOnTheInbox() {
        val line = NeedsYouLine.Inbox("item-1", lark = true, itemKind = "dm", who = "发布群", what = "要你确认")
        assertEquals(AppTab.Inbox, needsYouRoute(line).open(stacks))
        assertEquals(InboxItemRoute("item-1"), stacks.getValue(AppTab.Inbox).last())
    }
}
