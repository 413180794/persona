package persona.feature.agents

import androidx.datastore.preferences.core.stringPreferencesKey
import persona.core.contracts.Machine
import persona.core.domain.sort.*

/** Which sessions the list shows; its words are `AgentRange.label` (AgentsScreen.kt). */
enum class AgentRange { Running, All, History }

internal fun agentRangeKey(scope: String) = stringPreferencesKey("agent-range:$scope")

internal fun scopedAgentList(
    rows: List<AgentSession>, machines: List<Machine>, preferences: AgentsPreferencesSnapshot, settings: AgentListSettings,
    todos: List<persona.core.contracts.Todo> = emptyList(),
): AgentListResult {
    if (preferences.range == AgentRange.Running)
        return buildAgentList(rows, machines, preferences.pins, preferences.mutes, settings, todos)
    val history = rows.filter { row ->
        val s = row.session
        !s.isLive && !s.assistant && (settings.machineFilter.isEmpty() || settings.machineFilter == s.machineId) &&
            (settings.query.isEmpty() || listOf(s.displayTitle, s.cwd, s.lastText).any { it?.contains(settings.query, true) == true })
    }
    val filtered = history.filter { settings.quick.matches(it) }
    val groups = filtered.groupBy { it.session.machineId to it.session.cwd }.map { (project, members) ->
        val id = "history/${project.first}/${project.second}"
        val ordered = members.sortedWith(compareByDescending<AgentSession> { it.updatedAt }.thenBy { it.id })
        val folded = id in settings.folded
        AgentListGroup(id, persona.core.contracts.CoreText.plain(project.second), ordered, if (folded) emptyList() else ordered, folded)
    }.sortedWith(compareByDescending<AgentListGroup> { it.rows.first().updatedAt }.thenBy { it.id })
    val running = if (preferences.range == AgentRange.All)
        buildAgentList(rows.filter { it.session.isLive }, machines, preferences.pins, preferences.mutes, settings, todos)
    else AgentListResult(emptyMap(), emptyList(), emptyList())
    return running.copy(groups = running.groups + groups, counts = AgentQuickFilter.entries.associateWith { filter ->
        (running.counts[filter] ?: 0) + history.count(filter::matches)
    })
}
