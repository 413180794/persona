package persona.android.debug

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.core.view.WindowCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.android.PersonaApp
import persona.android.ui.AppearanceOverride
import persona.core.contracts.JsonCodec
import persona.core.contracts.Machine
import persona.core.contracts.SessionKey
import persona.core.designsystem.AppStyle
import persona.core.designsystem.JDialog
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.LocalGlassLevel
import persona.core.hub.HttpAssistantManagement
import persona.feature.assistant.proactive.AssistantProactiveScreen
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import okhttp3.OkHttpClient
import okhttp3.Request
import java.net.URI

/**
 * Debug builds only: Settings › Assistant › "Proactive" on its own, against a fake hub (`scripts/mock/mock_hub.py`), for captures before
 * the page is reached from settings. Not in the release manifest or bytecode.
 *
 *     adb shell am start -n <package>/persona.android.debug.ProactiveDebugActivity --es debug.mockAddress http://<loopback address>:<port> \
 *       [--es debug.theme light|dark] [--es debug.style glass|brief|board] [--es debug.page routine:<id>|new|new:<template>|followups|runs]
 */
class ProactiveDebugActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val address = loopback(intent.getStringExtra("debug.mockAddress"))
        val forced = when (intent.getStringExtra("debug.theme")) { "dark" -> true; "light" -> false; else -> null }
        val app = application as PersonaApp
        intent.getStringExtra("debug.style")?.let { key ->
            app.appearance.setStyle(requireNotNull(AppStyle.entries.firstOrNull { it.key == key }) { "未知的界面风格" })  // i18n-ignore: debug-only developer tool
        }
        val page = intent.getStringExtra("debug.page")
        val client = OkHttpClient()
        // The fake hub takes any token; nothing here reads the app's own pairing.
        val management = HttpAssistantManagement(address, "mock", client)
        setContent {
            val style by app.appearance.style.collectAsStateWithLifecycle()
            val glass by app.appearance.glass.collectAsStateWithLifecycle()
            AppearanceOverride(forced) {
                val dark = isSystemInDarkTheme()
                SideEffect {
                    WindowCompat.getInsetsController(window, window.decorView).apply {
                        isAppearanceLightStatusBars = !dark
                        isAppearanceLightNavigationBars = !dark
                    }
                }
                CompositionLocalProvider(LocalGlassLevel provides app.appearance.deviceLevel(glass)) {
                    PersonaTheme(darkTheme = dark, style = style) {
                        var machines by remember { mutableStateOf(emptyList<Machine>()) }
                        LaunchedEffect(Unit) { machines = withContext(Dispatchers.IO) { runCatching { machines(client, address) }.getOrDefault(emptyList()) } }
                        var opened by remember { mutableStateOf<SessionKey?>(null) }
                        Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background)) {
                            AssistantProactiveScreen(onBack = ::finish, onOpenConversation = { opened = it }, management = management,
                                machines = machines, initialPage = page)
                        }
                        opened?.let { key ->
                            JDialog(onDismiss = { opened = null }, title = "会打开这段对话",  // i18n-ignore: debug-only developer tool
                                text = "接进设置后，这里会切到 Persona 打开 ${key.machineId} 上的对话 ${key.sessionId}。",  // i18n-ignore: debug-only developer tool
                                confirm = "知道了" to { opened = null }, dismiss = null)  // i18n-ignore: debug-only developer tool
                        }
                    }
                }
            }
        }
    }

    private fun machines(client: OkHttpClient, address: String): List<Machine> {
        val request = Request.Builder().url("${address.trimEnd('/')}/api/v1/machines").header("Authorization", "Bearer mock").build()
        return client.newCall(request).execute().use { response ->
            JsonCodec.parseToJsonElement(response.body?.string().orEmpty()).jsonObject.getValue("machines").jsonArray
                .map { JsonCodec.decodeFromJsonElement(Machine.serializer(), it) }
        }
    }

    /** Only a fake hub on this device's loopback (adb reverse), the same rule as the debug launch adapter. */
    private fun loopback(address: String?): String {
        val value = requireNotNull(address) { "需要 debug.mockAddress" }  // i18n-ignore: debug-only developer tool
        val uri = URI(value)
        val loopback = byteArrayOf(127, 0, 0, 1).joinToString(".") { it.toString() }
        require(uri.scheme == "http" && uri.host == loopback && uri.userInfo == null && uri.rawPath in listOf("", "/") &&
            uri.query == null && uri.fragment == null && uri.port in 1..65535) { "debug.mockAddress 只能是本机回环地址上的假 hub" }  // i18n-ignore: debug-only developer tool
        return value
    }
}
