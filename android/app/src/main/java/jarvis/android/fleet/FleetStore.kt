package jarvis.android.fleet

import android.content.Context
import androidx.core.content.edit
import jarvis.android.data.Hub
import jarvis.android.data.JsonCodec
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

/**
 * The live [FleetState], built from the hub's sessions / interactions / mutes (debounced, off the main thread), for
 * the ongoing notification ([FleetNotification]), the home-screen widget and the Quick Settings tile. Its last value
 * is kept on disk because the widget and the tile draw while the process may not be running.
 */
@OptIn(FlowPreview::class)
object FleetStore {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private var job: Job? = null

    private val _state = MutableStateFlow<FleetState?>(null)
    /** What this process last built (null until the first one: read [saved]). */
    val state: StateFlow<FleetState?> = _state.asStateFlow()

    private fun prefs(ctx: Context) = ctx.getSharedPreferences("fleet", Context.MODE_PRIVATE)

    /** Follows [hub] (a new one replaces the old); [onChange] runs for each new state, off the main thread. */
    fun follow(ctx: Context, hub: Hub, onChange: suspend (FleetState) -> Unit) {
        val app = ctx.applicationContext
        job?.cancel()
        job = scope.launch {
            combine(hub.sessions, hub.interactions, hub.machines, hub.mutes) { byMachine, ixs, machines, _ ->
                FleetState.build(byMachine.values.flatten(), ixs, machines, hub::isMuted)
            }
                .debounce(300)
                .distinctUntilChanged()
                .collect { st ->
                    prefs(app).edit { putString("snapshot", JsonCodec.encodeToString(FleetState.serializer(), st)) }
                    _state.value = st
                    onChange(st)
                }
        }
    }

    /** The last state any process saw (the widget / tile without a running app); empty if none. */
    fun saved(ctx: Context): FleetState {
        _state.value?.let { return it }
        val s = prefs(ctx).getString("snapshot", null) ?: return FleetState()
        return runCatching { JsonCodec.decodeFromString(FleetState.serializer(), s) }.getOrDefault(FleetState())
    }

    /** Unpaired: nothing to show anywhere. */
    fun clear(ctx: Context) {
        job?.cancel()
        job = null
        prefs(ctx).edit { remove("snapshot") }
        _state.value = FleetState()
    }
}
