package persona.feature.settings

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.contracts.api.*
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

@Composable
internal fun DevicesPage(
    controller: DeviceSettingsState,
    available: Boolean,
    onBack: () -> Unit,
    footer: @Composable () -> Unit = {},
) {
    val state by controller.state.collectAsState()
    val scope = rememberCoroutineScope()
    var pending by remember { mutableStateOf<PairedDevice?>(null) }
    LaunchedEffect(controller) { if (available) controller.loadDevices() }
    JListPage(title = "设备", onBack = onBack) {
        if (!available) item { JEmptyState("设备管理暂不可用", "请检查配对和 hub 版本。") }
        else {
            if (state.devices == null && state.busy) item { JLoading(text = "正在读取设备…") }
            if (state.devices?.devices?.isEmpty() == true) item { JEmptyState("暂无已配对设备", "配对后可在这里查看和撤销设备。") }
            items(state.devices?.devices.orEmpty(), key = { it.id }) { device ->
                val current = device.id == state.devices?.currentId
                JGroupItem(GroupPosition.Single, Modifier.padding(bottom = Tokens.Space.m)) {
                    JRow(device.name.ifBlank { "未命名设备" }, leadingIcon = PhIcons.DeviceMobile,
                        value = if (current) "本机" else null,
                        subtitle = "${device.platformLabel(current)}，${when (device.role) { "full" -> "完整权限"; "answer" -> "只能回答"; else -> "角色未提供" }}")
                    SettingsCaption("最近使用：${lastUsed(device.lastSeen)}", Modifier.padding(horizontal = Tokens.Space.m))
                    JButton(if (current) "撤销本机" else "撤销", { pending = device }, Modifier.padding(Tokens.Space.m),
                        kind = JButtonKind.Secondary, size = JButtonSize.Small, enabled = !state.busy && !state.needsCleanup)
                }
            }
            item { Action(if (state.busy) "正在处理" else "刷新设备", enabled = !state.busy && !state.needsCleanup) { scope.launch { controller.loadDevices() } } }
        }
        item { footer() }
    }
    pending?.let { device ->
        val current = device.id == state.devices?.currentId
        JDialog(onDismiss = { pending = null }, title = if (current) "撤销本机？" else "撤销这台设备？",
            text = if (current) "本机将退出配对，并清除这个 hub 的本地数据。再次使用需要重新配对。" else "撤销后，这台设备会断开连接，再次使用需要重新配对。",
            destructive = true, confirmEnabled = !state.busy,
            confirm = "撤销" to { pending = null; scope.launch { controller.revoke(device) }; Unit })
    }
}

@Composable
internal fun NotificationPreferences(controller: DeviceSettingsState, available: Boolean): LazyListScope.() -> Unit {
    val state by controller.state.collectAsState()
    val scope = rememberCoroutineScope()
    LaunchedEffect(controller) { if (available) controller.loadPreferences() }
    val prefs = state.preferences?.takeIf { available }
    val enabled = !state.busy && !state.needsCleanup
    return {
        item { JSectionHeader("这台设备的通知偏好") }
        if (!available) item { JEmptyState("通知偏好暂不可用", "请检查配对和 hub 版本。") }
        if (prefs != null) {
            item {
                JGroupItem(GroupPosition.Single) {
                    JSwitchRow("需要你处理时", prefs.input, { scope.launch { controller.savePreferences(prefs.copy(input = it)) } },
                        leadingIcon = PhIcons.BellSimple, enabled = enabled)
                }
            }
            item { JSectionHeader("Agent 做完时") }
            DoneNotification.entries.forEachIndexed { index, mode ->
                item {
                    JGroupItem(GroupPosition.of(index, DoneNotification.entries.size)) {
                        JRow(when (mode) { DoneNotification.Off -> "不通知"; DoneNotification.Long -> "只通知长任务"; DoneNotification.Always -> "每次都通知" },
                            enabled = enabled, chevron = false,
                            onClick = { scope.launch { controller.savePreferences(prefs.copy(done = mode)) } },
                            trailing = { JRadio(selected = prefs.done == mode, onClick = null, enabled = enabled) })
                    }
                }
            }
            item {
                Column(Modifier.padding(vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    SettingsCaption("在电脑上看过的任务不通知。")
                    prefs.longTurnSeconds?.let { SettingsCaption("hub 配置的长任务门槛：$it 秒") }
                    prefs.doneDelaySeconds?.let { SettingsCaption("hub 配置的完成通知等待时间：$it 秒（实际送达时间未实测）") }
                }
            }
            item {
                JGroupItem(GroupPosition.of(0, 2)) {
                    JSwitchRow("锁屏实时状态", prefs.liveActivity, { scope.launch { controller.savePreferences(prefs.copy(liveActivity = it)) } }, enabled = enabled)
                    SettingsCaption("默认开启。Android 16 及以上可在锁屏和状态栏胶囊显示状态计数；关闭或系统不支持时保留普通常驻通知。",
                        Modifier.padding(horizontal = Tokens.Space.m).padding(bottom = Tokens.Space.m))
                }
            }
            item {
                JGroupItem(GroupPosition.of(1, 2)) {
                    JSwitchRow("锁屏显示详情", prefs.lockDetails, { scope.launch { controller.savePreferences(prefs.copy(lockDetails = it)) } }, enabled = enabled)
                    SettingsCaption("关闭后，锁屏隐藏通知详情。", Modifier.padding(horizontal = Tokens.Space.m).padding(bottom = Tokens.Space.m))
                }
            }
        }
        if (available && state.preferences == null) item {
            Action(if (state.busy) "正在读取" else "重试读取", enabled = !state.busy) { scope.launch { controller.loadPreferences() } }
        }
        if (state.busy && state.preferences != null) item { SettingsCaption("正在保存…") }
    }
}

@Composable
internal fun AboutSettingsPage() {
    val context = LocalContext.current
    val info = remember(context) { context.packageManager.getPackageInfo(context.packageName, 0) }
    JGroupItem(GroupPosition.of(0, 2)) { JRow("版本", value = info.versionName ?: "未提供") }
    JGroupItem(GroupPosition.of(1, 2)) { JRow("构建号", trailing = { Text("${info.longVersionCode}", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3) }) }
}

private fun PairedDevice.platformLabel(current: Boolean): String = when {
    current -> "Android"
    platform == "android" || kind == "android" -> "Android"
    platform == "ios" || platform == "iphone" || kind == "iphone" -> "iPhone"
    platform == "watchos" || kind == "watch" -> "手表"
    kind == "phone" -> "手机（平台未提供）"
    else -> "平台未提供"
}

private fun lastUsed(value: String?): String {
    if (value.isNullOrBlank()) return "尚无记录"
    return runCatching { DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm").withZone(ZoneId.systemDefault()).format(Instant.parse(value)) }
        .getOrDefault("时间未提供")
}
