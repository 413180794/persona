package persona.feature.inbox

import android.content.res.Resources

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
internal fun bulkScope(res: Resources, section: String, sourceLabel: String?, kindLabel: String?, query: String): String? =
    listOfNotNull(
        when (section) {
            "action" -> res.getString(R.string.inbox_bay_needs_you); "fyi" -> res.getString(R.string.inbox_group_fyi)
            "noise" -> res.getString(R.string.inbox_section_folded); else -> null
        },
        sourceLabel?.takeIf { it.isNotBlank() },
        kindLabel?.takeIf { it.isNotBlank() },
        query.trim().takeIf { it.isNotEmpty() }?.let { res.getString(R.string.inbox_bulk_search, it) },
    ).joinToString(" · ").ifEmpty { null }

/** The menu entry: the count, and whether it is the whole inbox or the current filter. */
internal fun bulkLabel(res: Resources, count: Int, scope: String?): String =
    res.getQuantityString(if (scope == null) R.plurals.inbox_bulk_all else R.plurals.inbox_bulk_filtered, count, count)

/** What the notice says after a bulk 已处理; null when nothing moved and nothing needs saying. */
internal fun bulkNotice(res: Resources, result: InboxBulkResult): String? {
    val newer = result.changedSince.size
    return when {
        result.changed.isEmpty() && newer > 0 -> res.getQuantityString(R.plurals.inbox_bulk_newer, newer, newer)
        result.changed.isEmpty() -> null
        newer > 0 -> res.getQuantityString(R.plurals.inbox_bulk_marked_newer, result.changed.size, result.changed.size, newer)
        else -> res.getQuantityString(R.plurals.inbox_bulk_marked, result.changed.size, result.changed.size)
    }
}
