package persona.feature.machines

import android.content.res.Resources
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import persona.core.contracts.api.AgentLauncher
import persona.core.contracts.api.MachineManagement
import persona.core.contracts.api.SessionsRepository
import persona.core.designsystem.failureReason
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

/** What a finished action says; the route turns it into text in the page's language. */
internal sealed interface MachineNotice {
    data object Connected : MachineNotice
    /** Still offline after the wait, with the machine's error when the hub has one. */
    data class NotYet(val reason: String?) : MachineNotice
    data class ReconnectFailed(val reason: String) : MachineNotice
    data class DeployFailed(val reason: String) : MachineNotice

    fun text(resources: Resources): String = when (this) {
        Connected -> resources.getString(R.string.machines_notice_connected)
        is NotYet -> reason?.let { resources.getString(R.string.machines_notice_not_yet_reason, it) }
            ?: resources.getString(R.string.machines_notice_not_yet)
        is ReconnectFailed -> resources.getString(R.string.machines_notice_reconnect_failed, reason)
        is DeployFailed -> resources.getString(R.string.machines_notice_deploy_failed, reason)
    }
}

private const val RECONNECT_POLL_MILLIS = 3_000L

internal data class MachineActionsState(
    /** The machine's launchers; null until read (offline machines have none to list). */
    val launchers: List<AgentLauncher>? = null,
    val launchersLoading: Boolean = false,
    /** Why the launchers could not be read (the hub's reason), shown as 读不到启动器：… */
    val launchersError: String? = null,
    val reconnecting: Boolean = false,
    /** Machines whose 重新部署 was sent and not answered yet; the hub's `deploying` takes over after that. */
    val deployStarting: Set<String> = emptySet(),
)

/**
 * What the machine pages change on a machine (iOS `MachineDetailView`): its launchers, 重新连接 (waits up to
 * [reconnectWaitMillis] for it to come online and says how it went) and 重新部署. Results are short notices.
 */
internal class MachineActionsViewModel(
    private val management: MachineManagement,
    private val sessions: SessionsRepository,
    private val reconnectWaitMillis: Long = 15_000,
) : ViewModel() {
    private val mutable = MutableStateFlow(MachineActionsState())
    val state = mutable.asStateFlow()
    private val noticeChannel = Channel<MachineNotice>(Channel.BUFFERED)
    /** One notice per finished action, for the app's feedback. */
    val notices = noticeChannel.receiveAsFlow()
    private var launchersJob: Job? = null

    /** Again when it comes online (after 重新连接): an offline machine has no launchers to list. */
    fun loadLaunchers(machineId: String, online: Boolean) {
        launchersJob?.cancel()
        if (!online) {
            mutable.update { it.copy(launchers = null, launchersLoading = false, launchersError = null) }
            return
        }
        mutable.update { it.copy(launchersLoading = true, launchersError = null) }
        launchersJob = viewModelScope.launch {
            try {
                val launchers = management.launchers(machineId)
                mutable.update { it.copy(launchers = launchers, launchersLoading = false) }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) {
                mutable.update { it.copy(launchersLoading = false, launchersError = failureReason(error)) }
            }
        }
    }

    fun reconnect(machineId: String) {
        if (state.value.reconnecting) return
        mutable.update { it.copy(reconnecting = true) }
        viewModelScope.launch {
            try {
                try { management.reconnect(machineId) }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (error: Exception) {
                    noticeChannel.trySend(MachineNotice.ReconnectFailed(failureReason(error)))
                    return@launch
                }
                val back = withTimeoutOrNull(reconnectWaitMillis) {
                    coroutineScope {
                        // The hub pushes the machine list when the node is back; reading it now and then covers a missed push.
                        val poll = launch {
                            while (true) {
                                delay(RECONNECT_POLL_MILLIS)
                                try { sessions.refresh() } catch (cancelled: CancellationException) { throw cancelled } catch (_: Exception) { }
                            }
                        }
                        sessions.machines.first { list -> list.any { it.id == machineId && it.online } }.also { poll.cancel() }
                    }
                }
                noticeChannel.trySend(if (back != null) MachineNotice.Connected else {
                    MachineNotice.NotYet(sessions.machines.value.firstOrNull { it.id == machineId }?.error?.takeIf { it.isNotBlank() })
                })
            } finally { mutable.update { it.copy(reconnecting = false) } }
        }
    }

    fun deploy(machineId: String) {
        if (machineId in state.value.deployStarting) return
        mutable.update { it.copy(deployStarting = it.deployStarting + machineId) }
        viewModelScope.launch {
            try {
                management.deploy(machineId)
                // Progress and the outcome come with the machine list (deploying, deploy_error, node_mismatch).
                try { sessions.refresh() } catch (cancelled: CancellationException) { throw cancelled } catch (_: Exception) { }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { noticeChannel.trySend(MachineNotice.DeployFailed(failureReason(error))) }
            finally { mutable.update { it.copy(deployStarting = it.deployStarting - machineId) } }
        }
    }
}
