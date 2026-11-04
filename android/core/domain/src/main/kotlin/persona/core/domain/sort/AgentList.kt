package persona.core.domain.sort

import persona.core.contracts.AgentState
import persona.core.contracts.Machine
import persona.core.contracts.Todo
import persona.core.contracts.state

/** Kept IDs retain membership, not a previous row position. Reselecting a filter replaces kept. */
data class AgentListSettings(
    val grouping: AgentGrouping = AgentGrouping.MachineSpace,
    val quick: AgentQuickFilter = AgentQuickFilter.All,
    val machineFilter: String = "",
    val query: String = "",
    val kept: Set<String> = emptySet(),
    val folded: Set<String> = emptySet(),
    val expandedIdle: Set<String> = emptySet(),
    val expandedQuiet: Set<String> = emptySet(),
)

enum class AgentGrouping {
    MachineSpace, State, Todo,
    // Retained for callers of the original list projection; stored preferences migrate to MachineSpace.
    Space, Machine;

    companion object {
        fun fromPreference(value: String?): AgentGrouping = when (value) {
            State.name -> State
            Todo.name -> Todo
            else -> MachineSpace
        }
        val selectable = listOf(MachineSpace, State, Todo)
    }
}
enum class AgentQuickFilter(val state: AgentState?) {
    All(null), NeedsYou(AgentState.NeedsYou), Unread(AgentState.Unread), Working(AgentState.Working);
    fun matches(session: AgentSession) = state == null || session.state == state
}

data class AgentListGroup(
    val id: String,
    val title: String,
    val rows: List<AgentSession>,
    val visibleRows: List<AgentSession>,
    val folded: Boolean = false,
    val parent: String? = null,
    val stateCounts: Map<AgentState, Int>? = null,
    val spoken: String? = null,
)

data class AgentListResult(
    val counts: Map<AgentQuickFilter, Int>,
    val groups: List<AgentListGroup>,
    val unreadShown: List<String>,
    val quietTitle: String? = null,
    val machines: List<AgentMachineGroup> = emptyList(),
)

data class AgentMachineGroup(
    val machine: Machine,
    val groups: List<String>,
    val sessionCount: Int,
    val spaceCount: Int,
    val stateCounts: Map<AgentState, Int>,
    val folded: Boolean,
) {
    val id: String get() = "machine/${machine.id}"
}

fun agentRank(session: AgentSession, mutes: AgentPreferences = AgentPreferences()): Int = when {
    session.state == AgentState.NeedsYou -> 0
    (session.state == AgentState.Working || session.state == AgentState.Unread || session.isWaiting) && mutes.mutes(session) -> 4
    session.state == AgentState.Unread -> 1
    session.state == AgentState.Working -> 2
    session.isWaiting -> 3
    else -> 5
}

fun buildAgentList(
    sessions: List<AgentSession>,
    machines: List<Machine> = emptyList(),
    pins: AgentPreferences = AgentPreferences(),
    mutes: AgentPreferences = AgentPreferences(),
    settings: AgentListSettings = AgentListSettings(),
    todos: List<Todo> = emptyList(),
): AgentListResult {
    val base = sessions.filter { row ->
        val s = row.session
        !s.assistant && (settings.machineFilter.isEmpty() || settings.machineFilter == s.machineId) &&
            (settings.query.isEmpty() || listOf(s.title, s.cwd, s.lastText, s.spaceName, s.slotName)
                .any { it?.contains(settings.query, ignoreCase = true) == true })
    }
    val counts = AgentQuickFilter.entries.filter { it != AgentQuickFilter.All }
        .associateWith { filter -> base.count(filter::matches) }
    val filtered = base.filter { settings.quick.matches(it) || it.id in settings.kept }
    val pinned = filtered.filter { it.id in pins.sessions }.sortedBy { pins.sessions.indexOf(it.id) }
    val live = filtered.filter { it.id !in pins.sessions && it.isLive }
    val quick = settings.quick != AgentQuickFilter.All
    val comparator = compareBy<AgentSession> { if (quick) 0 else agentRank(it, mutes) }
        .thenByDescending { it.sortDate }.thenBy { it.session.sessionId }.thenBy { it.id }
    fun ordered(rows: List<AgentSession>) = rows.sortedWith(comparator)
    fun stays(row: AgentSession) = !row.isQuiet || row.isWaiting
    fun showIdle(key: String) = key in settings.expandedIdle || settings.query.isNotEmpty()
    fun group(id: String, title: String, rows: List<AgentSession>, visible: List<AgentSession>): AgentListGroup {
        val folded = id in settings.folded
        return AgentListGroup(id, title, rows, if (folded) emptyList() else visible, folded)
    }
    val result = mutableListOf<AgentListGroup>()
    if (pinned.isNotEmpty()) result += if (settings.grouping == AgentGrouping.MachineSpace)
        group("pinned", "置顶", pinned, pinned) else AgentListGroup("pinned", "置顶", pinned, pinned)
    var quietTitle: String? = null
    when (settings.grouping) {
        AgentGrouping.MachineSpace -> {
            // Keep each session in a single leaf group; machine headers only reference those leaves.
            val treeRows = filtered.filter { it.isLive }
            val known = machines + treeRows.map { it.session.machineId }.distinct()
                .filter { id -> machines.none { it.id == id } }.map { Machine(it, "机器信息待更新") }
            val machineGroups = known.sortedBy { !it.online }.mapNotNull { machine ->
                val members = treeRows.filter { it.session.machineId == machine.id }
                if (members.isEmpty()) return@mapNotNull null
                val nested = buildAgentList(members, pins = pins, mutes = mutes,
                    settings = settings.copy(grouping = AgentGrouping.Space))
                val machineFolded = "machine/${machine.id}" in settings.folded
                result += nested.groups.filterNot { it.id == "pinned" }.map { group ->
                    val folded = group.folded || group.id in settings.folded
                    group.copy(folded = folded, visibleRows = if (machineFolded || folded) emptyList() else group.visibleRows)
                }
                val spaces = agentSpaces(members)
                AgentMachineGroup(machine, nested.groups.filterNot { it.id == "pinned" }.map { it.id }, members.size, spaces.size,
                    AgentState.entries.associateWith { state -> members.count { it.state == state } },
                    machineFolded)
            }
            return AgentListResult(counts, result, filtered.filter { it.state == AgentState.Unread }.map { it.id },
                machines = machineGroups)
        }
        AgentGrouping.State -> {
            val titles = listOf("needs" to "需要你", "unread" to "完成未看", "working" to "工作中",
                "waiting" to "待唤醒", "muted" to "已屏蔽", "idle" to "已看")
            val byRank = live.groupBy { agentRank(it, mutes) }
            titles.forEachIndexed { rank, (key, title) ->
                val rows = ordered(byRank[rank].orEmpty())
                if (rows.isNotEmpty()) result += group("state/$key", title, rows,
                    if (key == "idle" && !showIdle("state-idle")) emptyList() else rows)
            }
        }
        AgentGrouping.Todo -> {
            val sessionMap = sessions.associateBy { it.session.key }
            val uncompletedTodos = todos.filter { !it.isDone }.sortedWith(
                compareBy<Todo> { it.state { k -> sessionMap[k]?.session }.rank }
                    .thenByDescending { it.created ?: java.time.Instant.EPOCH }
                    .thenBy { it.id }
            )
            val claimed = mutableSetOf<persona.core.contracts.SessionKey>()
            for (todo in uncompletedTodos) {
                val linkedKeys = todo.currentAgents.map { it.key }.toSet()
                val matched = live.filter { (it.session.todo?.id == todo.id && it.session.todo?.id?.isNotEmpty() == true) || it.session.key in linkedKeys }
                if (matched.isNotEmpty()) {
                    val rows = ordered(matched)
                    val folded = "todo/${todo.id}" in settings.folded
                    val visible = if (folded) emptyList() else rows
                    val stateCounts = AgentState.entries.associateWith { state -> rows.count { it.state == state } }
                    result += AgentListGroup(
                        id = "todo/${todo.id}",
                        title = todo.title,
                        rows = rows,
                        visibleRows = visible,
                        folded = folded,
                        stateCounts = stateCounts
                    )
                    claimed.addAll(matched.map { it.session.key })
                }
            }
            val unlinked = live.filter { it.session.key !in claimed }
            if (unlinked.isNotEmpty()) {
                val rows = ordered(unlinked)
                val folded = "todo/none" in settings.folded
                val visible = if (folded) emptyList() else rows
                val stateCounts = AgentState.entries.associateWith { state -> rows.count { it.state == state } }
                result += AgentListGroup(
                    id = "todo/none",
                    title = "没有待办",
                    rows = rows,
                    visibleRows = visible,
                    folded = folded,
                    stateCounts = stateCounts
                )
            }
        }
        AgentGrouping.Machine -> machines.forEach { machine ->
            val rows = ordered(live.filter { it.session.machineId == machine.id })
            if (rows.isNotEmpty()) result += group("machine/${machine.id}", machine.name, rows,
                if (quick || showIdle("machine-${machine.id}")) rows else rows.filter(::stays))
        }
        AgentGrouping.Space -> {
            val spaces = agentSpaces(live)
            fun pinnedSpace(space: AgentSpace) = space.preferenceKey in pins.spaces
            fun mutedSpace(space: AgentSpace) = space.preferenceKey in mutes.spaces
            fun leading(space: AgentSpace) = pinnedSpace(space) && !mutedSpace(space)
            val active = spaces.filter(::leading) + if (quick) {
                spaces.filterNot(::leading).sortedWith(compareByDescending<AgentSpace> { it.updatedAt }.thenBy { it.id })
            } else {
                spaces.filter { !it.quiet && !pinnedSpace(it) && !mutedSpace(it) } +
                    spaces.filter { !it.quiet && mutedSpace(it) }
            }
            val quiet = if (quick) emptyList() else spaces.filter { it.quiet && !leading(it) }
            if (quiet.isNotEmpty()) quietTitle = "空闲的空间"
            fun spaceGroup(space: AgentSpace, isQuiet: Boolean): AgentListGroup {
                val rows = if (isQuiet) space.members else ordered(space.members)
                val folded = if (isQuiet) space.id !in settings.expandedQuiet && settings.query.isEmpty()
                    else space.id in settings.folded
                val visible = when {
                    folded -> emptyList()
                    isQuiet || quick || showIdle(space.id) -> rows
                    else -> rows.filter(::stays)
                }
                val states = listOf(AgentState.NeedsYou, AgentState.Working, AgentState.Unread, AgentState.Idle, AgentState.Unknown)
                val stateCounts = states.associateWith { state -> rows.count { it.state == state } }
                val labels = listOf(AgentState.NeedsYou to "需要你", AgentState.Working to "工作中", AgentState.Unread to "完成未看")
                val spoken = labels.mapNotNull { (state, label) -> stateCounts.getValue(state).takeIf { it > 0 }?.let { "$it 个$label" } } +
                    (stateCounts.getValue(AgentState.Idle) + stateCounts.getValue(AgentState.Unknown))
                        .takeIf { it > 0 }?.let { listOf("$it 个空闲") }.orEmpty()
                return AgentListGroup(space.id, space.name, rows, visible, folded,
                    if (isQuiet) "quiet" else null, stateCounts, spoken.joinToString("，"))
            }
            result += active.map { spaceGroup(it, false) }
            result += quiet.map { spaceGroup(it, true) }
        }
    }
    return AgentListResult(counts, result, filtered.filter { it.state == AgentState.Unread }.map { it.id }, quietTitle)
}
