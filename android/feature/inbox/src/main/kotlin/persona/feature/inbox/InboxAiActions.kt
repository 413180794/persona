package persona.feature.inbox

import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.tooling.preview.Preview

/**
 * What can be done with an item besides replying by hand (iOS `InboxActionBar`, the old app's detail): AI 草拟 — a
 * reply in the user's voice, edited and confirmed here — and 交给 AI, the item handed to Persona or an Agent with a
 * request ([InboxHandOffSheet]). With [primaryAction] (the mail page's 回复) they sit beside it as decision buttons.
 */
@Composable
internal fun InboxAiActions(
    onDraft: () -> Unit,
    onHandOff: () -> Unit,
    modifier: Modifier = Modifier,
    primaryAction: JAction? = null,
    draftEnabled: Boolean = true,
) {
    if (primaryAction == null) Row(
        modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
    ) {
        JChip("AI 草拟", icon = PhIcons.Sparkle, onClick = { if (draftEnabled) onDraft() })
        JChip("交给 AI", icon = PhIcons.Robot, onClick = onHandOff)
    } else DecisionButtons(primary = primaryAction, modifier = modifier,
        secondary = JAction("交给 AI", PhIcons.Robot, onClick = onHandOff),
        more = listOf(JAction("AI 草拟", PhIcons.Sparkle, enabled = draftEnabled, onClick = onDraft)))
}

@Preview(showBackground = true, widthDp = 320)
@Preview(showBackground = true, widthDp = 320, fontScale = 1.5f)
@Composable
private fun InboxAiActionsPreview() {
    PersonaTheme(dynamicColor = false) {
        InboxAiActions({}, {}, Modifier.padding(Tokens.Space.m))
    }
}
