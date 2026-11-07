package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * A terminal: a session's, or a pane's by its address ([sessionId] `pane:<pane id>`, docs/integrations/terminal.md "Standalone terminal").
 * [title] is what the page calls a pane with no session (its place: space › tab › title); a session's is its own.
 */
@Serializable @SerialName("persona.core.navigation.TerminalRoute")
data class TerminalRoute(val machineId: String, val sessionId: String, val title: String = "") : NavKey

/** Terminal: every online machine's workspaces, tabs and panes, any of them opened as a terminal (feature terminal_panes). */
@Serializable @SerialName("persona.core.navigation.TerminalPanesRoute") data object TerminalPanesRoute : NavKey
