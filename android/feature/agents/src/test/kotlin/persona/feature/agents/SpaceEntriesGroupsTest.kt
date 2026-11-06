package persona.feature.agents

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.lifecycle.ViewModelProvider
import androidx.navigation3.runtime.NavKey
import androidx.navigation3.runtime.entryProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import persona.core.navigation.SpaceRoute

/** The space page builds its own model; it gets the hub groups too, so its rows change through their own group (#4702). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class SpaceEntriesGroupsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val work = Session("mac~g0a1b2c3d", "s", paneId = "pane")
    private val calls = mutableListOf<String>()

    private val operations = object : AgentOperations {
        override val scope = "pairing"
        override suspend fun pins() = AgentOperationPreferences()
        override suspend fun mutes() = AgentOperationPreferences()
        override suspend fun savePins(value: AgentOperationPreferences) { calls += "savePins" }
        override suspend fun saveMutes(value: AgentOperationPreferences) { calls += "saveMutes" }
        override suspend fun renameSession(key: SessionKey, title: String?, tabName: String?) { calls += "rename" }
        override suspend fun renameSpace(machineId: String, workspaceId: String, name: String) = Unit
        override suspend fun closeSession(key: SessionKey) { calls += "close" }
    }

    private val dependencies = object : AgentsDependencies {
        private val sessions = object : SessionsRepository {
            override val sessions = MutableStateFlow(listOf(work))
            override val machines = MutableStateFlow(listOf(Machine(work.machineId, online = true)))
            override suspend fun refresh() = Unit
        }
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
        private val wiring = object : AgentsWiring {
            override val state = MutableStateFlow(AgentsWiringState(operations = AgentsOperationsBindings(operations, object : AssistantConversations {
                override val assistant = MutableStateFlow(AssistantSnapshot())
                override val pinned = MutableStateFlow(emptySet<SessionKey>())
                override suspend fun startConversation(): SessionKey = error("unused")
                override suspend fun refresh() = Unit
            })))
        }
        // home is selected and offline; work, the session's group, is online.
        private val groups = object : HubGroups by HubGroups.None {
            override val groups = MutableStateFlow(listOf(
                HubGroupInfo("g11111111", "home", null, true, ConnectionState.WaitingForNetwork, emptyList()),
                HubGroupInfo("g0a1b2c3d", "work", "g0a1b2c3d", false, ConnectionState.Online, emptyList())))
        }
        override fun sessions() = sessions
        override fun interactions() = object : InteractionCenter {
            override val pending = MutableStateFlow(emptyList<Interaction>())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
        }
        override fun connection() = object : HubConnection {
            override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
            override val events = MutableSharedFlow<ServerEvent>()
            override fun reconnect() = Unit
            override fun send(message: JsonObject) = true
        }
        override fun sender() = object : MessageSender {
            override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) { calls += "send" }
        }
        override fun preferences() = preferences
        override fun wiring() = wiring
        override fun groups() = groups
        override fun machines(): MachineManagement? = null
    }

    @Test fun aSpaceRowOfAnOnlineGroupIsChangedWhileTheSelectedOneIsOffline() {
        val navigator = object : Navigator {
            override fun navigate(key: NavKey) = Unit
            override fun back() = Unit
            override fun selectTab(tab: AppTab) = Unit
        }
        val provider = entryProvider<NavKey> { spaceEntries(navigator, dependencies) }
        rule.setContent { provider(SpaceRoute("mac~g0a1b2c3d/s", "s")).Content() }
        rule.waitForIdle()
        // The model the page made (same key as SpaceEntries' viewModel(key = ...)).
        val model = ViewModelProvider(rule.activity)["space::pairing", AgentsViewModel::class.java]
        rule.runOnIdle { model.close(work) }
        rule.waitForIdle()
        assertNotEquals(R.string.agents_notice_offline, model.notice.value?.id)
        assertEquals(listOf("close"), calls)
    }
}
