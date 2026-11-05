package persona.feature.assistant

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext

/** Whether the drawer's 自动对话 is open (#3264), kept on this phone: folded until the user opens it. */
internal class AssistantPreferences(context: Context) {
    private val preferences = context.applicationContext.getSharedPreferences("jarvis-assistant", Context.MODE_PRIVATE)

    var autoOpen: Boolean
        get() = preferences.getBoolean(AUTO_OPEN, false)
        set(value) { preferences.edit().putBoolean(AUTO_OPEN, value).apply() }

    private companion object { const val AUTO_OPEN = "auto_conversations_open" }
}

/** 自动对话's open state as Compose state: read once, written through on every change. */
@Composable
internal fun rememberAutoOpen(): MutableState<Boolean> {
    val context = LocalContext.current
    val preferences = remember(context) { AssistantPreferences(context) }
    return remember(preferences) {
        object : MutableState<Boolean> {
            private val backing = mutableStateOf(preferences.autoOpen)
            override var value: Boolean
                get() = backing.value
                set(value) { backing.value = value; preferences.autoOpen = value }
            override fun component1() = value
            override fun component2(): (Boolean) -> Unit = { value = it }
        }
    }
}
