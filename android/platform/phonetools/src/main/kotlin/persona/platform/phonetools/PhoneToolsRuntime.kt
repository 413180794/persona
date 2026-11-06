package persona.platform.phonetools

import android.content.Context
import persona.core.contracts.HubScope
import persona.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*

/** Owned by the paired hub scope, never by a settings screen or an Activity. */
class PhoneToolsRuntime(
    context: Context, hubScope: HubScope, private val deviceId: String,
    private val connection: HubConnection, private val transport: PhoneToolsTransport,
    parentScope: CoroutineScope, private val simulator: Boolean = false,
    private val automation: Boolean = false,
    /** What the hub and settings call this phone (register_phone device_name): "manufacturer model", as the old app sent. */
    private val deviceName: String = listOf(android.os.Build.MANUFACTURER, android.os.Build.MODEL)
        .filter { it.isNotBlank() }.joinToString(" ").ifBlank { "Android" },
) : AutoCloseable {
    private val job = SupervisorJob(parentScope.coroutineContext[Job])
    private val scope = CoroutineScope(parentScope.coroutineContext + job + Dispatchers.Main.immediate)
    val settings = PhoneToolSettings(context.applicationContext, hubScope, scope)
    val permissions = PhonePermissions(context.applicationContext)
    val confirmations = PhoneConfirmations(context.applicationContext)
    private val visibility = MutableStateFlow(false)
    private val resident = MutableStateFlow(false)
    private val permissionRevision = MutableStateFlow(0)
    private val backend = AndroidPhoneTools(context) { visibility.value }
    private fun available() = !automation && connection.state.value == ConnectionState.Online && (visibility.value || resident.value)
    val registry = PhoneToolRegistry({ settings.enabled.value }, ::available, { visibility.value }, permissions::allowed,
        confirmations::confirm, backend::execute)

    init {
        require(deviceId.isNotBlank())
        scope.launch {
            connection.state.collectLatest { state ->
                transport.discardPending()
                if (state != ConnectionState.Online) {
                    transport.register(deviceId, deviceName, simulator, emptyList())
                    return@collectLatest
                }
                coroutineScope {
                    launch {
                        combine(settings.enabled, visibility, resident, permissionRevision) { _, _, _, _ -> registry.enabledSpecs() }
                            .distinctUntilChanged().collect { specs -> transport.register(deviceId, deviceName, simulator, specs) }
                    }
                    transport.calls.collect { call ->
                        // Cancellation retracts the confirmation when a connection or active scope ends.
                        coroutineScope {
                            val execution = async { registry.call(call) }
                            val watcher = launch {
                                combine(visibility, resident, settings.enabled, permissionRevision) { visible, _, _, _ -> visible }.collect { visible ->
                                    val tool = PhoneToolCatalog.all.firstOrNull { it.spec.name == call.name }
                                    if (!available() || tool != null &&
                                        (tool.source !in settings.enabled.value || !permissions.allowed(call.name, tool.source) || tool.foregroundOnly && !visible)) {
                                        execution.cancel()
                                    }
                                }
                            }
                            try {
                                val result = try { execution.await() }
                                catch (cancelled: CancellationException) {
                                    currentCoroutineContext().ensureActive()
                                    PhoneToolText.interrupted()
                                }
                                transport.result(call.callId, result)
                            } finally { watcher.cancel() }
                        }
                    }
                }
            }
        }
    }
    /** Feed application foreground state, not the visibility of the settings destination. */
    fun setForeground(value: Boolean) { visibility.value = value; confirmations.setForeground(value); refreshPermissions() }
    /** True only while the shared foreground service is actually keeping this connection alive. */
    fun setResidentConnection(value: Boolean) { resident.value = value }
    fun refreshPermissions() { permissionRevision.value += 1 }
    override fun close() {
        transport.register(deviceId, deviceName, simulator, emptyList())
        confirmations.close(); job.cancel(); transport.discardPending()
    }
}
