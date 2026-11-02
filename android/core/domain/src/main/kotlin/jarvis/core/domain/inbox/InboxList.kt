package jarvis.core.domain.inbox

import jarvis.core.contracts.parseTime
import jarvis.core.domain.sort.AgentPreferences
import jarvis.core.domain.sort.AgentSession

/** List-only input; read flags and agent execution state do not determine triage or completion. */
data class InboxItem(
    val id: String,
    val account: String = "",
    val kind: String = "",
    val title: String = "",
    val sender: String? = null,
    val preview: String = "",
    val summary: String? = null,
    val tier: String? = null,
    val updatedAt: String? = null,
    val doneAt: String? = null,
) {
    val triage: InboxTier get() = InboxTier.entries.firstOrNull { it.wire == tier } ?: InboxTier.Action
    val isTriaged: Boolean get() = !tier.isNullOrEmpty()
    val isDone: Boolean get() = doneAt != null
}

enum class InboxTier(val wire: String, val title: String) {
    Action("action", "需要你"), Fyi("fyi", "知悉即可"), Noise("noise", "已折叠")
}

data class InboxInteraction(
    val id: String,
    val machineId: String,
    val sessionId: String,
    val createdAt: String? = null,
    val sessionTitle: String = "",
    val summary: String? = null,
    val title: String = "",
) {
    val sessionKey: String get() = "$machineId/$sessionId"
}

data class InboxListSettings(
    val source: String = "",
    val kind: String = "",
    val query: String = "",
    val expanded: Set<String> = setOf("action", "fyi"),
)

enum class InboxRowType { Item, Interaction }
data class InboxRow(val type: InboxRowType, val id: String)
data class InboxGroup(
    val id: String,
    val title: String,
    val rows: List<InboxRow>,
    val visibleRows: List<InboxRow>,
    val folded: Boolean,
) {
    val count: Int get() = rows.size
}
data class InboxListResult(
    val groups: List<InboxGroup>,
    val actionCount: Int,
    val filterCounts: Map<String, Int> = emptyMap(),
)

fun orderHubInbox(items: List<InboxItem>): List<InboxItem> = items.sortedWith(
    compareByDescending<InboxItem> { parseTime(it.updatedAt) }.thenBy { it.id },
)

/** Preserve cached ordering across different timestamps; sort only members sharing a real timestamp. */
private fun breakUpdateTies(items: List<InboxItem>): List<InboxItem> {
    val ties = items.filter { parseTime(it.updatedAt) != null }.groupBy { parseTime(it.updatedAt) }
        .mapValues { (_, members) -> members.sortedBy { it.id }.iterator() }
    return items.map { item -> ties[parseTime(item.updatedAt)]?.next() ?: item }
}

fun buildInboxList(
    items: List<InboxItem>,
    interactions: List<InboxInteraction> = emptyList(),
    sessions: List<AgentSession> = emptyList(),
    mutes: AgentPreferences = AgentPreferences(),
    settings: InboxListSettings = InboxListSettings(),
): InboxListResult {
    fun matchesText(vararg fields: String?): Boolean = settings.query.isEmpty() ||
        fields.any { it?.contains(settings.query, ignoreCase = true) == true }
    val shown = items.filter { item ->
        (settings.source.isEmpty() || item.account == settings.source) &&
            (settings.kind.isEmpty() || item.kind == settings.kind || settings.kind == "group" && item.kind == "bot") &&
            matchesText(item.title, item.sender, item.preview, item.summary)
    }
    val bySession = sessions.associateBy { it.id }
    val waiting = if (settings.source.isEmpty() && settings.kind in setOf("", "agent")) {
        interactions.filter {
            !mutes.mutes(it.sessionKey, bySession[it.sessionKey]?.spacePreferenceKey) &&
                matchesText(it.sessionTitle, it.summary, it.title)
        }.sortedWith(compareBy<InboxInteraction> { parseTime(it.createdAt) }.thenBy { it.id })
    } else emptyList()
    val groups = mutableListOf<InboxGroup>()
    fun addGroup(id: String, title: String, rows: List<InboxRow>) {
        if (rows.isEmpty()) return
        val folded = id !in settings.expanded
        groups += InboxGroup(id, title, rows, if (folded) emptyList() else rows, folded)
    }
    for (tier in InboxTier.entries) {
        val rows = breakUpdateTies(shown.filter { !it.isDone && it.triage == tier })
            .map { InboxRow(InboxRowType.Item, it.id) }
        val agents = if (tier == InboxTier.Action) waiting.map { InboxRow(InboxRowType.Interaction, it.id) } else emptyList()
        addGroup(tier.wire, tier.title, agents + rows)
    }
    val done = shown.filter { it.isDone }
        .sortedWith(compareByDescending<InboxItem> { parseTime(it.doneAt) }.thenBy { it.id })
        .map { InboxRow(InboxRowType.Item, it.id) }
    addGroup("done", "已处理", done)
    return InboxListResult(groups, items.count { !it.isDone && it.triage == InboxTier.Action })
}
