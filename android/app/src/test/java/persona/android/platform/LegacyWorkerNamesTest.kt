package persona.android.platform

import org.junit.Assert.assertTrue
import org.junit.Test

class LegacyWorkerNamesTest {
    @Test fun queuedWorkersKeepLoadableLegacyNames() {
        assertTrue(ShareRetryWorker::class.java.isAssignableFrom(Class.forName("jarvis.android.platform.ShareRetryWorker")))
        assertTrue(persona.platform.phonetools.PhoneNotificationWorker::class.java.isAssignableFrom(
            Class.forName("jarvis.platform.phonetools.PhoneNotificationWorker")))
    }
}
