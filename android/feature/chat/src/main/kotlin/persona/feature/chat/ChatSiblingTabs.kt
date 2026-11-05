package persona.feature.chat

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import persona.core.contracts.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import java.time.Instant

/**
 * The tabs of this chat's space, itself included (iOS `AppModel.siblings`): the live agents in the same herdr
 * workspace on the same machine, one per pane (after /clear or a resume a pane is briefly listed under two session
 * ids: the open one wins, then the latest), in tab order. Empty when the session is not in a space.
 */
internal fun spaceTabs(session: Session?, sessions: List<Session>): List<Session> {
    if (session == null) return emptyList()
    val space = session.spaceId?.takeIf(String::isNotBlank) ?: session.workspaceId?.takeIf(String::isNotBlank) ?: return emptyList()
    val byPane = LinkedHashMap<String, Session>()
    val rest = mutableListOf<Session>()
    for (other in sessions + session) {
        if (other.machineId != session.machineId || other.assistant || other.agentState == AgentState.Offline) continue
        if ((other.spaceId?.takeIf(String::isNotBlank) ?: other.workspaceId) != space) continue
        val pane = other.paneId?.takeIf(String::isNotBlank)
        if (pane == null) { if (rest.none { it.key == other.key }) rest += other; continue }
        val had = byPane[pane]
        if (had != null && (had.key == session.key ||
                (other.key != session.key && (had.updated ?: Instant.EPOCH) >= (other.updated ?: Instant.EPOCH)))) continue
        byPane[pane] = other
    }
    return (byPane.values + rest).sortedWith(compareBy<Session> { it.slotIndex ?: Int.MAX_VALUE }.thenBy { it.sessionId })
}

/** A tab's name (iOS `Session.slotLabel`): its herdr tab name, "#n" for herdr's default number, else the title. */
internal fun tabLabel(session: Session): String {
    val name = session.slotName?.takeIf(String::isNotBlank)
    val index = session.slotIndex
    return when {
        name == null -> index?.let { "#$it" } ?: session.displayTitle
        index != null && name == index.toString() -> "#$index"
        else -> name
    }
}

internal fun Session.status(): Status = Status.entries.firstOrNull { it.name == agentState.name } ?: Status.Unknown

/**
 * Under the chat's title (iOS `SiblingTabs`): the space's tabs as chips with their state's dot, the current one
 * selected; it opens scrolled to the current one. ⋯ lists them all with their state. Nothing when the space has one
 * tab or there is no way to switch.
 */
@Composable
internal fun ChatSiblingTabs(session: Session?, sessions: List<Session>, onOpenSession: ((SessionKey) -> Unit)?) {
    val tabs = remember(session, sessions) { spaceTabs(session, sessions) }
    if (session == null || onOpenSession == null || tabs.size <= 1) return
    var all by remember { mutableStateOf(false) }
    val list = rememberLazyListState()
    val at = tabs.indexOfFirst { it.key == session.key }
    LaunchedEffect(session.key) { if (at > 0) list.animateScrollToItem(at - 1) }
    val waiting = tabs.any { it.key != session.key && it.agentState == AgentState.NeedsYou }
    Row(
        Modifier.fillMaxWidth().padding(horizontal = Tokens.Space.l, vertical = Tokens.Space.xs),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
    ) {
        LazyRow(Modifier.weight(1f), state = list, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            items(tabs, key = { it.key.path }) { tab ->
                val on = tab.key == session.key
                val description = stringResource(R.string.chat_tabs_tab_description, tabLabel(tab), tab.status().label)
                JChip(tabLabel(tab), Modifier.semantics { contentDescription = description },
                    selected = on, status = tab.status(), onClick = { if (!on) onOpenSession(tab.key) })
            }
        }
        JIconButton(PhIcons.DotsThree, pluralStringResource(if (waiting) R.plurals.chat_tabs_all_waiting else R.plurals.chat_tabs_all, tabs.size, tabs.size),
            { all = true }, badge = waiting)
    }
    if (all) JSheet(onDismiss = { all = false }, title = pluralStringResource(R.plurals.chat_tabs_sheet_title, tabs.size, tabs.size), scrollable = true) {
        tabs.forEach { tab ->
            val on = tab.key == session.key
            JRow(tabLabel(tab), subtitle = tab.status().label, leading = { JStatusDot(tab.status()) }, selected = on,
                onClick = { all = false; if (!on) onOpenSession(tab.key) })
        }
        Spacer(Modifier.height(Tokens.Space.l))
    }
}
