package jarvis.android.ui.machines

import jarvis.android.ui.settings.*
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Circle
import androidx.compose.material.icons.filled.Devices
import androidx.compose.material.icons.filled.Memory
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.TextButton
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.AgentState
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.data.Machine
import jarvis.android.settings.machineAgents
import jarvis.android.ui.color

/**
 * 设置 › 机器 (iOS `MachinesView`): a card per machine (OS, online, how many agents run / work / need you, versions,
 * the node_mismatch hint); a tap opens its page. The title says 连接中… while the hub is out of reach.
 */
@Composable
fun MachinesScreen(hub: Hub, onBack: () -> Unit, onOpenMachine: (String) -> Unit) {
    val machines by hub.machines.collectAsState()
    val conn by hub.conn.collectAsState()
    KitPage(if (conn == ConnState.Online || conn == ConnState.Unauthorized) "机器" else "机器 · 连接中…", onBack) {
        items(machines, key = { it.id }) { m ->
            MachineCard(hub, m, Modifier.padding(horizontal = 16.dp, vertical = 7.dp), onClick = { onOpenMachine(m.id) })
        }
        if (machines.isEmpty()) {
            item {
                KitEmpty(Icons.Default.Devices, "还没有机器", "hub 连上后，这里会显示 hub 所在的机器和它接入的各台电脑。",
                    Modifier.padding(top = 60.dp))
            }
        }
    }
}

/** One machine (iOS `MachineCard`): the list's cards and the detail page's header. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun MachineCard(hub: Hub, machine: Machine, modifier: Modifier = Modifier, onClick: (() -> Unit)? = null) {
    val c = kitColors
    val byMachine by hub.sessions.collectAsState()
    val live = machineAgents(byMachine[machine.id])
    val working = live.count { it.agentState == AgentState.Working }
    val waiting = live.count { it.agentState == AgentState.NeedsYou }
    val shape = RoundedCornerShape(KitCorner)
    Column(
        modifier.fillMaxWidth().kitCard().clip(shape).let { if (onClick != null) it.clickable(onClick = onClick) else it }.padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Row(verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OSBadge(machine.system, machine, size = 52.dp, online = machine.online)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text(machine.displayName, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(machine.hostname ?: machine.sshHost ?: machine.id, fontSize = 12.sp, fontFamily = FontFamily.Monospace,
                    color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            KitChip(if (machine.online) "在线" else "离线", color = if (machine.online) c.positive else Color.Unspecified, icon = Icons.Default.Circle)
        }
        if (machine.online) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Stat("${live.size}", "运行中", null, Modifier.weight(1f))
                Box(Modifier.width(0.5.dp).height(28.dp).background(c.separator))
                Stat("$working", "工作中", if (working > 0) AgentState.Working.color() else null, Modifier.weight(1f))
                Box(Modifier.width(0.5.dp).height(28.dp).background(c.separator))
                Stat("$waiting", "需要你", if (waiting > 0) AgentState.NeedsYou.color() else null, Modifier.weight(1f))
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                val sys = machine.system?.name?.takeIf { it.isNotBlank() }
                if (sys != null) KitChip(sys, icon = Icons.Default.Memory)
                else if (machine.os != null) KitChip(listOfNotNull(machine.os, machine.arch).joinToString("/"), icon = Icons.Default.Memory)
                machine.herdrVersion?.takeIf { it.isNotEmpty() }?.let { KitChip("herdr $it") }
                machine.claudeVersion?.takeIf { it.isNotEmpty() }?.let { KitChip("claude $it") }
            }
            if (machine.nodeMismatch == true || machine.deploying == true) DeployRow(hub, machine)
        } else if (!machine.error.isNullOrEmpty()) {
            Text(machine.error, fontSize = 12.sp, color = c.secondaryLabel, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** Not the hub's build: says so, and 重新部署 has the hub install its own build there (iOS `DeployRow`). */
@Composable
private fun DeployRow(hub: Hub, machine: Machine) {
    val c = kitColors
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var starting by remember { mutableStateOf(false) }
    val busy = machine.deploying == true || starting
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Icon(if (busy) Icons.Default.Refresh else Icons.Default.Warning, null, tint = c.caution, modifier = Modifier.size(14.dp))
        Text(
            when {
                busy -> "正在把 hub 这一版的 jarvisd 装到这台电脑…"
                !machine.deployError.isNullOrBlank() -> "上次部署没成：${machine.deployError}"
                else -> "jarvisd 和 hub 不是同一次部署"
            },
            fontSize = 12.sp, color = c.caution, modifier = Modifier.weight(1f),
        )
        if (busy) {
            CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
        } else {
            TextButton(onClick = {
                scope.launch {
                    starting = true
                    try {
                        hub.api.deploy(machine.id)
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        kitToast(ctx, "部署失败：${e.kitMessage}")
                    } finally {
                        starting = false
                    }
                }
            }) { Text("重新部署", maxLines = 1) }
        }
    }
}

@Composable
private fun Stat(value: String, label: String, tint: Color?, modifier: Modifier) {
    val c = kitColors
    Column(modifier, horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(value, fontSize = 22.sp, fontWeight = FontWeight.SemiBold, color = tint ?: c.label, maxLines = 1)
        Text(label, fontSize = 12.sp, color = c.secondaryLabel, maxLines = 1)
    }
}
