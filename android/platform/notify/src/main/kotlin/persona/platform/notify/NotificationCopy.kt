package persona.platform.notify

import androidx.annotation.StringRes
import persona.core.contracts.api.ConnectionState

@StringRes
internal fun ongoingTitle(state: ConnectionState): Int = when (state) {
    ConnectionState.Online -> R.string.notify_connection_online
    ConnectionState.Connecting -> R.string.notify_connection_connecting
    ConnectionState.Updating -> R.string.notify_connection_updating
    ConnectionState.WaitingForNetwork -> R.string.notify_connection_waiting_network
    ConnectionState.NeedsLocalNetworkPermission -> R.string.notify_connection_local_network
    ConnectionState.CertificateMismatch -> R.string.notify_connection_certificate
    ConnectionState.Revoked -> R.string.notify_connection_revoked
}

/** The title and the detail of the notice that replaces a request's notification, as string resources. */
internal fun actionResultCopy(result: NotificationActionResult): Pair<Int, Int>? = when (result) {
    NotificationActionResult.Expired -> R.string.notify_result_expired_title to R.string.notify_result_expired_detail
    NotificationActionResult.Gone -> R.string.notify_result_gone_title to R.string.notify_result_gone_detail
    NotificationActionResult.Locked -> R.string.notify_result_locked_title to R.string.notify_result_locked_detail
    NotificationActionResult.Invalid -> R.string.notify_result_invalid_title to R.string.notify_result_invalid_detail
    NotificationActionResult.Duplicate -> R.string.notify_result_unconfirmed_title to R.string.notify_result_duplicate_detail
    NotificationActionResult.ReadOnly -> R.string.notify_result_readonly_title to R.string.notify_result_readonly_detail
    NotificationActionResult.Submitted -> null
}
