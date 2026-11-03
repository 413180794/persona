package persona.core.domain.lines

import java.time.Duration
import java.time.Instant
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

data class TaskBadge(val text: String, val failed: Boolean)
data class TaskNoteText(val label: String, val subject: String, val outcome: String?, val failed: Boolean, val callId: String?)

data class TaskIndex(
    val notes: Map<String, List<TaskNote>> = emptyMap(),
    val calls: Map<String, String> = emptyMap(),
    val started: Set<String> = emptySet(),
    val descriptions: Map<String, String> = emptyMap(),
    val startedAt: Map<String, Instant> = emptyMap(),
    val progress: List<TaskProgressItem> = emptyList(),
) {
    fun note(item: LineItem): TaskNoteText {
        val note = requireNotNull(item.task)
        val call = calls[item.id]
        val label = when (note.kind) {
            "shell" -> "后台命令"
            "monitor" -> if (note.status == "event") "Monitor 事件" else "Monitor 结束"
            "agent" -> "子 Agent"
            else -> "后台任务"
        }
        val subject = (if (note.kind == "monitor" && note.status == "event")
            note.detail?.lineSequence()?.map { it.trim() }?.firstOrNull { it.isNotEmpty() } else null)
            ?: descriptions[call]?.takeIf { it.isNotEmpty() } ?: note.title
        val outcome = buildList {
            when (note.status) {
                "completed", "ended" -> add("✓")
                "failed" -> add(note.exitCode?.let { "✗ 退出码 $it" } ?: "✗")
                "killed", "stopped" -> add("已停止")
                "waiting" -> add("等待中")
                else -> note.exitCode?.takeIf { it != 0 }?.let { add("✗ 退出码 $it") }
            }
            val start = startedAt[call]
            val end = item.time
            if (note.status != "event" && start != null && end != null && end > start) {
                val s = Duration.between(start, end).seconds
                add(when {
                    s < 60 -> "$s 秒"
                    s < 3600 -> "${s / 60} 分"
                    s % 3600 / 60 == 0L -> "${s / 3600} 小时"
                    else -> "${s / 3600} 小时 ${s % 3600 / 60} 分"
                })
            }
        }.takeIf { it.isNotEmpty() }?.joinToString(" · ")
        return TaskNoteText(label, subject, outcome, note.failed, call)
    }

    fun badge(callId: String?): TaskBadge? {
        if (callId == null || (callId !in started && callId !in notes)) return null
        val notes = notes[callId].orEmpty()
        val events = notes.count { it.status == "event" }
        val last = notes.lastOrNull { it.status != "event" }
            ?: return TaskBadge(if (events > 0) "后台 · $events 个事件" else "后台 · 运行中", false)
        val state = when (last.status) {
            "failed" -> "失败"
            "killed", "stopped" -> "已停止"
            "waiting" -> "等待中"
            "ended" -> "已结束"
            else -> "已完成"
        }
        return TaskBadge("后台 · $state" + if (events > 0) " · $events 个事件" else "", last.failed)
    }

    companion object {
        private val launched = Regex("(?:running in background with ID|agentId|Monitor started \\(task|moved to the background \\(ID):?\\s*([A-Za-z0-9_-]+)")

        fun build(items: List<LineItem>): TaskIndex {
            val notes = mutableMapOf<String, MutableList<TaskNote>>()
            val calls = mutableMapOf<String, String>()
            val started = mutableSetOf<String>()
            val descriptions = mutableMapOf<String, String>()
            val startedAt = mutableMapOf<String, Instant>()
            val byTask = mutableMapOf<String, String>()
            val uses = mutableMapOf<String, LineItem>()
            fun describe(call: String) {
                val use = uses[call] ?: return
                val description = (use.item.toolInput as? JsonObject)?.get("description") as? JsonPrimitive
                if (description?.isString == true) descriptions[call] = description.content
                else descriptions.remove(call)
                use.time?.let { startedAt[call] = it } ?: startedAt.remove(call)
            }
            for (it in items) {
                when (it.kind) {
                    "tool_use" -> it.item.toolUseId?.let { id -> uses[id] = it }
                    "tool_result" -> {
                        val call = it.item.toolUseId ?: continue
                        val text = it.text ?: continue
                        val match = launched.find(text) ?: continue
                        byTask[match.groupValues[1]] = call
                        started += call
                        describe(call)
                    }
                    "notice" -> {
                        val note = it.task ?: continue
                        val call = note.toolUseId ?: byTask[note.taskId] ?: continue
                        notes.getOrPut(call) { mutableListOf() } += note
                        calls[it.id] = call
                        if (call !in descriptions) describe(call)
                    }
                }
            }
            return TaskIndex(notes.mapValues { it.value.toList() }, calls.toMap(), started.toSet(), descriptions.toMap(), startedAt.toMap(),
                rebuildTaskProgress(items))
        }
    }
}
