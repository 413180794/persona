package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * An extension's sign-in as the apps see it (#4507, `GET /api/v1/extensions/{id}/-/auth`, also in the list and the
 * detail): `unknown` (it is not running), `signed_out`, `pending` (waiting for the user to confirm), `signed_in`,
 * `expired` or `error` ([message] says why). [page] is the extension's own sign-in page for older apps: this one leaves
 * it out of the pages.
 */
@Serializable
data class ExtensionAuth(
    val title: String = "",
    val description: String? = null,
    val page: String? = null,
    val state: String = "",
    val account: String? = null,
    val message: String? = null,
    @SerialName("expires_at") val expiresAt: String? = null,
    @SerialName("checked_at") val checkedAt: String? = null,
) {
    val signedIn: Boolean get() = state == SignedIn
    /** Not signed in, or no longer: the list says so and installing leads to signing in. */
    val needsSignIn: Boolean get() = state == SignedOut || state == Expired

    companion object {
        const val Unknown = "unknown"
        const val SignedOut = "signed_out"
        const val Pending = "pending"
        const val SignedIn = "signed_in"
        const val Expired = "expired"
        const val Error = "error"
    }
}

/** A command-line program an extension's version runs with (#4507): [reused] is the copy already on the hub's machine. */
@Serializable
data class ExtensionToolView(
    val name: String = "",
    val title: String? = null,
    val version: String = "",
    val reused: Boolean = false,
)
