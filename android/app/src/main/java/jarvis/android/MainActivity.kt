package jarvis.android

import android.Manifest
import android.content.Intent
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import jarvis.android.data.PairingStore
import jarvis.android.data.SessionKey
import jarvis.android.ui.JarvisRoot
import jarvis.android.ui.JarvisTheme

class MainActivity : ComponentActivity() {
    /** A session to open, from a notification tap ("<machine>/<session>"). */
    private val openSession = mutableStateOf<SessionKey?>(null)
    /** A tab to show, from a notification tap (the assistant's questions open Jarvis). */
    private val openTab = mutableStateOf<String?>(null)

    private val askNotifications = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        handle(intent)
        if (Build.VERSION.SDK_INT >= 33) askNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        val app = application as JarvisApp
        setContent {
            JarvisTheme {
                val hub by app.hub.collectAsState()
                JarvisRoot(hub = hub, openSession = openSession, openTab = openTab)
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handle(intent)
    }

    override fun onStart() {
        super.onStart()
        (application as JarvisApp).hub.value?.kick()
        // The process may have started in the background (widget, tile, a notification button), where Android 12+
        // refuses the foreground service: start it now that the app is on screen (a no-op while it runs).
        if ((application as JarvisApp).hub.value != null) jarvis.android.service.HubService.start(this)
    }

    private fun handle(intent: Intent?) {
        intent?.getStringExtra(EXTRA_TAB)?.let { openTab.value = it }
        intent?.getStringExtra(EXTRA_SESSION)?.let { s ->
            val i = s.indexOf('/')
            if (i > 0) openSession.value = SessionKey(s.substring(0, i), s.substring(i + 1))
        }
        intent?.data?.toString()?.let { PairingStore.parse(it) }?.let { pendingPair.value = it }
    }

    companion object {
        const val EXTRA_SESSION = "session"
        const val EXTRA_TAB = "tab"
        /** A pairing link the activity was opened with, for the pairing screen to confirm. */
        val pendingPair = mutableStateOf<jarvis.android.data.Pairing?>(null)
    }
}
