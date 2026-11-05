package persona.feature.settings

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import persona.core.contracts.api.ExtensionTriageTemplate
import persona.core.designsystem.*

@Composable
internal fun TriageTemplatePicker(enabled: Boolean, onApply: (String) -> Unit) {
    val templates = LocalExtensionTriageTemplates.current
    if (templates.isEmpty()) return
    var choosing by remember { mutableStateOf(false) }
    var pending by remember { mutableStateOf<ExtensionTriageTemplate?>(null) }
    JButton("从模板填", { choosing = true }, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, enabled = enabled)
    if (choosing) JActionSheet(templates.map { template -> JAction(template.title) { pending = template; choosing = false } },
        { choosing = false }, title = "分拣规则模板")
    pending?.let { template ->
        JDialog(onDismiss = { pending = null }, title = "替换分拣规则草稿？",
            text = "${template.title}\n${template.extensionName}\n\n${template.text}\n\n只替换当前草稿，不会保存；核对后仍需点「保存」。",
            confirmEnabled = enabled, confirm = "填入草稿" to { onApply(template.text); pending = null })
    }
}
