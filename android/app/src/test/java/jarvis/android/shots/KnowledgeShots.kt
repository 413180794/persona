package jarvis.android.shots

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.requiredHeight
import androidx.compose.foundation.layout.wrapContentHeight
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.unit.Dp
import jarvis.android.data.ApiClient
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import jarvis.android.data.KbDetailResponse
import jarvis.android.data.KbEntry
import jarvis.android.data.KbKind
import jarvis.android.data.kbDetail
import jarvis.android.data.kbList
import jarvis.android.ui.AssistantPage
import jarvis.android.ui.AssistantPreload
import jarvis.android.ui.AssistantProfile
import jarvis.android.ui.JTheme
import jarvis.android.ui.MemoryForm
import jarvis.android.ui.RoutineForm
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.knowledge.KbTagForm
import jarvis.android.ui.knowledge.KnowledgeDetailScreen
import jarvis.android.ui.knowledge.KnowledgeListModel
import jarvis.android.ui.knowledge.KnowledgeScreen
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Rule
import org.junit.Test

/**
 * Screenshots of 知识库 and Jarvis' profile pages with scripts/mock/mock_hub.py's made-up data, light and dark (as [Shots]).
 * Paparazzi draws one frame, so each page is handed what it would load first (the list loaded, an entry's detail,
 * the profile's lists), and a sheet's body is drawn in a sheet's frame (its own window can't be drawn here).
 */
class KnowledgeShots {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6, theme = "android:Theme.Material.Light.NoActionBar", maxPercentDifference = 100.0)

    private val hub get() = MockHub.hub

    private fun shot(name: String, content: @Composable () -> Unit) {
        for (dark in listOf(false, true)) paparazzi.snapshot(name + if (dark) "-dark" else "") { ShotHost(dark) { content() } }
    }

    /** The list as the page has it once its loads are back: [kind]'s list, and [query]'s hits. */
    private fun list(kind: KbKind = KbKind.PROJECTS, query: String? = null): KnowledgeListModel {
        val m = KnowledgeListModel(hub, CoroutineScope(SupervisorJob() + Dispatchers.IO))
        runBlocking {
            withTimeout(10_000) {
                m.state.first { it.lists[KbKind.PROJECTS] != null }
                if (kind != KbKind.PROJECTS) { m.select(kind); m.state.first { it.lists[kind] != null } }
                if (query != null) { m.search(query); m.state.first { it.hits != null } }
            }
        }
        return m
    }

    private fun knowledge(name: String, m: KnowledgeListModel) = shot(name) {
        KnowledgeScreen(hub, onBack = {}, onAsk = {}, onOpenSession = {}, model = m)
    }

    /** The weather project as its list row has it, and what its page loads. */
    private fun project(): Pair<KbEntry, KbDetailResponse> = runBlocking {
        hub.api.kbList("projects").items.first { it.id == "weather-app" } to hub.api.kbDetail("projects", "weather-app")
    }

    /** The weather project's page, scrolled to its [from]th item (0: the top). */
    @Composable
    private fun EntryPage(e: KbEntry, d: KbDetailResponse, from: Int = 0) {
        KnowledgeDetailScreen(
            hub, e, onBack = {}, onOpen = {}, onAsk = {}, onOpenSession = {}, onChanged = {}, preloaded = d,
            listState = rememberLazyListState(initialFirstVisibleItemIndex = from),
        )
    }

    private fun entry(name: String, from: Int = 0) {
        val (e, d) = project()
        shot(name) { EntryPage(e, d, from) }
    }

    @Test fun knowledge() = knowledge("knowledge", list())

    @Test fun knowledgePeople() = knowledge("knowledge-people", list(KbKind.PEOPLE))

    @Test fun knowledgeSearch() = knowledge("knowledge-search", list(query = "天气"))

    @Test fun knowledgeEntry() = entry("knowledge-entry")

    /** From the timeline down: the profile, what it relates to, its code and its directory. */
    @Test fun knowledgeEntryLower() = entry("knowledge-entry-lower", from = 3)

    @Test fun knowledgeTags() {
        val (e, d) = project()
        val merged = d.detail.merged(e)
        shot("knowledge-tags") { SheetFrame("打标 · " + merged.name, under = { EntryPage(e, d) }) { KbTagForm(hub, merged, onSaved = {}) } }
    }

    /** Everything the profile's pages load, read from the mock as they would. */
    private fun preload(): AssistantPreload = runBlocking {
        AssistantPreload(
            routines = hub.api.routines(), followups = hub.api.followups(), runs = hub.api.runs(),
            notes = hub.api.notes().let { it.notes ?: it.effective }, memory = hub.api.memory(),
        )
    }

    @Composable
    private fun ProfilePage(p: AssistantPreload, page: AssistantPage) {
        AssistantProfile(hub, machineName = "服务器", onBack = {}, onOpenConversation = {}, start = page, preload = p)
    }

    private fun profile(name: String, page: AssistantPage) {
        val p = preload()
        shot(name) { ProfilePage(p, page) }
    }

    @Test fun profile() = profile("profile", AssistantPage.Profile)

    /** The profile's foot (了解你, 能访问): its header scrolls away with it here. */
    @Test fun profileLower() {
        val p = preload()
        shot("profile-lower") { ShiftedUp(JSize.box300 + JSize.box280) { ProfilePage(p, AssistantPage.Profile) } }
    }

    @Test fun routines() = profile("profile-routines", AssistantPage.Routines)

    @Test fun followups() = profile("profile-followups", AssistantPage.Followups)

    @Test fun runs() = profile("profile-runs", AssistantPage.Runs)

    @Test fun notes() = profile("profile-notes", AssistantPage.Notes)

    @Test fun memory() = profile("profile-memory", AssistantPage.Memory)

    @Test fun routineEditor() {
        val p = preload()
        val r = p.routines.orEmpty().first()
        shot("profile-routine-editor") {
            SheetFrame("定时任务", under = { ProfilePage(p, AssistantPage.Routines) }) { RoutineForm(r, isNew = false, onSave = {}, onDelete = {}, onRunNow = {}) }
        }
    }

    @Test fun routineNew() {
        val p = preload()
        shot("profile-routine-new") {
            SheetFrame("新建定时任务", under = { ProfilePage(p, AssistantPage.Routines) }) {
                RoutineForm(ApiClient.Routine(time = "09:00", weekdays = listOf(1, 2, 3, 4, 5)), isNew = true, onSave = {}, onDelete = {}, onRunNow = {})
            }
        }
    }

    @Test fun memoryEditor() {
        val p = preload()
        val f = p.memory.orEmpty().first { it.name == "morning-format.md" }
        shot("profile-memory-editor") { SheetFrame("早报格式", under = { ProfilePage(p, AssistantPage.Memory) }) { MemoryForm(f, onSave = {}) } }
    }
}

/** The page from [by] down, as if scrolled: laid out [by] taller and moved up (a page's own list can't be scrolled here). */
@Composable
private fun ShiftedUp(by: Dp, content: @Composable () -> Unit) {
    BoxWithConstraints(Modifier.fillMaxSize().clipToBounds()) {
        Box(Modifier.wrapContentHeight(Alignment.Top, unbounded = true).offset(y = -by).requiredHeight(maxHeight + by)) { content() }
    }
}

/**
 * A sheet as JSheet draws it over the page [under] it, dimmed (a sheet's own window can't be drawn here): the page's
 * color, the panel's corners, the grabber, the title and ✕, then the body.
 */
@Composable
private fun SheetFrame(title: String, under: @Composable () -> Unit, body: @Composable ColumnScope.() -> Unit) {
    val c = JTheme.colors
    Box(Modifier.fillMaxSize()) {
        under()
        Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = 0.32f)))
        Column(
            Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(top = JSize.shellBar)
                .clip(RoundedCornerShape(topStart = JRadius.panel, topEnd = JRadius.panel)).background(c.page),
        ) {
            Box(
                Modifier.align(Alignment.CenterHorizontally).padding(top = JSpace.s, bottom = JSpace.xs)
                    .size(width = JSize.wellL, height = JSize.grabber).background(c.fillPressed, CircleShape),
            )
            Box(Modifier.fillMaxWidth().padding(horizontal = JSpace.page, vertical = JSpace.s)) {
                Text(title, style = JTheme.type.heading, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center,
                    modifier = Modifier.align(Alignment.Center).padding(horizontal = JSize.tapTarget))
                CircleButton(JIcons.close, "关闭", {}, Modifier.align(Alignment.CenterEnd), size = JSize.wellL)
            }
            body()
            Spacer(Modifier.height(JSpace.l))
        }
    }
}
