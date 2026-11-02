package jarvis.android.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.NotificationsOff
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Snackbar
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.JarvisApp
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.CountBadge
import jarvis.android.ui.design.EmptyState
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.StatusDot
import jarvis.android.ui.design.surface
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.layout.size
import jarvis.android.data.Grouper
import jarvis.android.data.Hub
import jarvis.android.data.SessionKey
import kotlinx.coroutines.launch

/**
 * One herdr workspace (iOS SpaceView): its running agents as cards in tab order, a tile to open a new tab there,
 * and the space's menu. `spaceId` is "machine/space id", the list's SpaceGroup.id.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SpaceScreen(hub: Hub, spaceId: String, onOpen: (SessionKey) -> Unit, onBack: () -> Unit) {
    val byMachine by hub.sessions.collectAsState()
    val machines by hub.machines.collectAsState()
    val pins by hub.pins.collectAsState()
    val mutes by hub.mutes.collectAsState()
    val pending by hub.interactions.collectAsState()
    val scope = rememberCoroutineScope()
    var menu by remember { mutableStateOf(false) }
    var renaming by remember { mutableStateOf(false) }
    var newTab by remember { mutableStateOf(false) }

    val g = Grouper(pins, mutes)
    val space = g.spaces(byMachine.values.flatten().filter { !it.assistant }).firstOrNull { it.id == spaceId }
    val machine = machines.firstOrNull { it.id == space?.machineId }

    Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
        PageHeader(
            space?.name ?: "空间", onBack,
            subtitle = listOfNotNull(machine?.displayName, space?.sessions?.firstOrNull()?.cwd?.let { tailOf(it) }).joinToString(" · "),
        ) {
            if (space != null) Box {
                CircleButton(if (g.muted(space)) JIcons.muted else JIcons.more, "空间操作", { menu = true })
                DropdownMenu(menu, onDismissRequest = { menu = false }) {
                    val unread = space.sessions.filter { it.agentState == jarvis.android.data.AgentState.Unread }
                    if (unread.isNotEmpty()) DropdownMenuItem({ Text("全部标为已读（${unread.size}）", maxLines = 1) }, {
                        menu = false; JarvisApp.instance.scope.launch { hub.markSeen(unread) }
                    }, leadingIcon = { Icon(JIcons.done, null) })
                    DropdownMenuItem({ Text(if (g.pinned(space)) "取消置顶空间" else "置顶空间", maxLines = 1) }, {
                        menu = false
                        scope.launch { runCatching { hub.toggleSpacePin(space) }.onFailure { Toasts.show("置顶没成功：${it.message}", ToastStyle.ERROR) } }
                    }, leadingIcon = { Icon(JIcons.pin, null) })
                    DropdownMenuItem({ Text(if (g.muted(space)) "取消屏蔽空间" else "屏蔽这个空间", maxLines = 1) }, {
                        menu = false
                        scope.launch { runCatching { hub.toggleSpaceMute(space) }.onFailure { Toasts.show("屏蔽没成功：${it.message}", ToastStyle.ERROR) } }
                    }, leadingIcon = { Icon(if (g.muted(space)) JIcons.unmute else JIcons.mute, null) })
                    if (space.workspaceId != null) DropdownMenuItem({ Text("重命名空间…", maxLines = 1) }, { menu = false; renaming = true }, leadingIcon = { Icon(JIcons.edit, null) })
                }
            }
        }
        if (space == null) {
            // The space went away (its last agent closed, or herdr restarted with new ids).
            EmptyState(JIcons.space, "这个空间空了", "这个空间里已经没有在跑的 Agent 了。")
            return@Column
        }
        LazyVerticalGrid(
            GridCells.Fixed(2), Modifier.fillMaxSize(),
            contentPadding = androidx.compose.foundation.layout.PaddingValues(start = JSpace.page, end = JSpace.page, top = JSpace.s, bottom = JSpace.xxl),
            horizontalArrangement = Arrangement.spacedBy(JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.m),
        ) {
            items(space.sessions, key = { it.key.path }) { s ->
                val asks = pending.count { it.machineId == s.machineId && it.sessionId == s.sessionId }
                val tone = JTheme.tone(if (g.muted(s)) jarvis.android.data.AgentState.Idle else s.agentState)
                Column(
                    Modifier.heightIn(min = JSize.box140 + JSpace.s).surface(RoundedCornerShape(JTheme.style.corner)).clickable { onOpen(s.key) }.padding(JSpace.m),
                    verticalArrangement = Arrangement.spacedBy(JSpace.xs),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                        StatusDot(s.agentState, muted = g.muted(s))
                        Text(
                            // The tab's name, unless it just repeats the title below (renamed together) or is herdr's number.
                            s.slotName?.takeIf { it.isNotBlank() && it != s.slotIndex?.toString() && it != s.displayTitle } ?: "#${s.slotIndex ?: 0}",
                            style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1,
                            overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
                        )
                        if (asks > 0) CountBadge(asks, color = JTheme.needsYou.dot)
                    }
                    Text(s.displayTitle, style = JTheme.type.heading, color = JTheme.colors.label, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    Text(
                        listOf(s.agentState.label(), listTime(s.updated)).filter { it.isNotEmpty() }.joinToString(" · "),
                        style = JTheme.type.labelMedium, color = tone.text, maxLines = 1,
                    )
                    s.lastText?.takeIf { it.isNotBlank() }?.let {
                        Text(it.replace('\n', ' '), style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 4, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
            if (space.workspaceId != null && machine?.online == true) item(key = "new", span = { GridItemSpan(1) }) {
                Column(
                    Modifier.fillMaxWidth().heightIn(min = JSize.box140 + JSpace.s).clip(RoundedCornerShape(JTheme.style.corner))
                        .background(JTheme.colors.accent.copy(alpha = 0.06f))
                        .border(JSize.hairline * 2, JTheme.colors.accent.copy(alpha = 0.35f), RoundedCornerShape(JTheme.style.corner))
                        .clickable { newTab = true },
                    verticalArrangement = Arrangement.spacedBy(JSpace.s, Alignment.CenterVertically), horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Icon(JIcons.add, null, Modifier.size(JSize.icon), tint = JTheme.colors.accent)
                    Text("在这个空间开新标签", style = JTheme.type.secondaryStrong, color = JTheme.colors.accent, maxLines = 1)
                }
            }
        }
    }

    if (renaming && space != null) {
        RenameDialog(
            title = "重命名空间", current = space.name,
            note = "改的是 ${machine?.displayName ?: "电脑"} 上 herdr 的 workspace 名，置顶和屏蔽会跟着新名字走",
            onDismiss = { renaming = false },
            onSave = { name ->
                renaming = false
                JarvisApp.instance.scope.launch { runCatching { hub.renameSpace(space, name) }.onFailure { Toasts.show("重命名没成功：${it.message}", ToastStyle.ERROR) } }
            },
        )
    }
    if (newTab && space != null && machine != null) {
        val first = space.sessions.first()
        NewSessionSheet(
            hub, listOf(machine), onDismiss = { newTab = false }, onStarted = { newTab = false; onOpen(it) },
            preset = SpacePreset(machine.id, first.cwd, space.workspaceId!!, space.name),
        )
    }
}
