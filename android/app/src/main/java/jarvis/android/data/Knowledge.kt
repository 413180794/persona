package jarvis.android.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import java.time.Instant
import java.time.LocalDate

// The knowledge base under the hub's ~/.jarvis/data (docs/knowledge.md, iOS `Shared/Knowledge.swift`): projects,
// chats, people, docs, meetings and events, each ranked by importance (P0–P3) and relevance to the user, with tags.

/** The kinds, in the order the page shows them. */
enum class KbKind(val wire: String, val title: String) {
    PROJECTS("projects", "项目"),
    CHATS("chats", "会话"),
    PEOPLE("people", "人"),
    DOCS("docs", "文档"),
    MEETINGS("meetings", "会议"),
    EVENTS("events", "日程");

    companion object {
        fun of(wire: String?): KbKind? = entries.firstOrNull { it.wire == wire }
    }
}

/** One ranked entity of a list (`GET /api/v1/kb/{kind}`), also a detail's links. */
@Serializable
data class KbEntry(
    val kind: String = "",
    val id: String = "",
    val name: String = "",
    @SerialName("one_line") val oneLine: String? = null,
    val importance: String? = null,
    val relevance: Int = 0,
    val tags: List<String>? = null,
    @SerialName("updated") val updatedText: String? = null,
    /** Projects: "short · active"; chats: dm / group…; docs: the URL. */
    val extra: String? = null,
) {
    val key: String get() = "$kind/$id"
    val updated: Instant? get() = parseTime(updatedText)
    private val isProject get() = kind == KbKind.PROJECTS.wire

    /** Projects: 短期 / 长期. */
    val projectKind: String? get() = if (!isProject || extra == null) null else if (extra.startsWith("long")) "长期" else "短期"
    val projectDone: Boolean get() = isProject && (extra ?: "").endsWith("done")

    /** Marked "not a project" (by hand, or because it only came from notifications). */
    val projectIgnored: Boolean get() = isProject && (extra ?: "").endsWith("ignored")

    /** A project's wire kind and status from [extra] ("short" / "active" when missing). */
    val projectKindStatus: Pair<String, String>
        get() {
            val parts = (extra ?: "").split(" · ")
            return (parts.firstOrNull()?.takeIf { it.isNotBlank() } ?: "short") to (parts.getOrNull(1)?.takeIf { it.isNotBlank() } ?: "active")
        }

    /** A project's status when it is not the usual 进行中. */
    val projectStatusLabel: String?
        get() = if (!isProject) null else when (projectKindStatus.second) {
            "paused" -> "暂停"
            "done" -> "已完成"
            "ignored" -> "不是项目"
            else -> null
        }
}

@Serializable
data class KbList(val items: List<KbEntry> = emptyList(), val tags: List<String>? = null)

/** The projects list as iOS groups it: 重点 (P0 / P1) / 其他进行中 / 已完成 / 不是项目, each in the hub's order. */
data class KbProjectGroups(val focus: List<KbEntry>, val rest: List<KbEntry>, val done: List<KbEntry>, val ignored: List<KbEntry>) {
    companion object {
        fun of(items: List<KbEntry>): KbProjectGroups {
            val active = items.filter { !it.projectDone && !it.projectIgnored }
            val focus = active.filter { it.importance == "P0" || it.importance == "P1" }
            return KbProjectGroups(focus, active - focus.toSet(), items.filter { it.projectDone }, items.filter { it.projectIgnored })
        }
    }
}

@Serializable
data class KbDetail(
    val kind: String = "",
    val id: String = "",
    val path: String = "",
    val meta: JsonElement? = null,
    val summary: String? = null,
    val log: List<String>? = null,
    val recent: String? = null,
    val links: List<KbEntry>? = null,
) {
    private fun metaString(key: String): String? =
        ((meta as? JsonObject)?.get(key) as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull

    /** A project's code directories (`meta.dirs`, `<machine>:<path>`). */
    val dirs: List<String>
        get() = ((meta as? JsonObject)?.get("dirs") as? JsonArray)
            ?.mapNotNull { (it as? JsonPrimitive)?.takeIf { p -> p.isString }?.contentOrNull } ?: emptyList()

    /** [entry] updated from the meta (a linked row or a search hit starts with little more than an id). */
    fun merged(entry: KbEntry): KbEntry {
        val m = meta as? JsonObject ?: return entry
        var e = entry
        (metaString("name") ?: metaString("title"))?.takeIf { it.isNotEmpty() }?.let { e = e.copy(name = it) }
        metaString("importance")?.let { e = e.copy(importance = it) }
        (m["relevance"] as? JsonPrimitive)?.intOrNull?.let { e = e.copy(relevance = it) }
        (m["tags"] as? JsonArray)?.let { arr ->
            e = e.copy(tags = arr.mapNotNull { (it as? JsonPrimitive)?.takeIf { p -> p.isString }?.contentOrNull })
        }
        (metaString("one_line") ?: metaString("note"))?.takeIf { it.isNotEmpty() }?.let { e = e.copy(oneLine = it) }
        if (e.kind == KbKind.PROJECTS.wire) metaString("kind")?.let { k -> e = e.copy(extra = k + " · " + (metaString("status") ?: "active")) }
        return e
    }
}

@Serializable
data class KbDetailResponse(val detail: KbDetail = KbDetail(), val agents: List<Session>? = null)

/** A change made in the app; tagging set here is kept over the automatic one. */
@Serializable
data class KbUpdate(
    val importance: String? = null,
    val relevance: Int? = null,
    val tags: List<String>? = null,
    val status: String? = null,
    @SerialName("project_kind") val projectKind: String? = null,
)

@Serializable
data class KbHit(val path: String = "", val line: String = "") {
    /** The entry a hit is in: `chats/<id>/summary.md` → (chats, id), `docs/<id>.json` → (docs, id); null for anything else. */
    val entry: Pair<KbKind, String>?
        get() {
            val parts = path.split('/').filter { it.isNotEmpty() }
            if (parts.size < 2) return null
            val kind = KbKind.of(parts[0]) ?: return null
            val id = if (parts.size == 2 && parts[1].endsWith(".json")) parts[1].removeSuffix(".json") else parts[1]
            return if (id.isEmpty()) null else kind to id
        }

    /** Which file the line is from, said plainly. */
    val source: String
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

    /** The line without its markdown list marker, JSON key and quotes. */
    val text: String
        get() {
            var t = line.trim()
            for (p in listOf("- ", "* ", "## ", "# ")) if (t.startsWith(p)) t = t.removePrefix(p)
            if (t.startsWith("\"")) t.indexOf("\": ").takeIf { it >= 0 }?.let { t = t.substring(it + 3) }
            return t.trim(' ', ',', '"')
        }
}

/** The hits of one entry, in the order found; 档案 and 时间线 lines before 资料 (iOS `KBSearchGroup`). */
data class KbSearchGroup(val kind: KbKind, val id: String, val hits: List<KbHit>) {
    val key: String get() = kind.wire + "/" + id

    companion object {
        fun make(hits: List<KbHit>): List<KbSearchGroup> {
            val by = LinkedHashMap<String, Pair<Pair<KbKind, String>, MutableList<KbHit>>>()
            for (h in hits) {
                val ref = h.entry ?: continue
                val list = by.getOrPut(ref.first.wire + "/" + ref.second) { ref to mutableListOf() }.second
                if (list.none { it.text == h.text }) list += h
            }
            return by.values.map { (ref, list) ->
                KbSearchGroup(ref.first, ref.second, list.filter { it.source != "资料" } + list.filter { it.source == "资料" })
            }
        }
    }
}

/** A project's timeline line, `- 2026-10-30 23:16 · Agent · 标题 · 机器 · 说了什么`: when, from where, what. */
data class KbLogLine(val whenText: String, val source: String, val text: String) {
    /** "2026-10-30 23:16" → "10-30 23:16" (the year only when it is not this one). */
    fun shortWhen(today: LocalDate = LocalDate.now()): String {
        val year = today.year.toString() + "-"
        return if (whenText.startsWith(year)) whenText.removePrefix(year) else whenText
    }

    companion object {
        fun parse(raw: String): KbLogLine {
            var t = raw.trim()
            if (t.startsWith("- ")) t = t.removePrefix("- ")
            val parts = t.split(" · ")
            return if (parts.size >= 2 && parts[0].firstOrNull()?.isDigit() == true)
                KbLogLine(parts[0], if (parts.size > 2) parts.subList(1, parts.size - 1).joinToString(" · ") else "", parts.last())
            else KbLogLine("", "", t)
        }
    }
}

@Serializable private data class KbHits(val hits: List<KbHit> = emptyList())

private fun kbPath(kind: String, id: String? = null) = "/api/v1/kb/$kind" + (id?.let { "/$it" } ?: "")

suspend fun ApiClient.kbList(kind: String, tag: String? = null): KbList =
    JsonCodec.decodeFromString(raw("GET", kbPath(kind), query = if (tag.isNullOrEmpty()) emptyMap() else mapOf("tag" to tag)))

suspend fun ApiClient.kbDetail(kind: String, id: String): KbDetailResponse = JsonCodec.decodeFromString(raw("GET", kbPath(kind, id)))

suspend fun ApiClient.kbUpdate(kind: String, id: String, update: KbUpdate) {
    raw("PUT", kbPath(kind, id), JsonCodec.encodeToString(update))
}

suspend fun ApiClient.kbSearch(q: String): List<KbHit> =
    JsonCodec.decodeFromString<KbHits>(raw("GET", "/api/v1/kb-search", query = mapOf("q" to q))).hits

/** What 「问 Jarvis」 / 「开 Agent」 carry about an entry (iOS `KnowledgeDetailView.context`). */
object KbContext {
    /** A summary's leading `# Name` line is the page title already. */
    fun dropTitle(md: String): String = if (!md.startsWith("# ")) md else md.substringAfter('\n', "")

    /**
     * The profile without what the header already shows: its title, the one line, and the tags line of a person's
     * generated profile (「重要级 P0 · 与我相关性 95 · …」).
     */
    fun profile(md: String, oneLine: String?): String {
        val one = oneLine?.trim().orEmpty()
        return dropTitle(md).lines().filter { line ->
            val t = line.trim()
            !(t.startsWith("重要级 ") && "相关性" in t) && (one.isEmpty() || t != one)
        }.joinToString("\n").trim()
    }

    /** The `<jarvis-context>` tag the app puts in front of a message (iOS `ContextRef.xml`). */
    fun xml(title: String, text: String, kind: String = "context", url: String = ""): String {
        fun esc(s: String) = s.replace("&", "&amp;").replace("\"", "&quot;").replace("<", "&lt;").replace(">", "&gt;")
        return "<jarvis-context kind=\"$kind\" title=\"${esc(title)}\" url=\"${esc(url)}\">\n${text.replace("</jarvis-context>", "")}\n</jarvis-context>"
    }

    /** The entry's profile, its last 10 timeline lines and its directory, as a context tag. */
    fun context(entry: KbEntry, detail: KbDetail?): String {
        var text = detail?.summary ?: entry.oneLine ?: ""
        val log = detail?.log
        if (!log.isNullOrEmpty()) text += "\n\n时间线（最近）：\n" + log.takeLast(10).joinToString("\n")
        detail?.path?.takeIf { it.isNotEmpty() }?.let { text += "\n\n知识库目录：$it" }
        return xml("${KbKind.of(entry.kind)?.title.orEmpty()} · ${entry.name}", text)
    }

    fun ask(entry: KbEntry, detail: KbDetail?): String =
        context(entry, detail) + "\n\n说说「${entry.name}」现在怎么样了：最新进展、在等我的事、接下来该做什么，能替我处理的先处理。"

    /** A project's first code directory (`<machine>:<path>`) as (machine, path), for prefilling 开 Agent. */
    fun firstDir(dirs: List<String>): Pair<String, String>? =
        dirs.firstNotNullOfOrNull { d -> d.indexOf(':').takeIf { it > 0 }?.let { d.substring(0, it) to d.substring(it + 1) }?.takeIf { it.second.isNotEmpty() } }

    /** Tag editor text → tags (split on any whitespace, empty dropped). */
    fun parseTags(text: String): List<String> = text.split(Regex("\\s+")).filter { it.isNotEmpty() }
}
