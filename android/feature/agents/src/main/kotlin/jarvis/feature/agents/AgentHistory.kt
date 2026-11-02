package jarvis.feature.agents

import androidx.datastore.preferences.core.stringPreferencesKey
import jarvis.core.contracts.Machine
import jarvis.core.domain.sort.*

enum class AgentRange(val label: String) { Running("运行中"), All("全部"), History("历史") }

internal fun agentRangeKey(scope: String) = stringPreferencesKey("agent-range:$scope")

internal fun scopedAgentList(
    rows: List<AgentSession>, machines: List<Machine>, preferences: AgentsPreferencesSnapshot, settings: AgentListSettings,
): AgentListResult {
    if (preferences.range == AgentRange.Running)
        return buildAgentList(rows, machines, preferences.pins, preferences.mutes, settings)
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
        AgentListGroup(id, project.second.ifBlank { "未知目录" }, ordered, if (folded) emptyList() else ordered, folded)
    }.sortedWith(compareByDescending<AgentListGroup> { it.rows.first().updatedAt }.thenBy { it.id })
    val running = if (preferences.range == AgentRange.All)
        buildAgentList(rows.filter { it.session.isLive }, machines, preferences.pins, preferences.mutes, settings)
    else AgentListResult(emptyMap(), emptyList(), emptyList())
    return running.copy(groups = running.groups + groups, counts = AgentQuickFilter.entries.associateWith { filter ->
        (running.counts[filter] ?: 0) + history.count(filter::matches)
    })
}
