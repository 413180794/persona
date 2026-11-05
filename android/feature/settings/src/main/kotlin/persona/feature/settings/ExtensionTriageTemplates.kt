package persona.feature.settings

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import persona.core.contracts.api.ExtensionTriageTemplate
import persona.core.designsystem.*

@Composable
internal fun TriageTemplatePicker(enabled: Boolean, onApply: (String) -> Unit) {
    val templates = LocalExtensionTriageTemplates.current
    if (templates.isEmpty()) return
    var choosing by remember { mutableStateOf(false) }
    var pending by remember { mutableStateOf<ExtensionTriageTemplate?>(null) }
    JButton(stringResource(R.string.settings_ext_triage_from_template), { choosing = true }, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, enabled = enabled)
    if (choosing) JActionSheet(templates.map { template -> JAction(template.title) { pending = template; choosing = false } },
        { choosing = false }, title = stringResource(R.string.settings_ext_triage_templates))
    pending?.let { template ->
        JDialog(onDismiss = { pending = null }, title = stringResource(R.string.settings_ext_triage_replace_title),
            text = "${template.title}\n${template.extensionName}\n\n${template.text}\n\n" + stringResource(R.string.settings_ext_triage_replace_note),
            confirmEnabled = enabled, confirm = stringResource(R.string.settings_ext_triage_fill) to
 { onApply(template.text); pending = null })
    }
}
