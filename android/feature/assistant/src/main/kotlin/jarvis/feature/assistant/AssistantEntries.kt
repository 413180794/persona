package jarvis.feature.assistant

import androidx.compose.runtime.Composable
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import jarvis.core.navigation.AssistantRoute
import jarvis.core.navigation.Navigator

fun EntryProviderScope<NavKey>.assistantEntries(
    navigator: Navigator,
    content: @Composable () -> Unit = { ConnectedAssistant(navigator) },
) {
    entry<AssistantRoute> { AssistantAppHost(navigator) { content() } }
}
