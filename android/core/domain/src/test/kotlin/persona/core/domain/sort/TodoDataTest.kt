package persona.core.domain.sort

import persona.core.domain.zhAny

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.*
import java.time.Instant

class TodoDataTest {
    private fun s(
        id: String, state: String, machine: String = "server", updated: String = "2026-10-30T10:00:00Z",
        todoId: String? = null, starting: Boolean = false,
    ) = Session(
        machineId = machine, sessionId = id, state = state, status = "idle",
        updatedAt = updated, starting = starting,
        todo = todoId?.let { SessionTodo(id = it, title = "Todo $it") }
    )

    private fun todoAgent(
        sessionId: String, machineId: String = "server", released: Boolean = false, startError: String? = null
    ) = TodoAgent(
        machineId = machineId, sessionId = sessionId, linkedAt = "2026-11-01T10:00:00Z",
        releasedAt = if (released) "2026-11-01T11:00:00Z" else null, startError = startError
    )

    @Test
    fun todoStateOrder() {
        val sessions = mapOf(
            SessionKey("server", "needs") to s("needs", "needs_you"),
            SessionKey("server", "unread") to s("unread", "unread"),
            SessionKey("server", "working") to s("working", "working"),
            SessionKey("server", "starting") to s("starting", "idle", starting = true),
            SessionKey("server", "seen") to s("seen", "idle"),
            SessionKey("server", "failed") to s("failed", "idle"),
        )
        val lookup: (SessionKey) -> Session? = { sessions[it] }

        // 1. 已完成
        val tDone = Todo(id = "1", title = "T1", doneAt = "2026-11-02T10:00:00Z", agents = listOf(todoAgent("needs")))
        assertEquals(TodoState.Done, tDone.state(lookup))

        // 2. 等你 (有挂着的 Agent 需要你)
        val tNeeds = Todo(id = "2", title = "T2", agents = listOf(todoAgent("working"), todoAgent("needs")))
        assertEquals(TodoState.NeedsYou, tNeeds.state(lookup))

        // 3. 待查看 (有挂着的 Agent 完成了你还没看)
        val tUnread = Todo(id = "3", title = "T3", agents = listOf(todoAgent("working"), todoAgent("unread")))
        assertEquals(TodoState.Unread, tUnread.state(lookup))

        // 4. 进行中 (有挂着的 Agent 在工作或正在启动)
        val tWorking = Todo(id = "4", title = "T4", agents = listOf(todoAgent("seen"), todoAgent("working")))
        assertEquals(TodoState.Working, tWorking.state(lookup))
        val tStarting = Todo(id = "4b", title = "T4b", agents = listOf(todoAgent("seen"), todoAgent("starting")))
        assertEquals(TodoState.Working, tStarting.state(lookup))

        // 5. 待跟进 (挂着的 Agent 都停着: 已看、未运行、启动失败)
        val tFollowUpSeen = Todo(id = "5", title = "T5", agents = listOf(todoAgent("seen")))
        assertEquals(TodoState.FollowUp, tFollowUpSeen.state(lookup))
        val tFollowUpFailed = Todo(id = "5b", title = "T5b", agents = listOf(todoAgent("failed", startError = "crash")))
        assertEquals(TodoState.FollowUp, tFollowUpFailed.state(lookup))

        // 6. 未开始 (没有挂着的 Agent 或挂着的都已 released)
        val tNotStartedEmpty = Todo(id = "6", title = "T6", agents = emptyList())
        assertEquals(TodoState.NotStarted, tNotStartedEmpty.state(lookup))
        val tNotStartedReleased = Todo(id = "6b", title = "T6b", agents = listOf(todoAgent("needs", released = true)))
        assertEquals(TodoState.NotStarted, tNotStartedReleased.state(lookup))
    }

    @Test
    fun todoSorting() {
        val sessions = mapOf(
            SessionKey("server", "needs") to s("needs", "needs_you"),
            SessionKey("server", "unread") to s("unread", "unread"),
            SessionKey("server", "working") to s("working", "working"),
            SessionKey("server", "seen") to s("seen", "idle"),
        )
        val lookup: (SessionKey) -> Session? = { sessions[it] }

        val t1 = Todo(id = "done", title = "Done", doneAt = "2026-11-02T12:00:00Z", createdAt = "2026-11-01T10:00:00Z")
        val t2 = Todo(id = "needs_old", title = "Needs Old", createdAt = "2026-11-02T08:00:00Z", agents = listOf(todoAgent("needs")))
        val t3 = Todo(id = "needs_new", title = "Needs New", createdAt = "2026-11-02T10:00:00Z", agents = listOf(todoAgent("needs")))
        val t4 = Todo(id = "unread", title = "Unread", createdAt = "2026-11-02T09:00:00Z", agents = listOf(todoAgent("unread")))
        val t5 = Todo(id = "working", title = "Working", createdAt = "2026-11-02T09:00:00Z", agents = listOf(todoAgent("working")))
        val t6 = Todo(id = "followup", title = "Followup", createdAt = "2026-11-02T09:00:00Z", agents = listOf(todoAgent("seen")))
        val t7 = Todo(id = "not_started", title = "Not Started", createdAt = "2026-11-02T09:00:00Z")

        val sorted = listOf(t1, t7, t6, t5, t4, t2, t3).ordered(lookup)
        // 顺序应为: 等你(新->旧) -> 待查看 -> 进行中 -> 待跟进 -> 未开始 -> 已完成
        assertEquals(listOf("needs_new", "needs_old", "unread", "working", "followup", "not_started", "done"), (sorted.map { it.id }).zhAny())
    }

    @Test
    fun byTodoGrouping() {
        // 置顶: server/pin1; 屏蔽: server/mute1
        val pins = AgentPreferences(sessions = listOf("server/pin1"))
        val mutes = AgentPreferences(sessions = listOf("server/mute1"))

        val t1 = Todo(id = "t1", title = "Todo 1", createdAt = "2026-11-02T10:00:00Z", agents = listOf(todoAgent("a1"), todoAgent("pin1")))
        val t2 = Todo(id = "t2", title = "Todo 2", createdAt = "2026-11-02T09:00:00Z", agents = listOf(todoAgent("a2"), todoAgent("mute1")))
        val tDone = Todo(id = "t_done", title = "Todo Done", doneAt = "2026-11-02T11:00:00Z", agents = listOf(todoAgent("a_done")))

        val sA1 = AgentSession(s("a1", "idle", todoId = "t1", updated = "2026-11-02T09:00:00Z"))
        val sPin1 = AgentSession(s("pin1", "working", todoId = "t1", updated = "2026-11-02T08:00:00Z"))
        val sA2 = AgentSession(s("a2", "unread", todoId = "t2", updated = "2026-11-02T07:00:00Z"))
        val sMute1 = AgentSession(s("mute1", "working", todoId = "t2", updated = "2026-11-02T06:00:00Z"))
        val sDone = AgentSession(s("a_done", "idle", todoId = "t_done"))
        val sFree = AgentSession(s("free", "needs_you"))

        val sessions = listOf(sA1, sPin1, sA2, sMute1, sDone, sFree)
        val todos = listOf(t1, t2, tDone)

        val result = buildAgentList(
            sessions = sessions,
            pins = pins,
            mutes = mutes,
            settings = AgentListSettings(grouping = AgentGrouping.Todo),
            todos = todos
        )

        // 置顶在前，然后应该有两个待办段，加最后一个「没有待办」段
        val nonPinnedGroups = result.groups.filter { it.id != "pinned" }
        assertEquals(3, nonPinnedGroups.size)

        // t1 (包含 a1 idle) vs t2 (包含 a2 unread, mute1 working)
        // Todo 状态 Unread 排在 FollowUp 前面，所以 t2 段在前，t1 在后
        assertEquals("todo/t2", (nonPinnedGroups[0].id).zhAny())
        assertEquals("todo/t1", (nonPinnedGroups[1].id).zhAny())
        assertEquals("todo/none", (nonPinnedGroups[2].id).zhAny())

        // 在 t2 段中: a2 (unread) vs mute1 (muted working). Rank: Unread < Muted -> a2 在前
        assertEquals(listOf("a2", "mute1"), (nonPinnedGroups[0].rows.map { it.session.sessionId }).zhAny())

        // 在 t1 段中: a1 (idle) -> sPin1 在 pinned 组
        assertEquals(listOf("a1"), (nonPinnedGroups[1].rows.map { it.session.sessionId }).zhAny())

        // 在「没有待办」段中: sFree (needs_you) vs sDone (seen). NeedsYou < Seen -> free 在前
        assertEquals(listOf("free", "a_done"), (nonPinnedGroups[2].rows.map { it.session.sessionId }).zhAny())
    }

    @Test
    fun todoJsonDecodeFromHubResponse() {
        val createOrPatchResp = """
            {
                "todo": {
                    "id": "todo_created_123",
                    "title": "测试新建待办",
                    "notes": "这是说明内容",
                    "done_at": null,
                    "created_at": "2026-11-02T10:00:00Z",
                    "updated_at": "2026-11-02T10:00:00Z",
                    "agents": []
                }
            }
        """.trimIndent()

        val obj = JsonCodec.parseToJsonElement(createOrPatchResp) as kotlinx.serialization.json.JsonObject
        val parsed = JsonCodec.decodeFromJsonElement(Todo.serializer(), obj["todo"]!!)
        assertEquals("todo_created_123", (parsed.id).zhAny())
        assertEquals("测试新建待办", (parsed.title).zhAny())
        assertEquals("这是说明内容", (parsed.notes).zhAny())
        assertFalse(parsed.isDone)

        val listResp = """
            {
                "todos": [
                    {
                        "id": "t1",
                        "title": "待办1"
                    },
                    {
                        "id": "t2",
                        "title": "待办2"
                    }
                ]
            }
        """.trimIndent()

        val listParsed = JsonCodec.decodeFromString<TodosResponse>(listResp).todos
        assertEquals(2, listParsed.size)
        assertEquals(listOf("t1", "t2"), (listParsed.map { it.id }).zhAny())
    }
}
