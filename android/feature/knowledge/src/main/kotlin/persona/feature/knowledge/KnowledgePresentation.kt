package persona.feature.knowledge

import persona.core.contracts.Session
import persona.core.contracts.api.*
import kotlinx.serialization.json.*
import java.time.LocalDate

internal data class KnowledgeSection(
    val title: String,
    val entries: List<KnowledgeEntry>,
    val collapsed: Boolean = false,
    /** Said under a folded section: why its entries are there and how they come back. */
    val note: String? = null,
)

/** One entry found by a search, with up to all of its hits; [named] once a list told us the entry's name. */
internal data class KnowledgeSearchGroup(
    val kind: KnowledgeKind,
    val id: String,
    val entry: KnowledgeEntry,
    val named: Boolean,
    val hits: List<KnowledgeHit>,
) {
    val key: String get() = "${kind.wire}/$id"
}

/** Every kind the app knows: all conversations, private ones too (docs/redesign/parity.md 3.1). */
internal fun KnowledgeEntry.inScope(): Boolean = KnowledgeKind.fromWire(kind) != null

/** Projects as iOS groups them: 重点 (P0 / P1), 其他进行中, 已完成, then 不是项目 folded with a word on why. */
internal fun projectSections(entries: List<KnowledgeEntry>): List<KnowledgeSection> {
    val (ignored, visible) = entries.partition { it.projectStatus == "ignored" }
    val (done, active) = visible.partition { it.projectStatus == "done" }
    val (focus, rest) = active.partition { it.importance in setOf("P0", "P1") }
    return listOf(
        KnowledgeSection("重点", focus),
        KnowledgeSection(if (focus.isEmpty()) "进行中" else "其他进行中", rest),
        KnowledgeSection("已完成", done),
        KnowledgeSection("不是项目", ignored, collapsed = true,
            note = "只在通知、营销邮件、机器人会话里出现的，不会再更新。打标里改回「进行中」就恢复。"),
    ).filter { it.entries.isNotEmpty() }
}

/** The other kinds keep the hub's order (importance, then relevance); a tag names the section. */
internal fun kindSections(entries: List<KnowledgeEntry>, tag: String?): List<KnowledgeSection> =
    if (entries.isEmpty()) emptyList() else listOf(KnowledgeSection(tag?.let { "#$it" } ?: "按重要级和相关性", entries))

/** A project's `extra` is "short|long · active|paused|done|ignored"; missing halves read as short and active. */
internal val KnowledgeEntry.projectKind: String
    get() = extra.orEmpty().split(" · ").firstOrNull()?.trim()?.takeIf { it.isNotEmpty() } ?: "short"
internal val KnowledgeEntry.projectStatus: String
    get() = extra.orEmpty().split(" · ").getOrNull(1)?.trim()?.takeIf { it.isNotEmpty() } ?: "active"
internal val KnowledgeEntry.isProject: Boolean get() = kind == KnowledgeKind.Projects.wire

/** 暂停 / 已完成 / 不是项目: a project's status when it is not the usual 进行中. */
internal val KnowledgeEntry.projectStatusLabel: String?
    get() = if (!isProject) null else when (projectStatus) {
        "paused" -> "暂停"
        "done" -> "已完成"
        "ignored" -> "不是项目"
        else -> null
    }

/** The entry a hit is in: `chats/<id>/summary.md` → (chats, id), `docs/<id>.json` → (docs, id); null for anything else. */
internal fun KnowledgeHit.reference(): Pair<KnowledgeKind, String>? {
    val parts = path.split('/').filter { it.isNotEmpty() }
    if (parts.size < 2 || parts.any { it == "." || it == ".." }) return null
    val kind = KnowledgeKind.fromWire(parts[0]) ?: return null
    val id = if (parts.size == 2 && parts[1].endsWith(".json")) parts[1].removeSuffix(".json") else parts[1]
    return if (id.isEmpty()) null else kind to id
}

/** Which file the line is from, said plainly. */
internal val KnowledgeHit.source: String
    get() {
        val file = path.substringAfterLast('/')
        if ("/messages/" in path) return "聊天记录 · " + file.substringBeforeLast('.')
        return when (file) {
            "summary.md" -> "档案"
            "log.md" -> "时间线"
            "meta.json" -> "资料"
            else -> if (file.endsWith(".json")) "资料" else file
        }
    }

/** The line without its markdown list marker or heading, JSON key and quotes. */
internal val KnowledgeHit.text: String
    get() {
        var text = line.trim()
        for (prefix in listOf("- ", "* ", "## ", "# ")) if (text.startsWith(prefix)) text = text.removePrefix(prefix)
        if (text.startsWith("\"")) text.indexOf("\": ").takeIf { it >= 0 }?.let { text = text.substring(it + 3) }
        return text.trim(' ', ',', '"')
    }

/**
 * Hits grouped by the entry they are in, in the order found, each line once; 档案 and 时间线 lines before 资料
 * (iOS `KBSearchGroup`). Hits outside any entry (the directory's README) are no result. [index] names the entries
 * a list has shown; the others are named by their kind until one does.
 */
internal fun searchGroups(hits: List<KnowledgeHit>, index: Map<String, KnowledgeEntry>): List<KnowledgeSearchGroup> {
    val byEntry = LinkedHashMap<String, Pair<Pair<KnowledgeKind, String>, MutableList<KnowledgeHit>>>()
    for (hit in hits) {
        val reference = hit.reference() ?: continue
        val list = byEntry.getOrPut("${reference.first.wire}/${reference.second}") { reference to mutableListOf() }.second
        if (list.none { it.text == hit.text }) list += hit
    }
    return byEntry.map { (key, value) ->
        val (reference, list) = value
        val known = index[key]
        KnowledgeSearchGroup(reference.first, reference.second,
            known ?: KnowledgeEntry(reference.first.wire, reference.second, reference.first.title), known != null,
            list.filter { it.source != "资料" } + list.filter { it.source == "资料" })
    }
}

/** Search results by kind, in the order the list offers the kinds. */
internal fun searchSections(groups: List<KnowledgeSearchGroup>): List<Pair<KnowledgeKind, List<KnowledgeSearchGroup>>> =
    KnowledgeKind.entries.mapNotNull { kind -> groups.filter { it.kind == kind }.takeIf { it.isNotEmpty() }?.let { kind to it } }

/** Where every word of [query] (any case) occurs in [text], merged: the hub matches lines with all the words. */
internal fun queryRanges(text: String, query: String): List<IntRange> {
    val words = query.split(Regex("\\s+")).filter { it.isNotEmpty() }
    if (words.isEmpty() || text.isEmpty()) return emptyList()
    val on = BooleanArray(text.length)
    for (word in words) {
        var from = 0
        while (true) {
            val at = text.indexOf(word, from, ignoreCase = true)
            if (at < 0) break
            for (i in at until at + word.length) on[i] = true
            from = at + word.length
        }
    }
    val ranges = mutableListOf<IntRange>()
    var i = 0
    while (i < text.length) {
        if (!on[i]) { i++; continue }
        var j = i
        while (j < text.length && on[j]) j++
        ranges += i until j
        i = j
    }
    return ranges
}

/** A timeline line, `- 2026-10-30 23:16 · Agent · 标题 · 机器 · 说了什么`: when, from where, what. */
internal data class KnowledgeLogLine(val whenText: String, val source: String, val text: String) {
    /** "2026-10-30 23:16" → "10-30 23:16"; the year only when it is not this one. */
    fun shortWhen(today: LocalDate = LocalDate.now()): String {
        val year = "${today.year}-"
        return if (whenText.startsWith(year)) whenText.removePrefix(year) else whenText
    }

    companion object {
        fun parse(raw: String): KnowledgeLogLine {
            var text = raw.trim()
            if (text.startsWith("- ")) text = text.removePrefix("- ")
            val parts = text.split(" · ")
            return if (parts.size >= 2 && parts[0].firstOrNull()?.isDigit() == true)
                KnowledgeLogLine(parts[0], if (parts.size > 2) parts.subList(1, parts.size - 1).joinToString(" · ") else "", parts.last())
            else KnowledgeLogLine("", "", text)
        }
    }
}

/** Newest first, as the page shows the timeline. */
internal fun timeline(log: List<String>?): List<KnowledgeLogLine> = log.orEmpty().asReversed().map(KnowledgeLogLine::parse)

/** A summary's leading `# Name` line is the page title already. */
internal fun dropTitle(markdown: String): String = if (!markdown.startsWith("# ")) markdown else markdown.substringAfter('\n', "")

/**
 * The profile without what the header already shows: its title, the one line, and the line of a person's generated
 * profile that only repeats the tagging (「重要级 P0 · 与我相关性 95 · …」). Blank when nothing else is left.
 */
internal fun profile(markdown: String?, oneLine: String?): String {
    val one = oneLine?.trim().orEmpty()
    return dropTitle(markdown.orEmpty()).lines().filter { line ->
        val text = line.trim()
        !(text.startsWith("重要级 ") && "相关性" in text) && (one.isEmpty() || text != one)
    }.joinToString("\n").trim()
}

/** Related entries by kind, in the list's order. */
internal fun linkGroups(links: List<KnowledgeEntry>?): List<Pair<KnowledgeKind, List<KnowledgeEntry>>> {
    val byKind = links.orEmpty().distinctBy { it.key }.groupBy { it.kind }
    return KnowledgeKind.entries.mapNotNull { kind -> byKind[kind.wire]?.takeIf { it.isNotEmpty() }?.let { kind to it } }
}

/** A project's first code directory (`<machine>:<path>`) as (machine, path), to open an Agent there. */
internal fun firstDir(dirs: List<String>): Pair<String, String>? = dirs.firstNotNullOfOrNull { dir ->
    dir.indexOf(':').takeIf { it > 0 }?.let { dir.substring(0, it) to dir.substring(it + 1) }?.takeIf { it.second.isNotEmpty() }
}

/** `<machine>:<path>` split for display: the machine, and the path's last folders (which tell projects apart). */
internal fun dirParts(dir: String): Pair<String?, String> {
    val colon = dir.indexOf(':')
    return if (colon <= 0) null to pathTail(dir) else dir.substring(0, colon) to pathTail(dir.substring(colon + 1))
}

internal fun pathTail(path: String, keep: Int = 3): String {
    val parts = path.trimEnd('/').split('/')
    return if (parts.size <= keep + 1) path else "…/" + parts.takeLast(keep).joinToString("/")
}

/** The tag editor's text as tags: split on any whitespace, empty ones dropped. */
internal fun parseTags(text: String): List<String> = text.split(Regex("\\s+")).filter { it.isNotEmpty() }

/** Only live sessions that are not Persona itself can work on a project. */
internal fun projectAgents(agents: List<Session>?): List<Session> = agents.orEmpty().filter { !it.assistant && it.isLive }

internal fun KnowledgeDetail.entry(): KnowledgeEntry {
    val name = meta.string("name").ifBlank { meta.string("title") }.ifBlank { id }
    val projectExtra = if (kind == KnowledgeKind.Projects.wire)
        "${meta.string("kind").ifBlank { "short" }} · ${meta.string("status").ifBlank { "active" }}" else meta.string("kind")
    return KnowledgeEntry(kind, id, name,
        meta.string("one_line").ifBlank { meta.string("note") }, meta.string("importance"),
        (meta?.get("relevance") as? JsonPrimitive)?.intOrNull ?: 0, meta.strings("tags"),
        meta.string("updated").ifBlank { meta.string("last_at").ifBlank { meta.string("last_seen") } }, projectExtra)
}

internal fun JsonObject?.string(key: String): String = (this?.get(key) as? JsonPrimitive)?.contentOrNull.orEmpty()
internal fun JsonObject?.strings(key: String): List<String> = (this?.get(key) as? JsonArray)
    ?.mapNotNull { (it as? JsonPrimitive)?.contentOrNull }.orEmpty()

/** The tagging a form starts with: the entry's own, or P2 for an entry the hub has not ranked yet. */
internal data class KnowledgeTagForm(
    val importance: String,
    val relevance: Int,
    val tags: String,
    val projectKind: String,
    val status: String,
) {
    fun update(isProject: Boolean): KnowledgeUpdate = KnowledgeUpdate(importance, relevance.coerceIn(0, 100), parseTags(tags),
        status = if (isProject) status else null, projectKind = if (isProject) projectKind else null)

    companion object {
        fun of(entry: KnowledgeEntry) = KnowledgeTagForm(
            importance = entry.importance?.takeIf { it in IMPORTANCE } ?: "P2",
            // Kept as it is until the slider moves (then in steps of 5), so saving other fields does not change it.
            relevance = entry.relevance.coerceIn(0, 100),
            tags = entry.tags.orEmpty().joinToString(" "),
            projectKind = entry.projectKind,
            status = entry.projectStatus,
        )
        val IMPORTANCE = listOf("P0", "P1", "P2", "P3")
        val PROJECT_KINDS = listOf("short" to "短期（需求）", "long" to "长期（使命）")
        val PROJECT_STATUSES = listOf("active" to "进行中", "paused" to "暂停", "done" to "已完成", "ignored" to "不是项目")
    }
}

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

/** What 开 Agent hands the new-Agent page: where to start (a project's first code directory) and the first message. */
data class KnowledgeAgentStart(val machineId: String?, val cwd: String?, val prompt: String)

internal fun agentStart(detail: KnowledgeDetail): KnowledgeAgentStart {
    val dir = firstDir(detail.meta.strings("dirs"))
    return KnowledgeAgentStart(dir?.first, dir?.second, knowledgeContext(detail) + "\n\n")
}
