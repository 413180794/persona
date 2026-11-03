package persona.core.navigation

import androidx.compose.runtime.compositionLocalOf
import androidx.navigation3.runtime.NavKey

/**
 * The page a tab shows in its detail pane (a conversation, a mail, a settings page), or null. Lists mark the row it
 * belongs to, so beside a detail the list shows which item is open; the shell provides it for each tab.
 */
val LocalDetailKey = compositionLocalOf<NavKey?> { null }
