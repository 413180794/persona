package persona.core.data

import persona.core.contracts.HubScope
import persona.core.contracts.SessionKey
import persona.core.contracts.api.MessageRequest
import persona.core.contracts.api.MessageEvidence
import persona.core.contracts.api.MessageTransport
import persona.core.contracts.api.MessageTransportResult
import persona.core.contracts.api.MainConversationResolver
import persona.core.contracts.api.PendingMainConversation
import persona.core.contracts.api.ChatComposerService
import persona.core.contracts.api.ComposerDelivery
import persona.core.contracts.api.DeliveryState
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.awaitCancellation
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class OutboxMessageSenderTest {
    private val scope = HubScope("test-hub", "generation-a")
    private val session = SessionKey("machine", "session")

    @Test fun composerUsesOriginalReadinessAndPersistsManualRefusalRetry() = runTest {
        val store = FakeStore()
        val original = FakeTransport(online = false)
        val sender = OutboxMessageSender(scope, store, original, backgroundScope)
        var requests = 0
        sender.connectComposer { transport ->
            object : MessageTransport by transport, ChatComposerService {
                override val deliveries = MutableStateFlow(emptyMap<String, ComposerDelivery>())
                override suspend fun send(request: MessageRequest): MessageTransportResult {
                    requests++
                    return MessageTransportResult.HubError("input_draft", "none", retryable = false)
                }
                override suspend fun upload(session: SessionKey, name: String, file: java.io.File): String = error("Unused")
                override suspend fun interrupt(session: SessionKey) = Unit
                override suspend fun retryRefused(clientMessageId: String, clearDraft: Boolean): ComposerDelivery {
                    sender.recordComposerDelivery(clientMessageId, ComposerDelivery(DeliveryState.Sending))
                    return ComposerDelivery(DeliveryState.Sent).also { sender.recordComposerDelivery(clientMessageId, it) }
                }
            }
        }.also { composer ->
            sender.send(session, "保留原消息", "retry")
            runCurrent()
            assertEquals(0, requests)
            original.readyToSend.value = true
            runCurrent()
            assertEquals(1, requests)
            assertEquals(OutboxState.Refused, store.state("retry"))
            composer.retryRefused("retry", true)
            assertEquals(OutboxState.Sent, store.state("retry"))
            assertEquals("保留原消息", store.entries.value.single().text)
            assertTrue(original.requests.isEmpty())
        }
        sender.close()
    }

    @Test fun offlineFirstMessagesSurviveRestartAndBindToOneMainConversation() = runTest {
        val store = FakeStore()
        val transport = FakeTransport(online = false)
        var creates = 0
        val resolver = object : MainConversationResolver {
            override suspend fun findMainConversation(): SessionKey? = null
            override suspend fun createMainConversation(): SessionKey { creates++; return session }
        }
        val first = OutboxMessageSender(scope, store, transport, backgroundScope)
        first.connectMainConversation(resolver)
        first.sendToMain("first", "main-a")
        first.sendToMain("shared context", "main-b")
        assertEquals(listOf("first", "shared context"), store.entries.value.map { it.text })
        assertTrue(store.entries.value.all { it.session == PendingMainConversation })
        runCurrent()
        assertEquals(0, creates)
        assertTrue(transport.requests.isEmpty())
        first.close()

        val restarted = OutboxMessageSender(scope, store, transport, backgroundScope)
        restarted.connectMainConversation(resolver)
        runCurrent()
        transport.answers += MessageTransportResult.NotSent
        transport.readyToSend.value = true
        runCurrent()
        assertEquals(1, creates)
        assertTrue(store.entries.value.all { it.session == session })
        assertEquals(OutboxState.WaitingForNetwork, store.state("main-a"))
        transport.readyToSend.value = false
        runCurrent()
        transport.answers += MessageTransportResult.Accepted
        transport.answers += MessageTransportResult.Accepted
        transport.readyToSend.value = true
        runCurrent()
        assertEquals(1, creates)
        assertTrue(store.entries.value.all { it.state == OutboxState.Sent })
        assertEquals(listOf("main-a", "main-a", "main-b"), transport.requests.map { it.clientMessageId })
        assertTrue(transport.requests.all { it.session == session })
        restarted.close()
    }

    @Test fun lostCreateReceiptNeverCreatesAgainAndRetainsInputUntilMainIsFound() = runTest {
        val store = FakeStore()
        val transport = FakeTransport(online = true)
        var creates = 0
        var remoteMain: SessionKey? = null
        val resolver = object : MainConversationResolver {
            override suspend fun findMainConversation(): SessionKey? = remoteMain
            override suspend fun createMainConversation(): SessionKey {
                creates++
                throw java.io.IOException("Receipt lost")
            }
        }
        val first = OutboxMessageSender(scope, store, transport, backgroundScope)
        first.connectMainConversation(resolver)
        first.sendToMain("keep this input", "main-a")
        runCurrent()
        assertEquals(1, creates)
        assertEquals("keep this input", store.entries.value.single().text)
        assertTrue(transport.requests.isEmpty())
        first.close()

        val restarted = OutboxMessageSender(scope, store, transport, backgroundScope)
        restarted.connectMainConversation(resolver)
        runCurrent()
        assertEquals(1, creates)
        assertTrue(transport.requests.isEmpty())
        transport.readyToSend.value = false
        runCurrent()
        remoteMain = session
        transport.answers += MessageTransportResult.Accepted
        transport.readyToSend.value = true
        runCurrent()
        assertEquals(1, creates)
        assertEquals(session, transport.requests.single().session)
        assertEquals("keep this input", transport.requests.single().text)
        assertEquals(OutboxState.Sent, store.state("main-a"))
        restarted.close()
    }

    @Test fun durableQueueWaitsForNetworkAndDistinguishesRefusalFromUncertainDelivery() = runTest {
        val store = FakeStore()
        val transport = FakeTransport(online = false)
        val sender = OutboxMessageSender(scope, store, transport, backgroundScope) { 100 }
        sender.send(session, "hello", "message-a", null)
        // send() returns only after persistence, without waiting for the network.
        assertEquals(OutboxState.Queued, store.state("message-a"))
        runCurrent()
        assertEquals(OutboxState.WaitingForNetwork, store.state("message-a"))
        assertTrue(transport.requests.isEmpty())

        transport.answers += MessageTransportResult.NotSent
        transport.readyToSend.value = true
        runCurrent()
        assertEquals(OutboxState.WaitingForNetwork, store.state("message-a"))
        transport.readyToSend.value = false
        runCurrent()
        transport.answers += MessageTransportResult.HubError("input_draft", "none")
        transport.readyToSend.value = true
        runCurrent()
        assertEquals(OutboxState.Refused, store.state("message-a"))

        transport.answers += MessageTransportResult.HubError(delivered = "unknown")
        sender.send(session, "next", "message-b", null)
        runCurrent()
        assertEquals(OutboxState.Verifying, store.state("message-b"))
        sender.confirmDelivered(session, "message-b", null, 101)
        runCurrent()
        assertEquals(OutboxState.Shown, store.state("message-b"))
        assertEquals(OutboxState.Unknown, OutboxMessageSender.classify(MessageTransportResult.HubError()))
        sender.close()
    }

    @Test fun catchUpChecksAllUncertainStatesBeforeSendingAndUsesTheSameOperationId() = runTest {
        val store = FakeStore()
        listOf(OutboxState.Sending, OutboxState.Unknown, OutboxState.Verifying).forEachIndexed { index, state ->
            store.enqueue(OutboxEntry("uncertain-$index", session, "text-$index", state = state, createdAt = 100))
        }
        store.enqueue(OutboxEntry("next", session, "next", createdAt = 101))
        val transport = FakeTransport(online = false, supportsClientOps = true)
        transport.evidence["uncertain-0"] = MessageEvidence.Receipt(MessageTransportResult.Accepted)
        transport.evidence["uncertain-1"] = MessageEvidence.Shown
        val sender = OutboxMessageSender(scope, store, transport, backgroundScope)
        runCurrent()
        sender.onReady()
        assertEquals(listOf("uncertain-0", "uncertain-1", "uncertain-2"), transport.lookups)
        assertTrue(transport.requests.isEmpty())
        assertEquals(OutboxState.Sent, store.state("uncertain-0"))
        assertEquals(OutboxState.Shown, store.state("uncertain-1"))
        transport.answers += MessageTransportResult.Accepted
        transport.answers += MessageTransportResult.Accepted
        transport.readyToSend.value = true
        runCurrent()
        assertEquals(listOf("uncertain-2", "next"), transport.requests.map { it.clientMessageId })
        assertEquals("uncertain-2", transport.requests.first().headers["X-Jarvis-Op-Id"])
        sender.close()
    }

    @Test fun legacyUnknownDoesNotBlockLaterMessagesAndTimesOutToManualDisposition() = runTest {
        val store = FakeStore()
        val transport = FakeTransport(online = false)
        val sender = OutboxMessageSender(scope, store, transport, backgroundScope,
            reviewTimeoutMillis = 100, reconciliationTimeoutMillis = 10)
        sender.send(session, "cancel", "cancel")
        assertTrue(sender.cancel("cancel"))
        sender.send(session, "unknown", "unknown")
        sender.send(session, "later", "later")
        transport.answers += MessageTransportResult.NoReceipt
        transport.answers += MessageTransportResult.Accepted
        transport.readyToSend.value = true
        runCurrent()
        assertEquals(listOf("unknown", "later"), transport.requests.map { it.clientMessageId })
        assertTrue(!sender.cancel("unknown"))
        assertTrue(!sender.retry("unknown"))
        // A hung reconciliation is bounded and cannot hide the manual review deadline.
        transport.hangLookup = true
        transport.readyToSend.value = false
        runCurrent()
        transport.readyToSend.value = true
        runCurrent()
        advanceTimeBy(111)
        runCurrent()
        assertEquals(OutboxState.NeedsConfirmation, store.state("unknown"))
        assertEquals(2, transport.requests.size)
        assertTrue(sender.confirm("unknown", delivered = false))
        assertEquals(OutboxState.Refused, store.state("unknown"))
        transport.hangLookup = false
        transport.answers += MessageTransportResult.NoReceipt
        assertTrue(sender.retry("unknown"))
        runCurrent()
        assertTrue(sender.confirm("unknown", delivered = true))
        runCurrent()
        assertEquals(OutboxState.Sent, store.state("unknown"))
        assertTrue(sender.outbox.value.none { it.clientMessageId == "cancel" })
        sender.close()
    }

    @Test fun interruptedAndUnknownRequestsOnlyRetryWithDurableHubDeduplication() = runTest {
        val store = FakeStore()
        store.enqueue(OutboxEntry("message-a", session, "hello", state = OutboxState.Sending, createdAt = 100))
        val legacy = FakeTransport(online = true)
        val first = OutboxMessageSender(scope, store, legacy, backgroundScope)
        runCurrent()
        assertEquals(OutboxState.Unknown, store.state("message-a"))
        assertTrue(legacy.requests.isEmpty())
        first.close()

        val modern = FakeTransport(online = true, supportsClientOps = true)
        modern.answers += MessageTransportResult.NoReceipt
        modern.answers += MessageTransportResult.Accepted
        val second = OutboxMessageSender(scope, store, modern, backgroundScope)
        runCurrent()
        assertEquals(OutboxState.Sent, store.state("message-a"))
        assertEquals(2, modern.requests.size)
        modern.requests.forEach {
            assertEquals("message-a", it.headers["X-Jarvis-Op-Id"])
            assertEquals(scope, it.scope)
        }
        assertTrue(store.transitions.contains(OutboxState.Unknown to OutboxState.Sending))
        second.close()
    }

    private class FakeTransport(online: Boolean, override val supportsClientOps: Boolean = false) : MessageTransport {
        override val readyToSend = MutableStateFlow(online)
        val answers = ArrayDeque<MessageTransportResult>()
        val requests = mutableListOf<MessageRequest>()
        val lookups = mutableListOf<String>()
        val evidence = mutableMapOf<String, MessageEvidence>()
        var hangLookup = false
        override suspend fun reconcile(request: MessageRequest, createdAt: Long, allowTextMatch: Boolean): MessageEvidence? {
            lookups += request.clientMessageId
            if (hangLookup) awaitCancellation()
            return evidence[request.clientMessageId]
        }
        override suspend fun send(request: MessageRequest): MessageTransportResult {
            requests += request
            return answers.removeFirst()
        }
    }

    private class FakeStore : OutboxStore {
        override val entries = MutableStateFlow<List<OutboxEntry>>(emptyList())
        private var main: SessionKey? = null
        private var creationClaimed = false
        override suspend fun mainConversation(): SessionKey? = main
        override suspend fun claimMainCreation(): Boolean {
            if (creationClaimed) return false
            creationClaimed = true
            return true
        }
        override suspend fun bindMainConversation(session: SessionKey) {
            main = session
            entries.value = entries.value.map { if (it.mainTarget) it.copy(session = session) else it }
        }
        val transitions = mutableListOf<Pair<OutboxState, OutboxState>>()
        fun state(id: String) = entries.value.single { it.clientMessageId == id }.state
        override suspend fun enqueue(entry: OutboxEntry) { entries.value += entry }
        override suspend fun transition(id: String, from: Set<OutboxState>, to: OutboxState, error: String?): Boolean {
            val old = entries.value.single { it.clientMessageId == id }
            if (old.state !in from) return false
            transitions += old.state to to
            entries.value = entries.value.map { if (it == old) it.copy(state = to, error = error) else it }
            return true
        }
        override suspend fun recoverInterrupted() {
            entries.value = entries.value.map { if (it.state == OutboxState.Sending) it.copy(state = OutboxState.Unknown) else it }
        }
    }
}
