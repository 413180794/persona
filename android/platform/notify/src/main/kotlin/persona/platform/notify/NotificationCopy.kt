package persona.platform.notify

import android.content.Context
import androidx.annotation.StringRes
import persona.core.contracts.Interaction
import persona.core.contracts.api.ConnectionState
import persona.core.designsystem.resolve
import persona.core.domain.toolDisplayName

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

/**
 * The line under a request notification's title and the text it expands to, in [context]'s language: what a glance at
 * the lock screen should tell. An MCP tool reads 「github · create issue」 as on the Agents list, not its raw
 * `mcp__github__create_issue`; an MCP form says what it asks for (the server's message) instead of only that one waits.
 */
internal fun requestCopy(context: Context, ix: Interaction): Pair<String, String> {
    val headline = ix.copy(toolName = toolDisplayName(ix.toolName)).headline.resolve(context)
    val detail = when (ix.kind) {
        "question" -> ix.questions.orEmpty().joinToString("\n") { it.question }
        "plan" -> ix.plan ?: headline
        // The headline already carries the summary; the summary alone would drop which tool asks.
        "permission" -> headline
        "screen" -> ix.dialog?.let { "${it.question}\n${it.detail.orEmpty()}" } ?: ix.screen.orEmpty()
        "elicitation" -> elicitationMessage(ix)?.let { context.getString(R.string.notify_body_elicitation, it) } ?: headline
        else -> headline
    }
    val line = if (ix.kind == "elicitation") detail.lineSequence().first() else headline
    return line to detail
}

private fun elicitationMessage(ix: Interaction): String? =
    listOf(ix.summary, ix.elicitation?.message).firstNotNullOfOrNull { it?.trim()?.takeIf(String::isNotEmpty) }
