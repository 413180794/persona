package persona.feature.inbox

import androidx.navigation3.runtime.NavKey
import persona.core.navigation.InboxItemUiRoute

/**
 * Feed identity is stable even when the detail was opened at another inbox boundary. An extension's page of an item
 * (inbox_item) is that item's.
 */
internal fun isInboxItemSelected(detail: NavKey?, itemId: String, feedId: String?): Boolean = when (detail) {
    is LarkRoute -> feedId != null && detail.feedId == feedId
    is LarkMomentRoute -> detail.itemId == itemId
    is MailRoute -> feedId == null && detail.itemId == itemId
    is InboxItemUiRoute -> detail.itemId == itemId
    is InboxItemDetailRoute -> detail.itemId == itemId
    is InboxEventRoute -> detail.itemId == itemId
    is ConnectorThreadRoute -> detail.itemId == itemId
    else -> false
}
