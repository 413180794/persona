package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * A terminal: a session's, or a pane's by its address ([sessionId] `pane:<pane id>`, docs/terminal.md「独立终端」).
 * [title] is what the page calls a pane with no session (its place: 空间 › 标签页 › title); a session's is its own.
 */
@Serializable @SerialName("jarvis.core.navigation.TerminalRoute")
data class TerminalRoute(val machineId: String, val sessionId: String, val title: String = "") : NavKey

/** 终端: every online machine's workspaces, tabs and panes, any of them opened as a terminal (feature terminal_panes). */
@Serializable @SerialName("persona.core.navigation.TerminalPanesRoute") data object TerminalPanesRoute : NavKey
