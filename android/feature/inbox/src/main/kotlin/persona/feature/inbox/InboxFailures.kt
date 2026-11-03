package persona.feature.inbox

import persona.core.designsystem.operationFeedback
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import java.io.IOException

/**
 * Why something did not go through, for 「没草拟出来：…」「没交过去：…」: the hub's own words when it gave a reason
 * (a draft the model could not write), the usual operation feedback for the connection, the local reason otherwise.
 */
internal fun failureReason(error: Throwable): String {
    if (error !is IOException) return error.message?.takeIf { it.isNotBlank() } ?: "请稍后重试"
    val failure = HubFailure.from(error)
    // Only the hub's own refusals carry words worth showing; pairing, certificates and the network have their own.
    val hubSaid = failure.message.takeIf {
        failure.kind in setOf(HubError.Server, HubError.Protocol) && it.isNotBlank() && it != failure.kind.name && !it.startsWith("HTTP ")
    }
    return hubSaid ?: operationFeedback(error).message
}
