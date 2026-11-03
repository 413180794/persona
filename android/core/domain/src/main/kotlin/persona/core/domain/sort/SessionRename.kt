package persona.core.domain.sort

import persona.core.contracts.Session
import kotlinx.coroutines.flow.Flow

/** A blank field leaves its remote value untouched. Tab names require an actual herdr pane. */
data class SessionRename(val title: String? = null, val tabName: String? = null) {
    val changed: Boolean get() = title != null || tabName != null
    fun apply(session: Session): Session = session.copy(title = title ?: session.title, slotName = tabName ?: session.slotName)
    fun matches(session: Session): Boolean = (title == null || session.title == title) &&
        (tabName == null || session.slotName == tabName)

    companion object {
        fun changes(session: Session, title: String?, tabName: String?): SessionRename = SessionRename(
            title?.trim()?.takeIf { it.isNotEmpty() && it != session.title },
            tabName?.trim()?.takeIf { !session.paneId.isNullOrBlank() && it.isNotEmpty() && it != session.slotName },
        )
    }
}

/** Shared by the Agents leaves, space detail and chat title for the active pairing. */
interface SessionRenamer {
    fun project(source: Flow<List<Session>>): Flow<List<Session>>
    suspend fun rename(session: Session, title: String?, tabName: String?): Boolean
}
