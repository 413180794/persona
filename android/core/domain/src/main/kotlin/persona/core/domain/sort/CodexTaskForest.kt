package persona.core.domain.sort

import persona.core.contracts.Session

/** Only Codex provenance defines this hierarchy; terminal pane relationships keep their existing behavior. */
val Session.isCodexTask: Boolean get() = sourceKind == "codex_history" || sourceKind == "codex_app_server"
private val Session.hasCodexIdentity: Boolean get() = agent == "codex" || sourceKind?.startsWith("codex_") == true
val Session.isCodexSubagent: Boolean get() = hasCodexIdentity &&
    (isSubagent || !parentSessionId.isNullOrBlank() || !parentThreadId.isNullOrBlank() ||
        (!rootSessionId.isNullOrBlank() && rootSessionId != sessionId))

data class CodexTaskForest(val roots: List<AgentSession>, val children: Map<String, List<AgentSession>>) {
    /** Search retains an ancestor when a descendant matches, so the matching conversation remains reachable. */
    fun searchSettings(settings: AgentListSettings): AgentListSettings {
        if (settings.query.isEmpty()) return settings
        fun matches(row: AgentSession): Boolean = settings.admits(row.session) || children[row.id].orEmpty().any(::matches)
        return settings.copy(queryAncestors = settings.queryAncestors + roots.filter(::matches).map { it.id })
    }
}

fun codexTaskForest(rows: List<AgentSession>): CodexTaskForest {
    val codex = rows.filter { it.session.hasCodexIdentity }
    val bySession = codex.associateBy { it.session.machineId to it.session.sessionId }
    val byThread = codex.filter { !it.session.sourceThreadId.isNullOrBlank() }
        .groupBy { Triple(it.session.machineId, it.session.sourceInstanceId, it.session.sourceThreadId) }
    val parents = mutableMapOf<String, String>()
    codex.forEach { row ->
        val s = row.session
        val parent = s.parentSessionId?.let { bySession[s.machineId to it] }
            ?: s.parentThreadId?.takeIf { s.parentSessionId.isNullOrBlank() }?.let { byThread[Triple(s.machineId, s.sourceInstanceId, it)]?.singleOrNull() }
            ?: s.rootSessionId?.let { bySession[s.machineId to it] }
        if (s.isCodexSubagent && parent != null && parent.id != row.id) parents[row.id] = parent.id
    }
    // Corrupt cycles must not hide every member or recurse forever. Keep one deterministic orphan root.
    parents.keys.sorted().forEach { start ->
        val visited = mutableSetOf<String>()
        var current: String? = start
        while (current != null && visited.add(current)) current = parents[current]
        if (current != null) parents.remove(current)
    }
    val children = rows.filter { it.id in parents }.groupBy { parents.getValue(it.id) }
        .mapValues { (_, members) -> members.sortedWith(compareByDescending<AgentSession> { it.updatedAt }.thenBy { it.id }) }
    return CodexTaskForest(rows.filterNot { it.id in parents }, children)
}
