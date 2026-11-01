package jarvis.android.ui.machines

import jarvis.android.ui.settings.*
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Speed
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.AgentState
import jarvis.android.data.Engines
import jarvis.android.data.Hub
import jarvis.android.data.Launcher
import jarvis.android.data.Session
import jarvis.android.settings.machineAgents
import jarvis.android.settings.modelLabel
import jarvis.android.settings.relativeTime
import jarvis.android.ui.color
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

/**
 * One machine (iOS `MachineDetailView`): its card, the Claude sessions running there (a tap opens the chat), 系统与监控,
 * its engine (EngineScreen), its launchers, and 重新连接 (waits up to 15 s for it to come online and says so if it does not).
 */
@Composable
fun MachineDetailScreen(
    hub: Hub,
    machineId: String,
    onBack: () -> Unit,
    onOpenSession: (jarvis.android.data.SessionKey) -> Unit,
    onOpenMonitor: () -> Unit,
    onOpenEngine: () -> Unit,
) {
    val c = kitColors
    val ctx = LocalContext.current
    val machines by hub.machines.collectAsState()
    val byMachine by hub.sessions.collectAsState()
    val machine = machines.firstOrNull { it.id == machineId }
    val sessions = remember(byMachine, machineId) { machineAgents(byMachine[machineId]).sortedByDescending { it.updated } }
    val online = machine?.online == true
    var launchers by remember { mutableStateOf<List<Launcher>>(emptyList()) }
    var launcherError by remember { mutableStateOf<String?>(null) }
    var reconnecting by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    // Again when it comes online (after 重新连接): an offline machine has no launchers to list.
    LaunchedEffect(online) {
        if (!online) { launchers = emptyList(); return@LaunchedEffect }
        try {
            launchers = hub.api.launchers(machineId)
            launcherError = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            launcherError = "读不到启动器：${e.kitMessage}"
        }
    }

    fun reconnect() {
        scope.launch {
            reconnecting = true
            try {
                hub.api.reconnect(machineId)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                kitToast(ctx, "重新连接失败：${e.kitMessage}")
                reconnecting = false
                return@launch
            }
            val ok = withTimeoutOrNull(15_000) { hub.machines.first { l -> l.any { it.id == machineId && it.online } } }
            reconnecting = false
            if (ok == null) {
                val err = hub.machines.value.firstOrNull { it.id == machineId }?.error
                kitToast(ctx, "还没连上" + (err?.takeIf { it.isNotBlank() }?.let { "：$it" } ?: ""))
            } else {
                kitToast(ctx, "已连上")
            }
        }
    }

    KitPage(machine?.displayName ?: machineId, onBack) {
        if (machine == null) {
            item { Text("hub 上没有这台机器了。", fontSize = 15.sp, color = c.secondaryLabel, modifier = Modifier.padding(32.dp)) }
            return@KitPage
        }
        item(key = "card") { MachineCard(hub, machine, Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
        item(key = "sessions") {
            KitGroup(header = "运行中的 Claude", separatorStart = 38.dp) {
                if (sessions.isEmpty()) {
                    row {
                        Text(if (online) "没有运行中的会话" else "机器离线，连上后显示它的会话", fontSize = 17.sp, color = c.secondaryLabel,
                            modifier = Modifier.padding(16.dp))
                    }
                }
                sessions.forEach { s -> row { SessionLine(s) { onOpenSession(s.key) } } }
            }
        }
        item(key = "monitor") {
            KitGroup(Modifier.padding(top = 20.dp), footer = "只显示所选引擎里的会话。") {
                row { KitRow("系统与监控", icon = Icons.Default.Speed, onClick = onOpenMonitor) }
                row { KitRow("引擎", icon = Icons.Default.Terminal, value = Engines.currentName(machine), onClick = onOpenEngine) }
            }
        }
        item(key = "launchers") {
            KitGroup(header = if (online) "启动器 ${launchers.size}" else "启动器") {
                when {
                    !online -> row { Text("机器离线，连上后显示", fontSize = 17.sp, color = c.secondaryLabel, modifier = Modifier.padding(16.dp)) }
                    launcherError != null && launchers.isEmpty() -> row {
                        Row(Modifier.padding(16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Icon(Icons.Default.Warning, null, tint = c.danger, modifier = Modifier.size(16.dp))
                            Text(launcherError.orEmpty(), fontSize = 13.sp, color = c.danger)
                        }
                    }
                }
                launchers.forEach { l ->
                    row {
                        Row(Modifier.fillMaxWidth().heightIn(min = 44.dp).padding(horizontal = 16.dp, vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically) {
                            Text(l.name, fontSize = 16.sp, fontFamily = FontFamily.Monospace, color = c.label, modifier = Modifier.weight(1f), maxLines = 1,
                                overflow = TextOverflow.Ellipsis)
                            Text(modelLabel(l.model).orEmpty(), fontSize = 12.sp, color = c.secondaryLabel, maxLines = 1)
                        }
                    }
                }
            }
        }
        item(key = "reconnect") {
            KitGroup(
                Modifier.padding(top = 20.dp),
                footer = if (machine.kind == "local") "hub 所在的机器，直接运行 node。" else "hub 通过 ssh ${machine.sshHost ?: machine.id} 接入。",
            ) {
                row {
                    Row(
                        Modifier.fillMaxWidth().clickable(enabled = !reconnecting) { reconnect() }.heightIn(min = 48.dp)
                            .padding(horizontal = 16.dp, vertical = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Icon(Icons.Default.Refresh, null, tint = c.accent, modifier = Modifier.size(22.dp))
                        Text(if (reconnecting) "正在重新连接…" else "重新连接", fontSize = 17.sp, color = c.accent, maxLines = 1,
                            modifier = Modifier.weight(1f))
                        if (reconnecting) CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                    }
                }
            }
        }
    }
}

/** A session on the machine's page: state dot, title, the latest line, when. */
@Composable
private fun SessionLine(s: Session, onClick: () -> Unit) {
    val c = kitColors
    val state = s.agentState
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).heightIn(min = 56.dp).padding(start = 16.dp, end = 10.dp, top = 10.dp, bottom = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(Modifier.size(10.dp).background(state.color(), CircleShape))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(s.displayTitle, fontSize = 17.sp, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    fontWeight = if (state == AgentState.Unread) FontWeight.SemiBold else FontWeight.Normal,
                    modifier = Modifier.weight(1f, fill = false))
                if (state == AgentState.NeedsYou) Text("需要你", fontSize = 12.sp, color = c.danger, maxLines = 1)
            }
            s.lastText?.takeIf { it.isNotBlank() }?.let {
                Text(it.replace('\n', ' '), fontSize = 13.sp, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        Text(relativeTime(s.updated), fontSize = 13.sp, color = c.secondaryLabel, maxLines = 1)
        Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = c.tertiaryLabel, modifier = Modifier.size(20.dp))
    }
}
