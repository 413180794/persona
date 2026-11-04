package persona.android.di

import android.app.Application
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import persona.android.PersonaApp
import persona.android.platform.parsedPairingLink
import persona.core.contracts.HubScope
import persona.core.contracts.api.*
import persona.core.data.HubCredentials
import persona.core.data.HubDataStore
import persona.core.domain.Pairing
import persona.core.hub.*
import persona.feature.settings.SettingsServices
import java.io.OutputStream
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.*

@Module
@InstallIn(SingletonComponent::class)
object PairingModule {
    @Provides @Singleton fun pairing(service: persona.android.platform.PlatformPairing): PairingService = service
    @Provides @Singleton fun diagnostics(service: persona.android.platform.LockScreenTests): SettingsDiagnostics = service
    @Provides @Singleton fun updates(service: WiredPairingService): AppUpdates = service

    @Provides @Singleton fun settings(
        pairing: PairingService, connection: HubConnection, sessions: SessionsRepository,
        updates: AppUpdates, diagnostics: SettingsDiagnostics, devices: WiredDeviceSettings, runtime: ConnectionRuntime,
        machines: MachineManagement, models: ModelConfiguration, plugins: PluginManagement,
        platform: persona.android.platform.PlatformPairing,
    ) = SettingsServices(pairing, connection, sessions, updates, diagnostics, binding = devices.binding,
        // Explicit: the runtime cannot be a ConnectionEndpoints itself (its HubConnection.state has another type).
        disconnect = { pairing.unpair() }, endpoints = runtime.endpoints, machines = machines, models = models, plugins = plugins,
        hubs = runtime.hubGroups, addHub = platform.adding)
}

/** Pairs with pinned probing and hands verified credentials to the connection runtime, which stores and connects. */
@Singleton
class WiredPairingService @Inject constructor(application: Application, private val runtime: ConnectionRuntime) :
    PairingService, SettingsDiagnostics, AppUpdates {
    private val app = application as PersonaApp
    private val lock = Mutex()
    override val isPaired = runtime.pairedState

    /** The runtime restores (and migrates the old plaintext pairing) at start; this only picks up the result. */
    suspend fun restore() = lock.withLock {
        runtime.settled()
    }

    override suspend fun pair(request: PairingRequest) = pair(request) {}

    // Storage discards the old pairings, including when pairing with the same hub again.
    override suspend fun pair(request: PairingRequest, progress: (PairingProgress) -> Unit) = pair(request, progress) { runtime.pair(it) }

    /** Pairs one more hub next to the existing ones (settings › hub › 添加 hub); the same checks as [pair]. */
    suspend fun addHub(request: PairingRequest, progress: (PairingProgress) -> Unit) = pair(request, progress) { runtime.addHub(it) }

    private suspend fun pair(request: PairingRequest, progress: (PairingProgress) -> Unit, save: suspend (HubCredentials) -> Unit) = lock.withLock {
        var networking: HubPairingClient? = null
        try {
            progress(PairingProgress(PairingStage.Parse))
            val parsed = request.parsedPairingLink()
                ?: request.copy(url = request.url.trim(), token = request.token.trim())
            val addresses = HubPairingClient.addresses(parsed)
            networking = HubPairingClient(parsed.hub, parsed.fp, addresses, app.clientVersion())
            val result = networking.pair(parsed.token, app.deviceName, progress)
            val saved = HubCredentials(result.address, result.hubId, result.fingerprint,
                result.candidates, result.token, result.deviceId)
            withContext(NonCancellable) {
                save(saved)
                progress(PairingProgress(PairingStage.Complete))
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { throw error.pairingException() }
        finally { networking?.close() }
    }

    override suspend fun unpair() = lock.withLock {
        // The navigation change cancels its screen's coroutine. Cleanup must still finish.
        withContext(NonCancellable) {
            runtime.unpair()
        }
    }

    override suspend fun read(): ConnectionDiagnostics = lock.withLock {
        runtime.readDiagnostics()
    }

    override suspend fun reprobe(): ConnectionDiagnostics = lock.withLock {
        runtime.reconnect()
        runtime.readDiagnostics()
    }

    override suspend fun scheduleLockScreenTest(delaySeconds: Int) {
        throw UnsupportedOperationException("当前 hub 尚未提供锁屏测试接口")
    }

    override suspend fun check(): AppUpdate? = lock.withLock {
        val saved = runtime.activeCredentials() ?: return@withLock null
        activeClient().checkUpdate(saved.deviceToken)
    }

    override suspend fun download(update: AppUpdate, destination: OutputStream) = lock.withLock {
        activeClient().download(requireNotNull(runtime.activeCredentials()).deviceToken, update, destination)
    }

    private fun activeClient() = HubPairingClient(runtime.connectionFor(checkNotNull(runtime.scopedData.value)))

    private fun Exception.pairingException(): PairingException {
        if (this is PairingException) return this
        val reason = if (this is IllegalArgumentException) PairingFailure.InvalidCode
        else if (this is SecurityException) PairingFailure.LocalPermission
        else when (HubFailure.from(this).kind) {
            HubError.AddressNotFound -> PairingFailure.Dns
            HubError.ConnectionRefused -> PairingFailure.Refused
            HubError.Unreachable, HubError.NoNetwork -> PairingFailure.Unreachable
            HubError.Timeout -> PairingFailure.Timeout
            HubError.CertificateMismatch, HubError.HubMismatch -> PairingFailure.Certificate
            HubError.PairingExpired -> PairingFailure.Expired
            HubError.Incompatible -> PairingFailure.Incompatible
            HubError.TlsRequired -> PairingFailure.TlsRequired
            HubError.LocalNetworkPermission -> PairingFailure.LocalPermission
            else -> PairingFailure.Unknown
        }
        return PairingException(reason).also { it.initCause(this) }
    }
}

private fun Application.clientVersion() = packageManager.getPackageInfo(packageName, 0).versionName ?: "unknown"
