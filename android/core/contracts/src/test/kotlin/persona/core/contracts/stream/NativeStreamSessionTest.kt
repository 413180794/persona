package persona.core.contracts.stream

import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.generated.*

class NativeStreamSessionTest {
    private val identity = NativeStreamIdentity("owner", "group-a", "example", "page", "navigation-a")
    private class Transport : NativeStreamTransport {
        val requests = mutableListOf<ExtensionUIStreamRequest>()
        var failure: Exception? = null
        override suspend fun send(request: ExtensionUIStreamRequest) { requests.add(request); failure?.let { throw it } }
    }
    private fun session(transport: Transport, scope: NativeStreamIdentity = identity): NativeStreamSession {
        var sequence = 0
        return NativeStreamSession(scope, ExtensionUIContext(platform = "android", locale = "en", uiVersion = 6,
            stream = ExtensionUIStreamCapability(protocol = 1, components = listOf("button", "field", "text"))),
            transport, id = { "id-${++sequence}" })
    }
    private fun snapshot(epoch: String = "epoch-a", version: Int = 1) = """{"protocol":1,"session":"session-a","epoch":"$epoch","version":$version,"root_key":"root","view":{"sections":[{"key":"section","items":[{"type":"button","key":"button","title":"Run","action":{"id":"run"}},{"type":"field","key":"draft","name":"body","label":"Body","kind":"text","value":"initial"},{"type":"text","key":"text","text":"Original"}]}]}}"""
    private fun delta(op: String = """{"op":"replace","key":"text","item":{"type":"text","key":"text","text":"Updated"}}""", epoch: String = "epoch-a", base: Int = 1) =
        """{"protocol":1,"session":"session-a","epoch":"$epoch","base_version":$base,"version":${base + 1},"operations":[$op]}"""
    private suspend fun open(session: NativeStreamSession, transport: Transport) {
        session.visibility(true, true)
        assertEquals(NativeStreamPhase.Opening, session.state.value.phase)
        val request = transport.requests.last() as ExtensionUIStreamOpenRequest
        assertEquals(1, request.protocol)
        assertTrue(session.snapshot(session.identity, request.requestId, snapshot()))
    }

    @Test fun coldOpenRequiresSnapshotBeforeEventsOrDeltas() = runBlocking {
        val transport = Transport(); val session = session(transport)
        assertFalse(session.event("button", "run", "press", JsonNull))
        session.visibility(true, true)
        assertFalse(session.delta(identity, delta()))
        assertFalse(session.snapshot(identity, "wrong-request", snapshot()))
        assertEquals(1, transport.requests.size)
        assertTrue(session.snapshot(identity, (transport.requests[0] as ExtensionUIStreamOpenRequest).requestId, snapshot()))
        assertTrue(session.event("button", "run", "press", JsonNull))
    }

    @Test fun callbacksAreLocalAndEventsSerializeUntilAcknowledged() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        assertFalse(session.event("text", "run", "press", JsonNull))
        assertFalse(session.event("root", "run", "press", JsonNull))
        assertFalse(session.event("button", "run", "change", JsonNull))
        assertTrue(session.event("button", "run", "press", JsonPrimitive(false)))
        assertTrue(session.event("button", "run", "press", JsonPrimitive(0)))
        assertEquals(1, transport.requests.filterIsInstance<ExtensionUIStreamEventRequest>().size)
        val first = transport.requests.last() as ExtensionUIStreamEventRequest
        session.acknowledged(identity.copy(hubGroup = "group-b"), first.requestId)
        assertEquals(1, transport.requests.filterIsInstance<ExtensionUIStreamEventRequest>().size)
        session.acknowledged(identity, first.requestId)
        val second = transport.requests.last() as ExtensionUIStreamEventRequest
        assertNotEquals(first.interaction.eventId, second.interaction.eventId)
        assertEquals(JsonPrimitive(0), second.interaction.value)
    }

    @Test fun safeUnsentRetryRetainsBothIdentifiersAndPayload() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        transport.failure = NativeStreamNotSent()
        session.event("button", "run", "press", JsonPrimitive(""))
        val first = transport.requests.last()
        transport.failure = null
        session.retryUnsent()
        assertSame(first, transport.requests.last())
        assertEquals(2, transport.requests.filterIsInstance<ExtensionUIStreamEventRequest>().size)
    }

    @Test fun uncertainWritesNeverRetryAndRecoveryOnlyRequestsSnapshot() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        transport.failure = IllegalStateException("Unknown delivery")
        session.event("button", "run", "press", JsonNull)
        transport.failure = null
        session.retryUnsent()
        assertEquals(1, transport.requests.filterIsInstance<ExtensionUIStreamEventRequest>().size)
        // Pending recovery is drained by the caller's lifecycle/refresh entry, not by replaying the write.
        session.requestSnapshot()
        assertTrue(transport.requests.last() is ExtensionUIStreamSnapshotRequest)
        assertEquals(NativeStreamPhase.Recovering, session.state.value.phase)
    }

    @Test fun draftsSurviveTextUpdatesMovesAndSnapshotButDisappearOnRemoval() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        assertTrue(session.edit("draft", JsonPrimitive("local draft")))
        assertTrue(session.delta(identity, delta()))
        assertEquals(JsonPrimitive("local draft"), session.state.value.drafts.getValue("draft").value)
        assertTrue(session.delta(identity, delta("""{"op":"move","key":"draft","parent":"section","before":"button"}""", base = 2)))
        session.requestSnapshot()
        val request = transport.requests.last() as ExtensionUIStreamSnapshotRequest
        assertTrue(session.snapshot(identity, request.requestId, snapshot(version = 3)))
        assertEquals(JsonPrimitive("local draft"), session.state.value.drafts.getValue("draft").value)
        assertTrue(session.delta(identity, delta("""{"op":"remove","key":"draft"}""", base = 3)))
        assertTrue(session.state.value.drafts.isEmpty())
    }

    @Test fun suspendStopsHiddenAndBackgroundEventsAndResumeRequiresSnapshot() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        session.visibility(false, true)
        assertTrue(transport.requests.last() is ExtensionUIStreamSuspendRequest)
        assertFalse(session.event("button", "run", "press", JsonNull))
        assertFalse(session.delta(identity, delta()))
        session.visibility(true, false)
        assertFalse(session.event("button", "run", "press", JsonNull))
        session.visibility(true, true)
        val resume = transport.requests.last() as ExtensionUIStreamResumeRequest
        assertFalse(session.delta(identity, delta()))
        assertTrue(session.snapshot(identity, resume.requestId, snapshot()))
        assertTrue(session.delta(identity, delta()))
    }

    @Test fun reconnectDropsQueuedWritesRejectsStaleEpochsAndRequiresFreshSnapshot() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        session.event("button", "run", "press", JsonNull)
        session.event("button", "run", "press", JsonNull)
        session.connection(false)
        assertFalse(session.delta(identity, delta()))
        session.connection(true)
        val resume = transport.requests.last() as ExtensionUIStreamResumeRequest
        assertFalse(session.delta(identity, delta()))
        assertTrue(session.snapshot(identity, resume.requestId, snapshot(epoch = "epoch-b")))
        assertEquals(0, session.state.value.pendingEvents)
        val oldTree = session.state.value.tree
        assertFalse(session.delta(identity, delta()))
        assertSame(oldTree, session.state.value.tree)
        val recover = transport.requests.last() as ExtensionUIStreamSnapshotRequest
        assertFalse(session.snapshot(identity, recover.requestId, snapshot()))
        assertTrue(session.snapshot(identity, recover.requestId, snapshot(epoch = "epoch-b")))
        assertEquals(1, transport.requests.filterIsInstance<ExtensionUIStreamEventRequest>().size)
    }

    @Test fun identityIsolatesOwnerGroupExtensionContributionAndNavigation() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        val others = listOf(identity.copy(owner = "other"), identity.copy(hubGroup = "group-b"),
            identity.copy(extension = "other"), identity.copy(contribution = "other"), identity.copy(navigation = "other"))
        others.forEach { assertFalse(session.delta(it, delta())) }
        assertEquals(1, session.state.value.tree!!.snapshot.version)
        val otherTransport = Transport(); val other = session(otherTransport, identity.copy(hubGroup = "group-b"))
        open(other, otherTransport)
        session.edit("draft", JsonPrimitive("mine"))
        assertTrue(other.state.value.drafts.isEmpty())
    }

    @Test fun overflowRollsBackAndNeverReplaysPendingWrites() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        repeat(64) { assertTrue(session.event("button", "run", "press", JsonNull)) }
        assertFalse(session.event("button", "run", "press", JsonNull))
        assertEquals(NativeStreamPhase.Recovering, session.state.value.phase)
        assertEquals(0, session.state.value.pendingEvents)
        assertEquals(1, transport.requests.filterIsInstance<ExtensionUIStreamEventRequest>().size)
        assertTrue(transport.requests.last() is ExtensionUIStreamSnapshotRequest)
    }

    @Test fun valuesBoundedAndSecretsNeverBecomeDrafts() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        assertFalse(session.event("button", "run", "press", buildJsonObject { put("nested", buildJsonObject { put("x", 1) }) }))
        assertFalse(session.event("button", "run", "press", JsonPrimitive("x".repeat(4097))))
        assertTrue(session.delta(identity, delta("""{"op":"replace","key":"draft","item":{"type":"field","key":"draft","name":"secret","label":"Secret","kind":"secret"}}""")))
        assertFalse(session.edit("draft", JsonPrimitive("do not send")))
    }

    @Test fun generationDisposalReopensWithoutAcceptingOldSessionOrLosingDrafts() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        session.edit("draft", JsonPrimitive("local"))
        val old = session.state.value.tree
        session.reopen()
        val request = transport.requests.last() as ExtensionUIStreamOpenRequest
        assertSame(old, session.state.value.tree)
        assertFalse(session.snapshot(identity, request.requestId, snapshot()))
        assertTrue(session.snapshot(identity, request.requestId, snapshot(epoch = "epoch-b").replace("session-a", "session-b")))
        assertEquals(JsonPrimitive("local"), session.state.value.drafts.getValue("draft").value)
        assertFalse(session.delta(identity, delta()))
    }

    @Test fun rowControlDraftAndCallbackRemainNodeLocalAcrossUnrelatedUpdates() = runBlocking {
        val transport = Transport(); val session = NativeStreamSession(identity,
            ExtensionUIContext(stream = ExtensionUIStreamCapability(protocol = 1, components = listOf("row", "text"))), transport)
        session.visibility(true, true)
        val request = transport.requests.last() as ExtensionUIStreamOpenRequest
        val rowSnapshot = """{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"root_key":"root","view":{"sections":[{"key":"section","items":[{"type":"row","key":"control","title":"Control","control":{"kind":"switch","name":"enabled","on":false,"action":{"id":"toggle"}}},{"type":"text","key":"text","text":"Original"}]}]}}"""
        assertTrue(session.snapshot(identity, request.requestId, rowSnapshot))
        assertTrue(session.edit("control", JsonPrimitive(true)))
        assertTrue(session.delta(identity, delta()))
        assertEquals(JsonPrimitive(true), session.state.value.drafts.getValue("control").value)
        assertTrue(session.event("control", "toggle", "change", JsonPrimitive(true)))
        assertFalse(session.event("section", "toggle", "change", JsonPrimitive(true)))
    }

    @Test fun lateOpenAfterNavigationRemovalIsClosedWithoutDisplayingIt() = runBlocking {
        val transport = Transport(); val session = session(transport)
        session.visibility(true, true)
        val open = transport.requests.last() as ExtensionUIStreamOpenRequest
        session.close()
        assertFalse(session.snapshot(identity, open.requestId, snapshot()))
        assertTrue(transport.requests.last() is ExtensionUIStreamCloseRequest)
        assertNull(session.state.value.tree)
        assertEquals(NativeStreamPhase.Closed, session.state.value.phase)
    }

    @Test fun closingOfflineSendsOnlyIdempotentCloseAfterReconnect() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        session.connection(false)
        session.close()
        session.connection(true)
        assertTrue(transport.requests.last() is ExtensionUIStreamCloseRequest)
        assertEquals(NativeStreamPhase.Closed, session.state.value.phase)
    }

    @Test fun unadvertisedComponentAndOversizedRequestRecoverOrRejectAtomically() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        val old = session.state.value.tree
        assertFalse(session.delta(identity, delta("""{"op":"replace","key":"text","item":{"type":"divider","key":"text"}}""")))
        assertSame(old, session.state.value.tree)
        val request = transport.requests.last() as ExtensionUIStreamSnapshotRequest
        assertTrue(session.snapshot(identity, request.requestId, snapshot()))
        val large = JsonObject((1..17).associate { "field-$it" to JsonPrimitive("x".repeat(4096)) })
        assertFalse(session.event("button", "run", "press", large))
        assertEquals(0, session.state.value.pendingEvents)
    }

    @Test fun queuedEventsAreNotRebasedOrReplayedAfterTreeChanges() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        session.event("button", "run", "press", JsonNull)
        val first = transport.requests.last() as ExtensionUIStreamEventRequest
        session.event("button", "run", "press", JsonNull)
        assertTrue(session.delta(identity, delta()))
        session.acknowledged(identity, first.requestId)
        assertEquals(1, transport.requests.filterIsInstance<ExtensionUIStreamEventRequest>().size)
        assertEquals(0, session.state.value.pendingEvents)
    }

    @Test fun incompatibleOpenSnapshotClosesItsAllocationInsteadOfReopeningForever() = runBlocking {
        val transport = Transport(); val session = session(transport)
        session.visibility(true, true)
        val open = transport.requests.last() as ExtensionUIStreamOpenRequest
        val incompatible = snapshot().replace("\"type\":\"text\",\"key\":\"text\",\"text\":\"Original\"", "\"type\":\"divider\",\"key\":\"text\"")
        assertFalse(session.snapshot(identity, open.requestId, incompatible))
        val close = transport.requests.last() as ExtensionUIStreamCloseRequest
        assertEquals("session-a", close.session)
        assertEquals("epoch-a", close.epoch)
        assertEquals(1, transport.requests.filterIsInstance<ExtensionUIStreamOpenRequest>().size)
        assertEquals(NativeStreamPhase.Closed, session.state.value.phase)
        assertNull(session.state.value.tree)
    }

    @Test fun incompatibleReopenedSnapshotClosesTheNewAllocationAsWellAsTheOldGeneration() = runBlocking {
        val transport = Transport(); val session = session(transport)
        session.rendererProfile { view -> if (view["title"] == JsonPrimitive("unsupported")) listOf("view.title") else emptyList() }
        open(session, transport)
        session.reopen()
        val request = transport.requests.last() as ExtensionUIStreamOpenRequest
        val incompatible = snapshot(epoch = "epoch-b").replace("session-a", "session-b").replace("\"view\":{", "\"view\":{\"title\":\"unsupported\",")
        assertFalse(session.snapshot(identity, request.requestId, incompatible))
        assertEquals(NativeStreamPhase.Closed, session.state.value.phase)
        assertEquals(setOf("session-a" to "epoch-a", "session-b" to "epoch-b"), transport.requests.filterIsInstance<ExtensionUIStreamCloseRequest>().map { it.session to it.epoch }.toSet())
    }

    @Test fun unsentLifecycleRequestsCanBeRetriedWhileHiddenOrClosed() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        transport.failure = NativeStreamNotSent()
        session.visibility(false, true)
        val suspend = transport.requests.last()
        transport.failure = null
        session.retryUnsent()
        assertSame(suspend, transport.requests.last())
        assertEquals(2, transport.requests.filterIsInstance<ExtensionUIStreamSuspendRequest>().size)
        transport.failure = NativeStreamNotSent()
        session.close()
        val close = transport.requests.last()
        transport.failure = null
        session.retryUnsent()
        assertSame(close, transport.requests.last())
        assertEquals(2, transport.requests.filterIsInstance<ExtensionUIStreamCloseRequest>().size)
        assertEquals(NativeStreamPhase.Closed, session.state.value.phase)
        session.close(); session.retryUnsent()
        assertEquals(2, transport.requests.filterIsInstance<ExtensionUIStreamCloseRequest>().size)
    }

    @Test fun explicitRetryIntentSurvivesSenderContention() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        transport.failure = NativeStreamNotSent()
        session.event("button", "run", "press", JsonNull)
        val original = transport.requests.last()
        transport.failure = null
        // Hold sender ownership across the retry, as a concurrent flush does during handoff.
        val field = NativeStreamSession::class.java.getDeclaredField("sending").also { it.isAccessible = true }
        val sending = field.get(session) as kotlinx.coroutines.sync.Mutex
        sending.lock()
        session.retryUnsent()
        sending.unlock()
        session.visibility(true, true) // drain only: no new snapshot or event
        assertEquals(2, transport.requests.filterIsInstance<ExtensionUIStreamEventRequest>().size)
        assertSame(original, transport.requests.last())
        session.retryUnsent()
        assertEquals(2, transport.requests.filterIsInstance<ExtensionUIStreamEventRequest>().size)
    }

    @Test fun expectedTreePreventsConfirmationAndScheduledEventsFromRebasing() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        val confirmed = session.state.value.tree!!
        assertTrue(session.delta(identity, delta()))
        assertFalse(session.event("button", "run", "press", JsonNull, expectedTree = confirmed))
        assertTrue(session.event("button", "run", "press", JsonNull, expectedTree = session.state.value.tree))
        assertEquals(2L, (transport.requests.last() as ExtensionUIStreamEventRequest).interaction.version)
    }

    @Test fun productionCodecRetainsAllScalarAndRequiredEmptyEventValues() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        val values = listOf(JsonNull, JsonPrimitive(false), JsonPrimitive(0), JsonPrimitive(""),
            JsonArray(emptyList()), JsonObject(emptyMap()), buildJsonObject { put("zero", 0); put("false", false); put("null", JsonNull); put("empty", "") })
        values.forEach { value ->
            assertTrue(session.event("button", "run", "press", value))
            val event = transport.requests.last() as ExtensionUIStreamEventRequest
            val encoded = persona.core.contracts.JsonCodec.encodeToString(ExtensionUIStreamRequest.serializer(), event)
            val raw = NativeStreamSchema.parse(encoded, "UIStreamRequest")
            assertEquals(value, raw.getValue("interaction").jsonObject.getValue("value"))
            session.acknowledged(identity, event.requestId)
        }
    }

    @Test fun queuedLifecycleWorkDrainsWhenAnotherSenderCompletes() = runBlocking {
        val entered = kotlinx.coroutines.CompletableDeferred<Unit>()
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        val requests = java.util.Collections.synchronizedList(mutableListOf<ExtensionUIStreamRequest>())
        val session = NativeStreamSession(identity,
            ExtensionUIContext(stream = ExtensionUIStreamCapability(protocol = 1, components = listOf("button", "field", "text"))),
            NativeStreamTransport { request ->
                requests.add(request)
                if (request is ExtensionUIStreamOpenRequest) { entered.complete(Unit); release.await() }
            })
        kotlinx.coroutines.withTimeout(5000) {
            val opening = async(kotlinx.coroutines.Dispatchers.Default) { session.visibility(true, true) }
            entered.await()
            val request = requests.single() as ExtensionUIStreamOpenRequest
            assertTrue(session.snapshot(identity, request.requestId, snapshot()))
            session.close()
            release.complete(Unit)
            opening.await()
        }
        assertTrue(requests.last() is ExtensionUIStreamCloseRequest)
        assertEquals(1, requests.filterIsInstance<ExtensionUIStreamCloseRequest>().size)
    }

    @Test fun disabledControlSlotsCannotExecuteAndFailuresDoNotEraseLaterControlDrafts() = runBlocking {
        val transport = Transport()
        val session = NativeStreamSession(identity, ExtensionUIContext(stream = ExtensionUIStreamCapability(protocol = 1, components = listOf("row"))), transport)
        session.visibility(true, true)
        val request = transport.requests.last() as ExtensionUIStreamOpenRequest
        val payload = """{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"root_key":"root","view":{"sections":[{"key":"section","items":[{"type":"row","key":"row","title":"Control","action":{"id":"toggle"},"control":{"kind":"switch","name":"enabled","disabled":true,"on":false,"action":{"id":"toggle"}}}]}]}}"""
        assertTrue(session.snapshot(identity, request.requestId, payload))
        assertFalse(session.event("row", "toggle", "change", JsonPrimitive("true")))
        assertTrue(session.event("row", "toggle", "press", JsonNull))
        session.acknowledged(identity, (transport.requests.last() as ExtensionUIStreamEventRequest).requestId)
        session.requestSnapshot()
        val refresh = transport.requests.last() as ExtensionUIStreamSnapshotRequest
        assertTrue(session.snapshot(identity, refresh.requestId, payload.replace("\"disabled\":true", "\"disabled\":false")))
        session.edit("row", JsonPrimitive("true"))
        assertTrue(session.event("row", "toggle", "change", buildJsonObject { put("enabled", "true") }))
        val first = transport.requests.last() as ExtensionUIStreamEventRequest
        session.rejected(identity.copy(owner = "other"), first.requestId)
        assertEquals(JsonPrimitive("true"), session.state.value.drafts.getValue("row").value)
        session.rejected(identity, first.requestId)
        assertTrue(session.state.value.drafts.isEmpty())
        session.edit("row", JsonPrimitive("true"))
        session.event("row", "toggle", "change", buildJsonObject { put("enabled", "true") })
        val second = transport.requests.last() as ExtensionUIStreamEventRequest
        session.edit("row", JsonPrimitive("false"))
        session.rejected(identity, second.requestId)
        assertEquals(JsonPrimitive("false"), session.state.value.drafts.getValue("row").value)
        session.edit("row", JsonPrimitive("true"))
        session.event("row", "toggle", "change", buildJsonObject { put("enabled", "true") })
        val third = transport.requests.last() as ExtensionUIStreamEventRequest
        session.edit("row", JsonPrimitive("false")); session.edit("row", JsonPrimitive("true"))
        session.rejected(identity, third.requestId)
        assertEquals("An ABA edit must not be reverted by the earlier failure", JsonPrimitive("true"), session.state.value.drafts.getValue("row").value)
        session.event("row", "toggle", "change", buildJsonObject { put("enabled", "true") })
        val fourth = transport.requests.last() as ExtensionUIStreamEventRequest
        session.event("row", "toggle", "change", buildJsonObject { put("enabled", "true") })
        session.rejected(identity, fourth.requestId)
        assertEquals("A queued newer attempt must keep its local value", JsonPrimitive("true"), session.state.value.drafts.getValue("row").value)
        session.close()
    }

    @Test fun pendingOpenReconnectRetainsOriginalRequestAndCompleteFrozenContext() = runBlocking {
        val transport = Transport(); val session = session(transport)
        session.visibility(true, true)
        val original = transport.requests.single() as ExtensionUIStreamOpenRequest
        session.connection(false); session.connection(true)
        assertSame(original, transport.requests.last())
        assertEquals(1, transport.requests.filterIsInstance<ExtensionUIStreamOpenRequest>().map { it.requestId }.toSet().size)
        assertTrue(session.snapshot(identity, original.requestId, snapshot()))
        session.close()
    }

    @Test fun queuedFenceRejectionIsExplicitAndSettlesOnlyItsFrozenDraft() = runBlocking {
        val transport = Transport(); val session = NativeStreamSession(identity,
            ExtensionUIContext(stream = ExtensionUIStreamCapability(protocol = 1, components = listOf("row", "button"))), transport)
        session.visibility(true, true)
        val open = transport.requests.last() as ExtensionUIStreamOpenRequest
        val payload = """{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"root_key":"root","view":{"sections":[{"key":"section","items":[{"type":"button","key":"button","title":"Run","action":{"id":"run"}},{"type":"row","key":"row","title":"Control","control":{"kind":"switch","name":"enabled","on":false,"action":{"id":"toggle"}}}]}]}}"""
        assertTrue(session.snapshot(identity, open.requestId, payload))
        val rejected = mutableListOf<ExtensionUIStreamEvent>(); session.onDiscard { rejected += it }
        session.event("button", "run", "press", JsonObject(emptyMap()))
        val first = transport.requests.last() as ExtensionUIStreamEventRequest
        session.edit("row", JsonPrimitive("true"))
        session.event("row", "toggle", "change", buildJsonObject { put("enabled", "true") })
        assertEquals(2, session.state.value.pendingEvents)
        assertTrue(session.delta(identity, delta("")))
        assertEquals("row", rejected.single().key)
        assertTrue(session.state.value.drafts.isEmpty())
        session.received(identity, first.requestId, 2)
        assertEquals(1, transport.requests.filterIsInstance<ExtensionUIStreamEventRequest>().size)
        assertEquals(0, session.state.value.pendingEvents)
    }

    @Test fun submitButtonSettlesAllSubmittedDraftInstancesAfterNormalizedAuthority() = runBlocking {
        val transport = Transport(); val session = session(transport)
        session.visibility(true, true)
        val open = transport.requests.last() as ExtensionUIStreamOpenRequest
        val payload = snapshot().replace("\"id\":\"run\"", "\"id\":\"run\",\"submit\":true")
        assertTrue(session.snapshot(identity, open.requestId, payload))
        session.edit("draft", JsonPrimitive("  User edit  "))
        session.event("button", "run", "press", buildJsonObject { put("body", "  User edit  ") })
        val first = transport.requests.last() as ExtensionUIStreamEventRequest
        assertTrue(session.delta(identity, delta("""{"op":"replace","key":"draft","item":{"type":"field","key":"draft","name":"body","label":"Body","kind":"text","value":"User edit"}}""")))
        session.received(identity, first.requestId, 2)
        assertTrue(session.state.value.drafts.isEmpty())
        assertEquals(JsonPrimitive("User edit"), session.state.value.tree!!.nodes.getValue("draft")["value"])
        session.edit("draft", JsonPrimitive("later"))
        session.event("button", "run", "press", buildJsonObject { put("body", "later") })
        val second = transport.requests.last() as ExtensionUIStreamEventRequest
        session.edit("draft", JsonPrimitive("newer")); session.edit("draft", JsonPrimitive("later"))
        assertTrue(session.delta(identity, delta("", base = 2)))
        session.received(identity, second.requestId, 3)
        assertEquals(JsonPrimitive("later"), session.state.value.drafts.getValue("draft").value)
    }

    @Test fun closeIsIdempotentAndDisposesLocalState() = runBlocking {
        val transport = Transport(); val session = session(transport); open(session, transport)
        session.edit("draft", JsonPrimitive("local"))
        session.close(); session.close(); session.visibility(true, true); session.connection(false); session.connection(true)
        assertEquals(1, transport.requests.filterIsInstance<ExtensionUIStreamCloseRequest>().size)
        assertEquals(NativeStreamPhase.Closed, session.state.value.phase)
        assertNull(session.state.value.tree)
        assertFalse(session.event("button", "run", "press", JsonNull))
    }
}
