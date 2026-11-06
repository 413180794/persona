package persona.feature.settings

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import persona.core.contracts.api.AgentTriage
import persona.core.contracts.api.TriageSwitches
import persona.core.contracts.api.TriageUnsupported
import persona.core.designsystem.failureReasonText

internal data class TriageSettingsState(
    /** The hub's switches; null until read (the switches wait, greyed). */
    val switches: TriageSwitches? = null,
    val loading: Boolean = false,
    /** A change is on its way: the switches wait. */
    val busy: Boolean = false,
    /** Why the switches could not be read; the page offers "Try again" with it unless the hub has no triage at all. */
    val readError: ResText? = null,
    /** The hub predates triage: reading again will not help. */
    val unsupported: Boolean = false,
    /** Why the last change was not saved (the switches went back). */
    val saveError: ResText? = null,
)

/** Settings › Session triage: reads the switches (again on "Try again") and saves a change, going back when the hub refuses it. */
internal class TriageSettingsModel(private val api: AgentTriage) {
    private val mutable = MutableStateFlow(TriageSettingsState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true, readError = null) }
        try {
            val read = api.switches()
            mutable.update { it.copy(switches = read, readError = null, unsupported = false) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: TriageUnsupported) { mutable.update { it.copy(unsupported = true, readError = ResText.of(R.string.settings_triage_read_failed, failureReasonText(error))) } }
        catch (error: Exception) { mutable.update { it.copy(readError = ResText.of(R.string.settings_triage_read_failed, failureReasonText(error))) } }
        finally { mutable.update { it.copy(loading = false) } }
    }

    suspend fun change(next: TriageSwitches) {
        val before = state.value
        if (before.busy || before.switches == null) return
        mutable.update { it.copy(switches = next, busy = true, saveError = null) }
        try {
            val saved = api.put(next)
            mutable.update { it.copy(switches = saved) }
        } catch (cancelled: CancellationException) {
            mutable.update { it.copy(switches = before.switches) }
            throw cancelled
        } catch (error: Exception) {
            mutable.update { it.copy(switches = before.switches, saveError = ResText.of(R.string.settings_triage_save_failed, failureReasonText(error))) }
        } finally { mutable.update { it.copy(busy = false) } }
    }
}
