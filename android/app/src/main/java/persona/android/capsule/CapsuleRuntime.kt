package persona.android.capsule

import android.app.Application
import android.content.Intent
import android.net.Uri
import android.widget.Toast
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ProcessLifecycleOwner
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import persona.android.MainActivity
import persona.android.PersonaApp
import persona.android.ui.AppServices
import persona.core.contracts.api.ExtensionManagement
import persona.core.contracts.api.FloatingCapsuleSettings
import persona.core.designsystem.*
import persona.core.navigation.*
import persona.feature.chat.ChatWiringSource
import persona.feature.settings.ExtensionCapsuleCards
import persona.platform.capsule.*
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class CapsuleRuntime @Inject constructor(
    application: Application,
    private val services: AppServices,
    private val wiring: ChatWiringSource,
    private val extensions: ExtensionManagement,
) : FloatingCapsuleSettings {
    private val app = application as PersonaApp
    private val preferences = CapsulePreferences(app)
    override val mode = preferences.mode.map { it.key }.stateIn(app.scope, SharingStarted.Eagerly, preferences.mode.value.key)
    private val permission = MutableStateFlow(CapsulePermission.granted(app))
    override val permissionGranted = permission.asStateFlow()
    private var host: CapsuleHost? = null
    private var started = false
    override fun setMode(mode: String) {
        val chosen = CapsuleMode.entries.single { it.key == mode }
        preferences.setMode(chosen)
        if (chosen != CapsuleMode.Off && !CapsulePermission.granted(app)) requestPermission()
    }
    override fun requestPermission() { app.startActivity(CapsulePermission.intent(app)) }
    override fun refreshPermission() {
        permission.value = CapsulePermission.granted(app)
        host?.refreshPermission()
    }
    fun start() {
        if (started) return
        started = true
        app.scope.launch(Dispatchers.Main.immediate) {
            combine(services.session, preferences.mode) { session, mode -> session.takeUnless { mode == CapsuleMode.Off } }
                .distinctUntilChanged().collectLatest { session ->
                if (session == null) return@collectLatest
                coroutineScope {
                    val source = CapsuleSessions(session, wiring, this) { key, assistant ->
                        val uri = if (key == null) Uri.parse(AskPersonaLink) else NotificationRoute.conversation(key.machineId, key.sessionId, assistant).uri()
                        launchApp(uri)
                    }
                    val window = CapsuleHost(app, preferences, source,
                        extraContent = { ExtensionCapsuleCards(extensions.takeUnless { session.demo != null }, ::openExtension) },
                        theme = { content -> CapsuleEnvironment(content) })
                    host = window
                    try {
                        window.start()
                        ProcessLifecycleOwner.get().lifecycle.currentStateFlow.collect { lifecycle ->
                            window.setForeground(lifecycle.isAtLeast(Lifecycle.State.STARTED))
                            refreshPermission()
                        }
                    } finally {
                        window.close()
                        if (host === window) host = null
                    }
                }
            }
        }
    }
    private fun launchApp(uri: Uri) {
        app.startActivity(Intent(app, MainActivity::class.java).setAction(Intent.ACTION_VIEW).setData(uri)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP))
    }
    private fun openExtension(route: ExtensionUiRoute) {
        launchApp(Uri.Builder().scheme("jarvis").authority("open").appendQueryParameter("target", "extension")
            .appendQueryParameter("extension", route.extensionId).appendQueryParameter("ui", route.uiId)
            .appendQueryParameter("title", route.title).apply { route.state?.let { appendQueryParameter("state", it) } }.build())
    }
    @Composable
    private fun CapsuleEnvironment(content: @Composable () -> Unit) {
        val currentStyle by app.appearance.style.collectAsState()
        val face by app.appearance.face.collectAsState()
        val pack by app.personas.current.collectAsState()
        val feedback = remember { object : AppFeedback {
            override fun failure(error: Throwable, operation: Boolean): Boolean = notice(error.message ?: "操作失败")
            override fun notice(message: String): Boolean {
                Toast.makeText(app, message, Toast.LENGTH_LONG).show()
                return true
            }
        } }
        val links = remember { object : UriHandler {
            override fun openUri(uri: String) {
                val target = Uri.parse(uri)
                require(target.scheme == "https" && !target.host.isNullOrBlank())
                app.startActivity(Intent(app, MainActivity::class.java).putExtra(MainActivity.EXTRA_CAPSULE_URL, uri)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP))
            }
        } }
        PersonaTheme(style = currentStyle) { CompositionLocalProvider(LocalAppStyleStore provides app.appearance, LocalPersonaDesign provides face,
            LocalPersonaPack provides pack, LocalPersonaName provides (pack?.manifest?.name ?: "Persona"),
            LocalAppFeedback provides feedback, LocalUriHandler provides links, content = content) }
    }
}
