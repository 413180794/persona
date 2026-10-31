package jarvis.android.ui.settings

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.AttachFile
import androidx.compose.material.icons.filled.Forum
import androidx.compose.material.icons.filled.Inventory2
import androidx.compose.material.icons.filled.Language
import androidx.compose.material.icons.filled.Photo
import androidx.compose.material.icons.filled.Public
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.settings.StorageKind
import jarvis.android.settings.StorageUsage
import jarvis.android.settings.storageText
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * 设置 › 存储空间 (iOS `StorageView`): how much Jarvis keeps on this phone, by kind, one bar like the system's storage
 * page; tap a kind to clear it, or 清除全部缓存. Settings and pairing stay. Measuring and clearing run off the main thread.
 */
@Composable
fun StorageScreen(onBack: () -> Unit) {
    val ctx = LocalContext.current.applicationContext
    val c = kitColors
    var usage by remember { mutableStateOf<Map<StorageKind, Long>?>(null) }
    var clearing by remember { mutableStateOf<StorageKind?>(null) }
    var confirmAll by remember { mutableStateOf(false) }
    var confirmOne by remember { mutableStateOf<StorageKind?>(null) }
    val scope = rememberCoroutineScope()

    suspend fun measure() { usage = withContext(Dispatchers.IO) { StorageUsage.scan(ctx) } }
    LaunchedEffect(Unit) { measure() }

    val total = usage?.values?.sum() ?: 0L
    val clearableTotal = StorageKind.entries.filter { it.clearable }.sumOf { usage?.get(it) ?: 0L }
    // Kinds this build has nothing of (飞书 before the inbox lands…) stay out of the list; App 数据 always shows.
    val shown = StorageKind.entries.filter { it == StorageKind.APP_DATA || (usage?.get(it) ?: 0L) > 0 }

    fun clear(kinds: List<StorageKind>) {
        scope.launch {
            for (k in kinds) {
                clearing = k
                withContext(Dispatchers.IO) { StorageUsage.clear(ctx, k) }
            }
            clearing = null
            kitToast(ctx, "已清除")
            measure()
        }
    }

    KitPage("存储空间", onBack) {
        item(key = "total") {
            KitGroup(
                Modifier.padding(top = 8.dp),
                footer = "Jarvis 在这台手机上占用的空间。缓存都能从 hub 重新取回：清除后，打开对话、看图片时会重新下载。",
            ) {
                row {
                    Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        Row(verticalAlignment = Alignment.Bottom) {
                            Text(if (usage == null) "计算中…" else storageText(total), fontSize = 34.sp, fontWeight = FontWeight.Bold,
                                color = c.label, modifier = Modifier.weight(1f), maxLines = 1)
                            if (usage != null) {
                                Text("其中缓存 ${storageText(clearableTotal)}", fontSize = 15.sp, color = c.secondaryLabel, maxLines = 1,
                                    modifier = Modifier.padding(bottom = 6.dp))
                            }
                        }
                        StorageBar(usage, total)
                    }
                }
            }
        }
        item(key = "kinds") {
            KitGroup(Modifier.padding(top = 20.dp), separatorStart = 58.dp) {
                shown.forEach { kind ->
                    row {
                        val bytes = usage?.get(kind) ?: 0L
                        val canClear = kind.clearable && bytes > 0 && clearing == null
                        Row(
                            Modifier.fillMaxWidth().clickable(enabled = canClear) { confirmOne = kind }
                                .heightIn(min = 56.dp).padding(horizontal = 16.dp, vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                        ) {
                            Box(Modifier.size(30.dp).background(kindColor(kind), RoundedCornerShape(6.dp)), contentAlignment = Alignment.Center) {
                                Icon(kindIcon(kind), null, tint = Color.White, modifier = Modifier.size(18.dp))
                            }
                            Column(Modifier.weight(1f)) {
                                Text(kind.title, fontSize = 17.sp, color = c.label, maxLines = 1)
                                Text(kind.detail, fontSize = 12.sp, color = c.secondaryLabel, maxLines = 2, overflow = TextOverflow.Ellipsis)
                            }
                            if (clearing == kind) CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                            else Text(usage?.let { storageText(bytes) } ?: "—", fontSize = 16.sp, color = c.secondaryLabel, maxLines = 1)
                        }
                    }
                }
            }
        }
        item(key = "clear") {
            KitGroup(Modifier.padding(top = 20.dp), footer = "点某一类可以只清它。") {
                row {
                    KitActionRow(
                        if (clearing != null) "正在清除…" else "清除全部缓存",
                        onClick = { confirmAll = true },
                        destructive = true,
                        enabled = usage != null && clearing == null && clearableTotal > 0,
                    )
                }
            }
        }
    }

    if (confirmAll) {
        AlertDialog(
            onDismissRequest = { confirmAll = false },
            title = { Text("清除全部 ${storageText(clearableTotal)} 缓存？") },
            text = { Text("设置和配对信息会保留。") },
            confirmButton = {
                TextButton(onClick = { confirmAll = false; clear(StorageKind.entries.filter { it.clearable }) }) {
                    Text("清除", color = c.danger, maxLines = 1)
                }
            },
            dismissButton = { TextButton(onClick = { confirmAll = false }) { Text("取消", maxLines = 1) } },
        )
    }
    confirmOne?.let { k ->
        AlertDialog(
            onDismissRequest = { confirmOne = null },
            title = { Text("清除「${k.title}」${storageText(usage?.get(k) ?: 0L)}？") },
            confirmButton = {
                TextButton(onClick = { confirmOne = null; clear(listOf(k)) }) { Text("清除", color = c.danger, maxLines = 1) }
            },
            dismissButton = { TextButton(onClick = { confirmOne = null }) { Text("取消", maxLines = 1) } },
        )
    }
}

/** One bar, a segment per kind (at least 2 dp so a small one still shows), 1.5 dp apart. */
@Composable
private fun StorageBar(usage: Map<StorageKind, Long>?, total: Long) {
    val c = kitColors
    val shown = StorageKind.entries.filter { (usage?.get(it) ?: 0L) > 0 && total > 0 }
    val grow by animateFloatAsState(if (usage == null) 0f else 1f, label = "storageBar")
    val colors = shown.map { kindColor(it) }
    Layout(
        content = { colors.forEach { col -> Box(Modifier.background(col)) } },
        modifier = Modifier.fillMaxWidth().height(12.dp).clip(RoundedCornerShape(6.dp)).background(c.fillChip),
    ) { measurables, constraints ->
        val w = constraints.maxWidth
        val h = constraints.maxHeight
        val gap = 1.5.dp.roundToPx()
        val widths = shown.map { k -> maxOf(2.dp.roundToPx(), ((usage!![k]!!.toDouble() / total) * w * grow).toInt()) }
        val placeables = measurables.mapIndexed { i, m -> m.measure(Constraints.fixed(widths[i], h)) }
        layout(w, h) {
            var x = 0
            placeables.forEach { p -> p.placeRelative(x, 0); x += p.width + gap }
        }
    }
}

@Composable
private fun kindColor(k: StorageKind): Color {
    val dark = kitColors.dark
    fun pick(light: Long, darkV: Long) = Color(0xFF000000 or if (dark) darkV else light)
    return when (k) {
        StorageKind.AGENT_CHATS -> pick(0x007AFF, 0x0A84FF)
        StorageKind.CHAT_IMAGES -> pick(0xAF52DE, 0xBF5AF2)
        StorageKind.FEISHU_MEDIA -> pick(0x30B0C7, 0x40C8E0)
        StorageKind.PAGE_STATE -> pick(0xFF9500, 0xFF9F0A)
        StorageKind.WEB -> pick(0xFF2D55, 0xFF375F)
        StorageKind.NETWORK -> pick(0x5856D6, 0x5E5CE6)
        StorageKind.OTHER -> pick(0x8E8E93, 0x98989D)
        StorageKind.APP_DATA -> pick(0x8E8E93, 0x636366)
    }
}

private fun kindIcon(k: StorageKind): ImageVector = when (k) {
    StorageKind.AGENT_CHATS -> Icons.Default.Forum
    StorageKind.CHAT_IMAGES -> Icons.Default.Photo
    StorageKind.FEISHU_MEDIA -> Icons.Default.AttachFile
    StorageKind.PAGE_STATE -> Icons.AutoMirrored.Filled.List
    StorageKind.WEB -> Icons.Default.Public
    StorageKind.NETWORK -> Icons.Default.Language
    StorageKind.OTHER -> Icons.Default.Inventory2
    StorageKind.APP_DATA -> Icons.Default.Settings
}
