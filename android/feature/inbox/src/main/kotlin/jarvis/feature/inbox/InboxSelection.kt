package jarvis.feature.inbox

import androidx.navigation3.runtime.NavKey

/** Feed identity is stable even when the detail was opened at another inbox boundary. */
internal fun isInboxItemSelected(detail: NavKey?, itemId: String, feedId: String?): Boolean = when (detail) {
    is LarkRoute -> feedId != null && detail.feedId == feedId
    is MailRoute -> feedId == null && detail.itemId == itemId
    else -> false
}
