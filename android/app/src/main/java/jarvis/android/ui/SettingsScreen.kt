package jarvis.android.ui

import android.annotation.SuppressLint
import android.content.Intent
import android.net.Uri
import android.os.PowerManager
import android.provider.Settings
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
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
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(hub: Hub, onBack: () -> Unit) {
    val ctx = LocalContext.current
    val machines by hub.machines.collectAsState()
    val conn by hub.conn.collectAsState()
    val scope = rememberCoroutineScope()
    var confirmUnpair by remember { mutableStateOf(false) }
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
                    Text(when (conn) { ConnState.Online -> "已连接" + (hub.serverVersion?.let { " · $it" } ?: ""); ConnState.Connecting -> "正在连接"; ConnState.Offline -> "连不上" })
                },
            )
            Header("机器")
            machines.forEach { m ->
                ListItem(
                    headlineContent = { Text(m.displayName) },
                    supportingContent = {
                        Text(
                            if (m.online) listOfNotNull("在线", m.os, m.herdrVersion?.let { "herdr $it" }, m.claudeVersion?.let { "claude $it" }).joinToString(" · ") +
                                (if (m.nodeMismatch == true) "\n这台电脑上的 jarvisd 不是 hub 这次部署的，需要重新部署" else "")
                            else "离线" + (m.error?.takeIf { it.isNotBlank() }?.let { "：$it" } ?: ""),
                        )
                    },
                    trailingContent = {
                        TextButton(onClick = { scope.launch { runCatching { hub.api.reconnect(m.id) } } }) { Text("重新连接", maxLines = 1) }
                    },
                )
            }
            Header("后台通知")
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
