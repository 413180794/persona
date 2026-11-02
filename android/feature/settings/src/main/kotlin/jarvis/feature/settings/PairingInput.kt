package jarvis.feature.settings

import jarvis.core.contracts.api.*
import jarvis.core.domain.Pairing
import java.net.*
import javax.net.ssl.SSLException

object PairingInput {
    fun parse(link: String): PairingRequest {
        val uri = runCatching { URI(link.trim()) }.getOrNull()
        if (uri?.scheme != "jarvis" || uri.rawAuthority != "pair") invalid()
        val parsed = Pairing.parse(link) ?: invalid()
        val keys = uri.rawQuery.orEmpty().split('&').map { it.substringBefore('=') }
        if (keys.filter { it != "alt" }.groupingBy { it }.eachCount().any { it.value > 1 }) invalid()
        if ("tls" in keys && parsed.tls == null) invalid()
        return validate(PairingRequest(parsed.url, parsed.token, parsed.hub, parsed.fp, parsed.tls, parsed.alt))
    }

    fun manual(address: String, token: String): PairingRequest =
        if (address.trim().startsWith("jarvis:")) parse(address) else validate(PairingRequest(address.trim(), token.trim()))

    private fun validate(request: PairingRequest): PairingRequest {
        if (request.token.isBlank() || request.tls?.let { it !in 1..65535 } == true) invalid()
        (listOf(request.url) + request.alt).forEach { address ->
            val uri = runCatching { URI(address) }.getOrNull() ?: invalid()
            if (uri.scheme !in listOf("http", "https") || uri.host.isNullOrBlank() ||
                uri.userInfo != null || uri.query != null || uri.fragment != null || uri.port > 65535 || uri.port == 0 || uri.port < -1) invalid()
        }
        val primary = URI(request.url)
        if (primary.scheme == "http" && request.fp == null && !localHost(primary.host)) {
            throw PairingException(PairingFailure.TlsRequired)
        }
        return request
    }
    private fun localHost(host: String): Boolean {
        val value = host.removePrefix("[").removeSuffix("]").lowercase()
        if (value == "localhost" || value.endsWith(".local")) return true
        if (':' in value) {
            val address = runCatching { InetAddress.getByName(value) }.getOrNull() ?: return false
            return address.isLoopbackAddress || address.isLinkLocalAddress || address.isSiteLocalAddress ||
                (address.address[0].toInt() and 0xfe) == 0xfc
        }
        val bytes = value.split('.').map { it.toIntOrNull() ?: return false }
        if (bytes.size != 4 || bytes.any { it !in 0..255 }) return false
        return bytes[0] == 10 || bytes[0] == 127 || (bytes[0] == 172 && bytes[1] in 16..31) ||
            (bytes[0] == 192 && bytes[1] == 168) || (bytes[0] == 169 && bytes[1] == 254) ||
            (bytes[0] == 100 && bytes[1] in 64..127)
    }
    private fun invalid(): Nothing = throw PairingException(PairingFailure.InvalidCode)
}

fun pairingFailure(error: Throwable): PairingFailure = when (error) {
    is PairingException -> error.reason
    is UnknownHostException -> PairingFailure.Dns
    is ConnectException -> PairingFailure.Refused
    is NoRouteToHostException -> PairingFailure.Unreachable
    is SocketTimeoutException -> PairingFailure.Timeout
    is SSLException -> PairingFailure.Certificate
    is SecurityException -> PairingFailure.LocalPermission
    else -> error.cause?.takeIf { it !== error }?.let(::pairingFailure) ?: PairingFailure.Unknown
}

fun PairingFailure.explanation(): String = when (this) {
    PairingFailure.InvalidCode -> "不是有效的 Jarvis 配对链接，请检查地址和配对码。"
    PairingFailure.Dns -> "找不到这个地址，请检查域名或重新扫码。"
    PairingFailure.Refused -> "hub 没有在这个端口上监听，请确认 hub 正在运行。"
    PairingFailure.Unreachable -> "当前网络到不了 hub，请切换网络后重试。"
    PairingFailure.Timeout -> "连不上 hub。在外面时检查路由器是否放行 TLS 端口的 IPv6 入站；在家里检查是否连着同一个网络。"
    PairingFailure.LocalPermission -> "在家里连 hub，需要允许 Jarvis 访问本地网络。"
    PairingFailure.Certificate -> "证书或 hub 身份不符。请在 hub 机器上重新运行 jarvisd hub pair，再扫码。"
    PairingFailure.Expired -> "配对码已失效或已被用过，请重新运行 jarvisd hub pair。"
    PairingFailure.Incompatible -> "当前连接组件或 hub 不支持这种配对方式，请更新后重试。"
    PairingFailure.TlsRequired -> "这个地址需要加密连接，请使用 HTTPS 地址重新配对。"
    PairingFailure.Unknown -> "连接没有完成，请检查网络后重试，或查看连接诊断。"
}

internal enum class PairingRecovery(val label: String) {
    Retry("重新探测"), Scan("重新扫码"), Permission("去授权"), Update("更新后重试")
}

internal fun PairingFailure.recovery(): PairingRecovery = when (this) {
    PairingFailure.LocalPermission -> PairingRecovery.Permission
    PairingFailure.InvalidCode, PairingFailure.Certificate, PairingFailure.Expired,
    PairingFailure.TlsRequired -> PairingRecovery.Scan
    PairingFailure.Incompatible -> PairingRecovery.Update
    PairingFailure.Dns, PairingFailure.Refused, PairingFailure.Unreachable,
    PairingFailure.Timeout, PairingFailure.Unknown -> PairingRecovery.Retry
}
