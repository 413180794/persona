package jarvis.feature.machines

import androidx.lifecycle.ViewModel
import jarvis.core.contracts.*
import jarvis.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*

internal data class MachinesState(
    val dashboard: MetricsDashboard? = null,
    val history: MetricsHistory? = null,
    val range: MetricRange = MetricRange.Hour,
    val now: Long = 0,
    val loading: Boolean = true,
    val failure: String? = null,
    val sessionsFailure: Boolean = false,
)

internal class MachinesViewModel(
    private val api: MachineMetricsApi,
    val sessions: SessionsRepository,
    private val machineId: String?,
    private val clock: () -> Long = { System.currentTimeMillis() / 1000 },
) : ViewModel() {
    private val mutable = MutableStateFlow(MachinesState(now = clock()))
    val state = mutable.asStateFlow()
    private val request = MutableStateFlow(0)

    fun selectRange(range: MetricRange) {
        if (range == state.value.range) return
        mutable.update { it.copy(range = range, history = null, loading = true, failure = null) }
        refresh()
    }
    fun refresh() { request.update { it + 1 } }

    /** The route starts this only while its lifecycle is resumed. */
    suspend fun poll() {
        request.collectLatest {
            coroutineScope {
                launch {
                    try { sessions.refresh(); mutable.update { it.copy(sessionsFailure = false) } }
                    catch (cancelled: CancellationException) { throw cancelled }
                    catch (_: Exception) { mutable.update { it.copy(sessionsFailure = true) } }
                }
                while (currentCoroutineContext().isActive) {
                    val range = state.value.range
                    load(range)
                    delay(if (machineId == null) MetricRange.Hour.pollMillis else range.pollMillis)
                }
            }
        }
    }

    internal suspend fun load(range: MetricRange) {
        try {
            val dashboard = if (machineId == null) api.dashboard() else null
            val history = machineId?.let { api.history(it, range) }
            currentCoroutineContext().ensureActive()
            if (range == state.value.range) mutable.update {
                it.copy(dashboard = dashboard, history = history, now = clock(), loading = false, failure = null)
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) {
            mutable.update { it.copy(now = clock(), loading = false, failure = if (error is MetricsUnavailable)
                "暂时没有监控接口。请确认机器仍在列表中，部署新版本的 hub 和 node 后再看。"
                else "暂时无法更新，连上 hub 后会自动重试。") }
        }
    }
}
