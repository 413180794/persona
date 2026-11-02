package jarvis.core.contracts.api

import jarvis.core.contracts.HubScope
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

@Serializable
enum class KnowledgeKind(val title: String) {
    @SerialName("projects") Projects("项目"),
    @SerialName("chats") Chats("群聊"),
    @SerialName("people") People("人");

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

@Serializable
data class KnowledgeDetailResponse(val detail: KnowledgeDetail)

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
}

/** The composition root clears this before disposing a pairing's HTTP client. */
interface KnowledgeConnection {
    val current: StateFlow<KnowledgeRepository?>
}
