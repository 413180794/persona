package persona.android.navigation

import persona.core.navigation.AppTab
import persona.core.navigation.ChatRoute
import persona.core.navigation.ConversationKind
import persona.core.navigation.InboxItemRoute
import persona.core.navigation.ResolvedRoute
import persona.feature.assistant.NeedsYouLine

/**
 * Where a line of the Persona home's 现在要你处理的事 opens, as a notification would open it: an agent's request in its
 * conversation on the Agents tab, an inbox item on the inbox tab (the page the inbox list would open for it).
 */
internal fun needsYouRoute(line: NeedsYouLine): ResolvedRoute = when (line) {
    is NeedsYouLine.Agent -> ResolvedRoute(AppTab.Agents, ChatRoute(line.session.machineId, line.session.sessionId, ConversationKind.Agent))
    is NeedsYouLine.Inbox -> ResolvedRoute(AppTab.Inbox, InboxItemRoute(line.itemId))
}
