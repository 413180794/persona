package jarvis.feature.settings

import androidx.compose.material3.Text
import androidx.compose.foundation.layout.*
import androidx.compose.ui.Modifier
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import jarvis.core.designsystem.*
import jarvis.core.contracts.api.DeviceDisconnectSummary
import jarvis.core.contracts.api.LockScreenTest
import jarvis.core.contracts.api.SettingsDiagnostics
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.util.Locale

@Composable
internal fun LockScreenTestSection(diagnostics: SettingsDiagnostics?, onCheckSettings: () -> Unit) {
    val scope = rememberCoroutineScope()
    val preferences = LocalContext.current.getSharedPreferences("settings-readiness", 0)
    var test by remember { mutableStateOf<LockScreenTest?>(null) }
    var summary by remember { mutableStateOf<DeviceDisconnectSummary?>(null) }
    var message by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var resumed by remember { mutableStateOf(false) }
    var feedback by remember { mutableStateOf<String?>(null) }
    suspend fun refresh() {
        try {
            test = diagnostics?.lockScreenTest()
            feedback = test?.id?.let { preferences.getString("lock-test-$it", null) }
            summary = diagnostics?.disconnectSummary()
            message = null
        } catch (error: CancellationException) { throw error }
        catch (_: Exception) { message = "测试记录暂时读不到，恢复连接后可重试。" }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { resumed = true; scope.launch { refresh() } }
    LifecycleEventEffect(Lifecycle.Event.ON_PAUSE) { resumed = false }
    LaunchedEffect(test?.id, test?.status, resumed) {
        while (resumed && test?.status in setOf("scheduled", "sent")) { delay(3_000); refresh() }
    }
    JCard(Modifier.padding(top = Tokens.Space.l), status = when (test?.status) {
        "received" -> if (test?.notificationShown == true) Status.Unread else Status.NeedsYou
        "scheduled", "sent" -> Status.Working
        null -> Status.Unknown
        else -> Status.NeedsYou
    }) {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            Text("锁屏测试", style = JarvisTheme.type.headline, color = JarvisTheme.palette.ink)
            Action(if (busy) "正在预约" else "开始锁屏测试", enabled = diagnostics != null && !busy && test?.status != "scheduled") {
                scope.launch {
                    busy = true
                    try {
                        checkNotNull(diagnostics).scheduleLockScreenTest()
                        refresh()
                        message = "已预约，请现在锁屏。hub 会按预约发送测试通知，回来后查看结果。"
                    } catch (error: CancellationException) { throw error }
                    catch (_: Exception) { message = "预约未确认，请先刷新记录；若仍无记录，请检查连接、hub 版本和本机配对凭据。" }
                    finally { busy = false }
                }
            }
            test?.let { record ->
                if (record.status == "sent") PendingBadge(PendingStatus.Unconfirmed)
                Text(lockTestStatus(record), style = JarvisTheme.type.callout, color = JarvisTheme.palette.ink2)
                SettingsCaption("预约时间：${localTime(record.dueAt)}")
                record.sentAt?.let { SettingsCaption("hub 尝试发送：${localTime(it)}") }
                record.receivedAt?.let { SettingsCaption("hub 收到回报：${localTime(it)}") }
                if (record.status == "received") SettingsCaption(when (record.notificationShown) {
                    true -> "App 已提交系统通知，是否响铃、亮屏仍需你确认。"
                    false -> "App 收到了测试事件，但通知未能展示。请检查通知权限和渠道。"
                    null -> "通知展示结果未知，请按实际情况确认。"
                })
                if (record.status != "scheduled") {
                    Action("响了，也亮屏了") {
                        feedback = "你已确认响铃并亮屏"
                        preferences.edit().putString("lock-test-${record.id}", feedback).apply()
                    }
                    Action("没响或没亮屏") {
                        feedback = "请检查通知权限、锁屏通知和后台设置，再测试一次。"
                        preferences.edit().putString("lock-test-${record.id}", feedback).apply()
                        onCheckSettings()
                    }
                }
            }
            if (test == null) JEmptyState("未实测", "预约锁屏测试后，在这里查看设备回报。")
            feedback?.let { SettingsCaption(it) }
            summary?.let {
                SettingsCaption("hub 记录：过去 24 小时断开 ${it.disconnects} 次，最长离线 ${String.format(Locale.ROOT, "%.0f", it.longestGapSeconds)} 秒。")
                SettingsCaption("并行连接分别计数，hub 重启造成的离线时段不计入最长值。")
            }
            message?.let { JNotice(it) }
            Action("刷新测试结果", enabled = !busy) { scope.launch { refresh() } }
        }
    }
}

internal fun lockTestStatus(test: LockScreenTest) = when (test.status) {
    "scheduled" -> "等待预约到期"
    "sent" -> "hub 已尝试发送，尚未收到设备回报，不能确认送达。"
    "received" -> "设备已回报收到测试事件"
    "unavailable" -> "到期时没有可用连接，请检查后台设置后重新测试。"
    "interrupted" -> "测试被 hub 重启中断，请重新预约。"
    else -> "测试状态未知，请刷新或更新 hub。"
}
