package persona.feature.assistant

import androidx.compose.runtime.Composable
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import persona.core.navigation.AssistantRoute
import persona.core.navigation.Navigator

fun EntryProviderScope<NavKey>.assistantEntries(
    navigator: Navigator,
    content: @Composable () -> Unit = { ConnectedAssistant(navigator) },
) {
    entry<AssistantRoute> { AssistantAppHost(navigator) { content() } }
}
