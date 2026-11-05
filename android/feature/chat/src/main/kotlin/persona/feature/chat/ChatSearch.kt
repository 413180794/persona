package persona.feature.chat

import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatRepository
import persona.core.domain.injectedOf
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.WorkItem
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.first
import kotlinx.serialization.json.*

internal data class ChatSearchHit(val itemId: String, val category: String, val snippet: String, val ranges: List<IntRange>)
internal data class ChatSearchResults(val hits: List<ChatSearchHit> = emptyList(), val truncated: Boolean = false)

private fun JsonElement.strings(): Sequence<String> = when (this) {
    is JsonObject -> values.asSequence().flatMap { it.strings() }
    is JsonArray -> asSequence().flatMap { it.strings() }
    is JsonPrimitive -> if (isString) sequenceOf(content) else emptySequence()
}

private fun Item.searchFields(): List<Pair<String, String>> = buildList {
    // Persona's message for the model: what the chat shows of it (its line, a scheduled run's ask), not its words
    injectedOf(this@searchFields)?.let { n ->
        add("系统事件" to listOfNotNull(n.line, n.detail).joinToString("\n"))
        return@buildList
    }
    val category = when (kind) {
        "text" -> "正文"
        "thinking" -> "思考"
        "tool_use" -> "工具输入"
        "tool_result" -> "工具输出"
        "notice", "recap" -> "系统事件"
        else -> return@buildList
    }
    text?.takeIf(String::isNotBlank)?.let { add(category to it) }
    if (kind == "tool_use") {
        toolName?.let { add("工具名" to it) }
        toolInput?.strings()?.joinToString("\n")?.takeIf(String::isNotBlank)?.let { add("工具输入" to it) }
    }
    task?.let { add("系统事件" to listOfNotNull(it.title, it.detail, it.status).joinToString("\n")) }
}

/** Literal matching preserves UTF-16 offsets used by Compose text spans. */
internal suspend fun searchChat(items: List<Item>, query: String): ChatSearchResults {
    val needle = query.trim()
    if (needle.isEmpty()) return ChatSearchResults()
    val hits = mutableListOf<ChatSearchHit>()
    for (item in items.distinctBy { it.id }.sortedWith(chatItemOrder).asReversed()) {
        currentCoroutineContext().ensureActive()
        for ((category, text) in item.searchFields()) {
            val first = text.indexOf(needle, ignoreCase = true)
            if (first < 0) continue
            if (hits.size == 100) return ChatSearchResults(hits, truncated = true)
            var start = (first - 45).coerceAtLeast(0)
            var end = (first + needle.length + 110).coerceAtMost(text.length)
            if (start > 0 && text[start].isLowSurrogate()) start--
            if (end < text.length && text[end].isLowSurrogate()) end++
            val snippet = (if (start > 0) "…" else "") + text.substring(start, end) + (if (end < text.length) "…" else "")
            val ranges = mutableListOf<IntRange>()
            var at = snippet.indexOf(needle, ignoreCase = true)
            while (at >= 0) {
                ranges += at until at + needle.length
                at = snippet.indexOf(needle, at + needle.length, ignoreCase = true)
            }
            hits += ChatSearchHit(item.id, category, snippet, ranges)
        }
    }
    return ChatSearchResults(hits)
}

internal data class ChatSearchTarget(val rowId: String, val expand: Set<String> = emptySet())

private fun WorkItem.containsItem(id: String): Boolean = when (this) {
    is WorkItem.Tool -> call.use.id == id || call.result?.id == id
    is WorkItem.Thought -> item.id == id
    is WorkItem.Narration -> item.id == id
    is WorkItem.Folded -> item.id == id
}

internal fun searchTarget(lines: List<ChatLine>, itemId: String): ChatSearchTarget? {
    // Prefer visible rows over the same items retained in work summaries.
    lines.forEach { line ->
        if (line is ChatLine.Row && line.row.id == itemId) return ChatSearchTarget(line.id)
        if (line is ChatLine.Step && line.step.containsItem(itemId)) return ChatSearchTarget(line.id)
        if (line is ChatLine.ApiError && line.item.id == itemId) return ChatSearchTarget(line.id)
        if (line is ChatLine.Recaps && line.items.any { it.id == itemId }) return ChatSearchTarget(line.id)
    }
    lines.forEach { line ->
        when (line) {
            is ChatLine.MergedUsers -> if (line.users.any { it.id == itemId })
                return ChatSearchTarget(itemId, setOf("wt-${line.turnId}"))
            is ChatLine.Work -> line.steps.firstOrNull { it.containsItem(itemId) }?.let {
                return ChatSearchTarget("${line.id}/${it.id}", setOf(line.id))
            }
            else -> Unit
        }
    }
    return null
}

/** Only the existing continuous history cursor is used; no disjoint search window is merged. */
internal suspend fun loadEarlierForSearch(repository: ChatRepository, key: SessionKey, onPage: (Int) -> Unit): String? {
    repeat(20) { page ->
        val before = repository.observe(key).first()
        if (!before.hasOlder) return null
        if (before.loading || before.loadingOlder || !before.verified) return "记录正在更新，请稍后继续"
        repository.loadOlder(key)
        val after = repository.observe(key).first { !it.loadingOlder }
        if (after.error != null) return "更早的记录未能载入，请重试"
        if (!after.verified || (after.items.minOfOrNull { it.seq } ?: Int.MAX_VALUE) >=
            (before.items.minOfOrNull { it.seq } ?: Int.MAX_VALUE)) return "本次没有载入更早的记录，可稍后重试"
        onPage(page + 1)
        if (!after.hasOlder) return null
    }
    return "本次已加载 20 页，可继续加载"
}
