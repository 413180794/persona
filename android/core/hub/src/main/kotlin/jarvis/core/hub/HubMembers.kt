package jarvis.core.hub

import jarvis.core.contracts.Endpoint
import jarvis.core.contracts.api.EndpointOverrides

/**
 * Another hub of the same group (docs/android.md「多 hub」): its candidates are tried after the connection's own hub's,
 * in list order, under the same route rules. [token] is its own device credential when this phone paired with it; null
 * borrows the group credential ([HubConnectionConfig.token]), which every hub of a mesh accepts (plan 2.6).
 * [fingerprint] pins its TLS endpoints, [overrides] are the route choices stored with its pairing.
 */
data class HubMember(
    val hubId: String,
    val fingerprint: String?,
    val token: String?,
    val endpoints: List<Endpoint>,
    val overrides: EndpointOverrides = EndpointOverrides(),
) {
    override fun toString(): String = "HubMember($hubId, ${endpoints.size} endpoints, token=${if (token == null) "group" else "own"})"
}
