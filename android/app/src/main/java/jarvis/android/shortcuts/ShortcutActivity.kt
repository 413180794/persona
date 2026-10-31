package jarvis.android.shortcuts

import android.content.Intent
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import jarvis.android.JarvisApp
import jarvis.android.MainActivity
import jarvis.android.data.Hub
import jarvis.android.data.Interaction
import jarvis.android.fleet.FleetNotification
import jarvis.android.ui.JarvisTheme
import kotlinx.coroutines.launch

/**
 * 「批准第一个待处理」 (iOS `ApproveFirstIntent`): shows the oldest permission / plan and settles it after a tap (iOS
 * does it straight from Siri; here the user sees what they approve first). Not exported: only the launcher's own
 * shortcut, started on our behalf, reaches it, so a link from another app can never approve anything.
 */
class ShortcutActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge() // no black band under the status bar while the dialog floats over the launcher
        val hub = (application as JarvisApp).hub.value
        setContent {
            JarvisTheme {
                if (hub == null) Message(TITLE, ShortcutText.NOT_PAIRED, onOpen = { startActivity(main()) }) else Approve(hub)
            }
        }
    }

    private sealed interface Load {
        data object Loading : Load
        data class Ready(val ix: Interaction?) : Load
        data class Failed(val message: String) : Load
    }

    @Composable
    private fun Approve(hub: Hub) {
        var state by remember { mutableStateOf<Load>(Load.Loading) }
        var working by remember { mutableStateOf(false) }
        val scope = rememberCoroutineScope()
        val machines by hub.machines.collectAsState()
        LaunchedEffect(Unit) {
            // Read fresh from the hub (as the iOS intent does): the socket may have been asleep.
            state = try {
                Load.Ready(ShortcutText.pending(hub.api.interactions()))
            } catch (e: Exception) {
                Load.Failed(ShortcutText.failure(e))
            }
        }
        when (val s = state) {
            Load.Loading -> AlertDialog(
                onDismissRequest = { finish() },
                title = { Text(TITLE, maxLines = 1) },
                text = {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                        CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 3.dp)
                        Text("正在看有什么等你…")
                    }
                },
                confirmButton = {},
            )
            is Load.Failed -> Message(TITLE, s.message, onOpen = null)
            is Load.Ready -> {
                val ix = s.ix
                val res = ix?.let(ShortcutText::resolution)
                when {
                    ix == null -> Message(TITLE, ShortcutText.NO_PENDING, onOpen = null)
                    res == null -> Message(TITLE, ShortcutText.needsApp(ix.kind), onOpen = {
                        startActivity(FleetNotification.openSession(this, ix.sessionKey))
                    })
                    else -> AlertDialog(
                        onDismissRequest = { if (!working) finish() },
                        title = { Text(TITLE, maxLines = 1) },
                        text = {
                            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                Text(ShortcutText.ask(ix))
                                val machine = machines.firstOrNull { it.id == ix.machineId }?.displayName
                                val where = listOfNotNull(ix.sessionTitle?.takeIf { it.isNotBlank() }, machine)
                                if (where.isNotEmpty()) Text(where.joinToString(" · "), color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        },
                        confirmButton = {
                            TextButton(enabled = !working, onClick = {
                                working = true
                                scope.launch {
                                    val msg = try {
                                        hub.resolve(ix, res)
                                        ShortcutText.done(ix)
                                    } catch (e: Exception) {
                                        ShortcutText.failure(e)
                                    }
                                    Toast.makeText(this@ShortcutActivity, msg, Toast.LENGTH_SHORT).show()
                                    finish()
                                }
                            }) { Text(if (ix.kind == "plan") "批准" else "允许", maxLines = 1) }
                        },
                        dismissButton = { TextButton(enabled = !working, onClick = { finish() }) { Text("取消", maxLines = 1) } },
                    )
                }
            }
        }
    }

    @Composable
    private fun Message(title: String, text: String, onOpen: (() -> Unit)?) {
        AlertDialog(
            onDismissRequest = { finish() },
            title = { Text(title, maxLines = 1) },
            text = { Text(text) },
            confirmButton = {
                if (onOpen != null) TextButton(onClick = { onOpen(); finish() }) { Text("打开 Jarvis", maxLines = 1) }
                else TextButton(onClick = { finish() }) { Text("好", maxLines = 1) }
            },
            dismissButton = if (onOpen != null) ({ TextButton(onClick = { finish() }) { Text("取消", maxLines = 1) } }) else null,
        )
    }

    private fun main() = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)

    companion object {
        private const val TITLE = "批准第一个待处理"
        const val ACTION_APPROVE = "jarvis.android.shortcut.APPROVE"
    }
}
