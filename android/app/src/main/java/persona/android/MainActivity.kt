package persona.android

import android.Manifest
import android.content.ComponentName
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
import persona.android.navigation.LaunchAdapter
import persona.android.navigation.LocalDeveloperTools
import persona.android.navigation.notificationRoute
import persona.android.navigation.open
import persona.android.navigation.terminalLink
import persona.android.platform.LockScreenTests
import persona.android.platform.PlatformPairing
import persona.android.platform.PlatformRoot
import persona.android.ui.AppServices
import persona.core.contracts.api.HubConnection
import persona.core.designsystem.GlassThrottle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.LocalAppStyleStore
import persona.core.designsystem.LocalGlassLevel
import persona.core.navigation.*
import persona.feature.settings.SettingsServices
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
        // 启动组件可能是 legacy alias；alias 不继承 target 的 metadata，应查询真实 Activity。
        packageManager.getActivityInfo(ComponentName(this, MainActivity::class.java), PackageManager.GET_META_DATA).metaData
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
            input.style?.let { (application as PersonaApp).appearance.setStyle(it) }
        }
        if (Build.VERSION.SDK_INT >= 33 && !shell.automation && !shell.demoSaved()) {
            askNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        val app = application as PersonaApp
        lifecycleScope.launch { shell.restoreDemo() }
        persona.android.platform.GlassFrameMonitor.attach(this) { !shell.automation }
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
                PersonaTheme(darkTheme = dark, style = style) {
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
        (application as PersonaApp).hub.value?.kick()
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
            input.style?.let { (application as PersonaApp).appearance.setStyle(it) }
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

/** Debug captures pin light or dark; nested PersonaTheme calls read the same configuration. */
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
