package persona.core.data

import android.content.Context
import androidx.room.Room
import persona.core.contracts.HubScope
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatTransport
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.HubPreferencesTransport
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.StateFlow
import java.io.File

/** Pairing-scoped lifetime; callers replace the entire owner when switching hubs. */
class ChatDataRepositories private constructor(
    items: ChatItemsCache,
    snapshotDirectory: File,
    transport: ChatTransport,
    connection: HubConnection,
    parentScope: CoroutineScope,
    foreground: StateFlow<Boolean>,
    preferenceTransport: HubPreferencesTransport?,
    private val ownedDatabase: ItemsDatabase? = null,
    private val legacyDirectory: File? = null,
    transcript: (suspend (SessionKey, List<Item>) -> Unit)? = null,
) {
    /** Reuses the pairing's database and lifetime; closing a view never discards its cache. */
    constructor(
        data: ScopedHubData,
        transport: ChatTransport,
        connection: HubConnection,
        foreground: StateFlow<Boolean>,
        preferenceTransport: HubPreferencesTransport? = null,
    ) : this(data.items, File(data.noBackupDirectory, "lists"), transport, connection, data.coroutineScope,
        foreground, preferenceTransport, transcript = data.messages::observeTranscript) {
        data.checkActive()
        data.openWindowReconciliation.register(scope, chats::reconcileOpened)
    }

    @Deprecated("Assemble with ScopedHubData so all stores share the pairing lifetime")
    constructor(
        context: Context,
        hubScope: HubScope,
        transport: ChatTransport,
        connection: HubConnection,
        parentScope: CoroutineScope,
        foreground: StateFlow<Boolean>,
        preferenceTransport: HubPreferencesTransport? = null,
    ) : this(openLegacyCache(context, hubScope), legacyDirectory(context, hubScope),
        hubScope, transport, connection, parentScope, foreground, preferenceTransport)

    private constructor(
        database: ItemsDatabase,
        directory: File,
        hubScope: HubScope,
        transport: ChatTransport,
        connection: HubConnection,
        parentScope: CoroutineScope,
        foreground: StateFlow<Boolean>,
        preferenceTransport: HubPreferencesTransport?,
    ) : this(SessionItemsCache(database, hubScope), File(directory, "lists"), transport, connection, parentScope,
        foreground, preferenceTransport, database, directory)

    private val job = SupervisorJob(parentScope.coroutineContext[Job])
    private val scope = CoroutineScope(parentScope.coroutineContext + job + Dispatchers.IO)
    val snapshots = ListSnapshotStore(snapshotDirectory)
    val sessions = HubSessionsRepository(transport, connection, scope, snapshots)
    val preferences = preferenceTransport?.let { remote ->
        HubPreferencesRepository(remote, scope, snapshots) { path ->
            sessions.sessions.value.firstOrNull { it.key.path == path }?.let { session ->
                session.spaceName?.let { "${session.machineId}/$it" }
            }
        }
    }
    val interactions = HubInteractionCenter(transport, connection, scope, snapshots)
    val chats = HubChatRepository(transport, connection, items, sessions, scope,
        foreground, seen = preferenceTransport?.let { remote -> { key -> remote.seen(key) } }, transcript = transcript)

    suspend fun close(discard: Boolean = false) = withContext(NonCancellable + Dispatchers.IO) {
        job.cancelAndJoin()
        ownedDatabase?.close()
        if (discard) legacyDirectory?.deleteRecursively()
    }

    companion object {
        // Snapshots must survive cache trimming, so the legacy owner keeps them out of cacheDir.
        private fun legacyDirectory(context: Context, scope: HubScope) =
            File(context.noBackupFilesDir, "chat-data/${scope.path}")

        private fun openLegacyCache(context: Context, scope: HubScope): ItemsDatabase {
            val directory = legacyDirectory(context, scope).apply { mkdirs() }
            return Room.databaseBuilder(context.applicationContext, ItemsDatabase::class.java,
                File(directory, "items.db").absolutePath).addMigrations(ItemsDatabase.MIGRATION_1_2).build()
        }
    }
}
