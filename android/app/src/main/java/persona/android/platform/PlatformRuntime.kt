package persona.android.platform

import persona.android.R
import android.app.Application
import persona.android.PersonaApp
import persona.android.data.Hub
import persona.android.di.ConnectionRuntime
import persona.core.data.AssistantMainConversationResolver
import persona.core.data.ScopedHubData
import persona.core.hub.HttpInboxAssistantTransport
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import java.io.File
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import javax.inject.Inject
import javax.inject.Singleton

internal data class PlatformSession(val hub: Hub, val data: ScopedHubData) {
    val directory get() = File(data.noBackupDirectory, "platform")
    fun active() = data.coroutineScope.coroutineContext.ensureActive()
    suspend fun <T> io(block: suspend () -> T): T = data.coroutineScope.async(Dispatchers.IO) {
        active()
        block()
    }.await()
}

@Singleton
class PlatformSessions @Inject constructor(application: Application, private val runtime: ConnectionRuntime) {
    private val app = application as PersonaApp
    internal val current = MutableStateFlow<PlatformSession?>(null)
    internal suspend fun requireSession(): PlatformSession {
        combine(app.pairingReady, app.pairingRestoreError) { ready, failed -> ready || failed }.first { it }
        check(app.hub.value != null) { app.getString(R.string.app_error_pair_in_app) }
        return withTimeout(10_000) { current.filterNotNull().first { it.hub === app.hub.value } }
    }
    /** Every open group's session, the selected one's included. */
    internal fun open(): List<PlatformSession> = runtime.groups.value.mapNotNull { group ->
        val hub = group.hub ?: return@mapNotNull null
        val data = group.data ?: return@mapNotNull null
        PlatformSession(hub, data).takeIf { data.credentials.deviceToken == hub.api.token }
    }
}

@Singleton
class PlatformRuntime @Inject constructor(
    application: Application,
    private val runtime: ConnectionRuntime,
    private val sessions: PlatformSessions,
    private val tests: LockScreenTests,
    private val shares: ShareQueue,
) {
    private val app = application as PersonaApp
    private var started = false
    fun start() {
        if (started) return
        started = true
        app.scope.launch {
            app.hub.collectLatest { hub ->
                sessions.current.value = null
                if (hub == null) return@collectLatest
                val data = runtime.dataStore.restore() ?: return@collectLatest
                if (data.credentials.deviceToken != hub.api.token) return@collectLatest
                data.messages.connectMainConversation(AssistantMainConversationResolver(
                    HttpInboxAssistantTransport(data.scope.path, hub.connection) { app.hub.value === hub }))
                val session = PlatformSession(hub, data)
                sessions.current.value = session
                try {
                    coroutineScope {
                        launch { tests.observe(session) }
                        launch { shares.observe(session) }
                    }
                } finally {
                    sessions.current.compareAndSet(session, null)
                }
            }
        }
        app.scope.launch {
            // The other open groups drain their own shares too, so one coming online sends them unselected (#4818).
            runtime.groups.combine(app.hub) { _, hub -> hub }.collectLatest { selected ->
                coroutineScope { sessions.open().filter { it.hub !== selected }.forEach { launch { shares.observe(it) } } }
            }
        }
    }
}

/** Rename only after flushing the complete document; a partial write never replaces committed work. */
internal fun writePlatformFile(file: File, text: String) {
    check(file.parentFile!!.isDirectory || file.parentFile!!.mkdirs())
    val pending = File(file.parentFile, file.name + ".partial")
    pending.outputStream().use { stream ->
        stream.write(text.toByteArray(Charsets.UTF_8))
        stream.fd.sync()
    }
    Files.move(pending.toPath(), file.toPath(), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
}
