package persona.feature.agents

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.runtime.*
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.navigation3.runtime.NavKey
import androidx.lifecycle.viewModelScope
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.data.HubSessionsRepository
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.*
import java.io.File

/** Actual repository + ViewModel + Compose: generic paged counts must not override local membership. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class AgentsVisibilityTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val stored = MutableStateFlow<Preferences?>(emptyPreferences())

    @Test fun offlineHistorySurvivesRunningAllHistoryAndLiveInvalidations() {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val connection = VisibilityConnection()
        val transport = VisibilityTransport()
        val repository = HubSessionsRepository(transport, connection, scope)
        val model = AgentsViewModel(repository, object : InteractionCenter {
            override val pending = MutableStateFlow<List<Interaction>>(emptyList())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
        }, object : HubConnection {
            override val state = MutableStateFlow(ConnectionState.Online)
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }, object : MessageSender {
            override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
        }, AgentsPreferences(object : DataStore<Preferences> {
            override val data = stored.filterNotNull()
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                transform(stored.value ?: emptyPreferences()).also { stored.value = it }
        }, object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }))
        var dark by mutableStateOf(false)
        rule.setContent { PersonaTheme(darkTheme = dark, reduceMotion = true) {
            DisposableEffect(Unit) { onDispose { model.viewModelScope.cancel(); scope.cancel() } }
            Box(Modifier.fillMaxSize().testTag("visibility-shot")) { AgentsScreen(model, Nowhere, AgentsMetadata()) }
        } }
        rule.waitUntil(10_000) { repository.listStatus.value.loaded && model.page.value.loaded }
        rule.runOnIdle { model.machine("computer") }
        rule.waitForIdle()
        assertTrue(repository.listStatus.value.filter.requiresCompleteMembership)
        assertFalse(repository.listStatus.value.paged)
        assertEquals(1, repository.sessions.value.size)
        assertCounts(model, 0)
        rule.onNodeWithText("Retained offline task").assertDoesNotExist()
        shot("running")
        rule.runOnIdle { dark = true }
        shot("running-dark")
        rule.runOnIdle { dark = false }

        for (range in listOf(AgentRange.All, AgentRange.History)) {
            rule.runOnIdle { model.range(range) }
            rule.waitUntil(5_000) { model.page.value.preferences.range == range && model.page.value.allCount == 1 }
            assertCounts(model, 1)
            rule.onNode(hasScrollToNodeAction()).performScrollToNode(hasText("Retained offline task"))
            rule.onNodeWithText("Retained offline task").assertIsDisplayed()
            shot(range.name.lowercase())
            rule.runOnIdle { dark = true }
            shot("${range.name.lowercase()}-dark")
            rule.runOnIdle { dark = false }
        }
        rule.runOnIdle { dark = true }
        rule.waitForIdle()
        shot("history-dark")

        // A paged socket still emits only notices: the complete fallback must refresh on them too.
        rule.runOnIdle { transport.rows = listOf(transport.offline.copy(status = "working", state = "working"))
            scope.launch { connection.events.emit(SessionsListEvent("new-revision")) } }
        rule.waitUntil(5_000) { repository.sessions.value.single().status == "working" }
        rule.waitForIdle()
        rule.waitUntil(5_000) { model.page.value.allCount == 0 }
        assertCounts(model, 0)
        rule.runOnIdle { model.range(AgentRange.Running) }
        rule.waitUntil(5_000) { model.page.value.allCount == 1 }
        assertCounts(model, 1)
        assertTrue(transport.fullReads >= 2)
        assertEquals(0, transport.countReads)
    }

    private fun assertCounts(model: AgentsViewModel, count: Int) {
        val page = model.page.value
        assertEquals(count, page.allCount)
        assertEquals(count, page.machineTotal)
        assertEquals(count, page.machineCounts.single().count)
        assertEquals(count, page.list.groups.flatMap { it.rows }.size)
    }
    private fun shot(name: String) {
        rule.waitForIdle()
        val file = File("build/visibility-shots/$name.png").apply { requireNotNull(parentFile).mkdirs() }
        file.outputStream().use { assertTrue(rule.onNodeWithTag("visibility-shot").captureToImage().asAndroidBitmap()
            .compress(Bitmap.CompressFormat.PNG, 100, it)) }
    }
    private object Nowhere : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
}

private class VisibilityConnection : HubConnection {
    override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
    override val events = MutableSharedFlow<ServerEvent>()
    override fun reconnect() = Unit
    override fun send(message: JsonObject) = true
}
private class VisibilityTransport : ChatTransport {
    val offline = Session("computer", "retained-history", title = "Retained offline task", agent = "codex",
        status = "offline", state = "offline", sourceKind = "codex_history", cwd = "/demo/project")
    var rows = listOf(offline)
    var fullReads = 0
    var countReads = 0
    override suspend fun machines() = listOf(Machine("computer", "Example computer", online = true))
    override suspend fun sessions(): List<Session> { fullReads++; return rows }
    override suspend fun sessionPage(filter: SessionListFilter, cursor: String?, since: String?) =
        SessionListPage(sessions = rows, total = rows.size, revision = "r1")
    override suspend fun sessionCounts(filter: SessionListFilter): SessionListCounts {
        countReads++; return SessionListCounts(total = 1, machine = mapOf("computer" to 1))
    }
    override suspend fun items(session: SessionKey, query: ItemsQuery) = ItemsResponse()
    override suspend fun interactions() = emptyList<Interaction>()
    override suspend fun resolve(id: String, resolution: Resolution) = Unit
    override suspend fun image(session: SessionKey, itemId: String) = byteArrayOf()
    override suspend fun fullItem(session: SessionKey, itemId: String) = Item(itemId)
}
