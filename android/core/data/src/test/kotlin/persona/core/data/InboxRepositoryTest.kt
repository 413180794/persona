package persona.core.data

import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.feature.inbox.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException
import java.nio.file.Files

@OptIn(ExperimentalCoroutinesApi::class)
class InboxRepositoryTest {
    @Test fun updatingWaitsForEveryLarkPageAndPreservesVerifiedWindowOnOnline() = runBlocking {
        val directory = Files.createTempDirectory("window-reconcile-test").toFile()
        val owner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        try {
            val transport = FakeTransport().apply { onMessages = { _, _, _ -> page(row("first", 10, "消息")) } }
            val hub = FakeHub()
            val repository = repository(transport, directory, owner, hub)
            val observed = MutableStateFlow(LarkConversation())
            owner.launch { repository.conversation("feed", null).collect { observed.value = it } }
            awaitState(observed) { it.verified }
            hub.state.value = ConnectionState.Updating
            awaitState(observed) { !it.verified }
            val requested = CompletableDeferred<Unit>()
            val lastPage = CompletableDeferred<JsonObject>()
            transport.onMessages = { _, _, after ->
                if (after == null) page(row("first", 10, "消息"), newer = "10") else {
                    requested.complete(Unit)
                    lastPage.await()
                }
            }
            val ready = async { hub.openWindowReconciliation.reconcile(5_000) }
            withTimeout(5_000) { requested.await() }
            assertFalse(ready.isCompleted)
            assertFalse(observed.value.verified)
            lastPage.complete(page(row("last", 11, "后续消息")))
            assertTrue(ready.await())
            val verified = awaitState(observed) { it.verified }
            hub.state.value = ConnectionState.Online
            repository.refresh()
            assertTrue(observed.value.verified)
            assertEquals(listOf("first", "last"), verified.messages.map { it.id })
        } finally {
            owner.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    @Test fun inboxAiUsesSharedOfflineOutboxAndRetainsRequestOnWriteFailure() = runTest {
        val directory = Files.createTempDirectory("inbox-jarvis-test").toFile()
        var failWrite = false
        val store = object : OutboxStore {
            override val entries = MutableStateFlow<List<OutboxEntry>>(emptyList())
            override suspend fun enqueue(entry: OutboxEntry) {
                if (failWrite) throw IOException("Write failed")
                val previous = entries.value.firstOrNull { it.clientMessageId == entry.clientMessageId }
                require(previous == null || previous.text == entry.text)
                if (previous == null) entries.value += entry
            }
            override suspend fun transition(id: String, from: Set<OutboxState>, to: OutboxState, error: String?): Boolean {
                val old = entries.value.firstOrNull { it.clientMessageId == id && it.state in from } ?: return false
                entries.value = entries.value.map { if (it == old) it.copy(state = to) else it }
                return true
            }
            override suspend fun recoverInterrupted() = Unit
        }
        val sender = OutboxMessageSender(HubScope("hub", "generation"), store, object : MessageTransport {
            override val readyToSend = MutableStateFlow(false)
            override val supportsClientOps = true
            override suspend fun send(request: MessageRequest): MessageTransportResult = error("Offline must not use transport")
        }, backgroundScope)
        try {
            val transport = FakeTransport()
            val hub = FakeHub().apply { state.value = ConnectionState.WaitingForNetwork }
            val snapshots = ListSnapshotStore(directory.resolve("snapshots"))
            snapshots.save("${transport.scope}:inbox", buildJsonObject {
                put("items", buildJsonArray { add(buildJsonObject { put("id", "item"); put("title", "事项"); put("preview", "已加载内容") }) })
            })
            val repository = repository(transport, directory, backgroundScope, hub, snapshots, sender)
            repository.snapshot.first { it.records.isNotEmpty() }
            sender.sendToMain("分享上下文", "shared-id")
            val request = InboxPersonaRequest("item", InboxAiAction.Handoff, "handoff-id", "<external>参考</external>")
            failWrite = true
            try { repository.askPersona(request); fail("Expected storage failure") } catch (_: IOException) { }
            assertEquals(listOf("shared-id"), store.entries.value.map { it.clientMessageId })
            failWrite = false
            repository.askPersona(request)
            repository.askPersona(request)
            repository.askPersona(InboxPersonaRequest("item", InboxAiAction.Summarize, "summary-id"))
            repository.askPersona(InboxPersonaRequest("item", InboxAiAction.DraftReply, "draft-id"))
            runCurrent()
            assertEquals(listOf("shared-id", "handoff-id", "summary-id", "draft-id"), store.entries.value.map { it.clientMessageId })
            assertTrue(store.entries.value.all { it.mainTarget && it.session == PendingMainConversation })
            assertTrue(store.entries.value.single { it.clientMessageId == "handoff-id" }.text.contains("&lt;external&gt;参考&lt;/external&gt;"))
            assertTrue(store.entries.value.single { it.clientMessageId == "summary-id" }.text.contains("已加载内容"))
            assertTrue(store.entries.value.all { it.state == OutboxState.WaitingForNetwork })
        } finally {
            sender.close()
            backgroundScope.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    @Test fun mailUnknownUsesReadOnlyReceiptAndKeepsOriginalClientId() = runTest {
        val directory = Files.createTempDirectory("inbox-receipt-test").toFile()
        val transport = FakeTransport()
        val journal = InboxReplyJournal(directory, transport.scope)
        val target = ReplyTarget.Inbox("mail-item")
        val request = ReplyRequest("回复", "original-id")
        var confirmed = false
        val lookups = mutableListOf<String>()
        val connection = object : InboxAssistantConnection {
            override val current = MutableStateFlow<InboxAssistantTransport?>(transport)
        }
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow<List<Machine>>(emptyList())
            override val sessions = MutableStateFlow<List<Session>>(emptyList())
            override suspend fun refresh() = Unit
        }
        val interactions = object : InteractionCenter {
            override val pending = MutableStateFlow<List<Interaction>>(emptyList())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
        }
        try {
            val repository = RealInboxRepository(connection, journal, sessions, interactions, backgroundScope,
                lookupReply = { actual, id ->
                    assertEquals(target, actual)
                    lookups += id
                    InboxReplyReceipt(if (confirmed) OutboxState.Sent else OutboxState.Unknown)
                }) { _, _ -> error("Unused") }
            runCurrent()
            repository.saveDraft(target, ReplyDraft(request.text, request.clientId))
            var sends = 0
            transport.onReply = { sends++; throw IOException("Receipt lost") }
            try { repository.reply(target, request); fail("Expected unknown outcome") } catch (_: IOException) { }
            repository.reconcilePendingReplies()
            assertEquals(ReplyDelivery.Unknown, repository.verifyReply(target, request.clientId))
            try { repository.reply(target, request); fail("Must not resend unknown") } catch (_: IOException) { }
            assertEquals(1, sends)
            assertEquals(request.clientId, repository.readDraft(target)?.clientId)
            confirmed = true
            assertEquals(ReplyDelivery.Sent, repository.verifyReply(target, request.clientId))
            assertEquals(1, sends)
            assertTrue(lookups.all { it == request.clientId })
            assertNull(repository.readDraft(target))
        } finally {
            backgroundScope.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    @Test fun persistsBeforeSendingRetainsRetryIdAndDoesNotClearNewerDraft() = runTest {
        val directory = Files.createTempDirectory("inbox-test").toFile()
        try {
            val journal = InboxReplyJournal(directory, "hub/generation")
            val transport = FakeTransport()
            val connection = object : InboxAssistantConnection {
                override val current = MutableStateFlow<InboxAssistantTransport?>(transport)
            }
            val sessions = object : SessionsRepository {
                override val machines = MutableStateFlow<List<Machine>>(emptyList())
                override val sessions = MutableStateFlow<List<Session>>(emptyList())
                override suspend fun refresh() = Unit
            }
            val interactions = object : InteractionCenter {
                override val pending = MutableStateFlow<List<Interaction>>(emptyList())
                override suspend fun refresh() = Unit
                override suspend fun resolve(id: String, resolution: Resolution) = Unit
            }
            val repository = RealInboxRepository(connection, journal, sessions, interactions, backgroundScope,
                classifyReplyFailure = { OutboxState.WaitingForNetwork }) { _, _ -> error("unused") }
            runCurrent()
            val target = ReplyTarget.Lark("feed")
            val request = ReplyRequest("回复", "reply-id")
            repository.saveDraft(target, ReplyDraft(request.text, request.clientId))
            var attempts = 0
            transport.onReply = { id ->
                assertEquals(request.clientId, id)
                assertEquals(request, InboxReplyJournal(directory, "hub/generation").read(transport.scope).outbox.single().request)
                if (attempts++ == 0) throw IOException("offline")
                repository.saveDraft(target, ReplyDraft("后续草稿", "new-id"))
                buildJsonObject { put("id", "server-id") }
            }
            try { repository.reply(target, request); fail("Expected network failure") } catch (_: IOException) { }
            assertEquals("reply-id", repository.readDraft(target)?.clientId)
            repository.reply(target, request)
            assertEquals("new-id", repository.readDraft(target)?.clientId)
            repository.reply(target, request)
            assertEquals(2, attempts)
            assertEquals("sent", journal.read(transport.scope).outbox.single().state)
        } finally {
            backgroundScope.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    @Test fun reconnectRevalidatesSparseRangeAndStalledCursorReplacesLatest() = runBlocking {
        val directory = Files.createTempDirectory("inbox-window-test").toFile()
        val owner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val hub = FakeHub()
        val transport = FakeTransport()
        val snapshots = ListSnapshotStore(directory.resolve("snapshots"))
        try {
            val repository = repository(transport, directory, owner, hub, snapshots)
            transport.onMessages = { around, _, after ->
                assertEquals(10L, around)
                assertNull(after)
                page(row("first", 10, "原消息"), row("deleted", 20, "已删除"))
            }
            val observed = MutableStateFlow(LarkConversation())
            owner.launch { repository.conversation("feed", 10).collect { observed.value = it } }
            awaitState(observed) { it.verified && it.messages.size == 2 }
            hub.state.value = ConnectionState.WaitingForNetwork
            awaitState(observed) { !it.verified }
            transport.onMessages = { around, _, after -> when (after) {
                null -> { assertEquals(10L, around); page(row("first", 10, "编辑后"), newer = "10") }
                "10" -> page(row("middle", 30, "中间消息"), newer = "30")
                "30" -> page(row("last", 90, "末尾消息"))
                else -> error("Unexpected cursor")
            } }
            hub.state.value = ConnectionState.Online
            val reconciled = awaitState(observed) { it.verified && it.messages.lastOrNull()?.id == "last" }
            assertEquals(listOf("first", "middle", "last"), reconciled.messages.map { it.id })
            assertEquals("编辑后", (reconciled.messages.first().parts.single() as LarkPart.Text).text)
            assertFalse(reconciled.hasNewer)

            withTimeout(5_000) {
                while (snapshots.restore<JsonObject>("${transport.scope}:lark:feed:10")?.array("messages")?.size != 3) delay(1)
            }
            val restoredOwner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
            try {
                val offlineHub = FakeHub().apply { state.value = ConnectionState.WaitingForNetwork }
                val restored = repository(transport, directory, restoredOwner, offlineHub, snapshots)
                val cached = withTimeout(5_000) { restored.conversation("feed", 10).first { it.messages.size == 3 } }
                assertFalse(cached.verified)
                assertEquals(listOf("first", "middle", "last"), cached.messages.map { it.id })
            } finally { restoredOwner.coroutineContext[Job]!!.cancelAndJoin() }

            transport.onMessages = { around, _, after ->
                if (around == null && after == null) page(row("latest", 500, "最新消息"))
                else page(row("first", 10, "编辑后"), newer = "10")
            }
            hub.events.emit(LarkEvent(listOf("feed")))
            val latest = awaitState(observed) { it.verified && it.windowRevision == 1L }
            assertEquals(listOf("latest"), latest.messages.map { it.id })
            assertTrue(latest.error!!.contains("最新"))
            transport.onMessages = { _, _, _ -> page(row("latest", 500, "不完整的编辑"), sync = "local") }
            hub.events.emit(InboxEvent())
            val incomplete = awaitState(observed) { it.error == "消息记录尚未同步完整" }
            assertFalse(incomplete.verified)
            assertEquals("最新消息", (incomplete.messages.single().parts.single() as LarkPart.Text).text)
            assertEquals(LarkWindowSyncState.Incomplete, repository.syncStates.value["feed"])
        } finally {
            owner.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    @Test fun restartRecoversReplyWithSameIdAndUnknownReceiptNeedsEcho() = runBlocking {
        val directory = Files.createTempDirectory("inbox-recovery-test").toFile()
        val owner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val transport = FakeTransport()
        val hub = FakeHub().apply { state.value = ConnectionState.WaitingForNetwork }
        val target = ReplyTarget.Lark("feed")
        val request = ReplyRequest("回复内容", "stable-client-id")
        val journal = InboxReplyJournal(directory, "hub/generation")
        try {
            journal.update(transport.scope) { InboxReplyDocument(
                drafts = mapOf(target.storageKey() to InboxStoredDraft(request.text, request.clientId)),
                outbox = listOf(InboxPendingReply(target, request, "feed")),
            ) }
            val attempts = java.util.concurrent.atomic.AtomicInteger()
            transport.onReply = { id ->
                assertEquals(request.clientId, id)
                attempts.incrementAndGet()
                buildJsonObject { put("state", "unknown") }
            }
            val repository = repository(transport, directory, owner, hub)
            val observed = MutableStateFlow(LarkConversation())
            owner.launch { repository.conversation("feed", null).collect { observed.value = it } }
            withTimeout(5_000) { while (repository.replyStatus(target, request.clientId) != ReplyDelivery.Unknown) delay(1) }
            hub.state.value = ConnectionState.Online
            withTimeout(5_000) {
                while (repository.replyStatus(target, request.clientId) != ReplyDelivery.Unknown) delay(1)
            }
            awaitState(observed) { it.messages.any { message -> message.clientId == request.clientId && message.delivery == LarkDelivery.Failed && message.pendingDelivery == DeliveryState.Unknown } }
            assertEquals(0, attempts.get())
            assertEquals(ReplyDelivery.Unknown, repository.verifyReply(target, request.clientId))
            assertEquals(request.clientId, repository.readDraft(target)?.clientId)
            repository.saveDraft(target, ReplyDraft("后续草稿", "new-client-id"))
            transport.onMessages = { _, _, _ -> page(row("echo", 50, request.text, request.clientId)) }
            hub.events.emit(LarkEvent(listOf("feed")))
            awaitState(observed) { it.verified && it.messages.any { message -> message.id == "echo" } }
            assertEquals(ReplyDelivery.Sent, repository.replyStatus(target, request.clientId))
            assertEquals("new-client-id", repository.readDraft(target)?.clientId)
            assertEquals(0, attempts.get())
            assertEquals(1, observed.value.messages.count { it.clientId == request.clientId })
            journal.clear(transport.scope)
            assertEquals(InboxReplyDocument(), journal.read(transport.scope))
        } finally {
            owner.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    @Test fun offlineAiAndLarkRepliesShareQueueAndRestartReconcilesLostReceipt() = runBlocking {
        val directory = Files.createTempDirectory("inbox-offline-test").toFile()
        var owner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val transport = FakeTransport()
        val hub = FakeHub().apply { state.value = ConnectionState.WaitingForNetwork }
        val snapshots = ListSnapshotStore(directory.resolve("snapshots"))
        val ai = ReplyTarget.Inbox("item")
        val lark = ReplyTarget.Lark("feed")
        val aiRequest = ReplyRequest("同样的回复", "ai-reply")
        val larkRequest = ReplyRequest("同样的回复", "lark-reply")
        val calls = java.util.concurrent.CopyOnWriteArrayList<String>()
        val echoed = java.util.concurrent.atomic.AtomicBoolean(false)
        try {
            snapshots.save("${transport.scope}:inbox", buildJsonObject {
                put("items", buildJsonArray { add(buildJsonObject {
                    put("id", "item"); put("lark", buildJsonObject { put("feed_id", "feed") })
                }) })
            })
            var repository = repository(transport, directory, owner, hub, snapshots)
            withTimeout(5_000) { repository.snapshot.first { it.records.isNotEmpty() } }
            for ((target, request) in listOf(ai to aiRequest, lark to larkRequest)) {
                repository.saveDraft(target, ReplyDraft(request.text, request.clientId))
                try { repository.reply(target, request); fail("Expected queued delivery") } catch (_: IOException) { }
            }
            repository.saveDraft(ai, ReplyDraft("后续草稿", "new-draft"))
            val journal = InboxReplyJournal(directory, transport.scope)
            assertEquals(listOf("ai-reply", "lark-reply"), journal.read(transport.scope).outbox.map { it.request.clientId })
            assertTrue(journal.read(transport.scope).outbox.all { it.outboxState == OutboxState.WaitingForNetwork && it.feedId == "feed" })
            owner.coroutineContext[Job]!!.cancelAndJoin()
            transport.onMessages = { _, _, _ ->
                if (echoed.get()) page(row("ai-server", 20, aiRequest.text, aiRequest.clientId))
                else page(row("unrelated", 10, aiRequest.text, "different-id"))
            }
            transport.onReply = { id ->
                assertTrue(journal.read(transport.scope).outbox.any { it.request.clientId == id })
                calls += id
                if (id == aiRequest.clientId) {
                    echoed.set(true)
                    throw IOException("Receipt lost")
                }
                buildJsonObject { put("id", "lark-server") }
            }
            owner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
            hub.state.value = ConnectionState.Online
            repository = repository(transport, directory, owner, hub, snapshots)
            withTimeout(5_000) {
                while (repository.replyStatus(lark, larkRequest.clientId) != ReplyDelivery.Sent) delay(1)
            }
            // The second queued reply also fetches echoes, including the first reply's lost receipt.
            assertEquals(ReplyDelivery.Sent, repository.replyStatus(ai, aiRequest.clientId))
            coroutineScope {
                launch { repository.verifyReply(ai, aiRequest.clientId) }
                launch { repository.reply(lark, larkRequest) }
            }
            assertEquals(listOf("ai-reply", "lark-reply"), calls.toList())
            assertEquals("new-draft", repository.readDraft(ai)?.clientId)
            assertNull(repository.readDraft(lark))
            assertTrue(journal.read(transport.scope).outbox.all { it.state == "sent" })
        } finally {
            owner.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    @Test fun feishuRowsCarryTheChatsPictureAtThumbnailSizeAndMailRowsDoNot() = runBlocking {
        val directory = Files.createTempDirectory("inbox-avatar-test").toFile()
        val owner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        try {
            val transport = FakeTransport().apply {
                items = JsonArray(listOf(
                    buildJsonObject { put("id", "lark:chat:1"); put("source", "lark"); put("kind", "group"); put("title", "群")
                        put("avatar", "/api/v1/lark/media/key"); put("lark", buildJsonObject { put("feed_id", "chat") }) },
                    buildJsonObject { put("id", "lark:dm:1"); put("source", "lark"); put("kind", "dm"); put("title", "人")
                        put("lark", buildJsonObject { put("feed_id", "dm") }) },
                    buildJsonObject { put("id", "mail:1"); put("source", "mail"); put("kind", "mail"); put("title", "信")
                        put("avatar", "/api/v1/lark/media/other") },
                ))
            }
            val repository = repository(transport, directory, owner, FakeHub())
            // The pairing's first emission clears the list; refresh after it, not before.
            withTimeout(5_000) { repository.snapshot.first { it.contentSource != null } }
            repository.refresh()
            val avatars = repository.snapshot.value.records.associate { it.item.id to it.avatar }
            assertEquals("hub/generation:/api/v1/lark/media/key?size=144", avatars["lark:chat:1"]?.cacheKey)
            assertNull(avatars["lark:dm:1"])
            assertNull(avatars["mail:1"])
            avatars["lark:chat:1"]!!.read()
            avatars["lark:chat:1"]!!.read()
            // Read with the pairing at the thumbnail size, and once: the second comes from the bytes cache.
            assertEquals(listOf("/api/v1/lark/media/key?size=144"), transport.mediaRefs)
            // A refresh rebuilds the records; the same picture is the same value, so unchanged rows stay equal.
            val before = repository.snapshot.value.records
            repository.refresh()
            assertEquals(before, repository.snapshot.value.records)
        } finally {
            owner.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    private suspend fun awaitState(state: StateFlow<LarkConversation>, predicate: (LarkConversation) -> Boolean) =
        withTimeout(5_000) { state.first(predicate) }

    private fun repository(transport: FakeTransport, directory: java.io.File, owner: CoroutineScope,
        hub: FakeHub, snapshots: ListSnapshotStore? = null, sender: MainConversationSender? = null): RealInboxRepository = RealInboxRepository(
        object : InboxAssistantConnection { override val current = MutableStateFlow<InboxAssistantTransport?>(transport) },
        InboxReplyJournal(directory, "hub/generation"),
        object : SessionsRepository {
            override val machines = MutableStateFlow<List<Machine>>(emptyList())
            override val sessions = MutableStateFlow<List<Session>>(emptyList())
            override suspend fun refresh() = Unit
        },
        object : InteractionCenter {
            override val pending = MutableStateFlow<List<Interaction>>(emptyList())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: Resolution) = Unit
        }, owner, snapshots = snapshots, hubConnection = hub, mainConversationSender = sender,
    ) { _, _ -> error("unused") }

    private class FakeHub : HubConnection, OpenWindowReconciliationOwner {
        override val openWindowReconciliation = OpenWindowReconciliation()
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }

    private fun row(id: String, position: Long, text: String, clientId: String? = null) = buildJsonObject {
        put("id", id); put("position", position); put("text", text)
        clientId?.let { put("client_id", it) }
    }
    private fun page(vararg rows: JsonObject, newer: String? = null, sync: String = "complete") = buildJsonObject {
        put("messages", JsonArray(rows.toList())); put("sync_state", sync)
        put("has_newer", newer != null); put("has_older", false)
        put("newer_cursor", newer ?: rows.lastOrNull()?.number("position")?.toString())
        put("older_cursor", rows.firstOrNull()?.number("position")?.toString())
    }

    private class FakeTransport(override val scope: String = "hub/generation") : InboxAssistantTransport {
        var onReply: suspend (String) -> JsonObject = { JsonObject(emptyMap()) }
        var items = JsonArray(emptyList())
        val mediaRefs = mutableListOf<String>()
        var onMessages: suspend (Long?, String?, String?) -> JsonObject = { _, _, _ -> buildJsonObject {
            put("messages", JsonArray(emptyList())); put("sync_state", "complete")
        } }
        override suspend fun reply(itemId: String?, feedId: String?, text: String, clientId: String) = onReply(clientId)
        override suspend fun inbox() = buildJsonObject { put("items", items) }
        override suspend fun setDone(itemId: String, done: Boolean) = JsonObject(emptyMap())
        override suspend fun messages(feedId: String, around: Long?, before: String?, after: String?) = onMessages(around, before, after)
        override suspend fun assistant() = JsonObject(emptyMap())
        override suspend fun createConversation(main: Boolean) = JsonObject(emptyMap())
        override suspend fun pins() = JsonObject(emptyMap())
        override suspend fun mutes() = JsonObject(emptyMap())
        override suspend fun media(ref: String) = byteArrayOf(1).also { mediaRefs += ref }
    }
}
