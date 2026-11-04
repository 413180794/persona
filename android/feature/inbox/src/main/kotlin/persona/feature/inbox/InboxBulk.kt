package persona.feature.inbox

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import persona.core.contracts.api.InboxDoneTarget
import persona.core.domain.inbox.InboxGroup
import persona.core.domain.inbox.InboxRowType

/**
 * What a bulk 已处理 (`POST /api/v1/inbox/done`) did: [changed] moved (撤销 sends these back), [unchanged] were already
 * so, [changedSince] took new messages after the list was drawn and stay open, [missing] are gone from the hub.
 */
data class InboxBulkResult(
    val changed: List<String> = emptyList(),
    val unchanged: List<String> = emptyList(),
    val changedSince: List<String> = emptyList(),
    val missing: List<String> = emptyList(),
) {
    companion object {
        fun from(data: JsonObject): InboxBulkResult {
            fun ids(key: String) = data[key]?.jsonArray.orEmpty().mapNotNull { element ->
                (element as? JsonPrimitive)?.contentOrNull ?: (element as? JsonObject)?.get("id")?.let { (it as? JsonPrimitive)?.contentOrNull }
            }
            val skipped = data["skipped"]?.jsonArray.orEmpty().mapNotNull { it as? JsonObject }
            fun skippedFor(reason: String) = skipped.filter { (it["reason"] as? JsonPrimitive)?.contentOrNull == reason }
                .mapNotNull { (it["id"] as? JsonPrimitive)?.contentOrNull }
            return InboxBulkResult(ids("changed"), ids("unchanged"), skippedFor("changed"), skippedFor("missing"))
        }
    }
}

/**
 * The open hub items of what the page shows, each with the version it was listed at: 全部标为已处理 over the current
 * filter and section. Agents waiting for an answer are not the inbox's to file, and folded sections count (their header
 * says how many they hold).
 */
internal fun bulkTargets(groups: List<InboxGroup>, records: Map<String, InboxRecord>): List<InboxDoneTarget> =
    groups.filter { it.id != "done" }.flatMap { it.rows }
        .filter { it.type == InboxRowType.Item }
        .mapNotNull { row -> records[row.id]?.item?.takeIf { !it.isDone } }
        .distinctBy { it.id }
        .map { InboxDoneTarget(it.id, it.version) }

/** The current view in words, for saying what 全部标为已处理 covers; null for the whole inbox. */
internal fun bulkScope(section: String, sourceLabel: String?, kindLabel: String?, query: String): String? =
    listOfNotNull(
        when (section) { "action" -> "需要你"; "fyi" -> "看看就好"; "noise" -> "已折叠"; else -> null },
        sourceLabel?.takeIf { it.isNotBlank() },
        kindLabel?.takeIf { it.isNotBlank() },
        query.trim().takeIf { it.isNotEmpty() }?.let { "搜索「$it」" },
    ).joinToString(" · ").ifEmpty { null }

/** The menu entry: the count, and whether it is the whole inbox or the current filter. */
internal fun bulkLabel(count: Int, scope: String?): String =
    if (scope == null) "全部标为已处理 · $count 件" else "当前筛选标为已处理 · $count 件"

/** What the notice says after a bulk 已处理; null when nothing moved and nothing needs saying. */
internal fun bulkNotice(result: InboxBulkResult): String? {
    val newer = result.changedSince.size
    return when {
        result.changed.isEmpty() && newer > 0 -> "有新消息进来，$newer 件没有标记"
        result.changed.isEmpty() -> null
        newer > 0 -> "已将 ${result.changed.size} 件标为已处理，$newer 件有新消息没标"
        else -> "已将 ${result.changed.size} 件标为已处理"
    }
}
