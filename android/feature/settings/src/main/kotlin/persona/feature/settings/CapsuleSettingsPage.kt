package persona.feature.settings

import androidx.compose.runtime.*
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import persona.core.contracts.api.FloatingCapsuleSettings
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

internal object CapsuleSettingsText {
    val modes = listOf("off" to "关闭", "active" to "有任务时显示", "always" to "一直显示")
    fun label(mode: String) = modes.firstOrNull { it.first == mode }?.second.orEmpty()
}

@Composable
internal fun CapsuleSettingsPage(settings: FloatingCapsuleSettings?, onBack: () -> Unit) {
    val mode = settings?.mode?.collectAsState()?.value ?: "off"
    val permitted = settings?.permissionGranted?.collectAsState()?.value == true
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { settings?.refreshPermission() }
    JListPage(title = "悬浮胶囊", onBack = onBack) {
        item { JNotice("在其他 App 中显示 Persona。拖动可贴边，点开查看任务、继续对话；回到 Persona 时自动隐藏。") }
        item { JSectionHeader("显示方式") }
        CapsuleSettingsText.modes.forEachIndexed { index, (key, title) ->
            item { JGroupItem(GroupPosition.of(index, CapsuleSettingsText.modes.size)) {
                JRow(title, value = if (key == mode) "已选择" else null, enabled = settings != null,
                    onClick = { settings?.setMode(key) })
            } }
        }
        item { JSectionHeader("系统权限") }
        item { JGroupItem(GroupPosition.Single) {
            JRow("显示在其他应用上层", leadingIcon = PhIcons.SquaresFour,
                value = if (permitted) "已允许" else "尚未允许", onClick = { settings?.requestPermission() })
        } }
        if (mode != "off" && !permitted) item {
            JNotice("开启悬浮权限后胶囊才会显示。", actionLabel = "去允许", onAction = { settings?.requestPermission() })
        }
    }
}
