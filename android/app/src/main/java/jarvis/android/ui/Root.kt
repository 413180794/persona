package jarvis.android.ui

import androidx.activity.compose.BackHandler
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import jarvis.android.data.Hub
import jarvis.android.data.SessionKey

/** Pairing until a hub is paired; then the Agents list, a chat, or settings. */
@Composable
fun JarvisRoot(hub: Hub?, openSession: MutableState<SessionKey?>) {
    if (hub == null) {
        PairScreen()
        return
    }
    var settings by rememberSaveable { mutableStateOf(false) }
    val open = openSession.value
    when {
        open != null -> {
            BackHandler { openSession.value = null }
            ChatScreen(hub, open, onBack = { openSession.value = null })
        }
        settings -> {
            BackHandler { settings = false }
            SettingsScreen(hub, onBack = { settings = false })
        }
        else -> AgentsScreen(hub, onOpen = { openSession.value = it }, onSettings = { settings = true })
    }
}
