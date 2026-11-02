package jarvis.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.unit.dp
import androidx.core.view.WindowCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import dagger.hilt.android.AndroidEntryPoint
import jarvis.android.navigation.LaunchAdapter
import jarvis.android.navigation.LocalDeveloperTools
import jarvis.android.navigation.notificationRoute
import jarvis.android.navigation.open
import jarvis.android.navigation.terminalLink
import jarvis.android.platform.LockScreenTests
import jarvis.android.platform.PlatformPairing
import jarvis.android.platform.PlatformRoot
import jarvis.android.ui.AppServices
import jarvis.core.contracts.api.HubConnection
import jarvis.core.designsystem.GlassThrottle
import jarvis.core.designsystem.JarvisTheme
import jarvis.core.designsystem.LocalAppStyleStore
import jarvis.core.designsystem.LocalGlassLevel
import jarvis.core.navigation.*
import jarvis.feature.settings.SettingsServices
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject lateinit var settingsServices: SettingsServices
    @Inject lateinit var connection: HubConnection
    @Inject lateinit var shell: AppServices
    @Inject lateinit var platformPairing: PlatformPairing

    /** A destination from a notification, deep link or share; resolved once session metadata allows. */
    private val openNavigation = mutableStateOf<((Router) -> ResolvedRoute?)?>(null)
    /** Only a debug launch adapter overrides the system appearance. */
    private val darkTheme = mutableStateOf<Boolean?>(null)

    private val askNotifications = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}
    /** Installed only by the debug manifest; release builds have no launch adapter or developer pages. */
    private val launchAdapter: LaunchAdapter? by lazy {
        packageManager.getActivityInfo(componentName, PackageManager.GET_META_DATA).metaData
            ?.getString("jarvis.launchAdapter")
            ?.let { Class.forName(it).getDeclaredConstructor().newInstance() as LaunchAdapter }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        // A recreated activity already restored its back stacks; replaying the launch intent would navigate again.
        if (savedInstanceState == null) handle(intent)
        else launchAdapter?.read(intent)?.let { input ->
            darkTheme.value = input.darkTheme
            shell.automation = input.automation
            shell.posture.value = input.posture
            input.style?.let { (application as JarvisApp).appearance.setStyle(it) }
        }
        if (Build.VERSION.SDK_INT >= 33 && !shell.automation && !shell.demoSaved()) {
            askNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        val app = application as JarvisApp
        lifecycleScope.launch { shell.restoreDemo() }
        jarvis.android.platform.GlassFrameMonitor.attach(this) { !shell.automation }
        setContent {
            val forced = darkTheme.value
            val style by app.appearance.style.collectAsStateWithLifecycle()
            val glassPreference by app.appearance.glass.collectAsStateWithLifecycle()
            // Levels are ordered best first, so the lower of the two choices is the later one.
            val glassLevel = maxOf(app.appearance.deviceLevel(glassPreference), GlassThrottle.ceiling.value)
            AppearanceOverride(forced) { CompositionLocalProvider(
                LocalDeveloperTools provides launchAdapter?.tools,
                LocalAppStyleStore provides app.appearance,
                LocalGlassLevel provides glassLevel,
            ) {
                val dark = isSystemInDarkTheme()
                SideEffect {
                    WindowCompat.getInsetsController(window, window.decorView).apply {
                        isAppearanceLightStatusBars = !dark
                        isAppearanceLightNavigationBars = !dark
                    }
                }
                JarvisTheme(darkTheme = dark, style = style) {
                    val hub by app.hub.collectAsStateWithLifecycle()
                    val ready by app.pairingReady.collectAsStateWithLifecycle()
                    val failed by app.pairingRestoreError.collectAsStateWithLifecycle()
                    if (ready) PlatformRoot(hub = hub, openNavigation = openNavigation, services = settingsServices, shell = shell)
                    else Surface(Modifier.fillMaxSize()) {
                        Column(Modifier.fillMaxSize().safeDrawingPadding().padding(24.dp)) {
                            if (failed) {
                                Text("配对信息暂时无法读取，原数据已保留。")
                                TextButton(onClick = app::restorePairing) { Text("重试", maxLines = 1) }
                            } else CircularProgressIndicator()
                        }
                    }
                }
            } }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handle(intent)
    }

    override fun onStart() {
        super.onStart()
        (application as JarvisApp).hub.value?.kick()
    }

    private fun handle(intent: Intent?) {
        if (intent == null) return
        if (intent.getBooleanExtra(LockScreenTests.EXTRA_LOCK_TEST, false)) {
            pendingLockTest.value = true
            intent.removeExtra(LockScreenTests.EXTRA_LOCK_TEST)
        }
        launchAdapter?.read(intent)?.let { input ->
            darkTheme.value = input.darkTheme
            shell.automation = input.automation
            shell.posture.value = input.posture
            input.style?.let { (application as JarvisApp).appearance.setStyle(it) }
            openNavigation.value = input.resolve
            input.prepare?.let { prepare -> lifecycleScope.launch {
                try { prepare(shell) }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (error: Exception) { shell.feedback.failure(error) }
            } }
            input.pairing?.let { request ->
                lifecycleScope.launch {
                    try {
                        platformPairing.pair(request)
                        // Automated launches skip the first-run checklist so pages can be captured directly.
                        if (input.automation) platformPairing.completeReadiness()
                    } catch (cancelled: CancellationException) { throw cancelled }
                    catch (error: Exception) { shell.feedback.failure(error) }
                }
            }
            return
        }
        intent.getStringExtra(EXTRA_SESSION)?.let { session ->
            openNavigation.value = { it.parse(RouteInput.Notification(session)) }
        }
        val uri = intent.data ?: return
        if (uri.host == "pair") {
            pendingPairLink.value = uri.toString()
            return
        }
        val link = uri.toString()
        val notification = notificationRoute(uri)
        val terminal = terminalLink(link)
        openNavigation.value = { router ->
            terminal ?: notification?.let { router.open(it) } ?: router.parse(RouteInput.DeepLink(link))
        }
    }

    companion object {
        val pendingLockTest = mutableStateOf(false)
        const val EXTRA_SESSION = "session"
        /** A pairing link the activity was opened with, for the pairing screen to confirm. */
        val pendingPairLink = mutableStateOf<String?>(null)
    }
}

/** Debug captures pin light or dark; nested JarvisTheme calls read the same configuration. */
@Composable
private fun AppearanceOverride(dark: Boolean?, content: @Composable () -> Unit) {
    if (dark == null) return content()
    val base = LocalConfiguration.current
    val configuration = remember(base, dark) {
        Configuration(base).apply {
            uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
        }
    }
    CompositionLocalProvider(LocalConfiguration provides configuration, content = content)
}
