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
import org.junit.Before
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.Haptic
import persona.core.domain.answer.AnswerAction

/** What answering a request on the Agents page plays, as iOS AppModel.resolve. */
@OptIn(ExperimentalCoroutinesApi::class)
class AgentsHapticsTest {
    private lateinit var model: AgentsViewModel
    private var request = Interaction("request", machineId = "m", sessionId = "s", kind = "permission", toolName = "Bash")
    private var groups: HubGroups = HubGroups.None
    private val state = MutableStateFlow(ConnectionState.Online)
    private var resolve: suspend () -> Unit = {}
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

    private fun TestScope.played(): List<Haptic> {
        model = AgentsViewModel(object : SessionsRepository {
            override val sessions = MutableStateFlow(listOf(Session(request.machineId, "s", state = "needs_input")))
            override val machines = MutableStateFlow(listOf(Machine(request.machineId, online = true)))
            override suspend fun refresh() = Unit
        }, object : InteractionCenter {
            override val pending = MutableStateFlow(listOf(request))
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = this@AgentsHapticsTest.resolve()
        }, object : HubConnection {
            override val state = this@AgentsHapticsTest.state
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }, object : MessageSender {
            override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
        }, preferences, hubGroups = groups)
        val seen = mutableListOf<Haptic>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { model.page.collect {} }
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { model.haptics.collect { seen += it } }
        runCurrent()
        model.answer(request, AnswerAction.Deny)
        runCurrent()
        return seen
    }

    @Test fun anAnswerThatWentThroughSucceeds() = runTest { assertEquals(listOf(Haptic.Success), played()) }

    @Test fun aChangedDialogNudges() = runTest {
        resolve = { throw InteractionPromptChangedException() }
        assertEquals(listOf(Haptic.Attention), played())
    }

    @Test fun aRequestAnsweredElsewherePlaysNothing() = runTest {
        resolve = { throw InteractionGoneException() }
        assertEquals(emptyList<Haptic>(), played())
    }

    @Test fun anUnconfirmedAnswerIsAnError() = runTest {
        resolve = { throw IllegalStateException("lost") }
        assertEquals(listOf(Haptic.Error), played())
    }

    @Test fun offlineItNudgesAndSendsNothing() = runTest {
        state.value = ConnectionState.Connecting
        resolve = { error("must not send") }
        assertEquals(listOf(Haptic.Attention), played())
    }

    private fun twoGroups(selected: ConnectionState, work: ConnectionState) = object : HubGroups by HubGroups.None {
        override val groups = MutableStateFlow(listOf(
            HubGroupInfo("g11111111", "home", null, true, selected, emptyList()),
            HubGroupInfo("g0a1b2c3d", "work", "g0a1b2c3d", false, work, emptyList())))
    }

    @Test fun anotherGroupOnlineIsAnsweredWhileTheSelectedOneIsOffline() = runTest {
        request = request.copy(machineId = "m~g0a1b2c3d")
        state.value = ConnectionState.WaitingForNetwork
        groups = twoGroups(ConnectionState.WaitingForNetwork, ConnectionState.Online)
        var sent = false
        resolve = { sent = true }
        assertEquals(listOf(Haptic.Success), played())
        assertEquals(true, sent)
    }

    @Test fun anotherGroupOfflineIsNotSentThroughTheSelectedOne() = runTest {
        request = request.copy(machineId = "m~g0a1b2c3d")
        groups = twoGroups(ConnectionState.Online, ConnectionState.Connecting)
        resolve = { error("must not send") }
        assertEquals(listOf(Haptic.Attention), played())
    }
}
