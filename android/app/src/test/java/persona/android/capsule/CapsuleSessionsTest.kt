package persona.android.capsule

import java.lang.reflect.Proxy
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import persona.feature.agents.AgentsPreferences
import persona.core.domain.sort.AgentPreferences
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import persona.android.ui.ShellSession
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.feature.chat.ChatAppBindings
import persona.feature.chat.ChatWiringSource
import persona.platform.capsule.CapsuleState

/** Regression cases from #2124: one surface, the original session, and an explicit destination. */
@OptIn(ExperimentalCoroutinesApi::class)
class CapsuleSessionsTest {
    private fun agent(machine: String, id: String, state: String = "working", quiet: Boolean = false) =
        Session(machine, id, title = "$machine $id", state = state, quiet = quiet)

    @Test fun attentionWinsOverWorkAndCompletionAndIdleMainStaysReachable() {
        val main = agent("home", "main", "idle").copy(assistant = true)
        val done = agent("home", "done", "unread")
        val working = agent("home", "working")
        val waiting = agent("home", "waiting", "needs_you")
        val entries = capsuleEntries(listOf(done, main, working, waiting), main.key, emptyList())
        assertEquals(listOf(CapsuleState.NeedsYou, CapsuleState.Working, CapsuleState.Done, CapsuleState.Idle), entries.map { it.state })
        assertEquals(capsuleId(main.key), entries.last().id)
        assertFalse(entries.any { it.id == MainEntry })
    }

    @Test fun quietWorkIsHiddenButAPendingRequestOnAnIdleTaskRemainsVisible() {
        val quietWork = agent("home", "quiet", quiet = true)
        val request = agent("home", "request", "idle")
        val plainIdle = agent("home", "idle", "idle")
        val waiting = Interaction("ask", request.machineId, request.sessionId, kind = "question", summary = "Which option?")
        val entries = capsuleEntries(listOf(quietWork, request, plainIdle), null, listOf(waiting))
        assertEquals(listOf(capsuleId(request.key), MainEntry), entries.map { it.id })
        assertEquals(CapsuleState.NeedsYou, entries.first().state)
        assertEquals("Which option?", entries.first().summary)
        assertEquals("open", entries.first().actions.single().id)
    }

    @Test fun mutedAndQuietTasksStayOutOfTheGlobalCapsuleEvenWithPendingRequests() {
        val muted = agent("home", "muted", "needs_you")
        val quiet = agent("home", "quiet", "needs_you", quiet = true)
        val visible = agent("home", "visible")
        val requests = listOf(Interaction("mute-ask", "home", "muted"), Interaction("quiet-ask", "home", "quiet"))
        val entries = capsuleEntries(listOf(muted, quiet, visible), null, requests,
            AgentPreferences(sessions = listOf("home/muted")))
        assertEquals(listOf(capsuleId(visible.key), MainEntry), entries.map { it.id })
    }

    @Test fun sameSessionIdOnDifferentHubsHasDifferentIdentityAndEachEntryAppearsOnce() {
        val primary = agent("machine", "same")
        val secondary = agent("machine~g0a1b2c3d", "same")
        assertNotEquals(capsuleId(primary.key), capsuleId(secondary.key))
        // The separator also keeps machine/session concatenations unambiguous.
        assertNotEquals(capsuleId(SessionKey("ab", "c")), capsuleId(SessionKey("a", "bc")))
        val ids = capsuleEntries(listOf(primary, secondary, primary), null, emptyList()).map { it.id }
        assertEquals(listOf(capsuleId(primary.key), capsuleId(secondary.key), MainEntry), ids)
    }

    @Test fun selectingAnotherHubsEntryObservesAndSendsOnlyItsQualifiedSessionKey() = runTest {
        val primary = agent("machine", "same")
        val secondary = agent("machine~g0a1b2c3d", "same")
        val fixture = Fixture(backgroundScope, listOf(primary, secondary))
        runCurrent()
        fixture.source.select(capsuleId(secondary.key))
        fixture.source.shown(true)
        runCurrent()
        assertEquals(secondary.key, fixture.observed.last())
        assertEquals(capsuleId(secondary.key), fixture.source.content.value.selectedId)
        fixture.source.send(fixture.source.content.value.selectedId, "Continue the selected task")
        assertEquals(listOf(secondary.key to "Continue the selected task"), fixture.sent)
        assertTrue(fixture.mainSent.isEmpty())
        fixture.source.open(capsuleId(secondary.key))
        assertEquals(secondary.key to false, fixture.opened.single())
    }

    @Test fun personaShortcutSelectsTheExistingMainInsteadOfSendingToTheMostUrgentAgent() = runTest {
        val urgent = agent("home", "urgent", "needs_you")
        val main = agent("home", "main", "idle").copy(assistant = true)
        val fixture = Fixture(backgroundScope, listOf(urgent), AssistantSnapshot(main.key, listOf(main)))
        runCurrent()
        fixture.source.select(null)
        runCurrent()
        assertEquals(capsuleId(main.key), fixture.source.content.value.selectedId)
        fixture.source.send(fixture.source.content.value.selectedId, "Talk to Persona")
        assertEquals(listOf(main.key to "Talk to Persona"), fixture.sent)
        fixture.source.open(fixture.source.content.value.selectedId)
        assertEquals(main.key to true, fixture.opened.single())
    }

    @Test fun anEmptyPersonaEntryUsesTheOriginalMainConversationSender() = runTest {
        val fixture = Fixture(backgroundScope)
        fixture.bindings.value = fixture.binding()
        runCurrent()
        fixture.source.select(null)
        runCurrent()
        assertEquals(MainEntry, fixture.source.content.value.selectedId)
        assertTrue(fixture.source.content.value.canSend)
        fixture.source.send(fixture.source.content.value.selectedId, "Start a conversation")
        assertEquals(listOf("Start a conversation"), fixture.mainSent)
        assertTrue(fixture.sent.isEmpty())
    }

    @Test fun thePersonaSelectionFollowsANewMainWhileAnotherAgentNeedsAttention() = runTest {
        val urgent = agent("home", "urgent", "needs_you")
        val fixture = Fixture(backgroundScope, listOf(urgent))
        fixture.bindings.value = fixture.binding()
        runCurrent()
        fixture.source.select(null)
        runCurrent()
        assertEquals(MainEntry, fixture.source.content.value.selectedId)
        fixture.source.send(MainEntry, "Start Persona")
        val created = agent("home", "created-main", "working").copy(assistant = true)
        fixture.assistant.value = AssistantSnapshot(created.key, listOf(created))
        runCurrent()
        assertEquals(capsuleId(created.key), fixture.source.content.value.selectedId)
        fixture.source.send(fixture.source.content.value.selectedId, "Continue Persona")
        assertEquals(listOf(created.key to "Continue Persona"), fixture.sent)
    }

    @Test fun changingTasksNeverShowsThePreviousTasksReplyUnderTheNewTitle() = runTest {
        val first = agent("home", "first")
        val second = agent("home~g0a1b2c3d", "second")
        val fixture = Fixture(backgroundScope, listOf(first, second))
        runCurrent()
        fixture.source.shown(true)
        fixture.source.select(capsuleId(first.key))
        runCurrent()
        fixture.messages.getValue(first.key).value = ChatSnapshot(loading = false,
            items = listOf(Item("private-first", role = "assistant", kind = "text", text = "First task reply")))
        runCurrent()
        assertEquals("private-first", fixture.source.content.value.messages.single().id)
        val wrongReplies = mutableListOf<String>()
        val collector = backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            fixture.source.content.collect { content ->
                if (content.selectedId == capsuleId(second.key)) wrongReplies += content.messages.map { it.id }
            }
        }
        fixture.source.select(capsuleId(second.key))
        runCurrent()
        assertFalse(wrongReplies.contains("private-first"))
        assertEquals(capsuleId(second.key), fixture.source.content.value.selectedId)
        assertTrue(fixture.source.content.value.messages.isEmpty())
        collector.cancel()
    }

    @Test fun aLateMainBindingEnablesSendingWithoutNeedingAnUnrelatedSessionPush() = runTest {
        val fixture = Fixture(backgroundScope)
        runCurrent()
        assertFalse(fixture.source.content.value.canSend)
        fixture.bindings.value = fixture.binding()
        runCurrent()
        assertTrue(fixture.source.content.value.canSend)
        fixture.bindings.value = fixture.binding(HubScope("different", "pairing"))
        runCurrent()
        assertFalse(fixture.source.content.value.canSend)
        assertTrue(runCatching { fixture.source.send(MainEntry, "Wrong pairing") }.isFailure)
        assertTrue(fixture.mainSent.isEmpty())
    }

    @Test fun readOnlyAndRemovedSessionsRejectSendsWithoutTouchingTheOutbox() = runTest {
        val readOnly = agent("home", "history").copy(sourceKind = "codex_history", capabilities = SessionCapabilities(read = true))
        val fixture = Fixture(backgroundScope, listOf(readOnly))
        runCurrent()
        fixture.source.select(capsuleId(readOnly.key))
        runCurrent()
        assertFalse(fixture.source.content.value.canSend)
        assertTrue(runCatching { fixture.source.send(capsuleId(readOnly.key), "Do something") }.isFailure)
        fixture.sessions.value = emptyList()
        runCurrent()
        assertTrue(runCatching { fixture.source.send(capsuleId(readOnly.key), "Stale task") }.isFailure)
        assertTrue(fixture.sent.isEmpty())
    }

    @Test fun cancellingThePairingScopePreventsOldCapsuleActionsFromSending() = runTest {
        val scope = CoroutineScope(backgroundScope.coroutineContext + SupervisorJob())
        val task = agent("home", "task")
        val fixture = Fixture(scope, listOf(task))
        runCurrent()
        scope.cancel()
        runCurrent()
        assertTrue(runCatching { fixture.source.send(capsuleId(task.key), "Old pairing") }.isFailure)
        assertTrue(fixture.sent.isEmpty())
    }

    @Test fun replyHistoryUsesOnlyTextAndDoesNotDuplicateAnAcknowledgedOutboxMessage() = runTest {
        val task = agent("home", "task")
        val fixture = Fixture(backgroundScope, listOf(task))
        runCurrent()
        fixture.source.select(capsuleId(task.key))
        fixture.source.shown(true)
        runCurrent()
        fixture.messages.getValue(task.key).value = ChatSnapshot(loading = false, items = listOf(
            Item("user", role = "user", kind = "text", text = "Question", clientMsgId = "accepted"),
            Item("thought", role = "assistant", kind = "thinking", text = "Private reasoning"),
            Item("answer", role = "assistant", kind = "text", text = "Answer"),
        ))
        fixture.outbox.value = listOf(
            OutboxMessage("accepted", task.key, "Question", DeliveryState.Sending),
            OutboxMessage("pending", task.key, "Follow-up", DeliveryState.WaitingForNetwork),
            OutboxMessage("other", SessionKey("another", "task"), "Other task", DeliveryState.Sending),
        )
        runCurrent()
        assertEquals(listOf("user", "answer", "pending"), fixture.source.content.value.messages.map { it.id })
        assertTrue(fixture.source.content.value.messages.last().text.contains("等待网络"))
    }

    private class Fixture(
        val scope: CoroutineScope,
        initial: List<Session> = emptyList(),
        assistantState: AssistantSnapshot = AssistantSnapshot(),
    ) {
        val sessions = MutableStateFlow(initial)
        val assistant = MutableStateFlow(assistantState)
        val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
        val sent = mutableListOf<Pair<SessionKey, String>>()
        val mainSent = mutableListOf<String>()
        val observed = mutableListOf<SessionKey>()
        val opened = mutableListOf<Pair<SessionKey?, Boolean>>()
        val messages = mutableMapOf<SessionKey, MutableStateFlow<ChatSnapshot>>()
        private val hubScope = HubScope("hub", "pairing")
        val bindings = MutableStateFlow<ChatAppBindings?>(null)
        val wiring = object : ChatWiringSource {
            override val current = bindings
        }
        private val sender = object : MessageSender {
            override val outbox = this@Fixture.outbox
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
                assertTrue(clientMessageId.isNotBlank())
                sent += session to text
            }
        }
        private val mainSender = object : MainConversationSender {
            override val outbox = this@Fixture.outbox
            override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) {
                assertTrue(clientMessageId.isNotBlank())
                mainSent += text
            }
        }
        private val shell = ShellSession(
            identity = hubScope.path,
            connection = unused(),
            sessions = object : SessionsRepository {
                override val machines = MutableStateFlow<List<Machine>>(emptyList())
                override val sessions = this@Fixture.sessions
                override suspend fun refresh() = Unit
            },
            chats = object : ChatRepository {
                override fun observe(session: SessionKey) = messages.getOrPut(session) {
                    MutableStateFlow(ChatSnapshot(loading = false))
                }.also { observed += session }
                override suspend fun loadOlder(session: SessionKey) = Unit
                override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
            },
            interactions = object : InteractionCenter {
                override val pending = MutableStateFlow<List<Interaction>>(emptyList())
                override suspend fun refresh() = Unit
                override suspend fun resolve(id: String, resolution: Resolution) = error("Capsule must use the existing interaction UI")
            },
            sender = sender,
            assistant = object : AssistantConversations {
                override val assistant = this@Fixture.assistant
                override val pinned = MutableStateFlow<Set<SessionKey>>(emptySet())
                override suspend fun refresh() = Unit
                override suspend fun startConversation() = error("Use the original main sender")
            },
            inbox = unused(),
            preferences = AgentsPreferences(object : DataStore<Preferences> {
                override val data = MutableStateFlow(emptyPreferences())
                override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                    transform(data.value).also { data.value = it }
            }, object : HubPreferences {
                override val preferences = MutableStateFlow(HubPreferencesSnapshot())
                override suspend fun refresh() = Unit
                override suspend fun togglePin(path: String, space: Boolean) = Unit
                override suspend fun toggleMute(path: String, space: Boolean) = Unit
                override suspend fun markSeen(session: SessionKey, version: String) = Unit
            }),
        )
        val source = CapsuleSessions(shell, wiring, scope) { key, assistant -> opened += key to assistant }
        fun binding(key: HubScope = hubScope) = ChatAppBindings(
            key, scope, unused(), unused(), unused(), unused(), unused(), unused(), mainSender,
        )
    }

    companion object {
        /** Unused system boundaries fail on any accidental access; the capsule and its flow logic run unchanged. */
        private inline fun <reified T> unused(): T = Proxy.newProxyInstance(T::class.java.classLoader, arrayOf(T::class.java)) { proxy, method, args ->
            when (method.name) {
                "toString" -> "Unused ${T::class.java.simpleName}"
                "hashCode" -> System.identityHashCode(proxy)
                "equals" -> proxy === args?.firstOrNull()
                else -> error("Unexpected ${T::class.java.simpleName}.${method.name}")
            }
        } as T
    }
}
