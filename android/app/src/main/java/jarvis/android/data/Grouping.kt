package jarvis.android.data

import java.time.Instant

/**
 * The Agents list's order and groupings, ported from the iOS app (ChatsView.swift: `rank`, `ordered`, `stays`,
 * `SpaceGroup.build`, and the section order of 按项目) so both phones list agents the same way. Plain Kotlin, no
 * Android types: unit tested in GroupingTest.
 */
enum class Grouping(val title: String) { Space("按项目"), State("按状态"), Machine("按机器") }

/** Nothing to look at now, but a /loop or background work will wake it: 待唤醒. */
val Session.isQuiet: Boolean get() = agentState == AgentState.Idle || agentState == AgentState.Unknown
val Session.isWaiting: Boolean
    get() = isQuiet && (loop != null || !background.isNullOrEmpty() || subagents.orEmpty().any { it.running })

/** The state sections of 按状态, in rank order. */
enum class Rank(val title: String) { NeedsYou("需要你"), Unread("完成未看"), Working("工作中"), Waiting("待唤醒"), Muted("已屏蔽"), Seen("已看") }

/** Agents that share a herdr workspace on one machine. */
data class SpaceGroup(
    val id: String, // machine/space id
    val machineId: String,
    val name: String,
    val index: Int,
    val workspaceId: String?,
    val sessions: List<Session>, // tab order
) {
    val state: AgentState get() = sessions.minByOrNull { it.agentState.priority }?.agentState ?: AgentState.Idle
    val updated: Instant get() = sessions.mapNotNull { it.updated }.maxOrNull() ?: Instant.EPOCH
    val isQuiet: Boolean get() = sessions.all { it.isQuiet && !it.isWaiting }
    /** The key pins and mutes use for a space: its name, since herdr workspace ids change when herdr restarts. */
    val pinKey: String get() = "$machineId/$name"
}

class Grouper(private val pins: Pins, private val mutes: Pins) {
    fun pinned(s: Session) = s.pinKey() in pins.sessions
    fun muted(s: Session) = mutes.mutes(s.pinKey(), s.spacePinKey())
    fun pinned(g: SpaceGroup) = g.pinKey in pins.spaces
    fun muted(g: SpaceGroup) = g.pinKey in mutes.spaces

    /** 需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看; a muted agent that needs you still comes first. */
    fun rank(s: Session): Rank {
        if (s.agentState == AgentState.NeedsYou) return Rank.NeedsYou
        val live = s.agentState == AgentState.Working || s.agentState == AgentState.Unread || s.isWaiting
        if (live && muted(s)) return Rank.Muted
        return when {
            s.agentState == AgentState.Unread -> Rank.Unread
            s.agentState == AgentState.Working -> Rank.Working
            s.isWaiting -> Rank.Waiting
            else -> Rank.Seen
        }
    }

    /** By rank, then most recently updated first. */
    fun ordered(list: List<Session>): List<Session> =
        list.sortedWith(compareBy<Session> { rank(it).ordinal }.thenByDescending { it.updated ?: Instant.EPOCH }.thenBy { it.sessionId })

    /** What stays out of a space's idle fold: anything not quiet, pinned, or about to wake. */
    fun stays(s: Session) = !s.isQuiet || pinned(s) || s.isWaiting

    /** Spaces of the running agents (offline ones have no herdr workspace), busiest and newest first. */
    fun spaces(sessions: List<Session>): List<SpaceGroup> =
        sessions.filter { it.isLive }
            .groupBy { "${it.machineId}/${it.spaceId ?: "-"}" }
            .map { (id, list) ->
                val first = list.first()
                SpaceGroup(
                    id = id, machineId = first.machineId,
                    name = first.spaceName?.takeIf { it.isNotBlank() } ?: first.folderName,
                    index = first.spaceIndex ?: 0, workspaceId = first.workspaceId ?: first.spaceId,
                    // Tab order, as in herdr: an agent keeps its place whatever its state.
                    sessions = list.sortedWith(compareBy<Session> { it.slotIndex ?: 0 }.thenBy { it.sessionId }),
                )
            }
            .sortedWith(compareBy<SpaceGroup> { it.state.priority }.thenByDescending { it.updated }.thenBy { it.id })

    data class SpaceLayout(val active: List<SpaceGroup>, val quiet: List<SpaceGroup>)

    /**
     * 按项目: pinned spaces lead, then the other busy ones, then muted spaces with something running (shown quietly);
     * quiet spaces fold into 空闲的空间.
     */
    fun spaceLayout(sessions: List<Session>): SpaceLayout {
        val groups = spaces(sessions)
        val pinnedSpaces = groups.filter { pinned(it) && !muted(it) }
        val active = pinnedSpaces +
            groups.filter { !it.isQuiet && !pinned(it) && !muted(it) } +
            groups.filter { !it.isQuiet && muted(it) }
        val quiet = groups.filter { it.isQuiet && !(pinned(it) && !muted(it)) }
        return SpaceLayout(active, quiet)
    }

    /** 按状态: one section per rank that has agents, each in `ordered` order. */
    fun byState(sessions: List<Session>): List<Pair<Rank, List<Session>>> =
        ordered(sessions).groupBy { rank(it) }.toSortedMap(compareBy { it.ordinal }).map { it.key to it.value }

    /** 按机器: a section per machine in the hub's order, its agents in `ordered` order. */
    fun byMachine(sessions: List<Session>, machines: List<Machine>): List<Pair<Machine, List<Session>>> {
        val by = sessions.groupBy { it.machineId }
        return machines.mapNotNull { m -> by[m.id]?.let { m to ordered(it) } }
    }
}

/** The quick filters over the list (iOS `AgentFilter`). */
enum class QuickFilter(val title: String, val state: AgentState?) {
    All("全部", null), NeedsYou("需要你", AgentState.NeedsYou), Unread("未读", AgentState.Unread), Working("工作中", AgentState.Working);

    fun matches(s: Session) = state == null || s.agentState == state
}

/** The search and machine menu over the list (iOS `ChatsView.base`): which agents are listed at all. */
fun Session.matches(query: String, machine: String?): Boolean {
    if (assistant) return false
    if (machine != null && machineId != machine) return false
    if (query.isBlank()) return true
    val q = query.trim()
    return listOfNotNull(title, cwd, lastText, spaceName, slotName).any { it.contains(q, ignoreCase = true) }
}

/**
 * Under a quick filter, what it showed stays until the filter is chosen again: an agent opened from 未读 is seen now
 * but keeps its place, so the next one is still right below it. `kept` grows with whatever matches.
 */
fun List<Session>.underFilter(f: QuickFilter, kept: Set<SessionKey>): List<Session> =
    if (f == QuickFilter.All) this else filter { f.matches(it) || it.key in kept }
