package persona.feature.agents

import androidx.compose.runtime.Composable
import persona.core.contracts.Machine
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HerdrState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.MachineManagement
import persona.core.contracts.api.SessionsRepository
import persona.core.designsystem.JNotice
import persona.core.designsystem.Status
import persona.core.designsystem.failureReason
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

internal fun Machine.herdrStopped(state: HerdrState?): Boolean =
    online && engine in setOf("herdr", "herdr-both") && state?.known == true && !state.running

internal class HerdrStarter(
    private val api: MachineManagement,
    private val repository: SessionsRepository,
    private val connection: HubConnection,
    private val scope: CoroutineScope,
    private val notice: (String) -> Unit,
) {
    private val pending = MutableStateFlow<Set<String>>(emptySet())
    val starting = pending.asStateFlow()
    private val statuses = MutableStateFlow<Map<String, HerdrState>>(emptyMap())
    val states = statuses.asStateFlow()
    private var generation = 0

    suspend fun refresh() {
        val before = generation
        val machines = if (connection.state.value == ConnectionState.Online) repository.machines.value
            .filter { it.online && it.engine in setOf("herdr", "herdr-both") } else emptyList()
        val results = coroutineScope {
            machines.map { machine -> async {
                try { machine.id to api.herdr(machine.id) }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (_: Exception) { null }
            } }.awaitAll().filterNotNull().toMap()
        }
        // A status read begun before a start must not replace its acknowledgement.
        if (before == generation) statuses.value = results + statuses.value.filterKeys { it in pending.value }
    }

    fun start(machineId: String) {
        if (machineId in pending.value || connection.state.value != ConnectionState.Online) return
        if (repository.machines.value.none { it.id == machineId && it.herdrStopped(statuses.value[machineId]) }) return
        generation++
        pending.update { it + machineId }
        scope.launch {
            try {
                val result = api.startHerdr(machineId)
                val latest = repository.machines.value.firstOrNull { it.id == machineId } ?: return@launch
                generation++
                statuses.update { it + (machineId to result) }
                notice(if (result.known && result.running) "${latest.displayName} 的 herdr 已启动" else "未确认 herdr 已启动，请刷新后查看")
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (e: Exception) {
                notice("启动 herdr 失败：${failureReason(e)}")
            } finally {
                pending.update { it - machineId }
            }
        }
    }
}

@Composable
internal fun HerdrNotice(machineName: String, starting: Boolean, onStart: () -> Unit) {
    JNotice(
        text = if (starting) "正在启动 herdr…" else "这台电脑的 herdr 没在运行",
        title = machineName,
        status = Status.Unknown,
        actionLabel = if (starting) null else "启动",
        onAction = if (starting) null else onStart,
    )
}
