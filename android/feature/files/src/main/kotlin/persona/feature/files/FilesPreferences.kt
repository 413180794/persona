package persona.feature.files

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext

/** The browser's "Show hidden files", kept on this phone (not with the pairing): it is how you like to browse, on any hub. */
internal class FilesPreferences(context: Context) {
    private val preferences = context.applicationContext.getSharedPreferences("persona-files", Context.MODE_PRIVATE)

    var showHidden: Boolean
        get() = preferences.getBoolean(SHOW_HIDDEN, false)
        set(value) { preferences.edit().putBoolean(SHOW_HIDDEN, value).apply() }

    private companion object { const val SHOW_HIDDEN = "show_hidden" }
}

/** "Show hidden files" as Compose state: read once, written through on every change. */
@Composable
internal fun rememberShowHidden(): MutableState<Boolean> {
    val context = LocalContext.current
    val preferences = remember(context) { FilesPreferences(context) }
    return remember(preferences) {
        object : MutableState<Boolean> {
            private val backing = mutableStateOf(preferences.showHidden)
            override var value: Boolean
                get() = backing.value
                set(value) { backing.value = value; preferences.showHidden = value }
            override fun component1() = value
            override fun component2(): (Boolean) -> Unit = { value = it }
        }
    }
}
