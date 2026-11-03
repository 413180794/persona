package persona.feature.terminal

import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import persona.core.contracts.SessionKey
import persona.core.contracts.api.TerminalTransport
import persona.core.navigation.Navigator
import persona.core.navigation.TerminalRoute

@EntryPoint
@InstallIn(SingletonComponent::class)
interface TerminalServices { fun terminal(): TerminalTransport }

fun EntryProviderScope<NavKey>.terminalEntries(navigator: Navigator, transport: TerminalTransport? = null, title: (SessionKey) -> String? = { null }) {
    entry<TerminalRoute> { route ->
        val context = LocalContext.current.applicationContext
        val services = remember(context, transport) { if (transport == null) EntryPointAccessors.fromApplication(context, TerminalServices::class.java) else null }
        val model = viewModel { TerminalViewModel(SessionKey(route.machineId, route.sessionId), transport ?: requireNotNull(services).terminal()) }
        TerminalScreen(model, navigator::back, title(model.key) ?: model.key.sessionId)
    }
}
