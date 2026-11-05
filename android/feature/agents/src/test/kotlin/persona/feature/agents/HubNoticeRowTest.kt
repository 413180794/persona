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
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.*
import persona.core.contracts.*
import persona.core.contracts.api.*
import org.junit.Test
import persona.core.contracts.api.HubCompat
import persona.core.contracts.api.HubNotice
import persona.core.contracts.api.HubRelease
import persona.core.contracts.api.HubUpdateStatus

@OptIn(ExperimentalCoroutinesApi::class)
class HubNoticeRowTest {
    private val newer = HubRelease(version = "1.1.0", build = 110, newer = true)

    private class FakeHubUpdates(private var currentStatus: HubUpdateStatus = HubUpdateStatus()) : HubUpdates {
        val eventFlow = MutableSharedFlow<HubUpdateStatus>()
        override val events = eventFlow
        var lastAuto: Boolean? = null
        var lastNoticeSeen: Boolean? = null
        var startCalled = false

        override suspend fun status() = currentStatus
        override suspend fun check() = currentStatus
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
                autoNotice = if (noticeSeen == true) false else currentStatus.autoNotice,
            )
            return currentStatus
        }
        override suspend fun history() = emptyList<HubUpdateRecord>()
    }

    private class FakeConnection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.Online)
        val eventFlow = MutableSharedFlow<ServerEvent>()
        override val events = eventFlow
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }

    private class FakeInteractionCenter : InteractionCenter {
        override val pending = MutableStateFlow<List<Interaction>>(emptyList())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }

    private class FakeMessageSender : MessageSender {
        override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
    }

    private class FakeSessionsRepository : SessionsRepository {
        override val sessions = MutableStateFlow<List<Session>>(emptyList())
        override val machines = MutableStateFlow<List<Machine>>(emptyList())
        override suspend fun refresh() = Unit
    }

    private fun preferences(): AgentsPreferences {
        val store = object : DataStore<Preferences> {
            override val data = MutableStateFlow<Preferences>(emptyPreferences())
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                transform(data.value).also { data.value = it }
        }
        val hub = object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }
        return AgentsPreferences(store, hub)
    }

    private suspend fun TestScope.withModel(
        status: HubUpdateStatus,
        appBuild: Long = 1L,
        deliverHello: Boolean = true,
        block: suspend TestScope.(AgentsViewModel, FakeHubUpdates, FakeConnection) -> Unit,
    ) {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        try {
            val updates = FakeHubUpdates(status)
            val connection = FakeConnection()
            val model = AgentsViewModel(
                repository = FakeSessionsRepository(),
                interactions = FakeInteractionCenter(),
                connection = connection,
                sender = FakeMessageSender(),
                preferences = preferences(),
                hubUpdates = updates,
                appBuild = appBuild,
            )
            val observer = backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
                model.hubNotice.collect {}
            }
            try {
                // WhileSubscribed needs a real collector; deliver the hot hello only after observers have started.
                runCurrent()
                if (deliverHello) connection.eventFlow.emit(Hello(serverVersion = "1.0.0", protocol = 1))
                runCurrent()
                block(model, updates, connection)
            } finally {
                observer.cancel()
                model.viewModelScope.cancel()
                runCurrent()
            }
        } finally {
            Dispatchers.resetMain()
        }
    }

    @Test
    fun textMappingMatchesCopy() {
        assertEquals("App 版本太旧，配不上这个 hub：请到 TestFlight 更新", HubNotice.text(HubNotice.AppTooOld).shown)
        assertEquals("hub 版本太旧，部分功能用不了", HubNotice.text(HubNotice.HubTooOld).shown)
        assertEquals("hub 正在更新：正在下载", HubNotice.text(HubNotice.Updating(persona.core.contracts.CoreText.of(persona.core.contracts.CoreText.Key.HubDownloading))).shown)
        assertEquals("起不来", HubNotice.text(HubNotice.RolledBack("起不来", 123)).shown)
        assertEquals("hub 会在空闲时自动更新", HubNotice.text(HubNotice.AutoOn).shown)
        assertEquals("hub 有新版本 1.5.0", HubNotice.text(HubNotice.Available("1.5.0", 150)).shown)
        assertEquals("hub 有新镜像 1.5.0", HubNotice.text(HubNotice.NewImage("1.5.0")).shown)
    }

    @Test fun theMostPressingComesFirstAndAClosedOneStaysClosed() {
        val status = HubUpdateStatus(channel = "release", canUpdate = true, latest = newer, autoNotice = true)
        assertEquals(HubNotice.AutoOn, HubNotice.of(status))
        val available = HubNotice.of(status.copy(autoNotice = false))
        assertEquals(HubNotice.Available("1.1.0", 110), available)
        assertNull(HubNotice.of(status.copy(autoNotice = false), dismissed = setOf(available!!.dismissKey!!)))
        assertEquals(HubNotice.Updating(persona.core.contracts.CoreText.of(persona.core.contracts.CoreText.Key.HubDownloading)), HubNotice.of(status.copy(state = "downloading")))
    }

    @Test fun anAppTooOldPreemptsTheRest() {
        val status = HubUpdateStatus(channel = "release", canUpdate = true, latest = newer, autoNotice = true)
        assertEquals(HubNotice.AppTooOld, HubNotice.of(status, HubCompat.AppTooOld))
        assertEquals(HubCompat.AppTooOld, HubCompat.of(protocolVersion = 1, minAppBuild = 200, appBuild = 150))
        assertEquals(HubCompat.Ok, HubCompat.of(protocolVersion = 1, minAppBuild = 200, appBuild = 150, connected = false))
    }
    @Test
    fun modelComputesNoticeAndDismisses() = runTest {
        withModel(
            HubUpdateStatus(
                version = "1.0.0", build = 100, canUpdate = true, autoNotice = true,
                latest = HubRelease(version = "1.1.0", build = 110, newer = true),
            )
        ) { model, updates, _ ->
            assertEquals(HubNotice.AutoOn, model.hubNotice.value)
            model.dismissHubNotice(HubNotice.AutoOn)
            runCurrent()
            assertEquals(HubNotice.AutoOn, model.hubNotice.value)

            model.noticeSeen()
            runCurrent()
            assertEquals(true, updates.lastNoticeSeen)

            updates.eventFlow.emit(
                HubUpdateStatus(
                    version = "1.0.0", build = 100, canUpdate = true, auto = false,
                    latest = HubRelease(version = "1.1.0", build = 110, newer = true),
                )
            )
            runCurrent()
            assertEquals(HubNotice.Available("1.1.0", 110), model.hubNotice.value)
            model.dismissHubNotice(HubNotice.Available("1.1.0", 110))
            runCurrent()
            assertNull(model.hubNotice.value)

            updates.eventFlow.emit(HubUpdateStatus(version = "1.0.0", build = 100, state = "downloading"))
            runCurrent()
            assertEquals(HubNotice.Updating(persona.core.contracts.CoreText.of(persona.core.contracts.CoreText.Key.HubDownloading)), model.hubNotice.value)
        }
    }

    @Test
    fun helloUpdatesCompatAndPreemptsStatus() = runTest {
        withModel(HubUpdateStatus(version = "1.0.0", build = 100, canUpdate = true), appBuild = 10L) { model, _, connection ->
            connection.eventFlow.emit(Hello(serverVersion = "1.0.0", minAppBuild = 20, protocol = 1))
            runCurrent()
            assertEquals(HubNotice.AppTooOld, model.hubNotice.value)
        }
    }

    @Test
    fun onlineWithoutHelloDoesNotInventALegacyHub() = runTest {
        withModel(HubUpdateStatus(state = "downloading"), deliverHello = false) { model, _, connection ->
            assertEquals(HubNotice.Updating(persona.core.contracts.CoreText.of(persona.core.contracts.CoreText.Key.HubDownloading)), model.hubNotice.value)
            connection.eventFlow.emit(Hello(serverVersion = "legacy", protocol = 0))
            runCurrent()
            assertEquals(HubNotice.HubTooOld, model.hubNotice.value)
            connection.eventFlow.emit(Hello(serverVersion = "1.0.0", protocol = 1))
            runCurrent()
            assertEquals(HubNotice.Updating(persona.core.contracts.CoreText.of(persona.core.contracts.CoreText.Key.HubDownloading)), model.hubNotice.value)
        }
    }

    @Test
    fun remembersOnlyEightDistinctDismissalsWithoutReorderingDuplicates() = runTest {
        withModel(HubUpdateStatus()) { model, updates, _ ->
            suspend fun show(build: Long): HubNotice.Available {
                val notice = HubNotice.Available("1.$build.0", build)
                updates.eventFlow.emit(
                    HubUpdateStatus(canUpdate = true, auto = false, latest = HubRelease(notice.version, build, newer = true))
                )
                runCurrent()
                return notice
            }
            for (build in 1L..10L) {
                val notice = show(build)
                assertEquals(notice, model.hubNotice.value)
                model.dismissHubNotice(notice)
                runCurrent()
                assertNull(model.hubNotice.value)
            }
            for (build in 1L..2L) assertEquals(show(build), model.hubNotice.value)
            for (build in 3L..10L) {
                show(build)
                assertNull("build $build must remain dismissed", model.hubNotice.value)
            }

            // Repeated callbacks do not grow the set or promote an old key ahead of newer distinct keys.
            model.dismissHubNotice(HubNotice.Available("1.3.0", 3))
            runCurrent()
            val next = show(11)
            assertEquals(next, model.hubNotice.value)
            model.dismissHubNotice(next)
            runCurrent()
            assertEquals(show(3), model.hubNotice.value)
            for (build in 4L..11L) {
                show(build)
                assertNull("build $build must remain dismissed", model.hubNotice.value)
            }
        }
    }
}
