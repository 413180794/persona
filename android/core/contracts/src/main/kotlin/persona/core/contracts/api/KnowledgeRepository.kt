package persona.core.contracts.api

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.HubScope
import persona.core.contracts.Session
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/** The kinds of the knowledge base, in the order the list offers them (docs/knowledge.md). */
@Serializable
enum class KnowledgeKind(val title: CoreText) {
    @SerialName("projects") Projects(CoreText.of(Key.KnowledgeProjects)),
    @SerialName("chats") Chats(CoreText.of(Key.KnowledgeChats)),
    @SerialName("people") People(CoreText.of(Key.KnowledgePeople)),
    @SerialName("docs") Docs(CoreText.of(Key.KnowledgeDocs)),
    @SerialName("meetings") Meetings(CoreText.of(Key.KnowledgeMeetings)),
    @SerialName("events") Events(CoreText.of(Key.KnowledgeEvents));

    val wire: String get() = name.lowercase()
    companion object {
        fun fromWire(value: String): KnowledgeKind? = entries.firstOrNull { it.wire == value }
    }
}

@Serializable
data class KnowledgeEntry(
    val kind: String,
    val id: String,
    val name: String,
    @SerialName("one_line") val oneLine: String? = null,
    val importance: String? = null,
    val relevance: Int = 0,
    val tags: List<String>? = null,
    val updated: String? = null,
    val extra: String? = null,
) {
    val key: String get() = "$kind/$id"
}

@Serializable
data class KnowledgeList(val items: List<KnowledgeEntry> = emptyList(), val tags: List<String>? = null)

@Serializable
data class KnowledgeDetail(
    val kind: String,
    val id: String,
    val path: String,
    val meta: JsonObject? = null,
    val summary: String? = null,
    val log: List<String>? = null,
    val recent: String? = null,
    val links: List<KnowledgeEntry>? = null,
)

/** [agents]: the live, non-assistant sessions working in a project's code directories (projects only). */
@Serializable
data class KnowledgeDetailResponse(val detail: KnowledgeDetail, val agents: List<Session>? = null)

/**
 * Tagging set by hand (`PUT /api/v1/kb/{kind}/{id}`); the hub keeps it over its automatic tagging. Only the fields
 * that are set go on the wire; an empty [tags] list is sent, and clears the tags.
 */
@Serializable
data class KnowledgeUpdate(
    val importance: String? = null,
    val relevance: Int? = null,
    val tags: List<String>? = null,
    val status: String? = null,
    @SerialName("project_kind") val projectKind: String? = null,
)

@Serializable
data class KnowledgeHit(val path: String, val line: String)

@Serializable
data class KnowledgeSearch(val hits: List<KnowledgeHit> = emptyList())

/** An immutable repository belongs to exactly one pairing generation. */
interface KnowledgeRepository {
    val scope: HubScope
    suspend fun list(kind: KnowledgeKind, tag: String? = null): KnowledgeList
    suspend fun detail(kind: KnowledgeKind, id: String): KnowledgeDetail
    suspend fun search(query: String): List<KnowledgeHit>

    /** The detail together with the Agents working on it; repositories without them answer the detail alone. */
    suspend fun entry(kind: KnowledgeKind, id: String): KnowledgeDetailResponse = KnowledgeDetailResponse(detail(kind, id))

    /** Saves hand-made tagging; see [KnowledgeUpdate]. */
    suspend fun update(kind: KnowledgeKind, id: String, update: KnowledgeUpdate): Unit =
        throw UnsupportedOperationException("Knowledge tagging unavailable")
}

/** The composition root clears this before disposing a pairing's HTTP client. */
interface KnowledgeConnection {
    val current: StateFlow<KnowledgeRepository?>
}
