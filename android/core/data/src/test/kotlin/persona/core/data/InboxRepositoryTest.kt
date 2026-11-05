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

    @Test fun bulkDoneIsOneRequestWithVersionsAndPutsBackWhatTheHubDidNotFile() = runBlocking {
        val directory = Files.createTempDirectory("inbox-bulk-test").toFile()
        val owner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        try {
            fun item(id: String, version: String) = buildJsonObject {
                put("id", id); put("source", "mail"); put("kind", "mail"); put("title", id); put("version", version)
            }
            val transport = FakeTransport().apply {
                items = JsonArray(listOf(item("a", "va"), item("b", "vb"), item("c", "vc")))
                onBulk = { _, _ ->
                    // As the hub: a is filed; b took a new mail (its version moved), so it is not.
                    val filedA = buildJsonObject {
                        put("id", "a"); put("source", "mail"); put("kind", "mail"); put("title", "a"); put("version", "va")
                        put("done_at", "2026-11-04T08:00:00Z"); put("done_by", "user")
                    }
                    items = JsonArray(listOf(filedA, item("b", "vb2"), item("c", "vc")))
                    buildJsonObject {
                    put("changed", JsonArray(listOf(filedA)))
                    put("unchanged", JsonArray(emptyList()))
                    put("skipped", JsonArray(listOf(buildJsonObject { put("id", "b"); put("reason", "changed") })))
                } }
            }
            val repository = repository(transport, directory, owner, FakeHub())
            withTimeout(5_000) { repository.snapshot.first { it.contentSource != null } }
            repository.refresh()
            val listed = repository.snapshot.value.records.associate { it.item.id to it.item.version }
            assertEquals(mapOf("a" to "va", "b" to "vb", "c" to "vc"), listed)

            val result = repository.setDone(listOf(InboxDoneTarget("a", "va"), InboxDoneTarget("b", "vb")), true)
            // One request carrying each version; b took new messages since and stays open; c was not asked for.
            assertEquals(listOf(listOf(InboxDoneTarget("a", "va"), InboxDoneTarget("b", "vb")) to true), transport.bulk)
            assertEquals(listOf("a"), result.changed)
            assertEquals(listOf("b"), result.changedSince)
            // Refreshed for b's new mail: its new version is what the next bulk sends.
            val after = repository.snapshot.value.records.associate { it.item.id to (it.item.isDone to it.item.version) }
            assertEquals(mapOf("a" to (true to "va"), "b" to (false to "vb2"), "c" to (false to "vc")), after)

            // A failure puts every item back.
            transport.onBulk = { _, _ -> throw IOException("offline") }
            assertThrows(IOException::class.java) { runBlocking { repository.setDone(listOf(InboxDoneTarget("c", "vc")), true) } }
            assertFalse(repository.snapshot.value.records.first { it.item.id == "c" }.item.isDone)
        } finally {
            owner.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    // A Feishu chat that keeps announcing itself unchanged, and a burst of inbox changes, over a shown list on a slow
    // hub: the list never goes back to loading (which inserts a row and pushes it down), Feishu events do not refresh
    // the inbox (the hub sends {"type":"inbox"} when it changes), the burst is one refresh instead of a queue, and an
    // event that comes in during a refresh still gets a refresh after it.
    @Test fun hubEventBurstsOverAShownListRefreshOnceWithoutLoading() = runBlocking<Unit> {
        val directory = Files.createTempDirectory("inbox-burst-test").toFile()
        val owner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val window = 200L
        try {
            val gate = java.util.concurrent.atomic.AtomicReference<CompletableDeferred<Unit>?>(null)
            val entered = java.util.concurrent.atomic.AtomicReference(CompletableDeferred<Unit>())
            val transport = FakeTransport().apply {
                items = JsonArray(listOf(buildJsonObject { put("id", "lark:chat:1"); put("source", "lark"); put("kind", "group")
                    put("title", "群"); put("lark", buildJsonObject { put("feed_id", "chat") }) }))
                onInbox = { entered.get().complete(Unit); gate.get()?.await() ?: delay(50) }
            }
            val hub = FakeHub()
            val repository = repository(transport, directory, owner, hub, refreshWindowMillis = window)
            // The pairing's first emission clears the list; refresh after it, not before.
            withTimeout(5_000) { repository.snapshot.first { it.contentSource != null } }
            repository.refresh()
            assertEquals(1, repository.snapshot.value.records.size)
            val loading = java.util.Collections.synchronizedList(mutableListOf<Boolean>())
            owner.launch { repository.snapshot.collect { loading += it.loading } }
            // The page being shown starts the polling fallback with one refresh of its own; let it finish.
            withTimeout(5_000) { repository.snapshot.first { it.refreshes >= 2 } }
            loading.clear()
            suspend fun settled(): Int { delay(window * 4); return transport.inboxCalls.get() }
            val before = settled()

            repeat(6) { hub.events.emit(LarkEvent(listOf("chat"))) }
            assertEquals("Feishu events alone do not refresh the inbox", before, settled())

            repeat(6) { hub.events.emit(InboxEvent()) }
            withTimeout(5_000) { while (transport.inboxCalls.get() == before) delay(10) }
            val refreshes = settled() - before
            assertTrue("a burst of inbox events is merged into one refresh, was $refreshes", refreshes in 1..2)
            assertFalse("a shown list never goes back to loading", loading.any { it })
            assertEquals(listOf("lark:chat:1"), repository.snapshot.value.records.map { it.item.id })

            val held = CompletableDeferred<Unit>()
            gate.set(held)
            entered.set(CompletableDeferred())
            val during = transport.inboxCalls.get()
            hub.events.emit(InboxEvent())
            withTimeout(5_000) { entered.get().await() }
            entered.set(CompletableDeferred())
            hub.events.emit(InboxEvent())
            gate.set(null)
            held.complete(Unit)
            withTimeout(5_000) { entered.get().await() }
            assertEquals("an event during a refresh gets one more refresh", during + 2, settled())
        } finally {
            owner.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    // A shown list over a reconnect (the hub goes Connecting and back Online, which refreshes) and over refreshes that
    // fail: the list stays, it never goes back to loading, and a failure is an error over the list rather than a reset.
    @Test fun reconnectsAndFailedRefreshesOverAShownListNeverShowLoading() = runBlocking<Unit> {
        val directory = Files.createTempDirectory("inbox-reconnect-test").toFile()
        val owner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        try {
            val failing = java.util.concurrent.atomic.AtomicBoolean(false)
            val transport = FakeTransport().apply {
                items = JsonArray(listOf(buildJsonObject { put("id", "lark:chat:1"); put("source", "lark"); put("kind", "group")
                    put("title", "群"); put("lark", buildJsonObject { put("feed_id", "chat") }) }))
                onInbox = { delay(50); if (failing.get()) throw IOException("hub went away") }
            }
            val hub = FakeHub()
            val repository = repository(transport, directory, owner, hub, refreshWindowMillis = 100)
            withTimeout(5_000) { repository.snapshot.first { it.contentSource != null } }
            repository.refresh()
            val seen = java.util.Collections.synchronizedList(mutableListOf<InboxSnapshot>())
            owner.launch { repository.snapshot.collect { seen += it } }
            withTimeout(5_000) { repository.snapshot.first { it.refreshes >= 2 } }
            seen.clear()

            repeat(2) {
                hub.state.value = ConnectionState.Connecting
                delay(100)
                hub.state.value = ConnectionState.Online
                val done = repository.snapshot.value.refreshes
                withTimeout(5_000) { repository.snapshot.first { it.refreshes > done } }
            }
            failing.set(true)
            val calls = transport.inboxCalls.get()
            hub.state.value = ConnectionState.Connecting
            delay(100)
            hub.state.value = ConnectionState.Online
            hub.events.emit(InboxEvent())
            withTimeout(5_000) { while (transport.inboxCalls.get() < calls + 2) delay(10) }
            withTimeout(5_000) { repository.snapshot.first { it.error != null } }

            assertFalse("a shown list never goes back to loading", seen.any { it.loading })
            assertTrue("the list stays through reconnects and failures", seen.all { it.records.map { r -> r.item.id } == listOf("lark:chat:1") })
            failing.set(false)
            repository.refresh()
            assertNull(repository.snapshot.value.error)
        } finally {
            owner.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    // An empty list still shows that it is loading while the first refresh runs.
    @Test fun emptyListShowsLoadingWhileRefreshing() = runBlocking<Unit> {
        val directory = Files.createTempDirectory("inbox-empty-loading-test").toFile()
        val owner = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        try {
            val started = CompletableDeferred<Unit>()
            val release = CompletableDeferred<Unit>()
            val transport = FakeTransport().apply { onInbox = { started.complete(Unit); release.await() } }
            val repository = repository(transport, directory, owner, FakeHub())
            withTimeout(5_000) { started.await() }
            withTimeout(5_000) { repository.snapshot.first { it.loading } }
            release.complete(Unit)
            withTimeout(5_000) { repository.snapshot.first { !it.loading } }
        } finally {
            owner.coroutineContext[Job]!!.cancelAndJoin()
            directory.deleteRecursively()
        }
    }

    private suspend fun awaitState(state: StateFlow<LarkConversation>, predicate: (LarkConversation) -> Boolean) =
        withTimeout(5_000) { state.first(predicate) }

    private fun repository(transport: FakeTransport, directory: java.io.File, owner: CoroutineScope,
        hub: FakeHub, snapshots: ListSnapshotStore? = null, sender: MainConversationSender? = null,
        refreshWindowMillis: Long = 1_000): RealInboxRepository = RealInboxRepository(
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
        refreshWindowMillis = refreshWindowMillis,
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
        val inboxCalls = java.util.concurrent.atomic.AtomicInteger()
        var onInbox: suspend () -> Unit = {}
        override suspend fun inbox(): JsonObject { inboxCalls.incrementAndGet(); onInbox(); return buildJsonObject { put("items", items) } }
        override suspend fun setDone(itemId: String, done: Boolean) = JsonObject(emptyMap())
        val bulk = mutableListOf<Pair<List<InboxDoneTarget>, Boolean>>()
        var onBulk: suspend (List<InboxDoneTarget>, Boolean) -> JsonObject = { _, _ -> JsonObject(emptyMap()) }
        override suspend fun setDone(items: List<InboxDoneTarget>, done: Boolean): JsonObject { bulk += items to done; return onBulk(items, done) }
        override suspend fun messages(feedId: String, around: Long?, before: String?, after: String?) = onMessages(around, before, after)
        override suspend fun assistant() = JsonObject(emptyMap())
        override suspend fun createConversation(main: Boolean) = JsonObject(emptyMap())
        override suspend fun pins() = JsonObject(emptyMap())
        override suspend fun mutes() = JsonObject(emptyMap())
        override suspend fun media(ref: String) = byteArrayOf(1).also { mediaRefs += ref }
    }
}
