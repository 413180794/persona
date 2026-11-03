package persona.feature.files

import persona.core.hub.HubError
import persona.core.hub.HubFailure

/**
 * Why a folder, file or diff could not be read, for the page: the machine's own words when it gave a reason (macOS
 * keeping 桌面 / 文稿 / 下载 from jarvisd explains what to allow, a missing file names it), the connection otherwise.
 */
internal fun filesLoadError(error: Throwable): String {
    val failure = (error as? HubFailure) ?: generateSequence(error) { it.cause }.take(8).filterIsInstance<HubFailure>().firstOrNull()
        ?: return "请检查连接后重试。"
    return when (failure.kind) {
        HubError.PairingExpired, HubError.HubMismatch -> "配对已失效，请重新扫码。"
        HubError.AddressNotFound, HubError.ConnectionRefused, HubError.Unreachable, HubError.Timeout,
        HubError.NoNetwork, HubError.Transport -> "连不上 hub，请检查连接后重试。"
        HubError.RateLimited -> "请求太频繁，请稍后重试。"
        else -> failure.message.takeIf { it.isNotBlank() && !it.startsWith("HTTP ") && it != failure.kind.name } ?: "请检查连接后重试。"
    }
}
