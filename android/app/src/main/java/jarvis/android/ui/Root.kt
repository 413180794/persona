package jarvis.android.ui

import androidx.activity.compose.BackHandler
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import jarvis.android.MainActivity
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.data.PairingEngine
import jarvis.android.data.SessionKey
import jarvis.android.ui.machines.EngineScreen
import jarvis.android.ui.shell.ShellRoot

/**
 * Pairing until a hub is paired (and its machine's engine chosen); then the ChatGPT-style shell (docs/android.md「外壳」, as on the iPhone): 助手 and
 * Agents under one switch, the drawer, 设置 as a sheet. A notification opens its chat ([openSession]); shortcuts and
 * the widget pick a page ([openTab]).
 */
@Composable
fun JarvisRoot(hub: Hub?, openSession: MutableState<SessionKey?>, openTab: MutableState<String?> = mutableStateOf(null)) {
    val conn = hub?.conn?.collectAsState()?.value
    // Not paired, the hub refused this device's token, or a pairing link was just opened: the pairing screen.
    if (hub == null || conn == ConnState.Unauthorized || MainActivity.pendingPair.value != null) {
        PairScreen(
            reason = if (hub != null && conn == ConnState.Unauthorized)
                "hub 不认这台手机的 token 了（在别的设备上撤销了，或者换了 hub），重新配对一下" else null,
        )
        return
    }
    // Right after pairing: which engine the hub's own machine uses (docs/engines.md). One must be chosen: back does
    // not skip the step (2026-11-01 user).
    PairingEngine.pending.value?.let { m ->
        BackHandler {}
        EngineScreen(hub, m.id, onDone = { PairingEngine.pending.value = null }, pairing = true, fallback = m)
        return
    }
    ShellRoot(hub, openSession, openTab)

    // A phone tool that changes data (a calendar event) waits for the user: ask on top of whatever is open.
    val runner = jarvis.android.JarvisApp.instance.phoneTools
    val asks by runner.pending.collectAsState()
    asks.firstOrNull()?.let { p ->
        AlertDialog(
            onDismissRequest = {},
            title = { Text("Claude 想在手机上操作") },
            text = { Text(p.summary) },
            confirmButton = { TextButton({ runner.decide(p.id, true) }) { Text("允许", maxLines = 1) } },
            dismissButton = { TextButton({ runner.decide(p.id, false) }) { Text("拒绝", maxLines = 1) } },
        )
    }
}
