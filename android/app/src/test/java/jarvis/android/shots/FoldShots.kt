package jarvis.android.shots

import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.mutableStateOf
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import com.android.resources.Density
import com.android.resources.ScreenOrientation
import jarvis.android.data.AgentState
import jarvis.android.data.SessionKey
import jarvis.android.ui.AgentsListOptions
import jarvis.android.ui.shell.AssistantModel
import jarvis.android.ui.shell.HingePx
import jarvis.android.ui.shell.LocalHingeOverride
import jarvis.android.ui.shell.PaneKind
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

/** Screens at 480 dpi (3 px a dp) unless said. */
private fun screen(w: Int, h: Int, density: Density = Density.XXHIGH) = DeviceConfig.PIXEL_6.copy(
    screenWidth = w, screenHeight = h, xdpi = density.dpiValue, ydpi = density.dpiValue, density = density,
    orientation = if (w > h) ScreenOrientation.LANDSCAPE else ScreenOrientation.PORTRAIT,
)

/** A near-square book-style inner screen (2312 × 2504, 771 dp wide) and its cover screen (1120 × 2528). */
private val INNER = screen(2312, 2504)
private val INNER_LANDSCAPE = screen(2504, 2312)
private val COVER = screen(1120, 2528)
/** A narrower, taller inner screen (700 × 840 dp, like the Galaxy Z Fold's). */
private val TALL_INNER = screen(2100, 2520)
/** A tri-fold opened all the way (3184 × 2232, 1061 dp wide), its folds at a third and two thirds. */
private val TRI_FOLD = screen(3184, 2232)
/** A tablet on its side (1280 × 800 dp at 320 dpi), and a phone on its side (800 × 360 dp). */
private val TABLET = screen(2560, 1600, Density.XHIGH)
private val PHONE_LANDSCAPE = screen(2400, 1080)

/** The shell on wide windows (two panes, the divider at its default) and narrow ones (one page). */
abstract class FoldShotsBase(device: DeviceConfig) {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = device, theme = "android:Theme.Material.Light.NoActionBar", maxPercentDifference = 100.0)

    protected val hub get() = MockHub.hub

    protected fun key(state: AgentState, machine: String): SessionKey =
        hub.sessions.value.values.flatten().first { it.agentState == state && it.machineId == machine }.key

    private fun loaded(key: SessionKey) {
        val store = hub.chat(key)
        runBlocking { withTimeout(10_000) { store.state.first { !it.loading } } }
    }

    protected fun shell(name: String, page: ShellPage, open: SessionKey? = null, routes: List<String> = emptyList(), hinges: List<HingePx>? = null,
        shares: Map<PaneKind, Float> = emptyMap(), inbox: String? = null, settings: List<String>? = null, space: String? = null,
    ) {
        for (dark in listOf(false, true)) {
            val model = AssistantModel(hub, CoroutineScope(SupervisorJob() + Dispatchers.IO), null)
            runBlocking { model.reload() }
            model.current?.let { loaded(SessionKey(model.machine, it)) }
            open?.let(::loaded)
            paparazzi.snapshot(name + if (dark) "-dark" else "") {
                CompositionLocalProvider(LocalHingeOverride provides hinges) {
                    ShotHost(dark) {
                        ShellRoot(hub, mutableStateOf(open), mutableStateOf(null), assistant = model, options = AgentsListOptions(null),
                            startPage = page, startRoutes = routes, startShares = shares,
                            startInbox = inbox, startSettings = settings, startSpace = space)
                    }
                }
            }
        }
    }
}

class FoldInnerShots : FoldShotsBase(INNER) {
    /** The fold flat down the middle of the inner screen, as WindowManager reports it. */
    private val fold = listOf(HingePx(1156f, 1156f))

    @Test fun agentsChat() = shell("fold-agents-chat", ShellPage.Agents, open = key(AgentState.NeedsYou, "macbook"), hinges = fold)

    @Test fun agentsNone() = shell("fold-agents", ShellPage.Agents, hinges = fold)

    @Test fun assistant() = shell("fold-assistant", ShellPage.Assistant, hinges = fold)

    @Test fun inbox() = shell("fold-inbox", ShellPage.Assistant, routes = listOf("inbox"), hinges = fold)

    @Test fun settingsLike() = shell("fold-machine", ShellPage.Assistant, routes = listOf("machines", "machine/macbook"), hinges = fold)

    /** The divider dragged as far right as it goes: the chat keeps a phone's width. */
    @Test fun dragged() = shell("fold-agents-chat-wide-list", ShellPage.Agents, open = key(AgentState.NeedsYou, "macbook"), hinges = fold,
        shares = mapOf(PaneKind.Agents to 0.7f))

    /** The inbox as two panes: the list, and the open item beside it. */
    @Test fun inboxItem() = shell("fold-inbox-item", ShellPage.Assistant, routes = listOf("inbox"), hinges = fold,
        inbox = hub.inbox.value.first { it.agent?.draft != null }.id)

    /** A Feishu item in the right pane: its chat as its page, no back button (the list is beside it). */
    @Test fun inboxFeishu() = shell("fold-inbox-feishu", ShellPage.Assistant, routes = listOf("inbox"), hinges = fold, inbox = "lark:f2:200")

    /** 设置 as two panes: the menu, and the page it opened (no back button: the menu is beside it). */
    @Test fun settings() = shell("fold-settings", ShellPage.Assistant, hinges = fold, settings = listOf("machines"))

    /** A space opened from the list shows in the right pane. */
    @Test fun spacePane() {
        val s = hub.sessions.value.values.flatten().first { it.machineId == "macbook" && it.spaceName == "weather" }
        shell("fold-agents-space", ShellPage.Agents, hinges = fold, space = "${s.machineId}/${s.spaceId ?: "-"}")
    }

    /** Half open like a book: the fold separates the two halves, the split is fixed on it. */
    @Test fun halfOpen() = shell("fold-book-agents-chat", ShellPage.Agents, open = key(AgentState.NeedsYou, "macbook"),
        hinges = listOf(HingePx(1156f, 1156f, separating = true)))
}

/** Turned sideways the fold is horizontal: no vertical fold, the panes split by the usual rule. */
class FoldLandscapeShots : FoldShotsBase(INNER_LANDSCAPE) {
    @Test fun agentsChat() = shell("fold-landscape-agents-chat", ShellPage.Agents, open = key(AgentState.Working, "macbook"))
}

class FoldCoverShots : FoldShotsBase(COVER) {
    @Test fun agents() = shell("fold-cover-agents", ShellPage.Agents)

    /** A phone's width: the Feishu item's chat pushed over the inbox list. */
    @Test fun inboxFeishu() = shell("fold-cover-inbox-feishu", ShellPage.Assistant, routes = listOf("inbox"), inbox = "lark:f2:200")
}

class FoldTallShots : FoldShotsBase(TALL_INNER) {
    @Test fun agentsChat() = shell("fold-tall-agents-chat", ShellPage.Agents, open = key(AgentState.NeedsYou, "macbook"),
        hinges = listOf(HingePx(1050f, 1050f)))
}

class TriFoldShots : FoldShotsBase(TRI_FOLD) {
    private val folds = listOf(HingePx(1061f, 1061f), HingePx(2123f, 2123f))

    @Test fun agentsChat() = shell("trifold-agents-chat", ShellPage.Agents, open = key(AgentState.NeedsYou, "macbook"), hinges = folds)

    @Test fun assistant() = shell("trifold-assistant", ShellPage.Assistant, hinges = folds)
}

class TabletShots : FoldShotsBase(TABLET) {
    @Test fun agentsChat() = shell("tablet-agents-chat", ShellPage.Agents, open = key(AgentState.NeedsYou, "macbook"))

    @Test fun assistant() = shell("tablet-assistant", ShellPage.Assistant)
}

class PhoneLandscapeShots : FoldShotsBase(PHONE_LANDSCAPE) {
    @Test fun agentsChat() = shell("phone-landscape-agents-chat", ShellPage.Agents, open = key(AgentState.Working, "macbook"))
}
