package persona.feature.machines

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.*

@Composable
internal fun MachinesScreen(
    machineId: String?, model: MachinesViewModel, navigator: Navigator, actions: MachineActionsViewModel? = null, launcherPages: Boolean = false,
    /** What plugins draw on this machine's page, after its own sections (docs/plugins.md「UI 插槽」). */
    pluginSections: (androidx.compose.foundation.lazy.LazyListScope.() -> Unit)? = null,
) {
    val state by model.state.collectAsStateWithLifecycle()
    val machines by model.sessions.machines.collectAsStateWithLifecycle()
    val sessions by model.sessions.sessions.collectAsStateWithLifecycle()
    val machine = machines.firstOrNull { it.id == machineId }
    val acting = actions?.state?.collectAsStateWithLifecycle()?.value ?: MachineActionsState()
    // An offline machine has no launchers; they are read again when it comes online (after 重新连接).
    if (actions != null && machineId != null) LaunchedEffect(actions, machineId, machine?.online) {
        actions.loadLaunchers(machineId, machine?.online == true)
    }
    val deployLine: @Composable (Machine?) -> Unit = { info ->
        if (info != null && showsDeploy(info)) MachineDeployLine(info, info.id in acting.deployStarting, actions?.let { { it.deploy(info.id) } })
    }
    var selection by rememberSaveable(machineId, state.range) { mutableStateOf<Long?>(null) }
    val metrics = state.dashboard?.machines.orEmpty().associateBy { it.machineId }
    val ids = (machines.map { it.id } + metrics.keys).distinct()
    val online = ids.count { metrics[it]?.online ?: machines.firstOrNull { m -> m.id == it }?.online ?: false }
    val attention = metrics.values.count { alerts(it.current(state.now), it.system?.cores).isNotEmpty() }
    JListPage(
        title = if (machineId == null) "机器监控" else machine?.displayName ?: "机器详情",
        summary = if (machineId == null) "$online/${ids.size} 台在线，$attention 台需要注意" else null,
        onBack = navigator::back, largeTitle = machineId == null,
        actions = { JIconButton(PhIcons.ArrowClockwise, "刷新", model::refresh) },
    ) {
        if (state.loading) item("loading") { JLinearProgress(null, Modifier.fillMaxWidth()) }
        state.failure?.let { message -> item("failure") { JNotice(message) } }
        if (state.sessionsFailure) item("sessions-failure") { JNotice("机器和会话列表暂未更新。") }
        if (machineId == null) {
            if (!state.loading && ids.isEmpty()) item("empty") {
                JEmptyState(if (state.failure == null) "还没有机器" else "暂时看不到机器", "hub 连上各台电脑后，这里会显示它们的状态。")
            }
            items(ids, key = { it }, contentType = { "machine" }) { id ->
                val info = machines.firstOrNull { it.id == id }
                val metric = metrics[id] ?: MachineMetrics(id, info?.online ?: false, info?.system)
                MachineCard(info, metric, machineSessions(sessions, id), state.now, state.dashboard?.step ?: 60, deploy = deployLine) {
                    navigator.navigate(MachineDetailRoute(id))
                }
            }
        } else {
            val metric = state.history?.metrics ?: MachineMetrics(machineId, machine?.online ?: false, machine?.system)
            val system = metric.system ?: machine?.system
            val current = metric.current(state.now)
            val agents = machineSessions(sessions, machineId)
            item("machine") { MachineCard(machine, metric, agents, state.now, state.history?.step ?: state.range.step, deploy = deployLine) }
            if (machine == null && machines.isNotEmpty()) item("gone") { JNotice("hub 上没有这台机器了。") }
            if (machine != null && actions != null) machineControls(machine, acting.reconnecting,
                onEngine = { navigator.navigate(EngineRoute(machine.id)) }, onReconnect = { actions.reconnect(machine.id) })
            item("now") {
                JSectionHeader("现在", trailing = MetricFormat.uptime(system?.bootTime, state.now)?.takeIf { metric.online }?.let { "已运行 $it" })
            }
            val values = listOf(
                "CPU" to MetricFormat.percent(current?.cpu),
                "内存" to "${MetricFormat.bytes(current?.memUsed)} / ${MetricFormat.bytes(current?.memTotal)}",
                "网络" to "↓ ${MetricFormat.rate(current?.netRx)}  ↑ ${MetricFormat.rate(current?.netTx)}",
                "负载" to MetricFormat.number(current?.load),
                "磁盘" to "${MetricFormat.bytes(current?.diskUsed)} / ${MetricFormat.bytes(current?.diskTotal)}",
                "交换" to if (current?.swapTotal == 0L) "未启用" else "${MetricFormat.bytes(current?.swapUsed)} / ${MetricFormat.bytes(current?.swapTotal)}",
            )
            itemsIndexed(values, key = { _, it -> "now-${it.first}" }, contentType = { _, _ -> "value" }) { index, value ->
                JGroupItem(GroupPosition.of(index, values.size)) { JRow(value.first, value = value.second) }
            }
            if (current == null) item("no-current") { JNotice(if (metric.online) "还没有新的采样" else "机器离线，以下曲线保留历史采样") }
            item("range") {
                JSectionHeader("历史趋势")
                JSegmented(MetricRange.entries.map { JSegment(when (it) {
                    MetricRange.Hour -> "1 小时"; MetricRange.SixHours -> "6 小时"; MetricRange.Day -> "24 小时"
                }) }, MetricRange.entries.indexOf(state.range), { selection = null; model.selectRange(MetricRange.entries[it]) })
                Text("长按曲线查看同一时刻的数值", Modifier.padding(vertical = 8.dp), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
            }
            if (selection != null) item("selection") { JTextButton("回到现在", { selection = null }) }
            // Disk history is in bytes; the chart reads it against the disk's size, so only when that is known.
            val diskTotal = (current?.diskTotal ?: metric.latest?.diskTotal)?.takeIf { it > 0 }
            val chartKinds = listOf(listOf(MetricKind.Cpu), listOf(MetricKind.Memory), listOf(MetricKind.Rx, MetricKind.Tx), listOf(MetricKind.Load)) +
                listOfNotNull(listOf(MetricKind.Disk).takeIf { diskTotal != null })
            items(chartKinds, key = { "chart-${it.first()}" }, contentType = { "chart" }) { kinds ->
                val disk = kinds.first() == MetricKind.Disk
                MetricChart(when { kinds.size == 2 -> "网络"; disk -> "磁盘（共 ${MetricFormat.bytes(diskTotal)}）"; else -> kinds.first().label },
                    if (disk) diskPercentPoints(metric.points, diskTotal) else metric.points, kinds,
                    kinds.map { kind -> when (kind) {
                        MetricKind.Cpu -> current?.cpu; MetricKind.Memory -> current?.memUsed?.toDouble()
                        MetricKind.Rx -> current?.netRx; MetricKind.Tx -> current?.netTx; MetricKind.Load -> current?.load
                        MetricKind.Disk -> percent(current?.diskUsed, current?.diskTotal)
                    } }, state.now, state.range.seconds, state.history?.step ?: state.range.step, selection, { selection = it },
                    reference = system?.cores?.takeIf { kinds.first() == MetricKind.Load && it > 0 }?.toDouble())
            }
            if (system?.os == "linux" && !current?.cores.isNullOrEmpty()) item("cores") { CoreBars(current.cores!!) }
            item("sessions-title") { JSectionHeader("运行中的会话", count = agents.size) }
            if (agents.isEmpty()) item("no-sessions") { JNotice("这台机器还没有运行中的会话") }
            items(agents, key = { "session-${it.sessionId}" }, contentType = { "session" }) { session ->
                val status = when (session.agentState) {
                    AgentState.NeedsYou -> Status.NeedsYou; AgentState.Working -> Status.Working
                    AgentState.Unread -> Status.Unread; AgentState.Idle -> Status.Idle
                    AgentState.Offline -> Status.Offline; AgentState.Unknown -> Status.Unknown
                }
                JCard(Modifier.padding(bottom = 12.dp), onClick = { navigator.navigate(ChatRoute(machineId, session.sessionId)) }, status = status) {
                    JRow(session.displayTitle, leading = { JStatusDot(status) }, trailing = { JTag(status.label, status = status) })
                    Text(session.agentLabel, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                    Text(session.cwd, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3, maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
            }
            if (machine != null && actions != null) machineLaunchers(machine.online, acting,
                onLauncher = if (launcherPages) ({ name -> navigator.navigate(LauncherRoute(machine.id, name)) }) else null)
            item("system") { JSectionHeader("系统信息") }
            val systemValues = listOf(
                "系统" to system?.name, "版本" to system?.version, "内核" to system?.kernel, "架构" to system?.arch,
                "型号" to system?.model, "处理器" to system?.cpu, "核心" to system?.cores?.takeIf { it > 0 }?.toString(),
                "内存" to MetricFormat.bytes(system?.memory?.takeIf { it > 0 }),
                "接入方式" to if (machine?.kind == "local") "hub 所在的机器" else if (machine?.kind == "ssh") "经 SSH 接入" else null,
                "开机于" to MetricFormat.boot(system?.bootTime),
                "herdr" to machine?.herdrVersion, "Claude Code" to machine?.claudeVersion, "Persona node" to machine?.nodeVersion,
            )
            itemsIndexed(systemValues, key = { _, it -> "system-${it.first}" }, contentType = { _, _ -> "value" }) { index, value ->
                JGroupItem(GroupPosition.of(index, systemValues.size)) { JRow(value.first, value = value.second?.takeIf { it.isNotBlank() } ?: "—") }
            }
            item("method") { Text("CPU 为整机使用率；内存不含可回收缓存。网络只计物理网卡，磁盘沿用 node 的系统盘统计。",
                Modifier.padding(vertical = 12.dp), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3) }
            pluginSections?.invoke(this)
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun MachineCard(
    machine: Machine?, metrics: MachineMetrics, sessions: List<Session>, now: Long, step: Int,
    deploy: (@Composable (Machine?) -> Unit)? = null, onClick: (() -> Unit)? = null,
) {
    val current = metrics.current(now)
    val system = metrics.system ?: machine?.system
    val alerts = alerts(current, system?.cores)
    val status = when { !metrics.online -> Status.Offline; alerts.isNotEmpty() || machine?.nodeMismatch == true -> Status.NeedsYou; else -> Status.Unread }
    JCard(Modifier.padding(bottom = 12.dp), onClick = onClick, status = status) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            OsBadge(system)
            Column(Modifier.weight(1f)) {
                Text(machine?.displayName ?: metrics.machineId, style = PersonaTheme.type.title, color = PersonaTheme.palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(listOfNotNull(system?.name?.takeIf { it.isNotBlank() }, system?.cores?.takeIf { it > 0 }?.let { "$it 核" },
                    system?.memory?.takeIf { it > 0 }?.let(MetricFormat::bytes)).joinToString("  "), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            }
            JTag(when (status) { Status.Offline -> "离线"; Status.NeedsYou -> "需要注意"; else -> "在线" }, status = status)
        }
        machine?.hostname?.takeIf { it.isNotBlank() }?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3) }
        if (!metrics.online) {
            Text("最后在线：${MetricFormat.lastSeen(machine?.lastSeen)}", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            machine?.error?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
        } else {
            Text("运行中 ${sessions.size} 个会话，${sessions.count { it.agentState == AgentState.Working }} 个工作中，${sessions.count { it.agentState == AgentState.NeedsYou }} 个需要你",
                style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            listOfNotNull(machine?.herdrVersion?.let { "herdr $it" }, machine?.claudeVersion?.let { "Claude $it" }).forEach {
                Text(it, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
            }
        }
        if (deploy != null && showsDeploy(machine)) deploy(machine)
        else if (machine?.nodeMismatch == true) Warning("这台电脑上的 Persona 和 hub 不是同一次部署")
        alerts.forEach { Warning(it.label) }
        val groups = listOf(listOf(MetricKind.Cpu), listOf(MetricKind.Memory), listOf(MetricKind.Rx, MetricKind.Tx))
        groups.forEach { kinds ->
            val value = when (kinds.first()) {
                MetricKind.Cpu -> MetricFormat.percent(current?.cpu)
                MetricKind.Memory -> MetricFormat.percent(percent(current?.memUsed, current?.memTotal))
                else -> "↓ ${MetricFormat.rate(current?.netRx)}  ↑ ${MetricFormat.rate(current?.netTx)}"
            }
            Row(Modifier.fillMaxWidth().padding(top = 12.dp), horizontalArrangement = Arrangement.SpaceBetween) {
                Text(if (kinds.size > 1) "网络" else kinds.first().label, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink2)
                Text(value, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink)
            }
            Plot(metrics.points, kinds, now, 3600, step, Modifier.fillMaxWidth().height(32.dp))
        }
    }
}

@Composable
private fun Warning(text: String) {
    Row(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        JIcon(PhIcons.Warning, null, size = 18.dp, tint = PersonaTheme.palette.danger.dot)
        Text(text, color = PersonaTheme.palette.danger.text, style = PersonaTheme.type.caption)
    }
}

@Composable
private fun OsBadge(system: MachineSystem?) {
    JIcon(if (system?.os == "darwin" || system?.distro == "macos") PhIcons.Laptop else PhIcons.Desktop,
        system?.name ?: system?.os ?: "电脑", size = 36.dp, tint = PersonaTheme.palette.ink2)
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun CoreBars(cores: List<Double>) {
    JCard(Modifier.padding(bottom = 12.dp)) {
        Text("每个核心", style = PersonaTheme.type.title, color = PersonaTheme.palette.ink)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            cores.forEachIndexed { index, usage ->
                Column(Modifier.width(76.dp)) {
                    Text("${index + 1} 核", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink2)
                    JLinearProgress(if (usage.isFinite()) (usage / 100).toFloat().coerceIn(0f, 1f) else 0f, Modifier.fillMaxWidth())
                    Text(MetricFormat.percent(usage), style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink)
                }
            }
        }
    }
}
