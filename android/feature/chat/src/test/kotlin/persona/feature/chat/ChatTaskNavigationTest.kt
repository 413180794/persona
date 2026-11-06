package persona.feature.chat

import persona.core.contracts.Item
import persona.core.domain.lines.*
import org.junit.Assert.*
import org.junit.Test

class ChatTaskNavigationTest {
    @Test fun opensTheMessagesWorkLineThenResolvesTheActualToolRow() {
        val items = listOf(
            Item("user-1", seq = 1, kind = "text", role = "user", text = "检查表单"),
            Item("use-1", seq = 2, kind = "tool_use", toolUseId = "task-1", toolName = "TaskList"),
            Item("result-1", seq = 3, kind = "tool_result", toolUseId = "task-1", text = "[]"),
            Item("user-2", seq = 4, kind = "text", role = "user", queued = true, text = "补充验证"),
            Item("use-2", seq = 5, kind = "tool_use", toolUseId = "task-2", toolName = "TaskList"),
            Item("result-2", seq = 6, kind = "tool_result", toolUseId = "task-2", text = "[]"),
        ).map { LineItem(it) }
        val collapsed = chatLines(items).lines
        // A finished turn of two messages is two exchanges (#2512): the call is in the second message's work line.
        val first = requireNotNull(taskRecordTarget(collapsed, "use-2"))
        assertEquals("w-user-2", first.expandGroup)
        val opened = chatLines(items, LineOptions(openWork = setOf(first.expandGroup!!))).lines
        assertEquals("w-user-2/use-2", taskRecordTarget(opened, "use-2")?.rowId)
        assertEquals("w-user-2/use-2", taskRecordTarget(opened, "result-2")?.rowId)
        assertNull(taskRecordTarget(opened, "not-loaded"))
        assertEquals(collapsed, chatLines(items, LineOptions(openWork = emptySet())).lines)
    }
    @Test fun foldedTaskShowsProgressAndOpensBeforeLocatingItsCall() {
        val taskSnapshot = persona.core.contracts.FoldTaskSnapshot(replace = "todo",
            entries = listOf(persona.core.contracts.FoldTask("todo:0", "检查表单", status = "in_progress")),
            sourceItemId = "use#0", sourceToolUseId = "call", sourceSeq = 3)
        val folded = Item("fold:thought#0", seq = 2, kind = "fold", fold = persona.core.contracts.Fold(
            items = 3, tools = 1, thoughts = 1, throughSeq = 4, throughId = "result#0", tasks = listOf(taskSnapshot)))
        val user = Item("user", seq = 1, kind = "text", role = "user", text = "检查表单")
        val timeline = ChatTimeline()
        val projection = timeline.lines(listOf(user, folded), false, emptySet())
        val task = projection.tasks.progress.single()
        assertEquals(TaskProgressStatus.InProgress, task.status)
        val closed = requireNotNull(taskRecordTarget(projection.lines, task.sourceItemId, task.sourceSeq))
        val open = setOf(closed.expandGroup!!)
        val loading = timeline.lines(listOf(user, folded), false, open)
        val destination = requireNotNull(taskRecordTarget(loading.lines, task.sourceItemId, task.sourceSeq))
        assertEquals(folded, destination.expandFold)
        assertNull(destination.rowId)
        val use = Item("use#0", seq = 3, kind = "tool_use", toolName = "TodoWrite", toolUseId = "call",
            toolInput = kotlinx.serialization.json.Json.parseToJsonElement("""{"todos":[{"content":"检查表单","status":"in_progress"}]}"""))
        timeline.unfold(folded, listOf(Item("thought#0", seq = 2, kind = "thinking"), use,
            Item("result#0", seq = 4, kind = "tool_result", toolUseId = "call", text = "OK")))
        val expanded = timeline.lines(listOf(user, folded), false, open)
        assertEquals(projection.tasks.progress, expanded.tasks.progress)
        assertNotNull(taskRecordTarget(expanded.lines, task.sourceItemId, task.sourceSeq)?.rowId)
    }

    /** "Jump to the step that started it": a task's notice finds the call that started it by its tool_use_id, opening the work line first. */
    @Test fun aNoticeFindsTheCallThatStartedIt() {
        val items = listOf(
            Item("user", seq = 1, kind = "text", role = "user", text = "跑测试"),
            Item("use#0", seq = 2, kind = "tool_use", toolUseId = "toolu_1", toolName = "Bash"),
            Item("result#0", seq = 3, kind = "tool_result", toolUseId = "toolu_1", text = "Command running in background with ID: b1"),
            Item("reply#0", seq = 4, kind = "text", role = "assistant", text = "在后台跑着"),
            Item("note#0", seq = 5, kind = "notice", text = "后台命令结束",
                task = persona.core.contracts.TaskNote(kind = "shell", status = "completed", title = "npm test", taskId = "b1")),
        ).map { it.asLineItem() }
        val projection = chatLines(items)
        val call = requireNotNull(projection.tasks.note(items.last()).callId)
        assertEquals("toolu_1", call)
        val closed = requireNotNull(callRecordTarget(projection.lines, call))
        val group = requireNotNull(closed.expandGroup)
        val opened = chatLines(items, LineOptions(openWork = setOf(group))).lines
        assertEquals("w-user/use#0", callRecordTarget(opened, call)?.rowId)
        assertNull(callRecordTarget(opened, "toolu_gone"))
    }

    /** The call is in a fold of the hub's that is not loaded: the fold before the notice opens, then the call is found. */
    @Test fun aNoticeOpensTheFoldItsCallIsIn() {
        val user = Item("user", seq = 1, kind = "text", role = "user", text = "跑测试")
        val folded = Item("fold:use#0", seq = 2, kind = "fold", fold = persona.core.contracts.Fold(
            items = 2, tools = 1, thoughts = 0, throughSeq = 3, throughId = "result#0"))
        val notice = Item("note#0", seq = 5, kind = "notice", text = "后台命令结束",
            task = persona.core.contracts.TaskNote(kind = "shell", status = "completed", title = "npm test", toolUseId = "toolu_1"))
        val timeline = ChatTimeline()
        val closed = timeline.lines(listOf(user, folded, notice), false, emptySet())
        val group = requireNotNull(callRecordTarget(closed.lines, "toolu_1", notice)?.expandGroup)
        val open = setOf(group)
        val loading = timeline.lines(listOf(user, folded, notice), false, open)
        assertEquals(folded, callRecordTarget(loading.lines, "toolu_1", notice)?.expandFold)
        assertNull(callRecordTarget(loading.lines, "toolu_1"))
        timeline.unfold(folded, listOf(Item("use#0", seq = 2, kind = "tool_use", toolName = "Bash", toolUseId = "toolu_1"),
            Item("result#0", seq = 3, kind = "tool_result", toolUseId = "toolu_1", text = "ok")))
        val loaded = timeline.lines(listOf(user, folded, notice), false, open)
        assertNotNull(callRecordTarget(loaded.lines, "toolu_1", notice)?.rowId)
        // A call that is in no fold before the notice is gone.
        assertNull(callRecordTarget(loaded.lines, "toolu_gone", notice))
    }

}
