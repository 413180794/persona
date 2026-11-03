package persona.core.data

import androidx.datastore.core.DataStore
import persona.core.contracts.api.EndpointOverrides
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable

/** endpoints.enc in the pairing's no-backup directory: erased with the rest of the pairing. */
@Serializable
internal data class EndpointOverridesDocument(
    val version: Int = 1,
    val manual: List<String> = emptyList(),
    val disabled: List<String> = emptyList(),
    /** The user's order of candidate keys; null until the user reorders. */
    val order: List<String>? = null,
    /** Written by the first version (one preferred candidate, tried first); read as an order that starts with it. */
    val preferred: String? = null,
)

/** The pairing's route choices (manual candidates, disabled ones, the order to try); a missing file is the default. */
class EndpointOverridesStore internal constructor(
    private val store: DataStore<EndpointOverridesDocument>,
    initial: EndpointOverrides,
) {
    private val lock = Mutex()
    private val current = MutableStateFlow(initial)
    val value: StateFlow<EndpointOverrides> = current.asStateFlow()

    /** Saves [transform] of the stored value and returns what was saved; [value] has it before this returns. */
    suspend fun update(transform: (EndpointOverrides) -> EndpointOverrides): EndpointOverrides = lock.withLock {
        store.updateData { transform(it.overrides()).document() }.overrides().also { current.value = it }
    }

    companion object {
        /** Reads the file first, so [value] is right from the start. */
        internal suspend fun open(store: DataStore<EndpointOverridesDocument>) =
            EndpointOverridesStore(store, store.data.first().overrides())
    }
}

private fun EndpointOverridesDocument.overrides() =
    EndpointOverrides(manual.distinct(), disabled.toSet(), order?.distinct() ?: preferred?.let(::listOf))

private fun EndpointOverrides.document() = EndpointOverridesDocument(manual = manual, disabled = disabled.toList(), order = order)
