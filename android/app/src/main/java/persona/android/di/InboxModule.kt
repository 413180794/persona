package persona.android.di

import persona.android.AppText
import persona.android.R
import android.app.Application
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import persona.android.PersonaApp
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import persona.core.data.*
import persona.core.hub.HttpInboxAssistantTransport
import persona.core.hub.HttpLarkConversationTransport
import persona.core.hub.HttpMailTransport
import persona.core.hub.HttpInboxActionsTransport
import persona.core.hub.HubFailure
import persona.core.hub.HubError
import okhttp3.HttpUrl.Companion.toHttpUrl
import persona.feature.inbox.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.serialization.json.jsonObject
import java.io.File
import javax.inject.Inject
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object InboxModule {
    @Provides @Singleton fun inbox(owner: InboxDataOwner): persona.core.contracts.api.InboxRepository = owner.inbox
    @Provides @Singleton fun inboxDetails(owner: InboxDataOwner): persona.feature.inbox.InboxRepository = owner.inbox
    @Provides @Singleton fun assistant(owner: InboxDataOwner): AssistantRepository = owner.assistant
    @Provides @Singleton fun conversations(owner: InboxDataOwner): AssistantConversations = owner.assistant
}

/** Replace all repositories and file owners together when the pairing generation changes. */
@Singleton
@OptIn(ExperimentalCoroutinesApi::class)
class InboxDataOwner @Inject constructor(
    application: Application,
    private val runtime: ConnectionRuntime,
    sessions: SessionsRepository,
    interactions: InteractionCenter,
    preferences: HubPreferences,
    stacks: GroupStacks,
) {
    private val app = application as PersonaApp
    private data class Repositories(val data: ScopedHubData, val inbox: persona.feature.inbox.InboxRepository,
        val assistant: AssistantConversations, val mail: StateFlow<MailTransport?>,
        val composers: ReplyComposers, val reading: LarkReadingStore, val names: GroupNames)
    // The inbox and Persona are the selected group's (docs/android.md「多 hub」): its own transports speak its plain
    // ids, which are moved into the merged lists' ids on the way out. Agent requests in the inbox stay merged.
    private val current = runtime.scopedData.mapLatest { data ->
        data?.coroutineScope?.async {
            if (runtime.scopedData.value !== data) return@async null
            val stack = stacks.stacks.map { list -> list.firstOrNull { it.data === data } }.filterNotNull().first()
            val names = stack.names
            val socket = runtime.connectionFor(data)
            fun isCurrent() =
                runtime.scopedData.value === data && data.coroutineScope.coroutineContext[Job]?.isActive == true
            val transport = HttpInboxAssistantTransport(data.scope.path, socket, ::isCurrent)
            val actions = HttpInboxActionsTransport(socket.http.client,
                { (socket.route.value?.endpoint?.url ?: throw HubFailure(HubError.NoNetwork)).toHttpUrl() },
                data.credentials.deviceToken, ::isCurrent)
            val media = InboxMedia(app, data)
            val mail = socket.route.filterNotNull().map { route ->
                media.mailTransport(HttpMailTransport(data.scope.path, route.endpoint.url, data.credentials.deviceToken, socket.http.client) {
                    isCurrent() && socket.route.value == route
                })
            }.stateIn(data.coroutineScope, SharingStarted.Eagerly, null)
            val connection = object : InboxAssistantConnection {
                override val current: StateFlow<InboxAssistantTransport?> = MutableStateFlow(transport)
            }
            val snapshots = ListSnapshotStore(File(data.noBackupDirectory, "lists"))
            val realInbox = RealInboxRepository(connection, data.inboxReplies, sessions, interactions, data.coroutineScope,
                snapshots = snapshots, hubPreferences = preferences, hubConnection = runtime,
                mainConversationSender = data.messages.inGroup(names, data.coroutineScope), actions = actions,
                lookupReply = { target, clientId ->
                    try {
                        socket.request("/api/v1/ops/${inboxReplyReceiptId(target, clientId)}").use { response ->
                            inboxReplyReceipt(JsonCodec.parseToJsonElement(response.body!!.string()).jsonObject)
                        }
                    } catch (e: CancellationException) { throw e }
                    catch (_: Exception) { null }
                },
                classifyReplyFailure = { failure ->
                    val error = HubFailure.from(failure)
                    when {
                        error.delivered == "none" && !error.retryable -> OutboxState.Refused
                        error.code in setOf("input_draft", "dialog_open", "op_id_reused", "invalid", "stale_session") -> OutboxState.Refused
                        error.delivered == "none" && error.retryable -> OutboxState.WaitingForNetwork
                        error.kind in setOf(HubError.NoNetwork, HubError.AddressNotFound, HubError.ConnectionRefused, HubError.Unreachable) -> OutboxState.WaitingForNetwork
                        else -> OutboxState.Unknown
                    }
                },
                sendThread = { root, request -> transport.replyInThread(root, request.text, request.clientId) },
                download = media::download,
                // 交给 AI to an Agent uses the same outbox as the Agents tab, and its targets come from the merged list:
                // the app-wide sender takes the session to its own group (by the merged id's suffix).
                messageSender = runtime.messages)
            val lark = socket.route.map { route -> route?.let {
                HttpLarkConversationTransport(transport, it.endpoint.url, data.credentials.deviceToken,
                    socket.http.client) { isCurrent() && socket.route.value == route }
            } }.stateIn(data.coroutineScope, SharingStarted.Eagerly, null)
            val wrapped = LarkInboxRepository(realInbox, lark, runtime.state,
                socket.events.filterIsInstance<persona.core.contracts.LarkEvent>().map { LarkInvalidation(it.feedIds.toSet()) },
                FileLarkWindowCache(File(data.noBackupDirectory, "lark-windows")) { write -> data.withInboxFiles(write) },
                localMessages = realInbox::larkOutbox, onMessages = realInbox::acceptLarkWindow)
            data.messages.connectReconciler(OutboxReconciler { realInbox.reconcilePendingReplies() })
            val reading = LarkReadingStore.open(InboxReadingStorage(data), data.scope.path, data.coroutineScope)
            // Persona' own transport gives plain ids, so it reads the group's own sessions and preferences.
            val assistant = RealAssistantRepository(connection, stack.repositories.sessions, data.coroutineScope, snapshots,
                stack.repositories.preferences ?: preferences.takeIf { names.plain })
            Repositories(data, wrapped, assistant.inGroup(names, data.coroutineScope), mail,
                ReplyComposers(wrapped, data.coroutineScope), reading, names)
        }?.await()
    }.stateIn(app.scope, SharingStarted.Eagerly, null)

    private fun <T> observe(initial: T, source: (Repositories) -> Flow<T>): StateFlow<T> = current.flatMapLatest {
        if (it == null || runtime.scopedData.value !== it.data) flowOf(initial) else source(it)
    }.stateIn(app.scope, SharingStarted.Eagerly, initial)

    private suspend fun <T> use(block: suspend (Repositories) -> T): T {
        val data = checkNotNull(runtime.scopedData.value) { AppText.get(R.string.app_error_pair_first) }
        val repositories = combine(current, runtime.scopedData) { repositories, active ->
            if (active !== data) throw CancellationException("Pairing changed")
            repositories?.takeIf { it.data === data }
        }.filterNotNull().first()
        return data.coroutineScope.async {
            check(runtime.scopedData.value === data) { AppText.get(R.string.app_error_switched) }
            block(repositories)
        }.await()
    }

    val inbox = object : persona.feature.inbox.InboxRepository {
        override val supportsThreadOutbox = true
        override val replyComposers = observe<ReplyComposers?>(null) { flowOf(it.composers) }
        override suspend fun readingStore() = use { it.reading }
        override fun replyUpdates(target: ReplyTarget) = current.flatMapLatest {
            if (it == null || runtime.scopedData.value !== it.data) emptyFlow() else it.inbox.replyUpdates(target)
        }
        override val snapshot = observe(InboxSnapshot()) { repositories ->
            if (repositories.names.plain) repositories.inbox.snapshot else repositories.inbox.snapshot.map { it.inGroup(repositories.names) }
        }
        override val mailTransport = observe<MailTransport?>(null) { it.mail }
        override val personaOutbox = observe(emptyList<OutboxMessage>()) { it.inbox.personaOutbox }
        override val entries = observe(emptyList<InboxEntry>()) { it.inbox.entries }
        override val connectionState = runtime.state
        override val preferences = current.flatMapLatest { repositories ->
            repositories?.data?.listPreferences?.data?.map { saved ->
                saved[stringPreferencesKey("inbox")]?.let { JsonCodec.decodeFromString<InboxPreferences>(it) }
                    ?: InboxPreferences()
            } ?: flowOf(InboxPreferences())
        }
        override suspend fun savePreferences(value: InboxPreferences) = use { repositories ->
            repositories.data.listPreferences.edit { it[stringPreferencesKey("inbox")] = JsonCodec.encodeToString(InboxPreferences.serializer(), value) }
            Unit
        }
        override suspend fun refresh() = use { it.inbox.refresh() }
        override suspend fun refreshNow() = use { it.inbox.refreshNow() }
        override fun conversation(feedId: String, startPosition: Long?) = current.flatMapLatest {
            if (it == null || runtime.scopedData.value !== it.data) flowOf(LarkConversation())
            else it.inbox.conversation(feedId, startPosition)
        }
        override suspend fun loadOlder(feedId: String) = use { it.inbox.loadOlder(feedId) }
        override suspend fun refreshConversation(feedId: String) = use { it.inbox.refreshConversation(feedId) }
        override suspend fun loadNewer(feedId: String) = use { it.inbox.loadNewer(feedId) }
        override suspend fun latestConversation(feedId: String) = use { it.inbox.latestConversation(feedId) }
        override suspend fun markVisible(feedId: String, messages: List<LarkMessage>) = use { it.inbox.markVisible(feedId, messages) }
        override suspend fun setDone(itemId: String, done: Boolean) = use { it.inbox.setDone(itemId, done) }
        override suspend fun setDone(items: List<persona.core.contracts.api.InboxDoneTarget>, done: Boolean) = use { it.inbox.setDone(items, done) }
        override suspend fun bulk(items: List<persona.core.contracts.api.InboxDoneTarget>, action: persona.core.contracts.api.InboxBulkAction) = use { it.inbox.bulk(items, action) }
        override suspend fun readDraft(target: ReplyTarget) = use { it.inbox.readDraft(target) }
        override suspend fun saveDraft(target: ReplyTarget, draft: ReplyDraft) = use { it.inbox.saveDraft(target, draft) }
        override suspend fun reply(target: ReplyTarget, request: ReplyRequest) = use { it.inbox.reply(target, request) }
        override suspend fun downloadFile(file: LarkPart.File) = use { it.inbox.downloadFile(file) }
        override suspend fun replyStatus(target: ReplyTarget, clientId: String) = use { it.inbox.replyStatus(target, clientId) }
        override suspend fun verifyReply(target: ReplyTarget, clientId: String) = use { it.inbox.verifyReply(target, clientId) }
        override suspend fun askPersona(itemId: String, action: InboxAiAction) = use { it.inbox.askPersona(itemId, action) }
        override suspend fun askPersona(request: InboxPersonaRequest) = use { it.inbox.askPersona(request) }
        override suspend fun personaContext(itemId: String) = use { it.inbox.personaContext(itemId) }
        override suspend fun draftReply(itemId: String, instruction: String) = use { it.inbox.draftReply(itemId, instruction) }
        override suspend fun handOff(request: InboxHandOffRequest) = use { it.inbox.handOff(request) }
        override suspend fun momentsPost(postId: String) = use { it.inbox.momentsPost(postId) }
        override suspend fun momentsComments(postId: String) = use { it.inbox.momentsComments(postId) }
        override suspend fun urlPreview(messageId: String, url: String) = use { it.inbox.urlPreview(messageId, url) }
        override suspend fun react(messageId: String, key: String, remove: Boolean) = use { it.inbox.react(messageId, key, remove) }
        override suspend fun cardAction(messageId: String, actionId: String) = use { it.inbox.cardAction(messageId, actionId) }
        override suspend fun userProfile(userId: String, feedId: String?) = use { it.inbox.userProfile(userId, feedId) }
        override suspend fun quickReactions() = use { it.inbox.quickReactions() }
        override suspend fun emojis() = use { it.inbox.emojis() }
    }
    val assistant = object : AssistantConversations {
        override val assistant = observe(AssistantSnapshot()) { it.assistant.assistant }
        override val pinned = observe(emptySet<persona.core.contracts.SessionKey>()) { it.assistant.pinned }
        override suspend fun refresh() = use { it.assistant.refresh() }
        override suspend fun startConversation() = use { it.assistant.startConversation() }
    }
}
