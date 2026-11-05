package persona.core.domain.sort

import persona.core.contracts.AgentState
import persona.core.contracts.Machine
import persona.core.contracts.Todo
import persona.core.contracts.state

/** Kept IDs retain membership, not a previous row position. Reselecting a filter replaces kept. */
data class AgentListSettings(
    val grouping: AgentGrouping = AgentGrouping.Repo,
    val quick: AgentQuickFilter = AgentQuickFilter.All,
    val machineFilter: String = "",
    val query: String = "",
    val kept: Set<String> = emptySet(),
    val folded: Set<String> = emptySet(),
    val expandedIdle: Set<String> = emptySet(),
    val expandedQuiet: Set<String> = emptySet(),
)

enum class AgentGrouping {
    Repo, MachineSpace, State, Todo,
    // Retained for callers of the original list projection; stored preferences migrate to MachineSpace.
    Space, Machine;

    companion object {
        /** A phone that never chose a grouping gets 按仓库 (#2252, as iOS #2108); any stored choice keeps its own. */
        fun fromPreference(value: String?): AgentGrouping = when (value) {
            null, Repo.name -> Repo
            State.name -> State
            Todo.name -> Todo
            else -> MachineSpace
        }
        val selectable = listOf(Repo, MachineSpace, State, Todo)
    }
}

/** A group's place in 按仓库: a repository, or one of its working trees (the main checkout or another). */
enum class RepoLevel { Repository, MainCheckout, Worktree }
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
    /** 按仓库: the repository or working tree this group is. */
    val repo: RepoLevel? = null,
    /** The group this one sits in: a working tree's repository, whose own [rows] are then empty (its trees hold them). */
    val under: String? = null,
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
    /** 按仓库: how many repositories it has. */
    val repoCount: Int? = null,
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
    if (pinned.isNotEmpty()) result += if (settings.grouping == AgentGrouping.MachineSpace || settings.grouping == AgentGrouping.Repo)
        group("pinned", "置顶", pinned, pinned) else AgentListGroup("pinned", "置顶", pinned, pinned)
    var quietTitle: String? = null
    fun stateCounts(rows: List<AgentSession>) = AgentState.entries.associateWith { state -> rows.count { it.state == state } }
    when (settings.grouping) {
        AgentGrouping.Repo -> {
            // Computer → repository → working tree → sessions, from each cwd (web `buildRepoMachineTree`, iOS `RepoTree`).
            // A repository with nothing going on is quiet: folded, after the busy ones, like a quiet space; a quiet working
            // tree is folded too but keeps its place (main checkout first, then by name, as on iOS).
            fun quiet(members: List<AgentSession>) = !quick && members.none(::stays)
            fun section(id: String, title: String, members: List<AgentSession>, level: RepoLevel, under: String?, machineFolded: Boolean): AgentListGroup {
                val isQuiet = quiet(members)
                val rows = ordered(members)
                val folded = if (isQuiet) id !in settings.expandedQuiet && settings.query.isEmpty() else id in settings.folded
                val visible = when {
                    folded || machineFolded -> emptyList()
                    isQuiet || quick || showIdle(id) -> rows
                    else -> rows.filter(::stays)
                }
                return AgentListGroup(id, title, rows, visible, folded, if (isQuiet) "quiet" else null, stateCounts(rows), repo = level, under = under)
            }
            val known = machines + live.map { it.session.machineId }.distinct()
                .filter { id -> machines.none { it.id == id } }.map { Machine(it, "机器信息待更新") }
            val machineGroups = known.sortedBy { !it.online }.mapNotNull { machine ->
                val members = live.filter { it.session.machineId == machine.id }
                if (members.isEmpty()) return@mapNotNull null
                val machineFolded = "machine/${machine.id}" in settings.folded
                val repos = agentRepos(members).sortedBy { quiet(it.members) }
                val groups = repos.flatMap { repo ->
                    // A quiet repository opens to all its sessions in one list, as iOS's 空闲的仓库 does. A repository with only
                    // its main checkout is drawn as one level but keeps the checkout's id, so a session opened under a quick
                    // filter stays in the same group when a second working tree appears or goes (retainOpenedRows).
                    if (repo.flat) listOf(section(repo.worktrees[0].id, repo.name, repo.members, RepoLevel.Repository, null, machineFolded))
                    else if (quiet(repo.members)) listOf(section(repo.id, repo.name, repo.members, RepoLevel.Repository, null, machineFolded))
                    else listOf(AgentListGroup(repo.id, repo.name, emptyList(), emptyList(), repo.id in settings.folded,
                        stateCounts = stateCounts(repo.members), repo = RepoLevel.Repository)) +
                        repo.worktrees.map { tree ->
                            section(tree.id, tree.name, tree.members, if (tree.main) RepoLevel.MainCheckout else RepoLevel.Worktree, repo.id, machineFolded)
                        }
                }
                result += groups
                AgentMachineGroup(machine, groups.map { it.id }, members.size, agentSpaces(members).size, stateCounts(members),
                    machineFolded, repoCount = repos.size)
            }
            return AgentListResult(counts, result, filtered.filter { it.state == AgentState.Unread }.map { it.id },
                machines = machineGroups)
        }
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
