package jarvis.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.LibraryBooks
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.voice.voicePlugin
import kotlinx.coroutines.launch
import java.time.Instant

/** A conversation's name in the history: its title, else its first words; a hand-off shows what it was about. */
fun Session.conversationTitle(): String {
    val t = title.takeIf { it.isNotBlank() && it != "Claude Code" } ?: lastText?.lineSequence()?.firstOrNull { it.isNotBlank() }?.take(30) ?: return "对话"
    // Started from 交给 Jarvis: the first message is a <jarvis-context title="…"> tag (claude names it after it).
    Regex("""<jarvis-context[^>]*title="([^"]*)"""").find(t)?.let { m ->
        return "交给 Jarvis：" + m.groupValues[1].replace("&quot;", "\"").replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&")
    }
    return if (t.startsWith("<jarvis-context")) "交给 Jarvis" else t
}

/** Focuses [focus] (and opens the keyboard) once when [on] turns true, then tells the caller it is done. */
@Composable
internal fun FocusComposer(on: Boolean, focus: androidx.compose.ui.focus.FocusRequester, onTaken: () -> Unit) {
    val keyboard = androidx.compose.ui.platform.LocalSoftwareKeyboardController.current
    LaunchedEffect(on) {
        if (!on) return@LaunchedEffect
        kotlinx.coroutines.delay(300) // after the tab's first layout: the text field has to be attached
        runCatching { focus.requestFocus(); keyboard?.show() }
        onTaken()
    }
}
