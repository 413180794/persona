package jarvis.android.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Computer
import androidx.compose.material.icons.filled.Key
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Smartphone
import androidx.compose.material.icons.filled.Watch
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.JarvisApp
import jarvis.android.data.Hub
import jarvis.android.data.HubException
import jarvis.android.data.parseTime
import jarvis.android.settings.DeviceToken
import jarvis.android.settings.deviceTokens
import jarvis.android.settings.relativeTime
import jarvis.android.settings.revokeDeviceToken
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** 设置 › 设备 (iOS `DevicesView`): every device with its own credential, revocable one by one. */
@Composable
fun DevicesScreen(hub: Hub, onBack: () -> Unit) {
    val c = kitColors
    val ctx = LocalContext.current
    var tokens by remember { mutableStateOf<List<DeviceToken>>(emptyList()) }
    var current by remember { mutableStateOf<String?>(null) }
    var loaded by remember { mutableStateOf(false) }
    var unsupported by remember { mutableStateOf<String?>(null) }
    var revoking by remember { mutableStateOf<DeviceToken?>(null) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        try {
            val r = hub.api.deviceTokens()
            tokens = r.tokens
            current = r.current.ifEmpty { null }
            unsupported = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: HubException) {
            when (e.status) {
                404 -> unsupported = "这个 hub 版本还不支持设备凭据。"
                503 -> unsupported = "hub 没有数据库，不支持设备凭据。"
                else -> kitToast(ctx, e.kitMessage)
            }
        } catch (e: Exception) {
            kitToast(ctx, e.kitMessage)
        }
        loaded = true
    }
    LaunchedEffect(Unit) { load() }

    fun revoke(t: DeviceToken) {
        scope.launch {
            try {
                hub.api.revokeDeviceToken(t.id)
                if (t.id == current) {
                    JarvisApp.instance.unpair() // this phone's own credential: back to pairing
                    return@launch
                }
                kitToast(ctx, "已撤销「${t.title}」")
                load()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                kitToast(ctx, e.kitMessage)
            }
        }
    }

    KitPage("设备", onBack, actions = {
        IconButton(onClick = { scope.launch { load() } }) { Icon(Icons.Default.Refresh, "刷新") }
    }) {
        val active = tokens.filter { !it.revoked }
        val revoked = tokens.filter { it.revoked }
        when {
            !loaded -> item { Box(Modifier.fillMaxWidth().padding(top = 80.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
            tokens.isEmpty() -> item {
                KitEmpty(
                    Icons.Default.Key, "还没有设备凭据",
                    unsupported ?: if (current == null) "hub 更新后，这台手机会自动换成自己的凭据。" else null,
                    Modifier.padding(top = 60.dp),
                )
            }
            else -> {
                item(key = "active") {
                    KitGroup(
                        Modifier.padding(top = 8.dp),
                        footer = "每台设备有自己的凭据，丢了哪台就撤销哪台，其他设备不受影响。手表只能看和回答，不能改设置、开关 Agent。" +
                            if (current == null) "这台手机还在用配对 token。" else "",
                        separatorStart = 52.dp,
                    ) {
                        active.forEach { t -> row { TokenRow(t, t.id == current, onRevoke = { revoking = t }) } }
                    }
                }
                if (revoked.isNotEmpty()) {
                    item(key = "revoked") {
                        KitGroup(header = "已撤销", separatorStart = 52.dp) {
                            revoked.forEach { t -> row { TokenRow(t, false, onRevoke = null) } }
                        }
                    }
                }
            }
        }
    }

    revoking?.let { t ->
        val self = t.id == current
        AlertDialog(
            onDismissRequest = { revoking = null },
            title = { Text("撤销「${t.title}」？") },
            text = { Text(if (self) "这台手机会立刻断开，要重新扫码配对。" else "那台设备会立刻断开，要重新配对才能用。") },
            confirmButton = {
                TextButton(onClick = { revoking = null; revoke(t) }) {
                    Text(if (self) "撤销并退出配对" else "撤销", color = c.danger, maxLines = 1)
                }
            },
            dismissButton = { TextButton(onClick = { revoking = null }) { Text("取消", maxLines = 1) } },
        )
    }
}

@Composable
private fun TokenRow(t: DeviceToken, isCurrent: Boolean, onRevoke: (() -> Unit)?) {
    val c = kitColors
    val dim = t.revoked
    Row(
        Modifier.fillMaxWidth().heightIn(min = 56.dp).padding(start = 16.dp, end = 4.dp, top = 8.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(
            when (t.kind) { "watch" -> Icons.Default.Watch; "phone" -> Icons.Default.Smartphone; else -> Icons.Default.Computer },
            null, tint = if (dim) c.secondaryLabel else c.accent, modifier = Modifier.size(24.dp),
        )
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(t.title, fontSize = 17.sp, color = if (dim) c.secondaryLabel else c.label, maxLines = 1,
                    overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                if (isCurrent) KitChip("本机", color = c.accent)
                if (t.role == "answer") KitChip("只能回答")
            }
            Text(detail(t), fontSize = 12.sp, color = c.secondaryLabel, maxLines = 1)
        }
        if (onRevoke != null) {
            TextButton(onClick = onRevoke) { Text("撤销", color = c.danger, maxLines = 1) }
        }
    }
}

private val added = DateTimeFormatter.ofPattern("yyyy年M月d日 HH:mm")

private fun detail(t: DeviceToken): String {
    parseTime(t.revokedAt)?.let { return "撤销于 " + relativeTime(it) }
    parseTime(t.lastSeen)?.let { return "最近使用 " + relativeTime(it) }
    parseTime(t.createdAt)?.let { return "添加于 " + added.format(it.atZone(ZoneId.systemDefault())) }
    return ""
}
