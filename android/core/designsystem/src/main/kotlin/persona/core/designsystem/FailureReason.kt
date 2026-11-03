package persona.core.designsystem

import persona.core.hub.HubError
import persona.core.hub.HubFailure
import kotlinx.serialization.SerializationException
import java.io.IOException

/**
 * Why a call failed, for the part after「失败：」: the hub's own words when it answered with a reason (409 不能开始,
 * 400 未知引擎, 503 机器离线), 连不上 hub when it did not answer, the app's usual sentence for pairing and
 * certificate trouble, an exception's own message for the app's typed failures.
 */
fun failureReason(error: Throwable): String {
    if (error is IOException) {
        val failure = HubFailure.from(error)
        if (failure.status != null && failure.kind != HubError.PairingExpired) {
            failure.message.takeIf { it.isNotBlank() && !it.startsWith("HTTP ") }?.let { return it }
        }
        return when (failure.kind) {
            HubError.NoNetwork, HubError.AddressNotFound, HubError.ConnectionRefused, HubError.Unreachable,
            HubError.Timeout, HubError.Transport -> "连不上 hub"
            else -> operationFeedback(error).message.removeSuffix("。")
        }
    }
    if (error is SerializationException) return "hub 的回答读不懂"
    return error.message?.takeIf { it.isNotBlank() } ?: "出错了"
}
