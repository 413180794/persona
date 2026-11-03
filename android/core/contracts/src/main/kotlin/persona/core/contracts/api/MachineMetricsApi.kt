package persona.core.contracts.api

import persona.core.contracts.MetricRange
import persona.core.contracts.MetricsDashboard
import persona.core.contracts.MetricsHistory

/** Bound to the active paired hub, alongside SessionsRepository. */
interface MachineMetricsApi {
    suspend fun dashboard(): MetricsDashboard
    suspend fun history(machineId: String, range: MetricRange): MetricsHistory
}

class MetricsUnavailable : Exception("Machine metrics are unavailable")
