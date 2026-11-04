package persona.feature.agents

import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*

class HubNoticeRowTest {

    private class FakeHubUpdates(
        private var currentStatus: HubUpdateStatus = HubUpdateStatus(),
    ) : HubUpdates {
        val eventFlow = MutableSharedFlow<HubUpdateStatus>()
        override val events = eventFlow
        var lastAuto: Boolean? = null
        var lastNoticeSeen: Boolean? = null
        var startCalled: Boolean = false

        override suspend fun status(): HubUpdateStatus = currentStatus
        override suspend fun check(): HubUpdateStatus = currentStatus
        override suspend fun start(now: Boolean): HubUpdateStatus {
            startCalled = true
            currentStatus = currentStatus.copy(state = "downloading")
            return currentStatus
        }
        override suspend fun settings(auto: Boolean?, noticeSeen: Boolean?): HubUpdateStatus {
            lastAuto = auto
            lastNoticeSeen = noticeSeen
            currentStatus = currentStatus.copy(
                auto = auto ?: currentStatus.auto,
                autoNotice = if (noticeSeen == true) false else currentStatus.autoNotice
            )
            return currentStatus
        }
        override suspend fun history(): List<HubUpdateRecord> = emptyList()
    }

    private class FakeConnection(
        override val state: MutableStateFlow<ConnectionState> = MutableStateFlow(ConnectionState.Online),
    ) : HubConnection {
        val eventFlow = MutableSharedFlow<ServerEvent>()
        override val events = eventFlow
        override val route = MutableStateFlow<ConnectionRoute?>(null)
        override val endpoints = MutableStateFlow<List<Endpoint>>(emptyList())
        override val http = object : HttpConnection {
            override val client = okhttp3.OkHttpClient()
            override val endpoint = null
            override val base = "http://127.0.0.1"
            override val token = ""
            override val fingerprint = null
        }
        override val fingerprint = MutableStateFlow<String?>(null)
        override val token = "fake-token"
        override fun connect() {}
        override fun disconnect() {}
        override fun refresh() {}
    }

    private class FakeInteractionCenter : InteractionCenter {
        override val pending = MutableStateFlow<List<Interaction>>(emptyList())
        override val answered = MutableStateFlow<List<Interaction>>(emptyList())
        override suspend fun resolve(id: String, resolution: Resolution) {}
        override suspend fun answer(sessionKey: SessionKey, id: String, resolution: Resolution) {}
    }

    private class FakeMessageSender : MessageSender {
        override val outbox = MutableStateFlow<List<OutgoingMessage>>(emptyList())
        override suspend fun send(message: OutgoingMessage) {}
        override suspend fun cancel(id: String) {}
    }

    private class FakeSessionsRepository : SessionsRepository {
        override val sessions = MutableStateFlow<List<Session>>(emptyList())
        override val machines = MutableStateFlow<List<Machine>>(emptyList())
        override suspend fun adopt(session: Session) {}
    }

    @Test
    fun textMappingMatchesCopy() {
        assertEquals("App 版本太旧，配不上这个 hub：请到 TestFlight 更新", HubNotice.text(HubNotice.AppTooOld))
        assertEquals("hub 版本太旧，部分功能用不了", HubNotice.text(HubNotice.HubTooOld))
        assertEquals("hub 正在更新：正在下载", HubNotice.text(HubNotice.Updating("正在下载")))
        assertEquals("起不来", HubNotice.text(HubNotice.RolledBack("起不来", 123)))
        assertEquals("hub 会在空闲时自动更新", HubNotice.text(HubNotice.AutoOn))
        assertEquals("hub 有新版本 1.5.0", HubNotice.text(HubNotice.Available("1.5.0", 150)))
        assertEquals("hub 有新镜像 1.5.0", HubNotice.text(HubNotice.NewImage("1.5.0")))
    }

    @Test
    fun modelComputesNoticeAndDismisses() = runTest {
        val fakeUpdates = FakeHubUpdates(
            HubUpdateStatus(
                version = "1.0.0",
                build = 100,
                canUpdate = true,
                autoNotice = true,
                latest = HubRelease(version = "1.1.0", build = 110, newer = true),
            )
        )
        val fakeConn = FakeConnection()
        val viewModel = AgentsViewModel(
            repository = FakeSessionsRepository(),
            interactions = FakeInteractionCenter(),
            connection = fakeConn,
            sender = FakeMessageSender(),
            preferences = FakeAgentsPreferences(),
            hubUpdates = fakeUpdates,
        )

        // Initial status received: autoNotice -> AutoOn
        assertEquals(HubNotice.AutoOn, viewModel.hubNotice.value)

        // Dismiss AutoOn (dismissKey is null, dismissal does not alter AutoOn)
        viewModel.dismissHubNotice(HubNotice.AutoOn)
        assertEquals(HubNotice.AutoOn, viewModel.hubNotice.value)

        // Notice seen tells hub and clears autoNotice
        viewModel.noticeSeen()
        assertEquals(true, fakeUpdates.lastNoticeSeen)

        // After autoNotice is gone, Available is next
        fakeUpdates.eventFlow.emit(
            HubUpdateStatus(
                version = "1.0.0",
                build = 100,
                canUpdate = true,
                auto = false,
                latest = HubRelease(version = "1.1.0", build = 110, newer = true),
            )
        )
        assertEquals(HubNotice.Available("1.1.0", 110), viewModel.hubNotice.value)

        // Dismiss Available by build
        viewModel.dismissHubNotice(HubNotice.Available("1.1.0", 110))
        assertNull(viewModel.hubNotice.value)

        // If an update starts, Updating notice appears (cannot be dismissed)
        fakeUpdates.eventFlow.emit(
            HubUpdateStatus(
                version = "1.0.0",
                build = 100,
                state = "downloading",
            )
        )
        assertEquals(HubNotice.Updating("正在下载"), viewModel.hubNotice.value)
    }

    @Test
    fun helloUpdatesCompatAndPreemptsStatus() = runTest {
        val fakeUpdates = FakeHubUpdates(
            HubUpdateStatus(version = "1.0.0", build = 100, canUpdate = true)
        )
        val fakeConn = FakeConnection()
        val viewModel = AgentsViewModel(
            repository = FakeSessionsRepository(),
            interactions = FakeInteractionCenter(),
            connection = fakeConn,
            sender = FakeMessageSender(),
            preferences = FakeAgentsPreferences(),
            hubUpdates = fakeUpdates,
            appBuild = 10L,
        )

        // Hub requires minAppBuild 20 > appBuild 10
        fakeConn.eventFlow.emit(
            Hello(serverVersion = "1.0.0", minAppBuild = 20, protocol = 1)
        )
        assertEquals(HubNotice.AppTooOld, viewModel.hubNotice.value)
    }

    private class FakeAgentsPreferences : AgentsPreferences(
        store = object : androidx.datastore.core.DataStore<androidx.datastore.preferences.core.Preferences> {
            override val data = flowOf(androidx.datastore.preferences.core.emptyPreferences())
            override suspend fun updateData(transform: suspend (t: androidx.datastore.preferences.core.Preferences) -> androidx.datastore.preferences.core.Preferences): androidx.datastore.preferences.core.Preferences =
                androidx.datastore.preferences.core.emptyPreferences()
        },
        hub = object : HubPreferences {
            override val preferences = MutableStateFlow(RemotePreferences())
            override suspend fun togglePin(id: String) {}
            override suspend fun toggleMute(id: String) {}
            override suspend fun markSeen(key: SessionKey, version: String) {}
        }
    )
}
