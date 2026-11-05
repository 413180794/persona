package persona.core.contracts.api

import persona.core.contracts.Session

/** Fresh machine snapshots verified against the current connection, excluding restored list caches. */
interface VerifiedSessionsRepository {
    fun verifiedSessions(): Map<String, List<Session>>
}
