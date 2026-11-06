package persona.platform.notify

import android.Manifest
import android.app.Application
import android.app.NotificationManager
import androidx.core.app.NotificationCompat
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

/** A tap that never reached the hub (#5008) brings the request back with its buttons, saying it was not sent. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class NotSentNotificationTest {
    private val app = ApplicationProvider.getApplicationContext<Application>()
    private val request = Interaction("request", machineId = "machine", sessionId = "session", kind = "permission", summary = "Deploy?")

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
        .single { it.tag == "interaction:${request.id}" }.notification

    @Test fun theRequestComesBackWithItsButtons() {
        val notifier = notifier()
        notifier.pending(request.id, request.sessionKey, persona.core.designsystem.PendingStatus.Verifying, null)
        notifier.notSent(request.id, request, request.sessionKey, null)
        val notification = shown()
        assertEquals(listOf("拒绝", "允许"), notification.actions.map { it.title.toString() })
        assertEquals("没连上 hub，没发出去，可以再点一次。", notification.extras.getCharSequence(NotificationCompat.EXTRA_TEXT).toString())
    }

    @Test fun withoutTheRequestItSaysNotSentInsteadOfDontSubmitAgain() {
        notifier().notSent(request.id, null, request.sessionKey, null)
        val notification = shown()
        assertEquals("没发出去", notification.extras.getCharSequence(NotificationCompat.EXTRA_TITLE).toString())
        assertTrue(notification.actions.isNullOrEmpty())
    }
}
