package jarvis.android.ui

import androidx.activity.compose.BackHandler
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import jarvis.android.MainActivity
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.data.SessionKey

/**
 * Pairing until a hub is paired; then the Agents list with, on top of it, a space page and / or a chat, or settings.
 * A chat opened from a space page goes back to that page; one opened from the list or a notification, to the list.
 */
@Composable
fun JarvisRoot(hub: Hub?, openSession: MutableState<SessionKey?>) {
    val conn = hub?.conn?.collectAsState()?.value
    // Not paired, the hub refused this device's token, or a pairing link was just opened: the pairing screen.
    if (hub == null || conn == ConnState.Unauthorized || MainActivity.pendingPair.value != null) {
        PairScreen(
            reason = if (hub != null && conn == ConnState.Unauthorized)
                "hub 不认这台手机的 token 了（在别的设备上撤销了，或者换了 hub），重新配对一下" else null,
        )
        return
    }
    var settings by rememberSaveable { mutableStateOf(false) }
    var space by rememberSaveable { mutableStateOf<String?>(null) }
    val open = openSession.value
    when {
        open != null -> {
            BackHandler { openSession.value = null }
            ChatScreen(hub, open, onBack = { openSession.value = null })
        }
        space != null -> {
            BackHandler { space = null }
            SpaceScreen(hub, space!!, onOpen = { openSession.value = it }, onBack = { space = null })
        }
        settings -> {
            BackHandler { settings = false }
            SettingsScreen(hub, onBack = { settings = false })
        }
        else -> AgentsScreen(
            hub, onOpen = { openSession.value = it }, onOpenSpace = { space = it }, onSettings = { settings = true },
        )
    }
}
