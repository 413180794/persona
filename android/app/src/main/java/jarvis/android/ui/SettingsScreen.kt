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
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * 设置. Its sub-pages (机器 and a machine's page and charts, Claude Code 设置, 设备, 通知, 存储空间) are a stack of
 * [SettingsRoute] paths in [stack], kept by the caller so it survives opening a chat from a machine's page.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    hub: Hub,
    onBack: () -> Unit,
    stack: MutableState<List<String>> = remember { mutableStateOf(emptyList()) },
    onOpenSession: (SessionKey) -> Unit = {},
) {
    val push: (SettingsRoute) -> Unit = { stack.value = stack.value + it.path }
    val top = stack.value.lastOrNull()?.let(SettingsRoute::of)
    if (top != null) {
        val pop = { stack.value = stack.value.dropLast(1) }
        BackHandler(onBack = pop)
        SettingsPage(hub, top, push, pop, onOpenSession)
        return
    }
    val ctx = LocalContext.current
    val machines by hub.machines.collectAsState()
    val conn by hub.conn.collectAsState()
    var confirmUnpair by remember { mutableStateOf(false) }
    // Measured each time 设置 shows (so after clearing in 存储空间, too).
    var storage by remember { mutableStateOf<Long?>(null) }
    LaunchedEffect(Unit) { storage = withContext(Dispatchers.IO) { StorageUsage.scan(ctx.applicationContext).values.sum() } }
    val pm = ctx.getSystemService(PowerManager::class.java)
    var unrestricted by remember { mutableStateOf(pm.isIgnoringBatteryOptimizations(ctx.packageName)) }

    Scaffold(topBar = {
        TopAppBar(
            title = { Text("设置") },
            navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
        )
    }) { pad ->
        Column(Modifier.padding(pad).fillMaxSize().verticalScroll(rememberScrollState())) {
            Header("hub")
            ListItem(
                headlineContent = { Text(hub.api.baseUrl) },
                supportingContent = {
                    Text(when (conn) { ConnState.Online -> "已连接" + (hub.serverVersion?.let { " · $it" } ?: ""); ConnState.Connecting -> "正在连接"; ConnState.Offline -> "连不上"; ConnState.Unauthorized -> "token 失效，需要重新配对" })
                },
            )
            Header("外观")
            StyleSettingsRow(onClick = { push(SettingsRoute.Style) })
            Header("电脑")
            val mismatch = machines.filter { it.online && it.nodeMismatch == true }
            NavItem(
                "机器", Icons.Default.Devices,
                "${machines.count { it.online }} / ${machines.size} 在线" +
                    (if (mismatch.isNotEmpty()) "\n${mismatch.joinToString("、") { it.displayName }} 要重新部署 jarvisd" else ""),
            ) { push(SettingsRoute.Machines) }
            NavItem("Claude Code 设置", Icons.Default.Tune, "各台电脑上的常用选项，可以一起改") { push(SettingsRoute.Claude) }
            NavItem("设备", Icons.Default.Key, "能连 hub 的手机、手表和网页") { push(SettingsRoute.Devices) }
            PhoneDataSection()
            Header("后台通知")
            NavItem("通知", Icons.Default.NotificationsActive, "需要你处理时、Agent 做完时") { push(SettingsRoute.Notifications) }
            Text(
                "这个 App 不用推送服务：它在后台和 hub 保持连接，有事就直接弹通知（通知栏里会常驻一条「已连接 hub」）。" +
                    "小米 / 红米等系统会清理后台，需要把电池策略设成「无限制」并打开「自启动」，否则锁屏一会儿就收不到。",
                style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp),
            )
            ListItem(
                headlineContent = { Text("电池优化") },
                supportingContent = { Text(if (unrestricted) "已关闭，不会被限制" else "还在限制后台，点右边关掉") },
                trailingContent = {
                    if (!unrestricted) TextButton(onClick = {
                        @SuppressLint("BatteryLife")
                        val i = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:${ctx.packageName}"))
                        runCatching { ctx.startActivity(i) }
                        unrestricted = pm.isIgnoringBatteryOptimizations(ctx.packageName)
                    }) { Text("关闭", maxLines = 1) }
                },
            )
            ListItem(
                headlineContent = { Text("自启动和省电策略") },
                supportingContent = { Text("打开系统里这个 App 的设置页：省电策略选「无限制」，打开「自启动」") },
                trailingContent = { TextButton(onClick = { openAutostart(ctx) }) { Text("去设置", maxLines = 1) } },
            )
            Header("这台手机")
            NavItem("存储空间", Icons.Default.SdStorage, "占用的空间，可以清除缓存", value = storage?.let(::storageText)) {
                push(SettingsRoute.Storage)
            }
            HorizontalDivider(Modifier.padding(vertical = 8.dp))
            ListItem(
                headlineContent = { Text("断开 hub", color = MaterialTheme.colorScheme.error) },
                supportingContent = { Text("忘掉地址和这台手机的 token，回到配对页") },
                modifier = Modifier.padding(bottom = 24.dp),
                trailingContent = { TextButton(onClick = { confirmUnpair = true }) { Text("断开", maxLines = 1) } },
            )
        }
    }
    if (confirmUnpair) {
        AlertDialog(
            onDismissRequest = { confirmUnpair = false },
            title = { Text("断开 hub？") },
            text = { Text("之后要重新扫码配对。hub 上这台手机的 token 可以在 iPhone App 的「设置 › 设备」里撤销。") },
            confirmButton = { TextButton(onClick = { confirmUnpair = false; JarvisApp.instance.unpair() }) { Text("断开", maxLines = 1) } },
            dismissButton = { TextButton(onClick = { confirmUnpair = false }) { Text("取消", maxLines = 1) } },
        )
    }
}

/** A row that opens a sub-page of 设置. */
@Composable
private fun NavItem(title: String, icon: ImageVector, detail: String, value: String? = null, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(title, maxLines = 1) },
        supportingContent = { Text(detail) },
        leadingContent = { Icon(icon, null, tint = MaterialTheme.colorScheme.primary) },
        trailingContent = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (value != null) Text(value, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
        modifier = Modifier.clickable(onClick = onClick),
    )
}

@Composable
private fun Header(text: String) {
    Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(start = 16.dp, top = 16.dp, bottom = 4.dp))
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
    val runner = jarvis.android.JarvisApp.instance.phoneTools
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
    Header("手机数据")
    Text(
        "让 Claude 用这台手机上的数据（通过 hub 调用 phone_* 工具）。默认都关着；新建日程这类会改数据的操作，执行前会先在手机上问你。" +
            "同时用 iPhone 和安卓时，最后连上 hub 的那台回答。",
        style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(horizontal = 16.dp),
    )
    jarvis.android.phone.Source.entries.forEach { src ->
        val granted = remember(tick, src) { jarvis.android.phone.PhoneTools.granted(ctx, src) }
        val on = src in enabled
        ListItem(
            headlineContent = { Text(src.title) },
            supportingContent = { Text(if (on && !granted) "没有权限，在系统设置里给 Jarvis 授权" else src.detail) },
            trailingContent = {
                androidx.compose.material3.Switch(on, { want ->
                    when {
                        !want -> runner.setEnabled(src, false)
                        granted -> runner.setEnabled(src, true)
                        else -> { asking = src; request.launch(src.permissions.toTypedArray()) }
                    }
                })
            },
        )
    }
}
