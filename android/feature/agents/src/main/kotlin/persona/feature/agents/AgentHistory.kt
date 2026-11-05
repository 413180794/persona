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
    val history = rows.filter { !it.session.isLive && settings.admitsHistory(it.session) && !(settings.hideBotmux && it.session.isBotmux) }
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

private fun AgentListSettings.admitsHistory(s: persona.core.contracts.Session): Boolean =
    !s.assistant && (machineFilter.isEmpty() || machineFilter == s.machineId) &&
        (query.isEmpty() || listOf(s.displayTitle, s.cwd, s.lastText).any { it?.contains(query, true) == true })

/**
 * The botmux sessions the 显示 botmux switch counts: what [scopedAgentList] would count under 飞书群 with botmux shown,
 * without building the list again (it runs on every push).
 */
internal fun botmuxCount(rows: List<AgentSession>, preferences: AgentsPreferencesSnapshot, settings: AgentListSettings): Int =
    rows.count { row ->
        val s = row.session
        s.isBotmux && when (preferences.range) {
            AgentRange.Running -> settings.admits(s)
            AgentRange.All -> if (s.isLive) settings.admits(s) else settings.admitsHistory(s)
            AgentRange.History -> !s.isLive && settings.admitsHistory(s)
        }
    }
