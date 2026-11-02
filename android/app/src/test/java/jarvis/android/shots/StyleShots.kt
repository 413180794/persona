package jarvis.android.shots

import androidx.compose.runtime.mutableStateOf
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import jarvis.android.data.AgentState
import jarvis.android.ui.AgentsListOptions
import jarvis.android.ui.AppStyle
import jarvis.android.ui.ChatScreen
import jarvis.android.ui.SpaceScreen
import jarvis.android.ui.StyleStore
import jarvis.android.ui.shell.AssistantModel
import jarvis.android.ui.shell.ShellPage
import jarvis.android.ui.shell.ShellRoot
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.After
import org.junit.Rule
import org.junit.Test

/** The Agents page, a chat and a space page in each 界面风格 (the glass one is in [Shots]). */
class StyleShots {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6, theme = "android:Theme.Material.Light.NoActionBar", maxPercentDifference = 100.0)

    private val hub get() = MockHub.hub

    @After fun glass() = StyleStore.set(AppStyle.GLASS)

    private fun each(name: String, content: @androidx.compose.runtime.Composable () -> Unit, before: () -> Unit = {}) {
        for (style in AppStyle.entries.filter { it != AppStyle.GLASS }) for (dark in listOf(false, true)) {
            before()
            StyleStore.set(style)
            paparazzi.snapshot("$name-${style.id}" + if (dark) "-dark" else "") { ShotHost(dark) { content() } }
        }
    }

    @Test fun agents() = each("agents", {
        val m = AssistantModel(hub, CoroutineScope(SupervisorJob() + Dispatchers.IO), null)
        ShellRoot(hub, mutableStateOf(null), mutableStateOf(null), assistant = m, options = AgentsListOptions(null), startPage = ShellPage.Agents)
    })

    @Test fun chat() {
        val key = hub.sessions.value.values.flatten().first { it.agentState == AgentState.NeedsYou && it.machineId == "studio" }.key
        each("chat-plan", { ChatScreen(hub, key, onBack = {}) }, before = {
            val store = hub.chat(key)
            runBlocking { withTimeout(10_000) { store.state.first { !it.loading } } }
        })
    }

    @Test fun space() {
        val s = hub.sessions.value.values.flatten().first { it.machineId == "macbook" && it.spaceName == "weather" }
        each("space", { SpaceScreen(hub, "${s.machineId}/${s.spaceId ?: "-"}", onOpen = {}, onBack = {}) })
    }
}
