package persona.feature.chat

import persona.core.contracts.Item
import persona.core.domain.lines.*
import org.junit.Assert.*
import org.junit.Test

class ChatTaskNavigationTest {
    @Test fun opensMergedTurnThenResolvesTheActualToolRow() {
        val items = listOf(
            Item("user-1", seq = 1, kind = "text", role = "user", text = "检查表单"),
            Item("use-1", seq = 2, kind = "tool_use", toolUseId = "task-1", toolName = "TaskList"),
            Item("result-1", seq = 3, kind = "tool_result", toolUseId = "task-1", text = "[]"),
            Item("user-2", seq = 4, kind = "text", role = "user", queued = true, text = "补充验证"),
            Item("use-2", seq = 5, kind = "tool_use", toolUseId = "task-2", toolName = "TaskList"),
            Item("result-2", seq = 6, kind = "tool_result", toolUseId = "task-2", text = "[]"),
        ).map { LineItem(it) }
        var expanded = emptySet<String>()
        val collapsed = chatLines(items).lines
        val first = requireNotNull(taskRecordTarget(collapsed, "use-2"))
        assertEquals("wt-user-1", first.expandGroup)
        expanded += first.expandGroup!!
        val second = requireNotNull(taskRecordTarget(chatLines(items, LineOptions(openWork = expanded)).lines, "use-2"))
        assertEquals("w-user-2", second.expandGroup)
        expanded += second.expandGroup!!
        val opened = chatLines(items, LineOptions(openWork = expanded)).lines
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
        val projection = timeline.lines(listOf(user, folded), null, emptySet())
        val task = projection.tasks.progress.single()
        assertEquals(TaskProgressStatus.InProgress, task.status)
        val closed = requireNotNull(taskRecordTarget(projection.lines, task.sourceItemId, task.sourceSeq))
        val open = setOf(closed.expandGroup!!)
        val loading = timeline.lines(listOf(user, folded), null, open)
        val destination = requireNotNull(taskRecordTarget(loading.lines, task.sourceItemId, task.sourceSeq))
        assertEquals(folded, destination.expandFold)
        assertNull(destination.rowId)
        val use = Item("use#0", seq = 3, kind = "tool_use", toolName = "TodoWrite", toolUseId = "call",
            toolInput = kotlinx.serialization.json.Json.parseToJsonElement("""{"todos":[{"content":"检查表单","status":"in_progress"}]}"""))
        timeline.unfold(folded, listOf(Item("thought#0", seq = 2, kind = "thinking"), use,
            Item("result#0", seq = 4, kind = "tool_result", toolUseId = "call", text = "OK")))
        val expanded = timeline.lines(listOf(user, folded), null, open)
        assertEquals(projection.tasks.progress, expanded.tasks.progress)
        assertNotNull(taskRecordTarget(expanded.lines, task.sourceItemId, task.sourceSeq)?.rowId)
    }

}
