package persona.feature.settings

import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.runtime.*
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import kotlinx.coroutines.launch

/** Owned by the injected navigation adapter, so removing/reinserting its composition does not discard local UI. */
internal class NativeExtensionLocal {
    val list = LazyListState()
    val roots = mutableStateMapOf<String, String>()
    val rootAttempts = mutableMapOf<String, Any>()
    var rejected by mutableStateOf(false)
    var controlPress: NativePress? = null
    var discardStepper: suspend (String, persona.core.contracts.stream.NativeStreamDraft?) -> Unit = { _, _ -> }
    var query by mutableStateOf<String?>(null)
    var attached = false
    var focused: Pair<String, String>? = null
    val focus = mutableMapOf<Pair<String, String>, androidx.compose.ui.focus.FocusRequester>()
    val reviews = mutableMapOf<String, CodeReviewMemory>()
    private val whitespace = mutableStateOf(false)
    fun review(key: String) = reviews.getOrPut(key) { CodeReviewMemory(whitespace) }
    private val slots = mutableMapOf<Pair<String, String>, MutableState<*>>()
    private val scrolls = mutableMapOf<Pair<String, String>, androidx.compose.foundation.ScrollState>()
    fun scroll(key: String, name: String) = scrolls.getOrPut(key to name) { androidx.compose.foundation.ScrollState(0) }
    @Suppress("UNCHECKED_CAST")
    fun <T> slot(key: String, name: String, initial: () -> T): MutableState<T> =
        slots.getOrPut(key to name) { mutableStateOf(initial()) } as MutableState<T>
    fun clear() {
        roots.clear(); rootAttempts.clear(); controlPress = null; rejected = false; query = null; reviews.clear(); slots.clear(); scrolls.clear(); focus.clear(); focused = null
    }
    fun prune(keys: Set<String>) {
        slots.keys.removeAll { it.first !in keys }
        scrolls.keys.removeAll { it.first !in keys }
        reviews.keys.retainAll(keys)
        if (focused?.first !in keys) focused = null
    }
}

/** A renderer remount restores only the input that actually held focus; blur/tabs do not steal it back. */
@Composable
internal fun androidx.compose.ui.Modifier.nativeInputFocus(local: NativeExtensionLocal?, key: String, slot: String = "input"): androidx.compose.ui.Modifier {
    if (local == null) return this
    val id = key to slot
    val requester = remember(local, id) { androidx.compose.ui.focus.FocusRequester() }
    val hasFocus = remember(local, id) { booleanArrayOf(false) }
    val scope = rememberCoroutineScope()
    DisposableEffect(local, id) {
        local.focus[id] = requester
        onDispose { local.focus.remove(id) }
    }
    return this.then(androidx.compose.ui.Modifier.focusRequester(requester).onFocusChanged { state ->
        hasFocus[0] = state.isFocused
        if (state.isFocused && local.attached) local.focused = id
        else if (local.focused == id) scope.launch {
            // Detach reports blur before effects dispose. A removed input cancels this job;
            // an actual blur on a retained input clears the saved target next frame.
            withFrameNanos { }
            if (!hasFocus[0] && local.attached && local.focused == id) local.focused = null
        }
    })
}
