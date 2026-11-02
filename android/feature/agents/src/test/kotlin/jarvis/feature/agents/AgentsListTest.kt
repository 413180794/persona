package jarvis.feature.agents

import jarvis.core.contracts.*
import jarvis.core.domain.answer.*
import jarvis.core.domain.sort.*
import org.junit.Assert.*
import org.junit.Test
import java.time.Instant

class AgentsListTest {
    @Test fun historyRangeKeepsRunningBehaviorAndSortsProjectsStably() {
        val working = Session(machineId = "machine", sessionId = "running", status = "working", cwd = "/code/project")
        val a = Session(machineId = "machine", sessionId = "a", status = "offline", cwd = "/code/project",
            title = "自定义名称", updatedAt = "2026-01-01T00:00:00Z")
        val b = a.copy(sessionId = "b")
        val other = a.copy(machineId = "other", sessionId = "c")
        val rows = listOf(b, working, other, a).map(::AgentSession)
        val machines = listOf(Machine("machine"), Machine("other"))
        val prefs = AgentsPreferencesSnapshot()
        val settings = AgentListSettings(grouping = AgentGrouping.State)
        val running = buildAgentList(rows, machines, settings = settings)
        assertEquals(running, scopedAgentList(rows, machines, prefs, settings))
        val history = scopedAgentList(rows, machines, prefs.copy(range = AgentRange.History), settings)
        assertEquals(2, history.groups.size)
        assertEquals(listOf("a", "b"), history.groups.first().rows.map { it.session.sessionId })
        assertEquals("自定义名称", history.groups.first().rows.first().session.displayTitle)
        assertEquals(history, scopedAgentList(rows.reversed(), machines, prefs.copy(range = AgentRange.History), settings))
        assertEquals(4, scopedAgentList(rows, machines, prefs.copy(range = AgentRange.All), settings).groups.sumOf { it.rows.size })
        assertEquals(1, scopedAgentList(rows, machines, prefs.copy(range = AgentRange.History), settings.copy(machineFilter = "other")).groups.size)
        assertEquals(running, scopedAgentList(rows, machines, prefs, settings))
        assertNotEquals(agentRangeKey("first/pairing"), agentRangeKey("second/pairing"))
    }

    @Test fun missingRequestSourceIsOmittedWhileKnownSourcesRemain() {
        val request = Interaction("request", kind = "permission")
        assertNull(request.sourceLabel(AgentsMetadata()))
        assertNull(request.sourceLabel(AgentsMetadata(requestSources = mapOf("request" to "  "))))
        assertEquals("子 Agent", request.sourceLabel(AgentsMetadata(requestSources = mapOf("request" to "子 Agent"))))
        assertEquals("终端", request.copy(kind = "screen").sourceLabel(AgentsMetadata()))
        assertNull(request.copy(toolName = "mcp__tools__read").sourceLabel(AgentsMetadata()))
        assertEquals("来自 MCP 服务器 example-service", request.copy(mcpServerName = "example-service").sourceLabel(AgentsMetadata()))
    }

    @Test fun idleRowsStayCollapsedUntilExpandedAndRemainHiddenInFoldedGroup() {
        val rows = listOf("working", "idle", "idle").mapIndexed { index, state ->
            AgentSession(Session(machineId = "machine", sessionId = "session-$index", state = state, spaceId = "space", spaceName = "空间"))
        }
        val settings = AgentListSettings()
        val collapsed = buildAgentList(rows, settings = settings).groups.single()
        assertEquals(2, collapsed.rows.size - collapsed.visibleRows.size)
        assertEquals(AgentState.Working, collapsed.visibleRows.single().state)
        val expanded = settings.copy(expandedIdle = setOf(collapsed.id))
        assertEquals(3, buildAgentList(rows, settings = expanded).groups.single().visibleRows.size)
        assertTrue(buildAgentList(rows, settings = expanded.copy(folded = setOf(collapsed.id))).groups.single().visibleRows.isEmpty())
    }

    @Test fun openedUnreadRowKeepsItsGroupAndPositionAfterReadReceipt() {
        val first = Session(machineId = "machine", sessionId = "first", state = "unread", updatedAt = "2026-01-02T00:00:00Z")
        val second = Session(machineId = "machine", sessionId = "second", state = "unread", updatedAt = "2026-01-01T00:00:00Z")
        val settings = AgentListSettings(grouping = AgentGrouping.State, quick = AgentQuickFilter.Unread)
        val original = buildAgentList(listOf(AgentSession(first), AgentSession(second)), settings = settings)
        val next = buildAgentList(listOf(AgentSession(first.copy(state = "idle")), AgentSession(second)),
            settings = settings.copy(kept = setOf(first.key.path)))
        val retained = retainOpenedRows(next, original, setOf(first.key.path))
        assertEquals(listOf("state/unread"), retained.groups.map { it.id })
        assertEquals(listOf(first.key.path, second.key.path), retained.groups.single().visibleRows.map { it.id })
        assertEquals(AgentState.Idle, retained.groups.single().visibleRows.first().state)
        assertEquals(1, retained.counts[AgentQuickFilter.Unread])
        val reselected = buildAgentList(listOf(AgentSession(first.copy(state = "idle")), AgentSession(second)), settings = settings)
        assertEquals(listOf(second.key.path), reselected.groups.flatMap { it.visibleRows }.map { it.id })
    }

    @Test fun quickAnswersPreserveWireLabelsAndEnforceTheInlineBoundary() {
        val question = Question(question = "选择处理方式", options = listOf(QuestionOption("先备份（推荐）"), QuestionOption("保留")))
        val request = Interaction("request", kind = "question", questions = listOf(question), expiresAt = "2026-01-01T00:01:00Z")
        assertNotNull(request.quickQuestion())
        val answer = AnswerBuilder.build(request, AnswerAction.PickOption, AnswerDraft(optionIndex = 0)).resolution
        assertEquals(mapOf(question.question to "先备份（推荐）"), answer?.answers)
        assertNull(request.copy(questions = listOf(question.copy(multiSelect = true))).quickQuestion())
        assertNull(request.copy(questions = listOf(question.copy(options = List(5) { QuestionOption("选项 $it") }))).quickQuestion())
        assertNull(request.copy(secret = true).quickQuestion())
        assertEquals("即将过期", countdown(request.expiresAt, Instant.parse("2026-01-01T00:00:30Z")))
        assertTrue(request.expired(Instant.parse("2026-01-01T00:01:00Z")))
        assertEquals(RequestExpiry.expiredLabel, countdown(request.expiresAt, Instant.parse("2026-01-01T00:01:01Z")))
        assertTrue(request.copy(expiresAt = "invalid").expired(Instant.parse("2026-01-01T00:00:30Z")))
        assertEquals("草稿 · [附件]", AgentRowMetadata(hasAttachments = true).draftLabel)
    }
}
