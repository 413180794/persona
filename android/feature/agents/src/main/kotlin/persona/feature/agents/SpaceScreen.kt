package persona.feature.agents

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import persona.core.contracts.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.sort.*
import java.time.Instant

/**
 * One herdr space in tab order, with the same actions as the home list. A page of its own ([spaceEntries]): beside
 * the list in a wide window, pushed on a phone; [onDismiss] goes back.
 */
@Composable
internal fun SpaceScreen(
    space: AgentSpace?, machines: List<Machine>, pending: List<Interaction>, preferences: AgentsPreferencesSnapshot,
    busy: Boolean, canStart: Boolean, snackbar: SnackbarHostState, now: Instant,
    onDismiss: () -> Unit, onOpen: (AgentSession) -> Unit, onNewTab: (AgentSpace) -> Unit,
    spaceMenu: @Composable (AgentSpace) -> Unit, sessionMenu: @Composable (AgentSession) -> List<JAction>,
    /** The swipe that closes a row's agent, when it can be closed (as in the list). */
    onClose: (AgentSession) -> (() -> Unit)? = { null },
    /** The name to show before the sessions are read; until then ([loaded] false) the page shows no empty state. */
    title: String = "",
    loaded: Boolean = true,
) {
    Box(Modifier.fillMaxSize()) {
        JListPage(title = space?.name ?: title.ifBlank { "空间" }, onBack = onDismiss,
            subtitle = machines.firstOrNull { it.id == space?.machineId }?.displayName,
            actions = { space?.let { spaceMenu(it) } }) {
            if (space == null) {
                if (loaded) item("empty") {
                    JEmptyState("空间里没有运行中的 Agent", "可以返回列表查看其他会话。", icon = PhIcons.Robot)
                }
            } else {
                item("location") {
                    JRow("目录", subtitle = space.cwd, leadingIcon = PhIcons.FolderSimple)
                    if (busy) JLinearProgress(null)
                }
                itemsIndexed(space.members, key = { _, row -> row.id }) { index, row ->
                    val requests = pending.count { it.sessionKey == row.session.key }
                    SlotRow(row, machines.firstOrNull { it.id == row.session.machineId }?.displayName,
                        AgentRowMetadata(), now, false, row.id in preferences.pins.sessions, preferences.mutes.mutes(row),
                        depth = 0, position = GroupPosition.of(index, space.members.size), located = false,
                        onOpen = { onOpen(row) }, menuItems = { sessionMenu(row) },
                        slotLabel = row.session.slotName?.takeIf { it.isNotBlank() && it != row.session.displayTitle }
                            // herdr's default tab names are numbers; a bare 「1」 reads as a count, so say #1.
                            ?.let { if (it.all(Char::isDigit)) "#$it" else it }
                            ?: "标签 ${row.session.slotIndex ?: 0}", requestCount = requests, onClose = onClose(row))
                }
                if (canStart) item("new-tab") {
                    JButton("在此开新标签", { onNewTab(space) }, Modifier.fillMaxWidth().padding(top = 16.dp), enabled = !busy, icon = PhIcons.Plus)
                }
            }
        }
        SnackbarHost(snackbar, Modifier.align(Alignment.BottomCenter).navigationBarsPadding())
    }
}
