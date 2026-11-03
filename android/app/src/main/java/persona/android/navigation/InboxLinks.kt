package persona.android.navigation

import androidx.navigation3.runtime.NavKey
import persona.core.navigation.InboxItemRoute
import persona.core.navigation.LarkChatRoute
import persona.feature.inbox.InboxRecord
import persona.feature.inbox.LarkRoute
import persona.feature.inbox.MailRoute

/**
 * Deep links name an inbox item or a Lark chat; open them as the inbox list would.
 * Null means the records do not show the target yet; [loaded] says no later refresh will.
 */
internal fun inboxDestination(key: NavKey, records: List<InboxRecord>, loaded: Boolean): InboxDestination? = when (key) {
    is InboxItemRoute -> records.firstOrNull { it.item.id == key.itemId }?.let { record ->
        InboxDestination.Open(record.feedId?.let { LarkRoute(it, record.item.id, record.startPosition) } ?: MailRoute(record.item.id))
    } ?: InboxDestination.Missing.takeIf { loaded }
    // A chat without a current item still opens at its latest messages.
    is LarkChatRoute -> records.firstOrNull { it.feedId == key.chatId }
        ?.let { InboxDestination.Open(LarkRoute(key.chatId, it.item.id, it.startPosition)) }
        ?: InboxDestination.Open(LarkRoute(key.chatId)).takeIf { loaded }
    else -> null
}

internal sealed interface InboxDestination {
    data class Open(val key: NavKey) : InboxDestination
    data object Missing : InboxDestination
}
