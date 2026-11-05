package persona.platform.phonetools

import android.app.NotificationManager
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(AndroidJUnit4::class)
class PhoneConfirmationsTest {
    private val context get() = ApplicationProvider.getApplicationContext<Context>()

    /**
     * minSdk is 30: the confirmation must post there too (setAuthenticationRequired is API 31, #4221). The unlock flag
     * does not exist on API 30, so only posting is checked there.
     */
    @Test @Config(sdk = [30]) fun postsTheConfirmationOnApi30() = postsAndDenies(expectAuthentication = false)

    @Test @Config(sdk = [35]) fun allowNeedsAnUnlockFromApi31() = postsAndDenies(expectAuthentication = true)

    private fun postsAndDenies(expectAuthentication: Boolean) = runTest {
        val confirmations = PhoneConfirmations(context)
        val answer = async { confirmations.confirm { "Add an event" } }
        runCurrent()
        val token = confirmations.pending.value?.token
        assertNotNull("confirmation should be pending", token)
        val posted = shadowOf(context.getSystemService(NotificationManager::class.java)).allNotifications
        assertEquals(1, posted.size)
        val actions = posted.single().actions
        assertEquals(2, actions.size)
        if (expectAuthentication) {
            assertFalse(actions[0].isAuthenticationRequired)
            assertTrue(actions[1].isAuthenticationRequired)
        }
        confirmations.resolve(token!!, false)
        assertFalse(answer.await())
        confirmations.close()
    }
}
