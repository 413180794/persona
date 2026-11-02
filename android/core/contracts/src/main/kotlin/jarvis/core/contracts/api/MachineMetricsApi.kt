package jarvis.core.contracts.api

import jarvis.core.contracts.MetricRange
import jarvis.core.contracts.MetricsDashboard
import jarvis.core.contracts.MetricsHistory

/** Bound to the active paired hub, alongside SessionsRepository. */
interface MachineMetricsApi {
    suspend fun dashboard(): MetricsDashboard
    suspend fun history(machineId: String, range: MetricRange): MetricsHistory
}

class MetricsUnavailable : Exception("Machine metrics are unavailable")
