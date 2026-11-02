package jarvis.android.debug

import android.content.Intent
import android.net.Uri
import jarvis.android.navigation.DeveloperTools
import jarvis.android.navigation.LaunchAdapter
import jarvis.android.navigation.LaunchInput
import jarvis.android.navigation.notificationRoute
import jarvis.android.navigation.open
import jarvis.android.navigation.terminalLink
import jarvis.core.navigation.MachinesRoute
import jarvis.core.navigation.RouteInput
import jarvis.feature.knowledge.KnowledgeRoute
import jarvis.platform.phonetools.PhoneToolsRoute
import jarvis.core.contracts.api.PairingRequest
import jarvis.core.navigation.AppTab
import jarvis.core.navigation.ResolvedRoute
import jarvis.core.navigation.parseDebug
import java.net.URI

/** Pages without a public deep link, for direct captures. */
private val pages = mapOf(
    "machines" to ResolvedRoute(AppTab.Agents, MachinesRoute),
    "knowledge" to ResolvedRoute(AppTab.Jarvis, KnowledgeRoute),
    "phone" to ResolvedRoute(AppTab.Settings, PhoneToolsRoute),
    "catalog" to ResolvedRoute(AppTab.Settings, ComponentCatalogRoute),
)

class DebugLaunchAdapter : LaunchAdapter {
    override val tools = DebugStatePreview()

    @Suppress("DEPRECATION")
    override fun read(intent: Intent): LaunchInput? {
        val extras = intent.extras ?: return null
        val arguments = extras.keySet().filter { it.startsWith("debug.") }.associateWith {
            extras.get(it)?.toString().orEmpty()
        }.toMutableMap()
        intent.getStringExtra("openSession")?.let { arguments["debug.openSession"] = it }
        if (arguments.isEmpty()) return null
        val state = arguments["debug.state"]
        require(state == null || state in DebugStatePreview.states) { "未知的调试状态" }
        require(state == null || arguments["debug.mockAddress"] == null) { "状态预览使用本地演示数据，请勿同时传入假 hub 地址" }
        tools.state = state
        if (state != null && arguments.keys.none { it.startsWith("debug.open") }) {
            arguments["debug.openTab"] = if (state in setOf("load-failed", "loading")) "Inbox" else "Agents"
        }
        val address = arguments["debug.mockAddress"]
        val pairing = address?.let {
            val uri = URI(it)
            val loopback = byteArrayOf(127, 0, 0, 1).joinToString(".") { byte -> byte.toString() }
            require(uri.scheme == "http" && uri.host == loopback && uri.userInfo == null &&
                uri.rawPath in listOf("", "/") && uri.query == null && uri.fragment == null && uri.port in 1..65535)
            PairingRequest(it, "mock")
        }
        // App-only pages and links are resolved here instead of by the shared parser.
        val catalog = arguments.remove("debug.openCatalog") == "true"
        val link = arguments.remove("debug.openLink")
        val page = arguments.remove("debug.openPage")?.let { requireNotNull(pages[it]) { "Unknown debug page" } }
        // 设置 › 助手 opens one of its parts for captures: notes, memory, memory-file, memory-edit, memory-delete, machine, launcher, timezone, move.
        arguments.remove("debug.assistantSettings")?.let { jarvis.feature.assistant.settings.AssistantSettingsPreview.open = it }
        return LaunchInput(
            resolve = { router ->
                when {
                    catalog -> ResolvedRoute(AppTab.Settings, ComponentCatalogRoute)
                    page != null -> page
                    // The same order as MainActivity: terminal fallback, notification tab, then general deep links.
                    link != null -> terminalLink(link) ?: notificationRoute(Uri.parse(link))?.let { router.open(it) }
                        ?: router.parse(RouteInput.DeepLink(link))
                    else -> router.parseDebug(arguments)
                }
            },
            darkTheme = when (arguments["debug.theme"]) { "dark" -> true; "light" -> false; else -> null },
            automation = state != null || arguments["debug.automation"] == "true", pairing = pairing,
            prepare = if (state == null) null else { shell -> shell.restoreDemo(); shell.enterDemo() },
            posture = arguments["debug.posture"]?.takeIf { it == "tabletop" || it == "book" },
            style = arguments["debug.style"]?.let { key ->
                requireNotNull(jarvis.core.designsystem.AppStyle.entries.firstOrNull { it.key == key }) { "未知的界面风格" }
            },
        )
    }
}
