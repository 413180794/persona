package jarvis.feature.chat

import jarvis.core.domain.lines.ChatLine
import jarvis.core.domain.lines.WorkItem
import jarvis.core.contracts.Item

internal data class TaskRecordTarget(val rowId: String? = null, val expandGroup: String? = null, val expandFold: Item? = null)

/** Resolve again after expansion: merged turns acquire different work-group keys. */
internal fun taskRecordTarget(lines: List<ChatLine>, itemId: String, sourceSeq: Int? = null): TaskRecordTarget? {
    lines.filterIsInstance<ChatLine.Step>().firstOrNull { it.step.containsTaskRecord(itemId, sourceSeq) }?.let {
        val folded = it.step as? WorkItem.Folded
        if (folded != null) return TaskRecordTarget(expandFold = folded.item.item)
        return TaskRecordTarget(rowId = it.id)
    }
    lines.filterIsInstance<ChatLine.Work>().firstOrNull { it.steps.any { step -> step.containsTaskRecord(itemId, sourceSeq) } }?.let {
        return TaskRecordTarget(expandGroup = it.id)
    }
    return lines.firstOrNull { it.holds(itemId) }?.let { TaskRecordTarget(rowId = it.id) }
}

private fun WorkItem.containsTaskRecord(id: String, sourceSeq: Int?): Boolean = when (this) {
    is WorkItem.Tool -> call.use.id == id || call.result?.id == id
    is WorkItem.Thought -> item.id == id
    is WorkItem.Narration -> item.id == id
    is WorkItem.Folded -> if (sourceSeq == null) item.item.fold?.tasks.orEmpty().any { it.sourceItemId == id }
        else item.fold?.let { fold ->
            val source = Item(id, seq = sourceSeq)
            chatItemOrder.compare(source, item.item) >= 0 &&
                chatItemOrder.compare(source, item.item.copy(id = fold.throughId, seq = fold.throughSeq)) <= 0
        } == true
}
