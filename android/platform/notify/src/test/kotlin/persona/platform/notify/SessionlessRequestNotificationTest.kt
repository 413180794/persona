package persona.platform.notify

import android.Manifest
import android.app.Application
import android.app.NotificationManager
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import persona.core.contracts.Interaction
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.DeviceNotificationPreferences
import persona.core.contracts.api.SessionsRepository
import persona.core.navigation.AppTab
import persona.core.navigation.NotificationRoute

/**
 * A card outside any session (`personad ask` in a plain terminal, #4515) has no conversation or terminal to open: its
 * notification opens Agents with that card open (#4808); the notice after a button on it opens Agents.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class SessionlessRequestNotificationTest {
    private val app = ApplicationProvider.getApplicationContext<Application>()
    private val script = Interaction("script", machineId = "machine", kind = "permission", summary = "Deploy?")
    private val agents = NotificationRoute(AppTab.Agents).uri()

    private fun notifier(): Notifier {
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow<List<Machine>>(emptyList())
            override val sessions = MutableStateFlow<List<Session>>(emptyList())
            override suspend fun refresh() = Unit
        }
        val preferences = object : NotificationPreferencesSource {
            override val preferences = MutableStateFlow<DeviceNotificationPreferences?>(DeviceNotificationPreferences())
            override suspend fun refreshPreferences() = preferences.value
        }
        return Notifier(app, sessions, preferences)
    }

    private fun shown() = app.getSystemService(NotificationManager::class.java).activeNotifications
        .single { it.tag == "interaction:${script.id}" }.notification

    @Test fun aTapOpensAgents() {
        notifier().postInteraction(script)
        val notification = shown()
        assertEquals(NotificationRoute.card("machine", "script").uri(), shadowOf(notification.contentIntent).savedIntent.data)
        assertEquals("persona://open?tab=agents&machine=machine&request=script", shadowOf(notification.contentIntent).savedIntent.data.toString())
        assertEquals(listOf("拒绝", "允许"), notification.actions.map { it.title.toString() })
    }

    @Test fun anExpiredAnswerOpensAgentsWithoutATerminalButton() {
        notifier().actionResult(script.id, script.sessionKey, NotificationActionResult.Expired, null)
        val notification = shown()
        assertEquals(agents, shadowOf(notification.contentIntent).savedIntent.data)
        assertTrue(notification.actions.isNullOrEmpty())
    }
}
