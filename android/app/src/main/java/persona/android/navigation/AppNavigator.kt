package persona.android.navigation

import androidx.navigation3.runtime.NavKey
import persona.core.navigation.*
import persona.core.contracts.SessionKey

internal class AppNavigator(
    private val stacks: Map<AppTab, MutableList<NavKey>>,
    private val selectedTab: () -> AppTab,
    private val onSelectTab: (AppTab) -> Unit,
    private val chatTab: (ChatRoute) -> AppTab? = { null },
    /** Returns true when the Jarvis home shows this conversation itself; the flag says whether to switch to it. */
    private val openAssistant: (ChatRoute, Boolean) -> Boolean = { _, _ -> false },
) : Navigator {
    override fun selectTab(tab: AppTab) = onSelectTab(tab)

    override fun navigate(key: NavKey) {
        val chat = when (key) { is ChatRoute -> key; is InteractionRoute -> key.chat; else -> null }
        if (chat != null && openAssistant(chat, true)) return
        if (chat != null && chat.kind != null) {
            Router().parse(RouteInput.Internal(key))?.let { selectTab(it.restore(stacks)); return }
        }
        if (key is SettingsPageRoute || key == PairingRoute) {
            val route = Router().parse(RouteInput.Internal(key))!!
            selectTab(route.restore(stacks)); return
        }
        val rootTab = when (key) {
            AssistantRoute -> AppTab.Jarvis
            AgentsRoute -> AppTab.Agents
            InboxRoute -> AppTab.Inbox
            SettingsRoute -> AppTab.Settings
            else -> null
        }
        if (rootTab != null) {
            selectTab(rootTab)
            return
        }
        if (key == ConnectionSettingsRoute) {
            selectTab(AppTab.Settings)
            return
        }
        val target = if (key is ChatRoute) chatTab(key)
            ?: stacks.entries.firstOrNull { key in it.value }?.key
            ?: selectedTab().takeIf { it == AppTab.Jarvis }
            ?: AppTab.Agents else selectedTab()
        selectTab(target)
        if (key is ChatRoute) stacks.filterKeys { it != target }.values.forEach { stack -> stack.removeAll { it == key } }
        val stack = stacks.getValue(target)
        if (stack.last() != key) stack.add(key)
    }

    /** Repair restored entries once session metadata becomes available. */
    fun restoreChatTabs() {
        val visible = stacks.getValue(selectedTab()).lastOrNull()
        stacks.forEach { (tab, stack) ->
            stack.filterIsInstance<ChatRoute>().forEach chatEntry@ { chat ->
                val owner = chatTab(chat) ?: return@chatEntry
                if (owner == AppTab.Jarvis && openAssistant(chat, visible == chat)) {
                    stack.removeAll { it == chat }
                    return@chatEntry
                }
                if (owner != tab) {
                    stack.removeAll { it == chat }
                    val destination = stacks.getValue(owner)
                    if (chat !in destination) {
                        if (owner == selectedTab() && visible != chat) destination.add(1, chat)
                        else destination.add(chat)
                    }
                    if (visible == chat) selectTab(owner)
                }
            }
        }
    }

    override fun back() {
        val stack = stacks.getValue(selectedTab())
        if (stack.size > 1) stack.removeAt(stack.lastIndex)
    }

    /** Remove every destination owned by the confirmed closed session, including hidden tabs. */
    fun sessionClosed(session: SessionKey) {
        stacks.values.forEach { stack ->
            stack.removeAll { key ->
                when (key) {
                    is ChatRoute -> key.machineId == session.machineId && key.sessionId == session.sessionId
                    is InteractionRoute -> key.chat.machineId == session.machineId && key.chat.sessionId == session.sessionId
                    is TerminalRoute -> key.machineId == session.machineId && key.sessionId == session.sessionId
                    else -> false
                }
            }
        }
    }
}
