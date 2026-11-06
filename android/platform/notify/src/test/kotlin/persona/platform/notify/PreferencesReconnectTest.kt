package persona.platform.notify

import android.Manifest
import android.app.Application
import android.app.NotificationManager
import android.os.Looper
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertFalse
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

/**
 * Preferences go null while the connection picks a new route (every reconnect, #4699). That means unknown, not off:
 * a pending request's notification stays, and only a real on → off change clears it.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class PreferencesReconnectTest {
    private val app = ApplicationProvider.getApplicationContext<Application>()
    private val request = Interaction("request", machineId = "machine", kind = "permission", summary = "Deploy?")
    private val prefs = MutableStateFlow<DeviceNotificationPreferences?>(DeviceNotificationPreferences(input = true))

    private fun notifier(): Notifier {
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow<List<Machine>>(emptyList())
            override val sessions = MutableStateFlow<List<Session>>(emptyList())
            override suspend fun refresh() = Unit
        }
        val preferences = object : NotificationPreferencesSource {
            override val preferences = prefs
            override suspend fun refreshPreferences() = prefs.value
        }
        return Notifier(app, sessions, preferences)
    }

    private fun emit(value: DeviceNotificationPreferences?) {
        prefs.value = value
        shadowOf(Looper.getMainLooper()).idle()
    }

    private fun shown() = app.getSystemService(NotificationManager::class.java).activeNotifications
        .any { it.tag == "interaction:${request.id}" }

    @Test fun aReconnectKeepsTheRequest() {
        val saved = prefs.value
        notifier().postInteraction(request)
        assertTrue(shown())
        emit(null)
        emit(saved)
        assertTrue(shown())
    }

    @Test fun turningRequestsOffStillClears() {
        notifier().postInteraction(request)
        assertTrue(shown())
        emit(DeviceNotificationPreferences(input = false))
        assertFalse(shown())
    }
}
