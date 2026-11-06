package persona.feature.files

import android.content.res.Resources
import persona.core.hub.HubError
import persona.core.hub.HubFailure

/**
 * Why a folder, file or diff could not be read, for the page: the machine's own words when it gave a reason (macOS
 * keeping Desktop / Documents / Downloads from personad explains what to allow, a missing file names it), the connection otherwise.
 */
internal fun filesLoadError(resources: Resources, error: Throwable): String {
    val failure = (error as? HubFailure) ?: generateSequence(error) { it.cause }.take(8).filterIsInstance<HubFailure>().firstOrNull()
        ?: return resources.getString(R.string.files_error_connection)
    return when (failure.kind) {
        HubError.PairingExpired, HubError.HubMismatch -> resources.getString(R.string.files_error_pairing)
        HubError.AddressNotFound, HubError.ConnectionRefused, HubError.Unreachable, HubError.Timeout,
        HubError.NoNetwork, HubError.Transport -> resources.getString(R.string.files_error_unreachable)
        HubError.RateLimited -> resources.getString(R.string.files_error_rate_limited)
        else -> failure.message.takeIf { it.isNotBlank() && !it.startsWith("HTTP ") && it != failure.kind.name } ?: resources.getString(R.string.files_error_connection)
    }
}
