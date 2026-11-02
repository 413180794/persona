package jarvis.android.ui.machines

import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.ChoiceChip
import jarvis.android.ui.design.PageHeader
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.navigationBarsPadding
import jarvis.android.ui.settings.*
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ShowChart
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import jarvis.android.data.Hub
import jarvis.android.data.HubException
import jarvis.android.data.Machine
import jarvis.android.data.MachineSystem
import jarvis.android.data.parseTime
import jarvis.android.settings.MachineMetrics
import jarvis.android.settings.MetricFormat
import jarvis.android.settings.MetricLevel
import jarvis.android.settings.MetricRange
import jarvis.android.settings.MetricSample
import jarvis.android.settings.machineMetrics
import jarvis.android.settings.osName
import jarvis.android.settings.relativeTime
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * One machine's system and its last hours (iOS `MachineMonitorView`, docs/machines.md「机器详情」): what it runs on,
 * the values now, and CPU / memory / network / load / disk over 1, 6 or 24 hours with the gaps left open. Hold a chart
 * to read a moment (all charts follow). Refreshes while on screen: every 10 s for the hour, every minute otherwise.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MachineMonitorScreen(hub: Hub, machineId: String, onBack: () -> Unit) {
    val c = kitColors
    val machines by hub.machines.collectAsState()
    val machine = machines.firstOrNull { it.id == machineId }
    var range by rememberSaveable { mutableStateOf(MetricRange.HOUR) }
    var data by remember { mutableStateOf<MachineMetrics?>(null) }
    var shownRange by remember { mutableStateOf<MetricRange?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }
    val selection = remember { mutableStateOf<Long?>(null) }
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    LaunchedEffect(range) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                val r = range
                try {
                    data = hub.api.machineMetrics(machineId, r)
                    shownRange = r
                    failure = null
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    if (e is HubException && e.status == 404) failure = "hub 还不支持机器监控：部署新版本的 hub 和这台电脑上的 node 后再看。"
                    else if (data == null) failure = if (e is java.io.IOException) "连不上 hub。连上后这里会自动刷新。" else e.kitMessage
                }
                delay(r.pollMillis)
            }
        }
    }

    val system = data?.system ?: machine?.system
    val online = data?.online ?: machine?.online ?: false
    Column(Modifier.fillMaxSize().background(c.page)) {
        PageHeader(machine?.displayName ?: machineId, onBack, subtitle = "系统与监控")
        Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding()
                .padding(top = JSpace.s, bottom = JSpace.xxl, start = JSpace.page, end = JSpace.page),
            verticalArrangement = Arrangement.spacedBy(JSpace.l),
        ) {
            Header(machine, system, online)
            val latest = data?.latest
            if (latest != null && online) NowGrid(latest, system?.cores)
            // 1 / 6 / 24 小时 as the app's choice chips (iOS: a segmented picker).
            Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                MetricRange.entries.forEach { r -> ChoiceChip(r.title, r == range, { range = r }) }
            }
            Charts(data, shownRange, system, selection, failure)
            val cores = data?.latest?.cores
            if (cores != null && online && cores.size > 1) CoreBars(cores)
            if (system != null) SystemFacts(system, machine)
            Text("历史保留 24 小时。最近 1 小时每 10 秒一个点，更早的每分钟一个点；中间断开的时段不连线。",
                style = JTheme.type.label, color = c.secondaryLabel, modifier = Modifier.padding(horizontal = JSpace.xs))
        }
    }
}

/** Now, ticking every 30 s (uptime, 最后在线). */
@Composable
private fun rememberNow(): State<Long> = produceState(System.currentTimeMillis()) {
    while (true) { delay(30_000); value = System.currentTimeMillis() }
}

@Composable
private fun StateDot(color: Color, size: Dp) {
    Box(Modifier.size(size).background(color, CircleShape))
}

@Composable
private fun Header(m: Machine?, system: MachineSystem?, online: Boolean) {
    val c = kitColors
    val now by rememberNow()
    Row(Modifier.fillMaxWidth().kitCard(padding = 14.dp), verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.l)) {
        OSBadge(system, m, size = 54.dp, online = online)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(osName(m, system) ?: "未知系统", style = JTheme.type.heading, color = c.label, maxLines = 2)
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                StateDot(if (online) c.positive else c.secondaryLabel, size = 7.dp)
                Text(if (online) "在线" else "离线", style = JTheme.type.secondaryStrong,
                    color = if (online) c.positive else c.secondaryLabel, maxLines = 1)
                val boot = system?.bootTime
                if (online && boot != null && boot > 0) {
                    Text("已运行 " + MetricFormat.uptime(boot, now), style = JTheme.type.secondary, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
                } else if (!online && parseTime(m?.lastSeen) != null) {
                    Text("最后在线 " + relativeTime(parseTime(m?.lastSeen), now), style = JTheme.type.secondary, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

@Composable
private fun Charts(data: MachineMetrics?, shownRange: MetricRange?, system: MachineSystem?, selection: androidx.compose.runtime.MutableState<Long?>, failure: String?) {
    val c = kitColors
    if (data != null && shownRange != null) {
        val end = System.currentTimeMillis() / 1000
        val start = end - shownRange.seconds
        val step = shownRange.step
        val memTotal = (data.latest?.memTotal?.takeIf { it > 0 } ?: system?.memory ?: 0L).toDouble()
        val live = if (data.online) data.latest else null
        val cores = (system?.cores ?: 0).toDouble()
        val cpu = MetricColors.cpu
        val mem = MetricColors.mem
        val rx = MetricColors.rx
        val tx = MetricColors.tx
        val load = MetricColors.load
        val cpuSeries = remember(cpu) { listOf(ChartSeries("CPU", cpu) { it.cpu }) }
        val memSeries = remember(mem, memTotal) { listOf(ChartSeries("内存", mem) { p -> p.mem?.let { if (memTotal > 0) 100 * it / memTotal else 0.0 } }) }
        val netSeries = remember(rx, tx) { listOf(ChartSeries("下行", rx) { it.rx }, ChartSeries("上行", tx) { it.tx }) }
        val loadSeries = remember(load) { listOf(ChartSeries("负载", load) { it.load }) }
        val disk = MetricColors.disk
        val diskAll = (data.latest?.diskTotal ?: 0L).toDouble()
        val diskSeries = remember(disk, diskAll) { listOf(ChartSeries("磁盘", disk) { p -> p.disk?.let { if (diskAll > 0) 100 * it / diskAll else null } }) }
        MetricChart("CPU", data.points, cpuSeries, start, end, step, { MetricFormat.percent(it) }, listOf(live?.cpu), selection, yFixed = 100.0)
        MetricChart(if (memTotal > 0) "内存（共 ${MetricFormat.bytes(memTotal)}）" else "内存", data.points, memSeries, start, end, step,
            { MetricFormat.percent(it) }, listOf(live?.memPercent), selection, yFixed = 100.0)
        MetricChart("网络", data.points, netSeries, start, end, step, { MetricFormat.rate(it) }, listOf(live?.netRx, live?.netTx), selection,
            binary = true, axisFormat = { MetricFormat.axisRate(it) })
        MetricChart("负载（1 分钟）", data.points, loadSeries, start, end, step, { MetricFormat.load(it) }, listOf(live?.load), selection,
            reference = if (cores > 0) cores to "${cores.toInt()} 核" else null)
        if (diskAll > 0) {
            MetricChart("磁盘（共 ${MetricFormat.bytes(diskAll)}）", data.points, diskSeries, start, end, step, { MetricFormat.percent(it) },
                listOf(live?.diskPercent), selection, yFixed = 100.0)
        }
        if (data.points.all { it.isGap }) {
            Text(if (data.online) "刚开始记录：过一会儿这里就有曲线了。" else "这段时间这台机器没有连上。",
                style = JTheme.type.caption, color = c.secondaryLabel, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth())
        }
    } else if (failure != null) {
        KitEmpty(icon = JIcons.chart, title = "看不到监控数据", message = failure, modifier = Modifier.padding(vertical = 30.dp))
    } else {
        Box(Modifier.fillMaxWidth().padding(vertical = 60.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
    }
}

/** The values now, two columns: CPU, memory, network, load, disk, swap (iOS `NowGrid`). */
@Composable
private fun NowGrid(s: MetricSample, cores: Int?) {
    val c = kitColors
    val tiles: List<@Composable (Modifier) -> Unit> = listOf(
        { m -> Tile(m, "CPU", MetricFormat.percent(s.cpu), null, s.cpu?.let { it / 100 }, MetricColors.cpu, MetricLevel.of(s.cpu, 75.0, 90.0)) },
        { m -> Tile(m, "内存", MetricFormat.percent(s.memPercent), MetricFormat.bytes(s.memUsed) + " / " + MetricFormat.bytes(s.memTotal),
            s.memPercent?.let { it / 100 }, MetricColors.mem, MetricLevel.of(s.memPercent, 80.0, 90.0)) },
        { m -> Tile(m, "网络", "↓ " + MetricFormat.rate(s.netRx), "↑ " + MetricFormat.rate(s.netTx), null, MetricColors.rx, MetricLevel.NORMAL) },
        { m -> Tile(m, "负载", MetricFormat.load(s.load), cores?.let { "$it 核" }, null, MetricColors.load,
            MetricLevel.of(s.load?.let { l -> cores?.let { l / maxOf(it, 1) } }, 1.0, 2.0)) },
        { m -> Tile(m, "磁盘", MetricFormat.percent(s.diskPercent), MetricFormat.bytes(s.diskUsed) + " / " + MetricFormat.bytes(s.diskTotal),
            s.diskPercent?.let { it / 100 }, c.secondaryLabel, MetricLevel.of(s.diskPercent, 80.0, 90.0)) },
        { m -> Tile(m, "交换", if (s.swapTotal > 0) MetricFormat.bytes(s.swapUsed) else "未启用",
            if (s.swapTotal > 0) "共 " + MetricFormat.bytes(s.swapTotal) else null, s.swapPercent?.let { it / 100 }, c.secondaryLabel, MetricLevel.NORMAL) },
    )
    Column(verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
        tiles.chunked(2).forEach { pair ->
            Row(Modifier.height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(JSpace.m)) { pair.forEach { it(Modifier.weight(1f).fillMaxHeight()) } }
        }
    }
}

@Composable
private fun Tile(modifier: Modifier, title: String, value: String, detail: String?, fraction: Double?, color: Color, level: MetricLevel) {
    val c = kitColors
    Column(modifier.kitCard(padding = 12.dp), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            Text(title, style = JTheme.type.label, color = c.secondaryLabel, maxLines = 1)
            if (level > MetricLevel.NORMAL) {
                Icon(JIcons.error, null, tint = MetricColors.level(level), modifier = Modifier.size(JSize.markXS))
                Text(if (level == MetricLevel.CRITICAL) "过高" else "偏高", style = JTheme.type.labelStrong, color = c.secondaryLabel, maxLines = 1)
            }
        }
        Text(value, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(detail ?: " ", style = JTheme.type.label, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (fraction != null) MeterBar(fraction, if (level > MetricLevel.NORMAL) MetricColors.level(level) else color)
        else Spacer(Modifier.height(6.dp)) // every tile the same height
    }
}

/** Each core now (Linux), one thin column each. */
@Composable
private fun CoreBars(cores: List<Double>) {
    val c = kitColors
    val cpu = MetricColors.cpu
    Column(Modifier.fillMaxWidth().kitCard(padding = 14.dp), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
        Row {
            Text("每个核心", style = JTheme.type.secondaryStrong, color = c.label, modifier = Modifier.weight(1f))
            Text("最忙 ${MetricFormat.percent(cores.maxOrNull())}", style = JTheme.type.label, color = c.secondaryLabel)
        }
        val gap = if (cores.size > 32) 1.dp else 2.dp
        Row(Modifier.fillMaxWidth().height(44.dp), horizontalArrangement = Arrangement.spacedBy(gap), verticalAlignment = Alignment.Bottom) {
            cores.forEach { v ->
                Box(Modifier.weight(1f).fillMaxHeight().background(c.label.copy(alpha = 0.07f), RoundedCornerShape(2.dp)), contentAlignment = Alignment.BottomCenter) {
                    Box(Modifier.fillMaxWidth().fillMaxHeight((v / 100).toFloat().coerceIn(0.05f, 1f)).background(cpu, RoundedCornerShape(2.dp)))
                }
            }
        }
    }
}

private val bootFormat = DateTimeFormatter.ofPattern("yyyy年M月d日 HH:mm")

/** 系统: OS, kernel, hardware, versions. */
@Composable
private fun SystemFacts(system: MachineSystem, machine: Machine?) {
    val c = kitColors
    val rows = buildList {
        add("系统" to ((system.name ?: osName(machine, system) ?: "—") + (system.build?.takeIf { it.isNotEmpty() }?.let { "（$it）" } ?: "")))
        system.kernel?.takeIf { it.isNotEmpty() }?.let { add("内核" to it) }
        add("架构" to (system.arch ?: machine?.arch ?: "—"))
        system.model?.takeIf { it.isNotEmpty() }?.let { add("机型" to it) }
        system.cpu?.takeIf { it.isNotEmpty() }?.let { add("处理器" to it) }
        add("核心" to "${system.cores ?: 0}")
        add("内存" to MetricFormat.bytes(system.memory ?: 0L))
        system.bootTime?.takeIf { it > 0 }?.let { add("开机于" to bootFormat.format(Instant.ofEpochSecond(it).atZone(ZoneId.systemDefault()))) }
        machine?.herdrVersion?.takeIf { it.isNotEmpty() }?.let { add("herdr" to it) }
        machine?.claudeVersion?.takeIf { it.isNotEmpty() }?.let { add("Claude Code" to it) }
        machine?.nodeVersion?.takeIf { it.isNotEmpty() }?.let { add("Jarvis node" to it) }
    }
    Column(Modifier.fillMaxWidth().kitCard(padding = 14.dp)) {
        Text("系统", style = JTheme.type.secondaryStrong, color = c.label, modifier = Modifier.padding(bottom = JSpace.s))
        rows.forEachIndexed { i, (k, v) ->
            Row(Modifier.fillMaxWidth().padding(vertical = JSpace.sm), horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
                Text(k, style = JTheme.type.secondary, color = c.secondaryLabel)
                Text(v, style = JTheme.type.secondary, color = c.label, textAlign = TextAlign.End, modifier = Modifier.weight(1f))
            }
            if (i < rows.size - 1) Box(Modifier.fillMaxWidth().height(0.5.dp).background(c.separator))
        }
    }
}

