package persona.platform.phonetools

import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import persona.core.designsystem.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation3.runtime.NavKey
import persona.core.navigation.Navigator
import kotlinx.coroutines.launch
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable @SerialName("jarvis.platform.phonetools.PhoneToolsRoute") data object PhoneToolsRoute : NavKey

@Composable
fun PhoneToolsScreen(runtime: PhoneToolsRuntime, navigator: Navigator) {
    val enabled by runtime.settings.enabled.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    var revision by remember { mutableIntStateOf(0) }
    var error by remember { mutableStateOf<String?>(null) }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        runtime.refreshPermissions(); revision++
    }
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    DisposableEffect(lifecycle, runtime) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) { runtime.refreshPermissions(); revision++ }
        }
        lifecycle.addObserver(observer)
        onDispose { lifecycle.removeObserver(observer) }
    }
    JListPage(title = "手机数据", onBack = navigator::back) {
        item {
            Text("开启后，所选数据可交给 AI 处理。只有 App 在前台或常驻连接可用时，才会向 hub 声明工具。",
                Modifier.padding(bottom = 16.dp), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
        }
        items(PhoneSource.entries, key = { it.name }) { source ->
            val status = remember(revision, source) { runtime.permissions.status(source) }
            JGroupItem(GroupPosition.Single, Modifier.padding(bottom = 12.dp)) {
                JSwitchRow(source.title, checked = source in enabled, subtitle = source.detail, onCheckedChange = { value ->
                    scope.launch {
                        try { runtime.settings.setEnabled(source, value); error = null }
                        catch (_: Exception) { error = "开关未保存，请重试。" }
                    }
                })
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    JTag(status)
                    if (runtime.permissions.permissions(source).isNotEmpty() || source == PhoneSource.Notifications) {
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            JButton(if (status == "部分授权") "重新选择" else "授权", {
                                val permissions = runtime.permissions.permissions(source)
                                if (permissions.isNotEmpty()) launcher.launch(permissions)
                            }, kind = JButtonKind.Secondary, size = JButtonSize.Small,
                                enabled = runtime.permissions.permissions(source).isNotEmpty())
                            JButton("系统设置", {
                                try { context.startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:${context.packageName}"))) }
                                catch (_: Exception) { error = "未能打开系统设置，请手动查看 Jarvis 的权限。" }
                            }, kind = JButtonKind.Secondary, size = JButtonSize.Small)
                        }
                    }
                }
            }
        }
        item {
            Text("Android 没有系统提醒事项接口。通知列表只包含 Jarvis 当前显示的通知；本模块不提供健康数据工具。",
                style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
        }
        error?.let { item { JNotice(it, Modifier.padding(top = 12.dp), status = Status.NeedsYou) } }
    }
}

/** Mount once above the app's navigation host so any screen can confirm a pending call. */
@Composable
fun PhoneConfirmationHost(confirmations: PhoneConfirmations) {
    val pending by confirmations.pending.collectAsStateWithLifecycle()
    pending?.let { request ->
        JDialog(onDismiss = { confirmations.resolve(request.token, false) }, title = "确认手机操作",
            content = {
                Text(request.summary, Modifier.heightIn(max = 360.dp).verticalScroll(rememberScrollState()),
                    style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
            }, confirm = "允许" to { confirmations.resolve(request.token, true) },
            dismiss = "拒绝" to { confirmations.resolve(request.token, false) })
    }
}
