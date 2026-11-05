package persona.feature.chat

import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatActions
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.SessionFiles
import persona.core.domain.sort.SessionRenamer
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent

/** The app supplies hub-scoped services and navigation without introducing feature-to-app dependencies. */
data class ChatChromeBindings(
    val covered: Boolean = false,
    val requestId: String? = null,
    val onFiles: (() -> Unit)? = null,
    val onGit: (() -> Unit)? = null,
    val gitLabel: String = "Git",
    val actions: ChatActions? = null,
    val connection: HubConnection? = null,
    val onOpenSession: ((SessionKey) -> Unit)? = null,
    val onConnectionDetails: (() -> Unit)? = null,
    val renamer: SessionRenamer? = null,
    /** Read-only access to the session's machine: the photos sent with messages (`<attached_images>`) load from it. */
    val files: SessionFiles? = null,
    /** Opens the session's machine's ports on the phone (docs/ports.md); null: port links open as plain pages. */
    val ports: persona.core.contracts.api.PortOpener? = null,
    /**
     * Opens a path from a message or a tool card in the session's files pane, scrolled to its line (`a.kt:12`) when
     * it names one; null: paths are not offered.
     */
    val onOpenPath: ((path: String, line: Int?) -> Unit)? = null,
    /** 「交给 Persona」 on a message: hands the given text to the main Persona conversation; null: not offered. */
    val onHandToPersona: ((String) -> Unit)? = null,
    /** The assistant conversation's tasks (docs/unified-entry.md); null for an agent's chat. */
    val tasks: persona.core.domain.tasks.TasksStore? = null,
    val onOpenTodo: ((String) -> Unit)? = null,
)

@EntryPoint
@InstallIn(SingletonComponent::class)
interface ChatRenameServices {
    fun sessionRenamer(): SessionRenamer
}
