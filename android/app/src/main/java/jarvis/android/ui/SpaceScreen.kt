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
import androidx.compose.material.icons.automirrored.filled.ArrowBack
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
    var notice by remember { mutableStateOf<String?>(null) }

    val g = Grouper(pins, mutes)
    val space = g.spaces(byMachine.values.flatten().filter { !it.assistant }).firstOrNull { it.id == spaceId }
    val machine = machines.firstOrNull { it.id == space?.machineId }

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
                title = {
                    Column {
                        Text(space?.name ?: "空间", maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(
                            listOfNotNull(machine?.displayName, space?.sessions?.firstOrNull()?.cwd?.let { tailOf(it) }).joinToString(" · "),
                            style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1,
                        )
                    }
                },
                actions = {
                    if (space != null) Box {
                        IconButton(onClick = { menu = true }) {
                            Icon(if (g.muted(space)) Icons.Default.NotificationsOff else Icons.Default.MoreVert, "空间操作")
                        }
                        DropdownMenu(menu, onDismissRequest = { menu = false }) {
                            val unread = space.sessions.filter { it.agentState == jarvis.android.data.AgentState.Unread }
                            if (unread.isNotEmpty()) DropdownMenuItem({ Text("全部标为已读（${unread.size}）") }, {
                                menu = false; JarvisApp.instance.scope.launch { hub.markSeen(unread) }
                            })
                            DropdownMenuItem({ Text(if (g.pinned(space)) "取消置顶空间" else "置顶空间") }, {
                                menu = false
                                scope.launch { runCatching { hub.toggleSpacePin(space) }.onFailure { notice = "置顶没成功：${it.message}" } }
                            })
                            DropdownMenuItem({ Text(if (g.muted(space)) "取消屏蔽空间" else "屏蔽这个空间") }, {
                                menu = false
                                scope.launch { runCatching { hub.toggleSpaceMute(space) }.onFailure { notice = "屏蔽没成功：${it.message}" } }
                            })
                            if (space.workspaceId != null) DropdownMenuItem({ Text("重命名空间…") }, { menu = false; renaming = true })
                        }
                    }
                },
            )
        },
        snackbarHost = { notice?.let { Snackbar(Modifier.padding(12.dp), action = { TextButton({ notice = null }) { Text("好", maxLines = 1) } }) { Text(it) } } },
    ) { pad ->
        if (space == null) {
            // The space went away (its last agent closed, or herdr restarted with new ids).
            Box(Modifier.padding(pad).fillMaxSize(), contentAlignment = Alignment.Center) {
                Text("这个空间里已经没有在跑的 Agent 了", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            return@Scaffold
        }
        LazyVerticalGrid(
            GridCells.Fixed(2), Modifier.padding(pad).fillMaxSize(),
            contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            items(space.sessions, key = { it.key.path }) { s ->
                val asks = pending.count { it.machineId == s.machineId && it.sessionId == s.sessionId }
                Card(onClick = { onOpen(s.key) }, modifier = Modifier.heightIn(min = 150.dp)) {
                    Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            if (g.muted(s)) StateDot(jarvis.android.data.AgentState.Idle) else StateDot(s.agentState)
                            Spacer(Modifier.width(6.dp))
                            Text(
                                // The tab's name, unless it just repeats the title below (renamed together) or is herdr's number.
                                s.slotName?.takeIf { it.isNotBlank() && it != s.slotIndex?.toString() && it != s.displayTitle } ?: "#${s.slotIndex ?: 0}",
                                style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1,
                                overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
                            )
                            if (asks > 0) Text("$asks 件需要你", style = MaterialTheme.typography.labelSmall, color = s.agentState.color(), maxLines = 1)
                        }
                        Text(s.displayTitle, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                        Text(
                            listOf(s.agentState.label(), ago(s.updated)).filter { it.isNotEmpty() }.joinToString(" · "),
                            style = MaterialTheme.typography.labelSmall, color = s.agentState.color(), maxLines = 1,
                        )
                        s.lastText?.takeIf { it.isNotBlank() }?.let {
                            Text(it.replace('\n', ' '), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 4, overflow = TextOverflow.Ellipsis)
                        }
                    }
                }
            }
            if (space.workspaceId != null && machine?.online == true) item(key = "new", span = { GridItemSpan(1) }) {
                OutlinedCard(
                    onClick = { newTab = true }, modifier = Modifier.fillMaxWidth().heightIn(min = 150.dp),
                    border = BorderStroke(1.dp, MaterialTheme.colorScheme.primary.copy(alpha = 0.4f)),
                    colors = CardDefaults.outlinedCardColors(containerColor = MaterialTheme.colorScheme.primary.copy(alpha = 0.05f)),
                ) {
                    Column(Modifier.fillMaxWidth().heightIn(min = 150.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
                        Icon(Icons.Default.Add, null, tint = MaterialTheme.colorScheme.primary)
                        Text("在这个空间开新标签", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, maxLines = 1)
                    }
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
                JarvisApp.instance.scope.launch { runCatching { hub.renameSpace(space, name) }.onFailure { notice = "重命名没成功：${it.message}" } }
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
