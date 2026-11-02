package jarvis.android.shots

import androidx.compose.runtime.mutableStateOf
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import jarvis.android.data.AgentState
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.ui.AgentsListOptions
import jarvis.android.ui.ChatScreen
import jarvis.android.ui.InboxScreen
import jarvis.android.ui.SettingsScreen
import jarvis.android.ui.shell.AssistantModel
import jarvis.android.ui.shell.ShellPage
import jarvis.android.ui.shell.ShellRoot
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Rule
import org.junit.Test

/**
 * Screenshots of the app's pages with scripts/mock_hub.py's made-up data, light and dark (docs/testing.md「安卓截图」):
 * `./scripts/android.sh shots` writes them to android/app/build/shots/. They are looked at, not compared.
 */
class Shots {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6, theme = "android:Theme.Material.Light.NoActionBar", maxPercentDifference = 100.0)

    private val hub get() = MockHub.hub

    private fun session(pred: (Session) -> Boolean) = hub.sessions.value.values.flatten().first(pred)

    private fun loaded(key: SessionKey) {
        val store = hub.chat(key)
        runBlocking { withTimeout(10_000) { store.state.first { !it.loading } } }
    }

    private fun assistant(current: String? = null): AssistantModel {
        val m = AssistantModel(hub, CoroutineScope(SupervisorJob() + Dispatchers.IO), null)
        runBlocking { m.reload() }
        if (current == "new") m.openNew() else current?.let(m::open)
        m.current?.let { loaded(SessionKey(m.machine, it)) }
        return m
    }

    private fun shell(name: String, page: ShellPage, current: String? = null, routes: List<String> = emptyList(), drawer: Float = 0f) {
        for (dark in listOf(false, true)) {
            // A page that leaves gives its chat back to the hub (ChatScreen's release): load it again for each shot.
            val model = assistant(current)
            paparazzi.snapshot(name + if (dark) "-dark" else "") {
                ShotHost(dark) {
                    ShellRoot(hub, mutableStateOf(null), mutableStateOf(null), assistant = model, options = AgentsListOptions(null),
                        startPage = page, startRoutes = routes, startDrawer = drawer)
                }
            }
        }
    }

    private fun chat(name: String, key: SessionKey, openWork: Boolean = false, style: jarvis.android.ui.AppStyle? = null) {
        for (dark in listOf(false, true)) {
            loaded(key)
            style?.let { jarvis.android.ui.StyleStore.set(it) }
            paparazzi.snapshot(name + if (dark) "-dark" else "") { ShotHost(dark) { ChatScreen(hub, key, onBack = {}, openWork = openWork) } }
            jarvis.android.ui.StyleStore.set(jarvis.android.ui.AppStyle.GLASS)
        }
    }

    @Test fun agents() = shell("agents", ShellPage.Agents)

    @Test fun assistantMain() = shell("assistant-main", ShellPage.Assistant)

    @Test fun assistantNew() = shell("assistant-new", ShellPage.Assistant, current = "new")

    @Test fun drawer() = shell("drawer", ShellPage.Assistant, drawer = 1f)

    @Test fun inbox() = shell("inbox", ShellPage.Assistant, routes = listOf("inbox"))

    @Test fun machines() = shell("machines", ShellPage.Assistant, routes = listOf("machines"))

    @Test fun machine() = shell("machine", ShellPage.Assistant, routes = listOf("machines", "machine/macbook"))

    @Test fun space() {
        val s = session { it.machineId == "macbook" && it.spaceName == "weather" }
        shell("space", ShellPage.Agents, routes = listOf("space/${s.machineId}/${s.spaceId ?: "-"}"))
    }

    @Test fun chatQuestion() = chat("chat-question", session { it.agentState == AgentState.NeedsYou && it.machineId == "macbook" }.key)

    @Test fun chatPlan() = chat("chat-plan", session { it.agentState == AgentState.NeedsYou && it.machineId == "studio" }.key)

    @Test fun chatWorking() = chat("chat-working", session { it.agentState == AgentState.Working && it.machineId == "macbook" }.key)

    @Test fun chatWork() = chat("chat-work", session { it.agentState == AgentState.Working && it.machineId == "macbook" }.key, openWork = true)

    @Test fun chatWorkCard() = chat("chat-work-clay", session { it.agentState == AgentState.Working && it.machineId == "macbook" }.key, openWork = true, style = jarvis.android.ui.AppStyle.CLAY)

    @Test fun settings() {
        for (dark in listOf(false, true)) {
            paparazzi.snapshot("settings" + if (dark) "-dark" else "") { ShotHost(dark) { SettingsScreen(hub, onBack = {}, asSheet = true) } }
        }
    }
}
