package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.data.AgentState
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.data.Interaction
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import kotlinx.coroutines.launch
import java.time.Duration
import java.time.Instant

/**
 * Every agent on every computer, in the order the iOS app's 按状态 grouping uses (ChatsView.rank): needs you →
 * done unseen → working → seen, newest first within a state. The assistant's conversations are not agents.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AgentsScreen(hub: Hub, onOpen: (SessionKey) -> Unit, onSettings: () -> Unit) {
    val conn by hub.conn.collectAsState()
    val machines by hub.machines.collectAsState()
    val byMachine by hub.sessions.collectAsState()
    val pending by hub.interactions.collectAsState()
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    val names = machines.associate { it.id to it.displayName }
    val sessions = byMachine.values.flatten().filter { !it.assistant }
        .sortedWith(compareBy<Session> { it.agentState.priority }.thenByDescending { it.updated ?: Instant.EPOCH })
    val (live, offline) = sessions.partition { it.isLive }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("Agents", maxLines = 1)
                        Text(
                            when (conn) {
                                ConnState.Online -> "${machines.count { it.online }}/${machines.size} 台电脑在线"
                                ConnState.Connecting -> "正在连接…"
                                ConnState.Offline -> "hub 连不上，正在重试"
                            },
                            style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                },
                actions = { IconButton(onClick = onSettings) { Icon(Icons.Default.Settings, "设置") } },
            )
        },
    ) { pad ->
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = { scope.launch { refreshing = true; hub.refresh(); refreshing = false } },
            modifier = Modifier.padding(pad).fillMaxSize(),
        ) {
            LazyColumn(Modifier.fillMaxSize()) {
                if (pending.isNotEmpty()) {
                    item { SectionTitle("需要你处理 · ${pending.size}") }
                    items(pending, key = { "ix/" + it.id }) { ix -> PendingRow(ix, names) { onOpen(ix.sessionKey) } }
                }
                if (live.isNotEmpty()) {
                    item { SectionTitle("在跑的 Agent") }
                    items(live, key = { it.key.path }) { s -> SessionRow(s, names[s.machineId]) { onOpen(s.key) } }
                }
                if (offline.isNotEmpty()) {
                    item { SectionTitle("最近 7 天") }
                    items(offline, key = { it.key.path }) { s -> SessionRow(s, names[s.machineId]) { onOpen(s.key) } }
                }
                if (sessions.isEmpty() && pending.isEmpty()) {
                    item {
                        Text(
                            if (conn == ConnState.Online) "各台电脑的 herdr 里还没有 Agent。在电脑上的 herdr 里启动 claude，这里就会出现。" else "等待连接 hub…",
                            Modifier.padding(24.dp), color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(
        text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary,
        modifier = Modifier.padding(start = 16.dp, top = 16.dp, bottom = 4.dp),
    )
}

@Composable
private fun PendingRow(ix: Interaction, names: Map<String, String>, onClick: () -> Unit) {
    Card(
        onClick = onClick,
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer),
        modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp),
    ) {
        Column(Modifier.padding(12.dp)) {
            Text(
                listOfNotNull(names[ix.machineId], ix.sessionTitle?.takeIf { it.isNotBlank() } ?: ix.cwd?.substringAfterLast('/')).joinToString(" › "),
                style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
            Text(ix.headline, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
fun StateDot(state: AgentState, size: Int = 10) {
    Box(Modifier.size(size.dp).clip(CircleShape).background(state.color()))
}

@Composable
private fun SessionRow(s: Session, machine: String?, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        StateDot(s.agentState)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    s.displayTitle, style = MaterialTheme.typography.bodyLarge,
                    fontWeight = if (s.agentState.priority <= 1) FontWeight.SemiBold else FontWeight.Normal,
                    maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false),
                )
                Spacer(Modifier.width(8.dp))
                Text(ago(s.updated), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            }
            val where = listOfNotNull(machine, s.spaceName?.takeIf { it.isNotBlank() }, s.agentLabel.takeIf { it != "Claude" }).joinToString(" · ")
            val sub = buildString {
                if (s.agentState == AgentState.NeedsYou || s.agentState == AgentState.Working || s.agentState == AgentState.Unread) append(s.agentState.label()).append(" · ")
                append(where)
            }
            Text(sub, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            s.lastText?.takeIf { it.isNotBlank() }?.let {
                Text(
                    it.replace('\n', ' '), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2, overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
    HorizontalDivider(Modifier.padding(start = 38.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
}

fun ago(t: Instant?): String {
    t ?: return ""
    val d = Duration.between(t, Instant.now())
    return when {
        d.seconds < 60 -> "刚刚"
        d.toMinutes() < 60 -> "${d.toMinutes()} 分钟前"
        d.toHours() < 24 -> "${d.toHours()} 小时前"
        else -> "${d.toDays()} 天前"
    }
}

