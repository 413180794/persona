package persona.feature.machines

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.unit.dp
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.AddComputerRoute
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File

/**
 * Machines › Add a computer (docs/assistant/onboarding.md 4.2): the form, the Mac and Linux instructions while waiting, "It's mine", and the machine
 * list with a computer that waits to be confirmed and one offline with a known error_code, in every style, light and dark.
 * Written to `build/onboarding-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1300dp-xhdpi")
class AddComputerShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/onboarding-shots").also { it.mkdirs() }

    private val machines = MutableStateFlow(emptyList<Machine>())
    private val repository = object : SessionsRepository {
        override val machines = this@AddComputerShotsTest.machines
        override val sessions = MutableStateFlow(emptyList<Session>())
        override suspend fun refresh() = Unit
    }
    private val pair = "persona://link?url=https%3A%2F%2Fhub.example.com&token=jp_example&machine=macbook&hub=example&fp=example&tls=18662"
    private val api = object : ComputerPairing {
        override val canAdd = flowOf(true)
        override suspend fun add(name: String, os: ComputerOs) = ComputerCode("macbook", name, pair, System.currentTimeMillis() + 598_000,
            command = "persona-link pair '$pair'", linuxInstall = "curl -fsSL https://example.com/install.sh | sh -s -- --node --pair '$pair'")
        override suspend fun again(machineId: String) = add(machineId, ComputerOs.Mac)
        override suspend fun machines(machineId: String) = machines.value
        override suspend fun confirm(machineId: String) {}
        override suspend fun reject(machineId: String) {}
    }
    private val opened = mutableListOf<NavKey>()
    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) { opened += key }
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
    private val metrics = object : MachineMetricsApi {
        override suspend fun dashboard() = MetricsDashboard(emptyList(), step = 10)
        override suspend fun history(machineId: String, range: MetricRange) = error("unused")
    }

    private enum class Page { Add, List }

    @Test fun light() = shots(false)

    @Config(sdk = [35], qualifiers = "zh-w400dp-h1300dp-night-xhdpi")
    @Test fun dark() = shots(true)

    private fun shots(dark: Boolean) {
        var style by mutableStateOf(AppStyle.Glass)
        var page by mutableStateOf(Page.Add)
        var run by mutableIntStateOf(0)
        val list = MachinesViewModel(metrics, repository, null) { 1_793_600_000L }
        runBlocking { list.load(MetricRange.Hour) }
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                key(run) {
                    Box(Modifier.testTag("shot").width(400.dp).height(1250.dp).background(PersonaTheme.palette.background)) {
                        when (page) {
                            Page.Add -> AddComputerPage(api, machines, null, onBack = {})
                            Page.List -> MachinesScreen(null, list, navigator, adds = true, canAdd = true)
                        }
                    }
                }
            }
        }
        val mode = if (dark) "dark" else "light"
        fun capture(name: String) {
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "add-computer-$name-${style.name.lowercase()}-$mode.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        for (s in AppStyle.entries) {
            style = s
            for (os in listOf("Mac", "Linux")) {
                machines.value = emptyList()
                page = Page.Add; run++
                rule.waitForIdle()
                rule.onNodeWithText(os).performClick()
                rule.onNode(hasSetTextAction()).performTextInput(if (os == "Mac") "小林的 MacBook" else "家里的服务器")
                if (os == "Mac") capture("form")
                rule.onNodeWithText("下一步").performClick()
                rule.waitForIdle()
                capture("waiting-${os.lowercase()}")
            }
            // It came online and waits: the question on the same screen.
            machines.value = listOf(Machine("macbook", "家里的服务器", online = true, trust = "pending", hostname = "home-server",
                system = MachineSystem(os = "linux", name = "Ubuntu 24.04")))
            rule.waitForIdle()
            rule.onNodeWithText("是我的电脑").assertExists()
            capture("joined")

            machines.value = listOf(
                Machine("macbook", "小林的 MacBook", online = true, trust = "pending", system = MachineSystem(os = "darwin", distro = "macos", name = "macOS 26")),
                Machine("server", "家里的服务器", online = false, errorCode = "link_offline", error = "link: not connected", lastSeen = "2026-11-05T08:30:00Z"),
            )
            page = Page.List; run++
            rule.waitForIdle()
            capture("list")
            rule.onNodeWithText("确认电脑").performClick()
            assertEquals(AddComputerRoute(confirm = "macbook"), opened.last())
        }
    }
}
