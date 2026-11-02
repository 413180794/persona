package jarvis.feature.chat

import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.ChatActions
import jarvis.core.contracts.api.HubConnection
import jarvis.core.domain.sort.SessionRenamer
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
)

@EntryPoint
@InstallIn(SingletonComponent::class)
interface ChatRenameServices {
    fun sessionRenamer(): SessionRenamer
}
