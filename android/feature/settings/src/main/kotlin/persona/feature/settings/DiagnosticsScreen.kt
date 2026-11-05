package persona.feature.settings

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.api.*
import persona.core.designsystem.*
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

@Composable
internal fun DiagnosticsScreen(services: SettingsServices, onBack: (() -> Unit)? = null, footer: @Composable () -> Unit = {}) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val controller = remember(services) {
        DiagnosticsState(services.connection, services.liveDiagnostics, services.diagnostics, services.pairingAttempts)
    }
    val data by controller.data.collectAsStateWithLifecycle()
    val pairing by controller.pairing.collectAsStateWithLifecycle()
    val busy by controller.busy.collectAsStateWithLifecycle()
    val message by controller.message.collectAsStateWithLifecycle()
    val summary by controller.summary.collectAsStateWithLifecycle()
    val state by services.connection.state.collectAsStateWithLifecycle()
    var network by remember { mutableStateOf(PhoneReadiness.network(context)) }
    var fingerprintExpanded by rememberSaveable { mutableStateOf(false) }
    val list = rememberLazyListState()
    val endpoints = rememberEndpointsSnapshot(services.endpoints, data.candidates, list)
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        network = PhoneReadiness.network(context)
        scope.launch { controller.refresh() }
        scope.launch { controller.refreshSummary() }
    }
    JListPage(title = "连接诊断", onBack = onBack, state = list) {
        item { ConnectionStatus(state) }
        // A failed pairing comes first: before a pairing works there is no connection to show anything about.
        pairing?.takeIf { it.failed }?.let { pairingAttempt(it) }
        if (endpoints != null) editableCurrentConnection(endpoints, data, state)
        else diagnosticGroup("当前连接", listOf(
            "当前路径" to data.currentPath?.let { path ->
                val parts = path.split(" · ", limit = 2)
                listOf(routeKind(parts.first())).plus(parts.drop(1)).joinToString("，")
            },
            "最后收包" to localTime(data.lastReceivedAt), "最近断线原因" to data.lastDisconnectReason,
        ))
        if (data.legacyHub) item { JNotice("hub 版本太旧，不能公网直连。", status = Status.NeedsYou) }
        if (endpoints != null) editableCandidates(endpoints, scope)
        else {
            item { JSectionHeader("候选地址", count = data.candidates.size) }
            if (data.candidates.isEmpty()) item { JEmptyState("等待探测结果", "运行连接尚无候选探测结果。") }
            data.candidates.forEach { candidate ->
                diagnosticGroup(routeKind(candidate.kind), listOf(
                    "地址" to candidate.url, "最近探测结果" to candidate.result.takeUnless { it.isBlank() },
                    "探测耗时" to candidate.probeMeasurement(), "探测时间" to localTime(candidate.checkedAt),
                ))
            }
        }
        diagnosticGroup("这台手机", listOf("当前网络" to network,
            "证书公钥指纹" to data.fingerprint?.takeIf(String::isNotBlank)?.let { if (fingerprintExpanded) it else it.take(16) }))
        if (!data.fingerprint.isNullOrBlank()) item {
            Action(if (fingerprintExpanded) "收起指纹" else "查看完整指纹") { fingerprintExpanded = !fingerprintExpanded }
        }
        diagnosticGroup("协商心跳", listOf(
            "当前间隔" to data.heartbeatSeconds?.let { "$it 秒" },
            "前台间隔" to data.foregroundHeartbeatSeconds?.let { "$it 秒" },
            "后台间隔" to data.backgroundHeartbeatSeconds?.let { "$it 秒" },
            "等待 pong 超时" to data.pongTimeoutSeconds?.let { "$it 秒" },
        ), note = "以下是 hub 确认的配置，实际收包频率未实测。")
        item {
            JSectionHeader("连接记录")
            SettingsCaption(summary?.let { "hub 记录：过去 24 小时断开 ${it.disconnects} 次，最长离线 ${String.format(Locale.ROOT, "%.0f", it.longestGapSeconds)} 秒。" }
                ?: "尚未取得 hub 的断线汇总")
            if (summary != null) SettingsCaption("并行连接分别计数，hub 重启造成的离线时段不计入最长值。")
        }
        if (data.sessions.isEmpty()) item { JEmptyState("暂无记录", "尚无 hub 连接时段记录。") }
        data.sessions.forEach { session ->
            diagnosticGroup("连接时段", listOf("连接时间" to localTime(session.connectedAt),
                "断开时间" to (localTime(session.disconnectedAt) ?: "仍在连接"), "断线原因" to session.reason))
        }
        pairing?.takeUnless { it.failed }?.let { pairingAttempt(it) }
        diagnosticGroup("蜂窝网络测试", listOf("测试结果" to context.getSharedPreferences("settings-readiness", 0).getString("cellular-test", null)))
        item {
            Column(Modifier.padding(vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                SettingsCaption("家宽地址变化后，在外面可能找不回 hub。建议给 hub 配一个 DDNS 名字；没有域名时，可以回家连内网重试。")
                message?.let { JNotice(it, status = Status.NeedsYou) }
                JButton("重新探测", {
                    network = PhoneReadiness.network(context)
                    scope.launch { controller.refresh(reprobe = true) }
                    scope.launch { controller.refreshSummary() }
                }, Modifier.fillMaxWidth(), loading = busy, loadingText = "正在读取…")
                footer()
            }
        }
    }
    endpoints?.let { EndpointOverlays(it) }
}

/** Wire times are ISO-8601 UTC; show the phone's local time, with the date only when it is not today. */
internal fun localTime(value: String?, zone: ZoneId = ZoneId.systemDefault(), today: LocalDate = LocalDate.now(zone)): String? {
    if (value.isNullOrBlank()) return value
    val instant = runCatching { Instant.parse(value) }.getOrNull()
        ?: runCatching { OffsetDateTime.parse(value).toInstant() }.getOrNull() ?: return value
    val time = instant.atZone(zone)
    return DateTimeFormatter.ofPattern(if (time.toLocalDate() == today) "HH:mm:ss" else "MM-dd HH:mm:ss").format(time)
}

private fun LazyListScope.pairingAttempt(attempt: PairingAttempt) {
    diagnosticGroup("最近一次配对", attempt.rows().map { (label, value) -> label to if (label == "时间") localTime(value) else value })
    attempt.candidates.forEach { candidate ->
        diagnosticGroup("配对时探测 · ${routeKind(candidate.kind)}", listOf(
            "地址" to candidate.url, "探测结果" to candidate.result.takeUnless { it.isBlank() },
            "探测时间" to localTime(candidate.checkedAt),
        ))
    }
}

private fun LazyListScope.diagnosticGroup(title: String, values: List<Pair<String, String?>>, note: String? = null) {
    item { JSectionHeader(title) }
    note?.let { item { SettingsCaption(it, Modifier.padding(bottom = Tokens.Space.s)) } }
    itemsIndexed(values) { index, (label, value) ->
        JGroupItem(GroupPosition.of(index, values.size)) { DiagnosticValue(label, value) }
    }
}

@Composable
internal fun DiagnosticValue(label: String, value: String?) {
    val shown = value?.takeIf(String::isNotBlank) ?: "暂无观测数据"
    if (shown.length > 36) {
        JRow(label)
        SelectionContainer {
            Text(shown, Modifier.fillMaxWidth().padding(horizontal = Tokens.Space.m).padding(bottom = Tokens.Space.m),
                style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink2)
        }
    } else JRow(label, trailing = {
        SelectionContainer {
            Text(shown, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink2,
                modifier = Modifier.widthIn(max = 180.dp))
        }
    })
}

@Composable
internal fun ConnectionStatus(state: ConnectionState, paired: Boolean = true) {
    val status = when {
        !paired -> Status.Offline
        state == ConnectionState.Online -> Status.Unread
        state == ConnectionState.Connecting || state == ConnectionState.Updating -> Status.Working
        state == ConnectionState.WaitingForNetwork -> Status.Offline
        else -> Status.NeedsYou
    }
    Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
        JStatusDot(status)
        StatusLabel(status, text = if (paired) state.label() else "未连接")
    }
}

internal fun ConnectionState.label(): String = when (this) {
    ConnectionState.Online -> "已连接"
    ConnectionState.Connecting -> "连接中…"
    ConnectionState.Updating -> "更新中…"
    ConnectionState.WaitingForNetwork -> "等待网络"
    ConnectionState.NeedsLocalNetworkPermission -> "需要本地网络权限"
    ConnectionState.CertificateMismatch -> "hub 的证书变了"
    ConnectionState.Revoked -> "配对已失效"
}
