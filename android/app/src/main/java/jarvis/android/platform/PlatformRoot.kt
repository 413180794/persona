package jarvis.android.platform

import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.platform.LocalContext
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import jarvis.android.MainActivity
import jarvis.android.data.Hub
import jarvis.android.ui.AppServices
import jarvis.android.ui.JarvisRoot
import jarvis.core.navigation.ResolvedRoute
import jarvis.core.navigation.Router
import jarvis.feature.settings.OnboardingScreen
import jarvis.feature.settings.SettingsServices

@EntryPoint
@InstallIn(SingletonComponent::class)
interface PlatformEntryDependencies {
    fun permission(): LocalNetworkPermission
    fun platformPairing(): PlatformPairing
}

@Composable
fun PlatformRoot(
    hub: Hub?,
    openNavigation: MutableState<((Router) -> ResolvedRoute?)?>,
    services: SettingsServices,
    shell: AppServices,
) {
    val application = LocalContext.current.applicationContext
    val dependencies = remember(application) { EntryPointAccessors.fromApplication(application, PlatformEntryDependencies::class.java) }
    val pairing = dependencies.platformPairing()
    var checklist by rememberSaveable { mutableStateOf(pairing.readinessPending()) }
    val openTest = MainActivity.pendingLockTest.value
    LocalNetworkPermissionDialog(dependencies.permission(), services.connection)
    // Automated launches capture pages directly; a pairing they made must not leave the checklist in front.
    if (hub != null && (checklist && !shell.automation || openTest)) {
        OnboardingScreen(services) {
            pairing.completeReadiness()
            checklist = false
            MainActivity.pendingLockTest.value = false
        }
    } else JarvisRoot(hub, openNavigation, services, shell, onPaired = {
        // The pairing flow ends with the same checklist; showing it again on arrival would ask twice.
        pairing.completeReadiness()
        checklist = false
    })
}
