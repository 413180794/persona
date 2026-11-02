package jarvis.android.ui

import android.annotation.SuppressLint
import android.content.Intent
import android.net.Uri
import android.os.PowerManager
import android.provider.Settings
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Devices
import androidx.compose.material.icons.filled.Key
import androidx.compose.material.icons.filled.NotificationsActive
import androidx.compose.material.icons.filled.SdStorage
import androidx.compose.material.icons.filled.Tune
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.ui.Alignment
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
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
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import jarvis.android.JarvisApp
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.data.SessionKey
import jarvis.android.settings.StorageUsage
import jarvis.android.settings.storageText
import jarvis.android.ui.settings.SettingsPage
import jarvis.android.ui.settings.SettingsRoute
import jarvis.android.ui.settings.KitActionRow
import jarvis.android.ui.settings.KitGroup
import jarvis.android.ui.settings.KitRow
import jarvis.android.ui.settings.KitSwitchRow
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PageHeader
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.navigationBarsPadding
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * 设置 (iOS SettingsView), a full-screen sheet over the shell with ✕: groups of rows with colored tiles like iOS's
 * Settings. Its sub-pages (机器 and a machine's page and charts, Claude Code 设置, 设备, 通知, 存储空间, 界面风格) are a
 * stack of [SettingsRoute] paths in [stack], kept by the caller so it survives opening a chat from a machine's page.
 */
@Composable
fun SettingsScreen(
    hub: Hub,
    onBack: () -> Unit,
    stack: MutableState<List<String>> = remember { mutableStateOf(emptyList()) },
    onOpenSession: (SessionKey) -> Unit = {},
    /** Shown as the shell's sheet: ✕ at the top right instead of a back button. */
    asSheet: Boolean = false,
    /** A wide window: only the menu (the left pane); a page it opens replaces the right pane's stack, which the shell draws. */
    menuOnly: Boolean = false,
) {
    val push: (SettingsRoute) -> Unit =
        if (menuOnly) ({ stack.value = listOf(it.path) }) else ({ stack.value = stack.value + it.path })
    val top = stack.value.lastOrNull()?.let(SettingsRoute::of)
    if (top != null && !menuOnly) {
        val pop = { stack.value = stack.value.dropLast(1) }
        BackHandler(onBack = pop)
        SettingsPage(hub, top, push, pop, onOpenSession)
        return
    }
    val ctx = LocalContext.current
    val machines by hub.machines.collectAsState()
    val conn by hub.conn.collectAsState()
    val style by StyleStore.style.collectAsState()
    var confirmUnpair by remember { mutableStateOf(false) }
    // Measured each time 设置 shows (so after clearing in 存储空间, too).
    var storage by remember { mutableStateOf<Long?>(null) }
    LaunchedEffect(Unit) { storage = withContext(Dispatchers.IO) { runCatching { StorageUsage.scan(ctx.applicationContext).values.sum() }.getOrNull() } }
    val pm = ctx.getSystemService(PowerManager::class.java)
    var unrestricted by remember { mutableStateOf(runCatching { pm?.isIgnoringBatteryOptimizations(ctx.packageName) == true }.getOrDefault(false)) }
    val hue = JTheme.hue
    val c = JTheme.colors

    Column(Modifier.fillMaxSize().background(c.page)) {
        if (asSheet) PageHeader("设置", onBack = null, actions = { CircleButton(JIcons.close, "关闭", onBack) })
        else PageHeader("设置", onBack)
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(bottom = JSpace.xxl)) {
            KitGroup(header = "hub") {
                row {
                    val state = when (conn) {
                        ConnState.Online -> "已连接" + (hub.serverVersion?.let { " · $it" } ?: "")
                        ConnState.Connecting -> "正在连接"
                        ConnState.Offline -> "连不上"
                        ConnState.Unauthorized -> "token 失效，需要重新配对"
                    }
                    KitRow(hub.api.baseUrl, onClick = null, icon = JIcons.server, iconColor = if (conn == ConnState.Online) c.positive else c.secondaryText, subtitle = state)
                }
            }
            KitGroup(header = "外观") {
                row { KitRow("界面风格", onClick = { push(SettingsRoute.Style) }, icon = JIcons.style, iconColor = hue.purple, value = style.title) }
            }
            KitGroup(header = "AI") {
                row { KitRow("模型配置", onClick = { push(SettingsRoute.Models) }, icon = JIcons.model, iconColor = c.ai, subtitle = "对话、分拣、翻译和各项 AI 功能统一选择模型") }
            }
            val mismatch = machines.filter { it.online && it.nodeMismatch == true }
            KitGroup(
                header = "电脑",
                footer = if (mismatch.isNotEmpty()) "${mismatch.joinToString("、") { it.displayName }} 要重新部署 jarvisd" else null,
            ) {
                row { KitRow("机器", onClick = { push(SettingsRoute.Machines) }, icon = JIcons.machines, iconColor = hue.blue, value = "${machines.count { it.online }} / ${machines.size} 在线") }
                row { KitRow("Claude Code 设置", onClick = { push(SettingsRoute.Claude) }, icon = JIcons.options, iconColor = hue.orange, subtitle = "各台电脑上的常用选项，可以一起改") }
                row { KitRow("设备", onClick = { push(SettingsRoute.Devices) }, icon = JIcons.key, iconColor = hue.green, subtitle = "能连 hub 的手机、手表和网页") }
            }
            PhoneDataSection()
            KitGroup(
                header = "后台通知",
                footer = "这个 App 不用推送服务：它在后台和 hub 保持连接，有事就直接弹通知（通知栏里会常驻一条「已连接 hub」）。" +
                    "小米 / 红米等系统会清理后台，需要把电池策略设成「无限制」并打开「自启动」，否则锁屏一会儿就收不到。",
            ) {
                row { KitRow("通知", onClick = { push(SettingsRoute.Notifications) }, icon = JIcons.notifications, iconColor = c.danger, subtitle = "需要你处理时、Agent 做完时") }
                row {
                    KitRow(
                        "电池优化", icon = JIcons.battery, iconColor = c.positive,
                        subtitle = if (unrestricted) "已关闭，不会被限制" else "还在限制后台，点这里关掉",
                        value = if (unrestricted) "无限制" else null,
                        onClick = if (unrestricted) null else ({
                            @SuppressLint("BatteryLife")
                            val i = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:${ctx.packageName}"))
                            runCatching { ctx.startActivity(i) }
                            unrestricted = pm?.isIgnoringBatteryOptimizations(ctx.packageName) == true
                        }),
                    )
                }
                row { KitRow("自启动和省电策略", onClick = { openAutostart(ctx) }, icon = JIcons.autostart, iconColor = hue.blue, subtitle = "省电策略选「无限制」，打开「自启动」") }
            }
            KitGroup(header = "这台手机") {
                row { KitRow("存储空间", onClick = { push(SettingsRoute.Storage) }, icon = JIcons.storage, iconColor = c.secondaryText, value = storage?.let(::storageText)) }
            }
            KitGroup(footer = "忘掉地址和这台手机的 token，回到配对页") {
                row { KitActionRow("断开 hub", onClick = { confirmUnpair = true }, destructive = true) }
            }
        }
    }
    if (confirmUnpair) {
        AlertDialog(
            onDismissRequest = { confirmUnpair = false },
            title = { Text("断开 hub？") },
            text = { Text("之后要重新扫码配对。hub 上这台手机的 token 可以在 iPhone App 的「设置 › 设备」里撤销。") },
            confirmButton = { TextButton(onClick = { confirmUnpair = false; JarvisApp.instance.unpair() }) { Text("断开", maxLines = 1, color = c.danger) } },
            dismissButton = { TextButton(onClick = { confirmUnpair = false }) { Text("取消", maxLines = 1) } },
        )
    }
}

/** MIUI / HyperOS keep autostart in their security app; fall back to the app's system settings page. */
private fun openAutostart(ctx: android.content.Context) {
    val tries = listOf(
        Intent().setClassName("com.miui.securitycenter", "com.miui.permcenter.autostart.AutoStartManagementActivity"),
        Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:${ctx.packageName}")),
    )
    for (i in tries) {
        if (runCatching { ctx.startActivity(i.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }.isSuccess) return
    }
}

/**
 * 手机数据: which of the phone's data Claude may use through the phone_* tools (docs/phone-tools.md). Off until
 * turned on; turning one on asks for its Android permission. Only the ones on are declared to the hub.
 */
@Composable
private fun PhoneDataSection() {
    val runner = runCatching { jarvis.android.JarvisApp.instance.phoneTools }.getOrNull() ?: return
    val enabled by runner.enabled.collectAsState()
    val ctx = LocalContext.current
    var asking by remember { mutableStateOf<jarvis.android.phone.Source?>(null) }
    var tick by remember { mutableStateOf(0) } // re-read permissions after a request
    val request = androidx.activity.compose.rememberLauncherForActivityResult(
        androidx.activity.result.contract.ActivityResultContracts.RequestMultiplePermissions(),
    ) { result ->
        val s = asking ?: return@rememberLauncherForActivityResult
        asking = null; tick++
        if (result.values.any { it }) runner.setEnabled(s, true)
    }
    val granted = remember(tick) { jarvis.android.phone.Source.entries.associateWith { jarvis.android.phone.PhoneTools.granted(ctx, it) } }
    PhoneDataGroup(
        enabled, granted,
        onToggle = { src, want ->
            when {
                !want -> runner.setEnabled(src, false)
                granted[src] == true -> runner.setEnabled(src, true)
                else -> { asking = src; request.launch(src.permissions.toTypedArray()) }
            }
        },
        health = { HealthRow(jarvis.android.phone.Source.Health in enabled, runner) },
    )
}

/** The 手机数据 group from what is on and granted; [health] draws the 健康 row (the screenshots pass each of its states). */
@Composable
internal fun PhoneDataGroup(
    enabled: Set<jarvis.android.phone.Source>,
    granted: Map<jarvis.android.phone.Source, Boolean>,
    onToggle: (jarvis.android.phone.Source, Boolean) -> Unit,
    health: @Composable () -> Unit,
) {
    KitGroup(
        header = "手机数据",
        footer = "让 Claude 用这台手机上的数据（通过 hub 调用 phone_* 工具）。默认都关着；新建日程这类会改数据的操作，执行前会先在手机上问你。" +
            "同时用 iPhone 和安卓时，最后连上 hub 的那台回答。",
    ) {
        jarvis.android.phone.Source.entries.forEach { src ->
            if (src == jarvis.android.phone.Source.Health) { row(health); return@forEach }
            row {
                val on = src in enabled
                KitSwitchRow(
                    src.title, on, { onToggle(src, it) },
                    subtitle = if (on && granted[src] != true) "没有权限，在系统设置里给 Jarvis 授权" else src.detail,
                    icon = JIcons.phone, iconTint = JTheme.hue.green,
                )
            }
        }
    }
}

/** What the 健康 row shows: Health Connect missing / out of date, or the switch with how much was granted. */
sealed interface HealthRowState {
    data class Unavailable(val reason: String) : HealthRowState
    data class Available(val on: Boolean, val some: Boolean, val all: Boolean) : HealthRowState
}

/**
 * 健康: Health Connect grants its own read permissions (its dialog, not Android's), and on Android 13 and older it is
 * an app that may be missing or out of date — then the row says so and offers to get it instead of a switch that
 * can't work. Permissions are re-read whenever the page comes back (the user may revoke them in Health Connect).
 */
@Composable
private fun HealthRow(on: Boolean, runner: jarvis.android.phone.PhoneToolRunner) {
    val ctx = LocalContext.current
    val health = jarvis.android.phone.Health
    var tick by remember { mutableStateOf(0) }
    val lifecycle = androidx.lifecycle.compose.LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(lifecycle) { lifecycle.currentStateFlow.collect { if (it == androidx.lifecycle.Lifecycle.State.RESUMED) tick++ } }
    val unavailable = remember(tick) { health.unavailableReason(ctx) }
    var granted by remember { mutableStateOf<Set<String>?>(null) }
    LaunchedEffect(tick) { granted = health.granted(ctx) }
    val request = androidx.activity.compose.rememberLauncherForActivityResult(
        androidx.health.connect.client.PermissionController.createRequestPermissionResultContract(),
    ) { result ->
        tick++
        if (result.isNotEmpty()) runner.setEnabled(jarvis.android.phone.Source.Health, true)
    }
    // Until Health Connect has answered, take it as granted: no warning flashes on the row.
    val all = granted?.containsAll(health.permissions) ?: true
    val state = unavailable?.let { HealthRowState.Unavailable(it) }
        ?: HealthRowState.Available(on, some = granted?.any { it in health.permissions } ?: true, all = all)
    HealthRowView(
        state,
        onGet = { runCatching { ctx.startActivity(health.installIntent().addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) } },
        onToggle = { want ->
            when {
                !want -> runner.setEnabled(jarvis.android.phone.Source.Health, false)
                all -> runner.setEnabled(jarvis.android.phone.Source.Health, true)
                else -> request.launch(health.permissions)
            }
        },
    )
}

@Composable
internal fun HealthRowView(state: HealthRowState, onGet: () -> Unit, onToggle: (Boolean) -> Unit) {
    val src = jarvis.android.phone.Source.Health
    when (state) {
        is HealthRowState.Unavailable -> KitRow(
            src.title, onClick = onGet, icon = JIcons.health, iconColor = JTheme.colors.danger,
            subtitle = "${state.reason}，装好后再打开（安卓 14 起系统自带）", value = "获取",
        )
        is HealthRowState.Available -> KitSwitchRow(
            src.title, state.on, onToggle,
            subtitle = when {
                state.on && !state.some -> "Health Connect 里没有允许读取，关掉再打开重新授权"
                state.on && !state.all -> "只允许了部分数据，缺的那些查不到"
                else -> src.detail
            },
            icon = JIcons.health, iconTint = JTheme.colors.danger,
        )
    }
}
