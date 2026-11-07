package persona.feature.machines

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File
import java.time.Instant

/**
 * 电脑 with several managers (docs/managers.md step 4): the computers under the manager they belong to, the default's
 * first (an unstamped one is its), the offline manager greyed with 最后同步于, in every style, light and dark. Written
 * to `build/managers-shots/machines-*.png`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1400dp-xhdpi")
class MachinesManagersShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val now = 1_793_600_000L
    private val roster = ManagerRoster(listOf(
        HubManager("assistant", "助手", online = true, isDefault = true, builtin = true),
        HubManager("m-3fa21c", "工作", online = true),
        HubManager("m-8b04d2", "家里", online = false, lastSeen = Instant.now().minusSeconds(3 * 3600).toString()),
        HubManager("m-c1e9f0", "测试", installing = "2026-11-05T06:07:00Z"),
    ), "assistant")
    private val lab = Machine("lab", name = "Lab", kind = "ssh", online = true, system = MachineSystem(os = "linux", name = "Ubuntu 24.04", cores = 8))
    private val mac = Machine("mac", name = "MacBook", kind = "local", online = true, manager = "m-3fa21c",
        system = MachineSystem(os = "darwin", distro = "macos", name = "macOS 26", cores = 10))
    private val nas = Machine("nas", name = "NAS", kind = "ssh", online = false, manager = "m-8b04d2", lastSeen = "2026-11-05T03:00:00Z",
        // what the hub puts on an offline manager's computers (hub-assistant_link.offline, in the user's language)
        error = "助手暂时连不上，稍后再试", system = MachineSystem(os = "linux", name = "Debian 13", cores = 4))
    private val sample = MetricSample(now - 5, cpu = 23.5, load = 1.2, memUsed = 14L shl 30, memTotal = 64L shl 30,
        netRx = 1.5 * 1024 * 1024, netTx = 240.0 * 1024, diskUsed = 210L shl 30, diskTotal = 500L shl 30)
    private val api = object : MachineMetricsApi {
        override suspend fun dashboard() = MetricsDashboard(listOf(MachineMetrics("lab", true, lab.system, sample),
            MachineMetrics("mac", true, mac.system, sample), MachineMetrics("nas", false, nas.system)), step = 10)
        override suspend fun history(machineId: String, range: MetricRange) = error("unused")
    }
    private val repository = object : SessionsRepository {
        override val machines = MutableStateFlow(listOf(lab, mac, nas))
        override val sessions = MutableStateFlow(emptyList<Session>())
        override suspend fun refresh() = Unit
    }
    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }

    private fun top(text: String) = rule.onAllNodesWithText(text)[0].fetchSemanticsNode().boundsInRoot.top

    @Test fun computersGroupByManager() {
        val list = MachinesViewModel(api, repository, null) { now }
        runBlocking { list.load(MetricRange.Hour) }
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var shown by mutableStateOf<ManagerRoster?>(roster)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalManagerRoster provides shown) {
                    Box(Modifier.testTag("shot").width(400.dp).height(1400.dp).background(PersonaTheme.palette.background)) {
                        MachinesScreen(null, list, navigator)
                    }
                }
            }
        }
        rule.waitForIdle()
        assertTrue(top("助手") < top("Lab") && top("Lab") < top("工作") && top("工作") < top("MacBook"))
        assertTrue(top("MacBook") < top("家里") && top("家里") < top("NAS"))
        assertTrue(rule.onAllNodesWithText("最后同步于 3 小时前").fetchSemanticsNodes().isNotEmpty())
        // Its manager offline: the card says so in our words, not the hub's for an unreachable assistant.
        rule.onNodeWithText("它的管理者「家里」离线，管理者回来后这台电脑会恢复在线。").assertExists()
        assertTrue(rule.onAllNodesWithText("助手暂时连不上，稍后再试").fetchSemanticsNodes().isEmpty())
        val dir = File(System.getProperty("persona.shots") ?: "build/managers-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(dir, "machines-grouped-${s.key}-${if (d) "dark" else "light"}.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        // One manager: no headers, and the hub's words as before.
        shown = ManagerRoster(listOf(roster.managers.first()), "assistant")
        rule.waitForIdle()
        assertEquals(0, rule.onAllNodesWithText("助手").fetchSemanticsNodes().size)
        rule.onNodeWithText("助手暂时连不上，稍后再试").assertExists()
    }

    /** A computer's page: Move to… with several managers, the menu listing the installed ones, and the move it sends. */
    @Test fun moveToAnotherManager() {
        val management = Management()
        val moved = management.moved
        val page = MachinesViewModel(api, repository, "mac") { now }
        val actions = MachineActionsViewModel(management, repository)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var shown by mutableStateOf<ManagerRoster?>(roster)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalManagerRoster provides shown) {
                    Box(Modifier.testTag("shot").width(400.dp).height(1400.dp).background(PersonaTheme.palette.background)) {
                        MachinesScreen("mac", page, navigator, actions)
                    }
                }
            }
        }
        rule.waitForIdle()
        val dir = File(System.getProperty("persona.shots") ?: "build/managers-shots").also { it.mkdirs() }
        fun save(name: String, node: androidx.compose.ui.test.SemanticsNodeInteraction) {
            rule.waitForIdle()
            val bitmap = node.captureToImage().asAndroidBitmap()
            File(dir, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        // The row says whose it is now.
        rule.onNode(hasText("移到…") and hasClickAction()).assertExists()
        save("machines-move-row-glass-light", rule.onNodeWithTag("shot"))
        // The menu, open while the style and the scheme change under it.
        rule.onNode(hasText("移到…") and hasClickAction()).performClick()
        rule.waitForIdle()
        rule.onNodeWithText("移到哪个管理者").assertExists()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            save("machines-move-${s.key}-${if (d) "dark" else "light"}", rule.onNode(isDialog()))
        }
        // The installed ones, the default first; this one's checked and not offered; offline marked.
        style = AppStyle.Glass; dark = false
        rule.waitForIdle()
        rule.onNodeWithText("家里 · 离线").assertExists()
        assertTrue(rule.onAllNodesWithText("测试").fetchSemanticsNodes().isEmpty())
        rule.onNodeWithText("助手").performClick()
        rule.waitForIdle()
        assertEquals(listOf("mac" to "assistant"), moved)
        // One manager: no row at all.
        shown = ManagerRoster(listOf(roster.managers.first()), "assistant")
        rule.waitForIdle()
        assertEquals(0, rule.onAllNodesWithText("移到…").fetchSemanticsNodes().size)
    }

    @Test fun onlyThisHubsOwnComputersMove() {
        assertTrue(movable(mac, null))
        // A computer only a linked member hub has, or another paired hub's.
        assertFalse(movable(mac.copy(kind = "hub"), null))
        assertFalse(movable(mac.copy(id = persona.core.contracts.GroupKeys.machine("mac", "g0000abcd")), null))
        assertTrue(movable(mac.copy(id = persona.core.contracts.GroupKeys.machine("mac", "g0000abcd")), "g0000abcd"))
    }

    /** An offline manager's computer: its page has no Reconnect row (that goes through the manager); with one manager, the row as before. */
    @Test fun anOfflineManagersComputerHasNoReconnect() {
        val page = MachinesViewModel(api, repository, "nas") { now }
        val actions = MachineActionsViewModel(Management(), repository)
        var shown by mutableStateOf<ManagerRoster?>(roster)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalManagerRoster provides shown) {
                    Box(Modifier.width(400.dp).height(1400.dp).background(PersonaTheme.palette.background)) {
                        MachinesScreen("nas", page, navigator, actions)
                    }
                }
            }
        }
        rule.waitForIdle()
        rule.onNode(hasText("移到…") and hasClickAction()).assertExists()
        assertEquals(0, rule.onAllNodesWithText("重新连接").fetchSemanticsNodes().size)
        shown = ManagerRoster(listOf(roster.managers.first()), "assistant")
        rule.waitForIdle()
        rule.onNodeWithText("重新连接").assertExists()
    }

    /** The roster is the selected group's hub's: another group's computer is not one of its managers'. */
    @Test fun anOfflineManagerIsOnlyThisGroupsComputers() {
        assertEquals("m-8b04d2", offlineManagerOf(roster, nas, null)?.id)
        assertEquals(null, offlineManagerOf(roster, nas.copy(online = true), null))
        assertEquals(null, offlineManagerOf(ManagerRoster(listOf(roster.managers.first()), "assistant"), nas, null))
        val elsewhere = nas.copy(id = persona.core.contracts.GroupKeys.machine("nas", "g0000abcd"))
        assertEquals(null, offlineManagerOf(roster, elsewhere, null))
        assertEquals("m-8b04d2", offlineManagerOf(roster, elsewhere, "g0000abcd")?.id)
    }

    /** Computer controls that only record the moves asked for. */
    private class Management : MachineManagement {
        val moved = mutableListOf<Pair<String, String>>()
        override suspend fun launchers(machineId: String) = emptyList<AgentLauncher>()
        override suspend fun launcherQuota(machineId: String, name: String, refresh: Boolean) = LauncherQuotaAnswer()
        override suspend fun reconnect(machineId: String) = Unit
        override suspend fun deploy(machineId: String) = Unit
        override suspend fun herdr(machineId: String): HerdrState = error("unused")
        override suspend fun startHerdr(machineId: String): HerdrState = error("unused")
        override suspend fun setEngine(machineId: String, engine: String) = Machine(machineId, engine = engine)
        override suspend fun claudeSettings(machineId: String) = emptyList<ClaudeSetting>()
        override suspend fun setClaudeSetting(machineId: String, key: String, value: kotlinx.serialization.json.JsonElement) = emptyList<ClaudeSetting>()
        override suspend fun codexSettings(machineId: String) = emptyList<ClaudeSetting>()
        override suspend fun setCodexSetting(machineId: String, key: String, value: kotlinx.serialization.json.JsonElement) = emptyList<ClaudeSetting>()
        override suspend fun moveToManager(machineId: String, manager: String): MachineMove {
            moved += machineId to manager
            return MachineMove(machineId, manager, "m-3fa21c")
        }
    }
}
