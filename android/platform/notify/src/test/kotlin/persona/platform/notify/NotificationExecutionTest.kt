package persona.platform.notify

import persona.core.contracts.*
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.InteractionGoneException
import persona.core.contracts.api.SessionsRepository
import persona.core.navigation.AppTab
import persona.core.navigation.NotificationRoute
import persona.core.navigation.TerminalRoute
import persona.core.designsystem.PendingStatus
import persona.core.domain.answer.RequestExpiry
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import java.time.Instant

class NotificationExecutionTest {
    private val now = Instant.parse("2030-01-01T00:00:00Z")
    private val request = Interaction("request", machineId = "machine", sessionId = "session", kind = "permission")

    @Test fun deadlineGuardsAnswerAndApprovalEvenWhenPersistenceCrossesTheDeadline() = runBlocking {
        val deadline = now.plusSeconds(1)
        val requests = listOf(
            request to "allow",
            request.copy(kind = "plan") to "approve_edits",
            request.copy(kind = "question", questions = listOf(Question("选择", options = listOf(QuestionOption("继续"))))) to "answer.0",
            request.copy(kind = "screen", dialog = ScreenDialog(options = listOf(DialogOption("1", "继续")))) to "option.1",
        )
        for ((ix, action) in requests) {
            for (expiresAt in listOf(null, deadline.toString(), now.toString(), now.minusSeconds(1).toString(), "invalid")) {
                val center = Center(ix.copy(expiresAt = expiresAt))
                val progress = mutableListOf<PendingStatus>()
                val executor = NotificationActionExecutor(center, { true }, { now })
                val expired = RequestExpiry.expired(expiresAt, now)
                assertEquals(if (expired) NotificationActionResult.Expired else NotificationActionResult.Submitted,
                    executor.execute(ix.id, action, null, { false }, onProgress = { progress += it }))
                assertEquals(if (expired) 0 else 1, center.writes)
                assertEquals(if (expired) listOf(PendingStatus.Verifying)
                    else listOf(PendingStatus.Verifying, PendingStatus.Sending), progress)
            }
            val center = Center(ix.copy(expiresAt = deadline.toString()))
            var time = now
            val executor = NotificationActionExecutor(center, { time = deadline; true }, { time })
            assertEquals(NotificationActionResult.Expired, executor.execute(ix.id, action, null, { false }))
            assertEquals(0, center.writes)

            val stale = Center(ix)
            val beforeRefresh = NotificationActionExecutor(stale, { error("Expired actions must not be claimed") }, { now })
            assertEquals(NotificationActionResult.Expired,
                beforeRefresh.execute(ix.id, action, null, { false }, expiresAt = now.toString()))
            assertEquals(0, stale.refreshes)
            assertEquals(0, stale.writes)
        }
        val link = terminalLink(TerminalRoute("machine & test", "session/分支"))
        val parameters = java.net.URI(link).rawQuery.split('&').associate {
            it.substringBefore('=') to java.net.URLDecoder.decode(it.substringAfter('='), "UTF-8")
        }
        assertEquals(mapOf("target" to "terminal", "machine" to "machine & test", "session" to "session/分支"), parameters)
    }

    @Test fun refreshExpiryLockAndDurableClaimGuardEveryResolve() = runBlocking {
        val center = Center(request)
        val claims = mutableMapOf<String, String>()
        fun executor() = NotificationActionExecutor(center, { id ->
            synchronized(claims) { if (id in claims) false else { claims[id] = "operation"; true } }
        }, { now })
        suspend fun execute(action: String = "allow", locked: Boolean = false, reply: String? = null) =
            executor().execute(request.id, action, reply, { locked })

        center.remote = null
        assertEquals(NotificationActionResult.Gone, execute())
        assertEquals(0, center.writes)
        for (deadline in listOf(now.toString(), now.minusSeconds(1).toString(), "invalid")) {
            center.remote = request.copy(expiresAt = deadline)
            assertEquals(NotificationActionResult.Expired, execute())
        }
        center.remote = request.copy(expiresAt = now.plusSeconds(1).toString())
        assertEquals(NotificationActionResult.Locked, execute(locked = true))
        assertEquals(NotificationActionResult.Invalid, execute(reply = "不要执行"))
        assertTrue(claims.isEmpty())

        val sent = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        center.onResolve = { sent.complete(Unit); release.await() }
        val first = async { execute(action = "deny", locked = true, reply = "需要确认") }
        sent.await()
        assertEquals(NotificationActionResult.Duplicate, execute())
        release.complete(Unit)
        assertEquals(NotificationActionResult.Submitted, first.await())
        assertEquals("需要确认", center.last?.message)
        assertEquals(false, center.last?.allow)
        assertEquals(1, center.writes)
        // Recreating the executor must not reset the claim after an ambiguous response.
        claims.clear()
        center.onResolve = { throw java.io.IOException("Response lost") }
        try { execute(); fail("Expected ambiguous failure") } catch (_: java.io.IOException) { }
        assertEquals(NotificationActionResult.Duplicate, execute())
        assertEquals(2, center.writes)
        claims.clear()
        center.onResolve = { throw InteractionGoneException() }
        assertEquals(NotificationActionResult.Gone, execute())
        assertEquals(3, center.writes)
    }

    @Test fun onlyHubApprovedAlertsAreDeliveredAndRoutesKeepTheirTab() = runBlocking {
        val center = Center(request)
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow<List<Machine>>(emptyList())
            override val sessions = MutableStateFlow(listOf(Session(machineId = "machine", sessionId = "session", assistant = true)))
            override suspend fun refresh() = Unit
        }
        var visible = false
        val posted = mutableListOf<Interaction>()
        val notifications = mutableListOf<NotifyEvent>()
        val retracted = mutableListOf<String>()
        val delivery = NotificationDelivery(center, sessions, { visible },
            { posted += it }, { notifications += it }, { retracted += it })
        val notice = NotifyEvent("interaction", "machine", "session", interactionId = request.id)
        delivery.accept(InteractionEvent(request))
        assertTrue(posted.isEmpty())
        assertEquals(0, center.refreshes)
        delivery.accept(notice)
        assertEquals(listOf(request), posted)
        visible = true
        delivery.accept(notice)
        assertEquals(1, posted.size)
        visible = false
        center.remote = request.copy(expiresAt = "2000-01-01T00:00:00Z")
        delivery.accept(notice)
        center.remote = null
        delivery.accept(notice)
        assertEquals(1, posted.size)
        delivery.accept(InteractionResolvedEvent(request.id))
        assertEquals(listOf(request.id, request.id), retracted)
        delivery.accept(notice.copy(kind = "done"))
        delivery.accept(notice.copy(kind = "assistant"))
        assertEquals(listOf("done", "assistant"), notifications.map { it.kind })
        assertEquals(AppTab.Jarvis, NotificationRoute.conversation("machine", "session", true).tab)
        assertEquals(AppTab.Agents, NotificationRoute.conversation("machine", "session", false).tab)
        assertNull(NotificationRoute.conversation("machine", "session", null).tab)
    }

    @Test fun devicePreferencesGateRawInteractionsAndCompletionWithoutChangingHubLongPolicy() = runBlocking {
        val center = Center(request)
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow<List<Machine>>(emptyList())
            override val sessions = MutableStateFlow(listOf(Session(machineId = "machine", sessionId = "session")))
            override suspend fun refresh() = Unit
        }
        var settings: persona.core.contracts.api.DeviceNotificationPreferences? =
            persona.core.contracts.api.DeviceNotificationPreferences(input = false, done = persona.core.contracts.api.DoneNotification.Off)
        var posted = 0
        val delivery = NotificationDelivery(center, sessions, { false }, { posted++ }, { posted++ }, {}, { settings })
        val notice = NotifyEvent("interaction", "machine", "session", interactionId = request.id)
        delivery.accept(InteractionEvent(request))
        delivery.accept(notice)
        delivery.accept(notice.copy(kind = "done"))
        assertEquals(0, posted)
        assertEquals(0, center.refreshes)
        settings = persona.core.contracts.api.DeviceNotificationPreferences(done = persona.core.contracts.api.DoneNotification.Long)
        delivery.accept(notice)
        delivery.accept(notice.copy(kind = "done"))
        assertEquals(2, posted)
        settings = null
        delivery.accept(notice)
        assertEquals(2, posted)
    }

    private class Center(request: Interaction) : InteractionCenter {
        override val pending = MutableStateFlow(listOf(request))
        var remote: Interaction? = request
        var refreshes = 0
        var writes = 0
        var last: Resolution? = null
        var onResolve: suspend () -> Unit = {}
        override suspend fun refresh() { refreshes++; pending.value = listOfNotNull(remote) }
        override suspend fun resolve(id: String, resolution: Resolution) {
            writes++
            last = resolution
            onResolve()
        }
    }
}
