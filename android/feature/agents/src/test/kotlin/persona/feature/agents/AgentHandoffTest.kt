package persona.feature.agents

import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class AgentHandoffTest {
    @Test fun retriesKeepIdentityAndNavigationWaitsForDurableTarget() = runTest {
        val attempts = mutableListOf<String>()
        val events = mutableListOf<String>()
        var failWrite = true
        val sender = object : MainConversationSender {
            override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
            override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) {
                attempts += clientMessageId
                if (failWrite) error("Local write failed")
                outbox.value = listOf(OutboxMessage(clientMessageId, PendingMainConversation, text,
                    DeliveryState.WaitingForNetwork, mainTarget = true))
                events += "committed"
            }
        }
        val session = Session("machine", "agent", title = "检查改动", cwd = "/project", lastText = "需要核对")
        suspend fun handoff() = handoffToMain(sender, "handoff-operation", session.lastText.orEmpty(),
            onQueued = { events += "persona" }, onBound = { events += "open:${it.path}" })
        try { handoff(); fail("The failed write must propagate") } catch (_: IllegalStateException) { }
        assertTrue(events.isEmpty())
        failWrite = false
        val pending = launch { handoff() }
        runCurrent()
        assertEquals(attempts.first(), attempts.last())
        assertEquals(listOf("committed", "persona"), events)
        assertTrue(pending.isActive)
        val target = SessionKey("machine", "main")
        sender.outbox.value = sender.outbox.value.map { it.copy(session = target) }
        pending.join()
        assertEquals("open:${target.path}", events.last())
        handoff()
        assertEquals(2, attempts.size)
        assertEquals(listOf("persona", "open:${target.path}"), events.takeLast(2))
        val again = launch {
            handoffToMain(sender, "next-operation", session.lastText.orEmpty(), {}, {})
        }
        runCurrent()
        assertEquals("next-operation", attempts.last())
        sender.outbox.value = sender.outbox.value.map { it.copy(session = target) }
        again.join()
    }
}
