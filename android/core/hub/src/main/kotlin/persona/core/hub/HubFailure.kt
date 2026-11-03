package persona.core.hub

import persona.core.contracts.ErrorBody
import persona.core.contracts.JsonCodec
import java.io.IOException
import java.net.ConnectException
import java.net.NoRouteToHostException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import javax.net.ssl.SSLHandshakeException
import javax.net.ssl.SSLPeerUnverifiedException
import okhttp3.Response

enum class HubError {
    AddressNotFound, ConnectionRefused, Unreachable, Timeout, CertificateMismatch,
    PairingExpired, Incompatible, TlsRequired, HubMismatch, LocalNetworkPermission,
    NoNetwork, RateLimited, Server, Protocol, Transport,
}

class HubFailure(
    val kind: HubError,
    override val message: String = kind.name,
    val status: Int? = null,
    val code: String? = null,
    val retryable: Boolean = true,
    val delivered: String? = null,
    val retryAfterSeconds: Long? = null,
    cause: Throwable? = null,
) : IOException(message, cause) {
    companion object {
        fun from(error: Throwable): HubFailure {
            if (error is HubFailure) return error
            val causes = generateSequence(error) { it.cause }.take(16).toList()
            causes.filterIsInstance<HubFailure>().firstOrNull()?.let { return it }
            val kind = when {
                causes.any { it is SSLHandshakeException || it is SSLPeerUnverifiedException } -> HubError.CertificateMismatch
                causes.any { it is UnknownHostException } -> HubError.AddressNotFound
                causes.any { it is NoRouteToHostException } -> HubError.Unreachable
                causes.any { it is ConnectException } -> HubError.ConnectionRefused
                causes.any { it is SocketTimeoutException } -> HubError.Timeout
                else -> HubError.Transport
            }
            return HubFailure(kind, retryable = kind != HubError.CertificateMismatch, cause = error)
        }

        internal fun from(response: Response): HubFailure {
            val body = runCatching {
                JsonCodec.decodeFromString<ErrorBody>(response.peekBody(64 * 1024).string())
            }.getOrNull()
            val kind = when {
                response.code == 401 || response.code == 403 -> HubError.PairingExpired
                body?.code == "tls_required" -> HubError.TlsRequired
                body?.code == "incompatible" -> HubError.Incompatible
                response.code == 429 -> HubError.RateLimited
                else -> HubError.Server
            }
            val terminal = kind in setOf(HubError.PairingExpired, HubError.Incompatible, HubError.TlsRequired)
            return HubFailure(
                kind, body?.error ?: "HTTP ${response.code}", response.code, body?.code,
                retryable = !terminal && (body?.retryable ?: (response.code >= 500 || response.code in setOf(408, 429))),
                delivered = body?.delivered,
                retryAfterSeconds = response.header("Retry-After")?.toLongOrNull()?.coerceAtLeast(0),
            )
        }
    }
}
