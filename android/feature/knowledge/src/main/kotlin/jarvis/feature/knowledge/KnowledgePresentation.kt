package jarvis.feature.knowledge

import jarvis.core.contracts.api.*
import kotlinx.serialization.json.*

internal data class KnowledgeSection(val title: String, val entries: List<KnowledgeEntry>, val collapsed: Boolean = false)
internal data class KnowledgeSearchGroup(val entry: KnowledgeEntry, val hits: List<KnowledgeHit>)

internal fun KnowledgeEntry.inScope(): Boolean = KnowledgeKind.fromWire(kind) != null &&
    (kind != KnowledgeKind.Chats.wire || extra == "group")

internal fun projectSections(entries: List<KnowledgeEntry>): List<KnowledgeSection> {
    val (ignored, visible) = entries.partition { it.extra.orEmpty().endsWith("ignored") }
    val (done, active) = visible.partition { it.extra.orEmpty().endsWith("done") }
    val (focus, rest) = active.partition { it.importance in setOf("P0", "P1") }
    return listOf(
        KnowledgeSection("重点", focus), KnowledgeSection("进行中", rest),
        KnowledgeSection("已完成", done), KnowledgeSection("不是项目", ignored, collapsed = true),
    ).filter { it.entries.isNotEmpty() }
}

internal fun KnowledgeHit.reference(): Pair<KnowledgeKind, String>? {
    val parts = path.split('/')
    if (parts.size < 3 || parts.any { it.isBlank() || it == "." || it == ".." }) return null
    val kind = KnowledgeKind.fromWire(parts[0]) ?: return null
    return kind to parts[1]
}

internal fun searchGroups(hits: List<KnowledgeHit>, index: Map<String, KnowledgeEntry>): List<KnowledgeSearchGroup> =
    hits.filter { it.reference() != null }.distinct().groupBy { hit ->
        val (kind, id) = requireNotNull(hit.reference())
        "${kind.wire}/$id"
    }.mapNotNull { (key, matches) ->
        val (kind, id) = requireNotNull(matches.first().reference())
        val entry = index[key] ?: KnowledgeEntry(kind.wire, id, id)
        if (kind == KnowledgeKind.Chats && !entry.inScope()) return@mapNotNull null
        KnowledgeSearchGroup(entry, matches.sortedBy {
            when (it.path.substringAfterLast('/')) { "summary.md" -> 0; "log.md" -> 1; "meta.json" -> 2; else -> 3 }
        })
    }

internal fun KnowledgeDetail.entry(): KnowledgeEntry {
    val projectExtra = if (kind == "projects") "${meta.string("kind")} · ${meta.string("status")}" else meta.string("kind")
    return KnowledgeEntry(kind, id, meta.string("name").ifBlank { id },
        meta.string("one_line").ifBlank { meta.string("note") }, meta.string("importance"),
        (meta?.get("relevance") as? JsonPrimitive)?.intOrNull ?: 0, meta.strings("tags"),
        meta.string("updated").ifBlank { meta.string("last_at").ifBlank { meta.string("last_seen") } }, projectExtra)
}

internal fun JsonObject?.string(key: String): String = (this?.get(key) as? JsonPrimitive)?.contentOrNull.orEmpty()
internal fun JsonObject?.strings(key: String): List<String> = (this?.get(key) as? JsonArray)
    ?.mapNotNull { (it as? JsonPrimitive)?.contentOrNull }.orEmpty()

internal fun knowledgeContext(detail: KnowledgeDetail): String {
    fun attribute(value: String) = value.replace("&", "&amp;").replace("\"", "&quot;")
        .replace("<", "&lt;").replace(">", "&gt;")
    val title = "${KnowledgeKind.fromWire(detail.kind)?.title.orEmpty()} · ${detail.entry().name}"
    val text = buildString {
        append(detail.summary.orEmpty().ifBlank { detail.entry().oneLine.orEmpty() })
        detail.log.orEmpty().takeLast(10).takeIf { it.isNotEmpty() }?.let {
            append("\n\n最近时间线：\n").append(it.joinToString("\n"))
        }
        detail.meta.strings("dirs").takeIf { it.isNotEmpty() }?.let {
            append("\n\n代码目录：\n").append(it.joinToString("\n"))
        }
        append("\n\n知识库目录：").append(detail.path)
    }.replace("</jarvis-context>", "&lt;/jarvis-context&gt;")
        .replace("<jarvis-context", "&lt;jarvis-context")
    return "<jarvis-context kind=\"context\" title=\"${attribute(title)}\" url=\"\">\n$text\n</jarvis-context>"
}
