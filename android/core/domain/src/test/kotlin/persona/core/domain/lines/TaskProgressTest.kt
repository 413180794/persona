package persona.core.domain.lines

import persona.core.domain.zhAny

import persona.core.contracts.Item
import persona.core.contracts.JsonCodec
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.Json
import org.junit.Assert.*
import org.junit.Test

class TaskProgressTest {
    @Test fun replaysConfirmedResultsAndKeepsTheirSourceRecords() {
        val items = mutableListOf<LineItem>()
        fun call(name: String, input: String, output: String? = "OK", failed: Boolean = false): String {
            val id = "call-${items.size}"
            items += LineItem(Item(id, seq = items.size, kind = "tool_use", toolName = name,
                toolUseId = id, toolInput = Json.parseToJsonElement(input)))
            if (output != null) items += LineItem(Item("$id-result", seq = items.size, kind = "tool_result",
                toolUseId = id, text = output, isError = failed))
            return id
        }
        fun tasks() = TaskIndex.build(items.reversed()).progress
        call("TodoWrite", """{"todos":[{"content":"检查表单","status":"in_progress"}]}""")
        call("TodoWrite", """{"todos":[{"content":"检查表单","status":"completed"}]}""", output = null)
        assertEquals(TaskProgressStatus.InProgress, tasks().single().status)
        call("TodoWrite", """{"todos":[]}""", failed = true)
        assertEquals(1, tasks().size)
        val todo = call("TodoWrite", """{"todos":[{"content":"检查表单","status":"completed"}]}""")
        assertEquals(TaskProgressStatus.Completed, tasks().single().status)
        assertEquals(todo, tasks().single().sourceItemId)
        call("TodoWrite", """{"todos":[]}""")
        assertTrue(tasks().isEmpty())

        call("TaskCreate", """{"subject":"补充验证","description":"覆盖空输入"}""", "Task #7 created successfully: 补充验证")
        val update = call("TaskUpdate", """{"taskId":"7","status":"in_progress"}""")
        call("TaskUpdate", """{"taskId":"7","status":"completed"}""", failed = true)
        assertEquals(TaskProgressStatus.InProgress, tasks().single().status)
        assertEquals(update, tasks().single().sourceItemId)
        call("TaskCreate", """{"subject":"更新文档"}""", """{"task":{"id":"8","status":"pending"}}""")
        val list = call("TaskList", "{}", "#7 [completed] 补充验证\n#8 [pending] 更新文档")
        assertEquals(listOf(TaskProgressStatus.Completed, TaskProgressStatus.Pending), tasks().map { it.status })
        assertEquals("覆盖空输入", (tasks().first().description).zhAny())
        assertTrue(tasks().all { it.sourceItemId == list })
        call("TaskList", "{}", "malformed result")
        assertEquals(2, tasks().size)
        call("TaskUpdate", """{"taskId":"8","status":"deleted"}""")
        assertEquals(1, tasks().size)
        call("TaskList", "{}", """{"tasks":[{"id":"7","subject":"补充验证","status":"completed"}]}""")
        assertEquals(TaskProgressStatus.Completed, tasks().single().status)
        call("TaskList", "{}", "[]")
        assertTrue(tasks().isEmpty())
    }
    @Test fun foldedSnapshotsMatchExpandedResultsAndAllowLaterCallsToOverride() {
        fun call(id: String, seq: Int, name: String, input: String, output: String) = listOf(
            LineItem(Item(id, seq = seq, kind = "tool_use", toolUseId = id, toolName = name,
                toolInput = Json.parseToJsonElement(input))),
            LineItem(Item("$id-result", seq = seq + 1, kind = "tool_result", toolUseId = id, text = output)),
        )
        val earlier = call("create", 1, "TaskCreate", """{"subject":"补充验证","description":"覆盖空输入"}""",
            "Task #7 created successfully")
        val todo = call("todo", 4, "TodoWrite", """{"todos":[{"content":"检查表单","status":"in_progress"}]}""", "OK")
        val update = call("update", 6, "TaskUpdate", """{"taskId":"7","status":"completed"}""", "OK")
        val listed = call("list", 8, "TaskList", "{}", "#7 [completed] 补充验证")
        val folded = JsonCodec.decodeFromString<Item>("""{
            "id":"fold:thought", "seq":3, "kind":"fold", "fold":{
                "items":7,"tools":3,"thoughts":1,"through_seq":9,"through_id":"list-result",
                "tasks":[
                    {"replace":"todo","source_item_id":"todo","source_tool_use_id":"todo","source_seq":4,
                     "entries":[{"id":"todo:0","subject":"检查表单","status":"in_progress"}]},
                    {"source_item_id":"update","source_tool_use_id":"update","source_seq":6,
                     "entries":[{"id":"task:7","status":"completed"}]},
                    {"replace":"task","source_item_id":"list","source_tool_use_id":"list","source_seq":8,
                     "entries":[{"id":"task:7","subject":"补充验证","status":"completed"}]}
                ]
            }}""")
        val collapsed = earlier + LineItem(folded)
        val progress = TaskIndex.build(collapsed.reversed()).progress
        assertEquals(TaskIndex.build(earlier + todo + update + listed).progress, progress)
        assertEquals(2, progress.size)
        assertEquals("覆盖空输入", (progress.first().description).zhAny())
        assertEquals("list", (progress.first().sourceItemId).zhAny())
        val later = call("later", 10, "TodoWrite", """{"todos":[{"content":"检查表单","status":"completed"}]}""", "OK")
        assertEquals("later", (TaskIndex.build(collapsed + later).progress.last().sourceItemId).zhAny())
        assertTrue(TaskIndex.build(collapsed + later).progress.all { it.status == TaskProgressStatus.Completed })
        val clear = folded.copy(seq = 12, fold = folded.fold!!.copy(tasks = listOf(
            persona.core.contracts.FoldTaskSnapshot(replace = "todo", entries = emptyList(),
                sourceItemId = "clear", sourceToolUseId = "clear", sourceSeq = 12),
            persona.core.contracts.FoldTaskSnapshot(replace = "task", entries = emptyList(),
                sourceItemId = "list-empty", sourceToolUseId = "list-empty", sourceSeq = 13),
        )))
        assertTrue(TaskIndex.build(collapsed + LineItem(clear)).progress.isEmpty())
    }

}
