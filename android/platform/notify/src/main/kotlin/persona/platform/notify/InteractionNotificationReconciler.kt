package persona.platform.notify

import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import persona.core.contracts.GroupKeys
import persona.core.contracts.Interaction

/**
 * Takes down request notifications whose request is no longer pending (#3401). A reconnect that did not resume misses
 * the `interaction_resolved` of a request answered elsewhere; the full refresh drops it from the list, and only this
 * notices. It compares the notification shade itself with the list, so a notification left by an earlier process (the
 * service restarted) goes too. Only while online, and only once the list has held still for [settle]: before the first
 * refresh after a cold start, or while a list is being rebuilt, it can be missing requests that are still pending.
 * A request of a group that is not open (closed, or merged into another under new ids) is left alone: its requests
 * left the list without being answered.
 */
internal class InteractionNotificationReconciler(
    /** Ids of the request notifications showing now. */
    private val showing: () -> Set<String>,
    private val retract: (String) -> Unit,
    private val settle: Long = SETTLE_MILLIS,
) {
    @OptIn(FlowPreview::class)
    suspend fun run(pending: Flow<List<Interaction>>, online: Flow<Boolean>, groups: Flow<Set<String?>>) {
        combine(pending, online, groups) { list, up, open -> if (up) list.mapTo(HashSet()) { it.id } to open else null }
            .debounce(settle)
            .distinctUntilChanged()
            .collect { state -> state?.let { (ids, open) -> stale(showing(), ids, open).forEach(retract) } }
    }

    companion object {
        const val SETTLE_MILLIS = 1_500L

        /** [groups]: the namespaces of the open groups (null is the primary group), as [GroupKeys.namespaceOf] gives them. */
        fun stale(showing: Set<String>, pending: Set<String>, groups: Set<String?>): Set<String> =
            showing.filterTo(HashSet()) { it !in pending && GroupKeys.namespaceOf(it) in groups }
    }
}
