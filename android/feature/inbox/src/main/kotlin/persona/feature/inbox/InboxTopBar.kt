package persona.feature.inbox

import androidx.compose.ui.res.stringResource

import androidx.compose.foundation.layout.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

@Composable
internal fun InboxTopBar(
    title: @Composable () -> Unit,
    onBack: () -> Unit,
    backEnabled: Boolean = true,
    actions: @Composable RowScope.() -> Unit = {},
) {
    Row(Modifier.fillMaxWidth().statusBarsPadding().padding(horizontal = Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
        JIconButton(PhIcons.ArrowLeft, stringResource(R.string.inbox_back), onBack, enabled = backEnabled)
        Box(Modifier.weight(1f)) { title() }
        actions()
    }
}
