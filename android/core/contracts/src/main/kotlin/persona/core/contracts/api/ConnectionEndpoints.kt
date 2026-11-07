package persona.core.contracts.api

import persona.core.contracts.CoreTextException
import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import kotlinx.coroutines.flow.StateFlow

/**
 * Route choices made on this phone for one pairing (docs/android/android.md "Connection addresses"). The hub's endpoint list never
 * overwrites them, and they are erased together with the pairing (unpair, revocation, pairing again).
 */
data class EndpointOverrides(
    /** Canonical base URLs the user added (`scheme://host:port`, explicit port, IPv6 in brackets), oldest first. */
    val manual: List<String> = emptyList(),
    /** Canonical keys of candidates from any source that the connection must not try. */
    val disabled: Set<String> = emptySet(),
    /**
     * Canonical keys in the order the user arranged them; null until the user reorders, while the default rule
     * applies. Keys no longer among the candidates are skipped; candidates missing from it follow at the end.
     */
    val order: List<String>? = null,
)

/** Where a candidate came from: the pairing link, the hub's authenticated list, or this phone. */
enum class EndpointSource { Pairing, Hub, Manual }

/** One candidate as settings shows it. [url] is passed back unchanged to the [ConnectionEndpoints] calls. */
data class EndpointEntry(
    val url: String,
    /** `ipv6` / `name` / `lan` / `ipv4` / `unknown`, as in the wire model. */
    val kind: String,
    val source: EndpointSource,
    /** Disabled candidates keep their place in the list but are never tried. */
    val enabled: Boolean = true,
    /** The connection is using this candidate right now. */
    val active: Boolean = false,
)

/** One pinned `GET /api/v1/health` started from settings: a single sample, never a performance baseline. */
data class EndpointTestResult(
    val url: String,
    val passed: Boolean,
    /** What it found ("Identity verified", "Connection timed out"…). */
    val result: CoreText,
    val elapsedMillis: Long,
    /** ISO-8601 UTC. */
    val testedAt: String,
)

data class ConnectionEndpointsState(
    /**
     * Every candidate in the order the connection tries them: the list shown is the order tried, nothing is
     * reordered out of sight. Disabled ones stay in place and are skipped.
     */
    val entries: List<EndpointEntry> = emptyList(),
    /** The candidate in use; null while connecting, offline or unpaired. */
    val activeUrl: String? = null,
    /** The user arranged the order; false while the default rule applies ("Restore default order" has nothing to do). */
    val customOrder: Boolean = false,
    /**
     * https addresses can be added and verified: the pairing pinned the hub's TLS key, or it had none and https is
     * checked against the system CAs (a hub behind a reverse proxy).
     */
    val pinned: Boolean = false,
    /** The latest test of each candidate in this process, keyed by [EndpointEntry.url]. */
    val tests: Map<String, EndpointTestResult> = emptyMap(),
    /** Candidates whose test is running, by [EndpointEntry.url]. */
    val testing: Set<String> = emptySet(),
)

sealed interface EndpointCheck {
    /** [url] is the canonical form that will be saved. */
    data class Valid(val url: String, val kind: String) : EndpointCheck
    /** [message] is shown to the user as is, in Chinese. */
    data class Invalid(val message: CoreText) : EndpointCheck
}

/** A refused change. [message] is shown to the user as is, in Chinese. */
class EndpointEditException(text: CoreText) : CoreTextException(text)

/** Manual candidates, disabling, the order to try and tests for the active pairing. */
interface ConnectionEndpoints {
    val state: StateFlow<ConnectionEndpointsState>

    /** Validates [address] without saving or touching the network; cheap enough for every keystroke. */
    fun check(address: String, replacing: String? = null): EndpointCheck

    /** Saves a manual candidate and returns its canonical url. Throws [EndpointEditException]. */
    suspend fun add(address: String): String

    /** Replaces the manual candidate [url], keeping its place and disabled flag; returns the new url. */
    suspend fun replace(url: String, address: String): String

    /** Deletes the manual candidate [url]. Candidates from the pairing or the hub can only be disabled. */
    suspend fun remove(url: String)

    /** Disables or re-enables a candidate from any source; it keeps its place either way. */
    suspend fun setEnabled(url: String, enabled: Boolean)

    /**
     * Makes [urls] (entry urls, normally all of them, top first) the order to try from now on. Current entries
     * missing from [urls] keep their relative order after them; unknown urls are ignored.
     */
    suspend fun reorder(urls: List<String>)

    /** Drops the user's order: the default rule applies again. */
    suspend fun resetOrder()

    /** A pinned health request to [url] over a new connection; the result is also kept in [ConnectionEndpointsState.tests]. */
    suspend fun test(url: String): EndpointTestResult
}
