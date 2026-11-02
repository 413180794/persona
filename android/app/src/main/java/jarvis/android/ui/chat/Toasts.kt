package jarvis.android.ui.chat

import android.os.Handler
import android.os.Looper
import android.widget.Toast
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ProcessLifecycleOwner
import jarvis.android.JarvisApp
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.surface
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow

/** How a toast reads: its symbol and how long it stays. */
enum class ToastStyle { INFO, SUCCESS, ERROR }

/**
 * Short confirmations and errors (「已拷贝」), from any thread. While the app is in front they show in the app (iOS's
 * toast: a capsule under the status bar with a symbol, [ToastHost]); otherwise as the system's toast.
 */
object Toasts {
    data class Message(val text: String, val style: ToastStyle, val id: Long)

    private val _current = MutableStateFlow<Message?>(null)
    val current = _current.asStateFlow()
    @Volatile private var hosts = 0

    fun show(text: String, style: ToastStyle = ToastStyle.INFO) {
        val front = runCatching { ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED) }.getOrDefault(false)
        if (hosts > 0 && front) {
            _current.value = Message(text, style, System.nanoTime())
            return
        }
        val app = runCatching { JarvisApp.instance }.getOrNull() ?: return
        Handler(Looper.getMainLooper()).post {
            Toast.makeText(app, text, if (style == ToastStyle.ERROR) Toast.LENGTH_LONG else Toast.LENGTH_SHORT).show()
        }
    }

    internal fun dismiss(id: Long) { if (_current.value?.id == id) _current.value = null }
    internal fun hosted(on: Boolean) { hosts += if (on) 1 else -1 }
}

/** Where in-app toasts show: over everything, under the status bar. */
@Composable
fun ToastHost(modifier: Modifier = Modifier) {
    DisposableEffect(Unit) { Toasts.hosted(true); onDispose { Toasts.hosted(false) } }
    val msg by Toasts.current.collectAsState()
    LaunchedEffect(msg?.id) {
        val m = msg ?: return@LaunchedEffect
        delay(if (m.style == ToastStyle.ERROR) 3500 else 2000)
        Toasts.dismiss(m.id)
    }
    // The last one stays drawn while it fades out.
    val shown = remember(msg?.id) { msg }
    Box(modifier.fillMaxWidth().statusBarsPadding().padding(top = JSize.shellBar + JSpace.xs), contentAlignment = Alignment.TopCenter) {
        AnimatedVisibility(msg != null, enter = fadeIn() + slideInVertically { -it / 2 }, exit = fadeOut() + slideOutVertically { -it / 2 }) {
            val m = shown ?: return@AnimatedVisibility
            val c = JTheme.colors
            Row(
                Modifier.padding(horizontal = JSpace.xl).widthIn(max = JSize.drawerMax).heightIn(min = JSize.wellL)
                    .surface(CircleShape, floating = true).padding(horizontal = JSpace.l, vertical = JSpace.s),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s),
            ) {
                when (m.style) {
                    ToastStyle.SUCCESS -> Icon(JIcons.success, null, Modifier.size(JSize.markL), tint = c.positive)
                    ToastStyle.ERROR -> Icon(JIcons.error, null, Modifier.size(JSize.markL), tint = c.danger)
                    ToastStyle.INFO -> Icon(JIcons.info, null, Modifier.size(JSize.markL), tint = c.secondaryText)
                }
                Text(m.text, style = JTheme.type.secondaryMedium, color = c.label, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}
