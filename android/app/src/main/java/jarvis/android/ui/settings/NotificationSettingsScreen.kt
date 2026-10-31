package jarvis.android.ui.settings

import android.content.Context
import android.content.Intent
import android.provider.Settings
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.NotificationsOff
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.SmsFailed
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.app.NotificationManagerCompat
import androidx.lifecycle.compose.LifecycleResumeEffect
import jarvis.android.data.Hub
import jarvis.android.service.Notifier
import jarvis.android.settings.DoneMode
import jarvis.android.settings.NotifyPrefs
import jarvis.android.settings.SettingsPrefs
import kotlinx.coroutines.launch

/**
 * 设置 › 通知 (iOS `NotificationSettingsView`): 「需要你处理时」 and 「Agent 做完时」 (不推送 / 只推长任务 / 每次都推).
 * Android has no push service: the phone's Notifier decides from these switches ([SettingsPrefs]), so a change applies
 * at once. They are kept on the hub under this device (`devices/{id}/prefs`, like iOS), only `input` / `done`,
 * so the hub's other fields stay. Sounds and lock screen stay in the system's channel settings.
 */
@Composable
fun NotificationSettingsScreen(hub: Hub, onBack: () -> Unit) {
    val ctx = LocalContext.current
    val c = kitColors
    val input by SettingsPrefs.notifyInput.collectAsState()
    val done by SettingsPrefs.doneMode.collectAsState()
    var longTurn by remember { mutableStateOf(SettingsPrefs.longTurnSeconds) }
    val scope = rememberCoroutineScope()

    var resumeTick by remember { mutableIntStateOf(0) }
    LifecycleResumeEffect(Unit) {
        resumeTick++
        onPauseOrDispose { }
    }
    val allowed = remember(resumeTick) { NotificationManagerCompat.from(ctx).areNotificationsEnabled() }

    // The hub's copy (and its long-turn threshold) may have changed on another device since the last connect.
    LaunchedEffect(Unit) {
        SettingsPrefs.sync(hub.api)
        longTurn = SettingsPrefs.longTurnSeconds
    }

    // Applies here at once (the Notifier reads SettingsPrefs); saved on the hub, or on the next connect if it is out of reach.
    fun save(input: Boolean, done: DoneMode) {
        scope.launch { SettingsPrefs.change(hub.api, input, done) }
    }

    KitPage("通知", onBack) {
        if (!allowed) {
            item(key = "off") {
                KitGroup(Modifier.padding(top = 8.dp)) {
                    row {
                        KitNoticeRow("通知没有打开", "下面的设置要在系统设置里允许 Jarvis 发通知后才生效。", icon = Icons.Default.NotificationsOff)
                    }
                    row { KitActionRow("去系统设置打开", icon = Icons.Default.Settings, onClick = { openChannel(ctx, null) }) }
                }
            }
        }
        item(key = "input") {
            KitGroup(
                Modifier.padding(top = if (allowed) 8.dp else 20.dp),
                footer = "授权和计划的通知上可以直接「允许 / 批准」或「拒绝 / 打回」。",
            ) {
                row {
                    KitSwitchRow("需要你处理时", input, onChange = { save(it, done) },
                        subtitle = "提问、计划审批、工具授权、终端等你输入", icon = Icons.Default.SmsFailed, iconTint = c.danger)
                }
            }
        }
        item(key = "done") {
            KitGroup(
                header = "Agent 做完时",
                footer = "App 在前台时不提醒（你正看着）。「长任务」指干了 ${NotifyPrefs.duration(longTurn)} 以上的那一轮，秒回的问题在哪问的就在哪看。",
            ) {
                DoneMode.entries.forEach { m -> row { KitChoiceRow(m.title, selected = m == done, onClick = { if (m != done) save(input, m) }) } }
            }
        }
        item(key = "channels") {
            KitGroup(
                header = "系统通知设置",
                footer = "声音、横幅、锁屏显示和勿扰在系统设置里按类别改。「后台连接」是保持连接的常驻通知，关掉这一类不影响连接。",
            ) {
                row { KitRow("需要你处理", onClick = { openChannel(ctx, Notifier.CH_INPUT) }) }
                row { KitRow("完成", onClick = { openChannel(ctx, Notifier.CH_DONE) }) }
                row { KitRow("后台连接", onClick = { openChannel(ctx, Notifier.CH_ONGOING) }) }
                row { KitRow("全部通知", titleColor = c.accent, onClick = { openChannel(ctx, null) }) }
            }
        }
    }
}

/** The system's page for one notification channel, or for all of the app's notifications. */
private fun openChannel(ctx: Context, channel: String?) {
    val i = if (channel != null) {
        Intent(Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_CHANNEL_ID, channel)
    } else {
        Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
    }.putExtra(Settings.EXTRA_APP_PACKAGE, ctx.packageName).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    runCatching { ctx.startActivity(i) }
}
