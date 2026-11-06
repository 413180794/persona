package persona.feature.agents

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.*
import kotlinx.serialization.json.JsonObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Before
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*

/** Session operations go through the session's own hub group, as answers do (#4702). */
@OptIn(ExperimentalCoroutinesApi::class)
class AgentsGroupOperationsTest {
    private lateinit var model: AgentsViewModel
    private val work = Session("mac~g0a1b2c3d", "s", paneId = "pane")
    private val calls = mutableListOf<String>()
    private val preferences = AgentsPreferences(object : DataStore<Preferences> {
        override val data = MutableStateFlow<Preferences>(emptyPreferences())
        override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
            transform(data.value).also { data.value = it }
    }, object : HubPreferences {
        override val preferences = MutableStateFlow(HubPreferencesSnapshot())
        override suspend fun refresh() = Unit
        override suspend fun togglePin(path: String, space: Boolean) = Unit
        override suspend fun toggleMute(path: String, space: Boolean) = Unit
        override suspend fun markSeen(session: SessionKey, version: String) = Unit
    })

    @Before fun setMain() { Dispatchers.setMain(StandardTestDispatcher()) }
    @After fun tearDown() {
        if (::model.isInitialized) model.viewModelScope.cancel()
        Dispatchers.resetMain()
    }

    private fun TestScope.model(selected: ConnectionState, other: ConnectionState): AgentsViewModel {
        val operations = object : AgentOperations {
            override val scope = "pairing"
            override suspend fun pins() = AgentOperationPreferences()
            override suspend fun mutes() = AgentOperationPreferences()
            override suspend fun savePins(value: AgentOperationPreferences) { calls += "savePins" }
            override suspend fun saveMutes(value: AgentOperationPreferences) { calls += "saveMutes" }
            override suspend fun renameSession(key: SessionKey, title: String?, tabName: String?) { calls += "rename" }
            override suspend fun renameSpace(machineId: String, workspaceId: String, name: String) = Unit
            override suspend fun closeSession(key: SessionKey) { calls += "close" }
        }
        model = AgentsViewModel(object : SessionsRepository {
            override val sessions = MutableStateFlow(listOf(work))
            override val machines = MutableStateFlow(listOf(Machine(work.machineId, online = true)))
            override suspend fun refresh() = Unit
        }, object : InteractionCenter {
            override val pending = MutableStateFlow(emptyList<Interaction>())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
        }, object : HubConnection {
            override val state = MutableStateFlow(selected)
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }, object : MessageSender {
            override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) { calls += "send" }
        }, preferences, bindings = AgentsOperationsBindings(operations, object : AssistantConversations {
            override val assistant = MutableStateFlow(AssistantSnapshot())
            override val pinned = MutableStateFlow(emptySet<SessionKey>())
            override suspend fun startConversation(): SessionKey = error("unused")
            override suspend fun refresh() = Unit
        }), hubGroups = object : HubGroups by HubGroups.None {
            override val groups = MutableStateFlow(listOf(
                HubGroupInfo("g11111111", "home", null, true, selected, emptyList()),
                HubGroupInfo("g0a1b2c3d", "work", "g0a1b2c3d", false, other, emptyList())))
        })
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { model.page.collect {} }
        runCurrent()
        return model
    }

    @Test fun anotherGroupOnlineIsChangedWhileTheSelectedOneIsOffline() = runTest {
        val model = model(ConnectionState.WaitingForNetwork, ConnectionState.Online)
        model.close(work)
        runCurrent()
        assertNotEquals(R.string.agents_notice_offline, model.notice.value?.id)
        model.pin(work.listSession(AgentsPreferencesSnapshot()))
        runCurrent()
        model.stopLoop(work)
        runCurrent()
        assertNotEquals(R.string.agents_notice_stop_loop_offline, model.notice.value?.id)
        assertEquals(listOf("close", "savePins", "send"), calls)
    }

    @Test fun anotherGroupOfflineIsNotChangedThroughTheSelectedOne() = runTest {
        val model = model(ConnectionState.Online, ConnectionState.Connecting)
        model.close(work)
        runCurrent()
        assertEquals(R.string.agents_notice_offline, model.notice.value?.id)
        model.stopLoop(work)
        runCurrent()
        assertEquals(R.string.agents_notice_stop_loop_offline, model.notice.value?.id)
        assertEquals(emptyList<String>(), calls)
    }
}
