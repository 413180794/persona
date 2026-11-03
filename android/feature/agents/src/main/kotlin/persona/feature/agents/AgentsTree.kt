package persona.feature.agents

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import persona.core.contracts.AgentState
import persona.core.contracts.Machine
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.sort.*

// The Agents tree (电脑 → 空间 → 会话) as lazy list lines. The lines are drawn by the design system's Agents components
// (AgentTreeHeader, AgentSpaceHeader, AgentRow, AgentIdleRow, MachineChip; docs/android-design.md 2.4).

/** One line of the flattened tree, in list order. [key] is the lazy item key. */
internal sealed interface AgentTreeEntry { val key: String }

/** A computer. Hidden when the list is already filtered to one computer (the title and chip say which). */
internal data class TreeMachineEntry(val machine: AgentMachineGroup, val first: Boolean) : AgentTreeEntry {
    override val key: String get() = "machine-header:${machine.id}"
}

/**
 * A group heading. [space] groups are herdr workspaces of a computer; the others (置顶, states, history projects)
 * are top-level sections. [depth] 1 sits under a computer heading. Quiet spaces (every agent finished and seen)
 * come after the active ones of their computer, folded by default and drawn weaker.
 */
internal data class TreeGroupEntry(val group: AgentListGroup, val space: Boolean, val depth: Int, val first: Boolean) : AgentTreeEntry {
    val quiet: Boolean get() = group.parent == QUIET
    override val key: String get() = "group:${group.id}"
}

/** A session. [located] rows sit outside the computer tree and show where they run. */
internal data class TreeRowEntry(val row: AgentSession, val depth: Int, val first: Boolean, val last: Boolean, val located: Boolean) : AgentTreeEntry {
    override val key: String get() = "session:${row.id}"
}

/** Finished sessions of an active space that are tucked away until asked for. */
internal data class TreeIdleEntry(val group: AgentListGroup, val hidden: Int, val depth: Int) : AgentTreeEntry {
    override val key: String get() = "idle:${group.id}"
}

private const val QUIET = "quiet"

/** Flattens the list into tree lines: 置顶 first, then every computer with its spaces, then any other groups. */
internal fun agentTreeEntries(list: AgentListResult, machineFilter: String): List<AgentTreeEntry> {
    val entries = mutableListOf<AgentTreeEntry>()
    fun group(group: AgentListGroup, space: Boolean, depth: Int, first: Boolean) {
        entries += TreeGroupEntry(group, space, depth, first)
        if (group.folded) return
        group.visibleRows.forEachIndexed { index, row ->
            entries += TreeRowEntry(row, depth, index == 0, index == group.visibleRows.lastIndex, located = !space)
        }
        val hidden = group.rows.size - group.visibleRows.size
        if (hidden > 0) entries += TreeIdleEntry(group, hidden, depth)
    }
    val byId = list.groups.associateBy { it.id }
    val treeIds = list.machines.flatMap { it.groups }.toSet()
    list.groups.filter { it.id == "pinned" }.forEach { group(it, space = false, depth = 0, first = entries.isEmpty()) }
    val headed = machineFilter.isEmpty() || list.machines.size > 1
    list.machines.forEach { machine ->
        // A computer whose sessions are all pinned has nothing left to hold here; 置顶 says where they run.
        if (machine.groups.none(byId::containsKey)) return@forEach
        if (headed) entries += TreeMachineEntry(machine, first = entries.isEmpty())
        if (headed && machine.folded) return@forEach
        machine.groups.mapNotNull(byId::get).forEachIndexed { index, it ->
            group(it, space = true, depth = if (headed) 1 else 0, first = index == 0)
        }
    }
    list.groups.filter { it.id != "pinned" && it.id !in treeIds }.forEach { group(it, space = false, depth = 0, first = entries.isEmpty()) }
    return entries
}

/** A computer in the filter strip: how many of its sessions the list would show. */
@Immutable
internal data class MachineCount(val machine: Machine, val count: Int)

/** Every computer, online ones first (the tree's order), with the count the list would show for it. */
internal fun machineCounts(machines: List<Machine>, rows: List<AgentSession>): List<MachineCount> {
    val counts = rows.groupingBy { it.session.machineId }.eachCount()
    return machines.sortedBy { !it.online }.map { MachineCount(it, counts[it.id] ?: 0) }
}

internal val Machine.deviceIcon: PhIcon get() = when (icon) {
    "server" -> PhIcons.HardDrives
    "desktop" -> PhIcons.Desktop
    else -> PhIcons.Laptop
}

// ------------------------------------------------------------------ filter strip

/** 全部机器 and every computer, side by side; tapping the selected computer goes back to all of them. */
@Composable
internal fun MachineFilterRow(machines: List<MachineCount>, total: Int, selected: String, onSelect: (String) -> Unit, modifier: Modifier = Modifier) {
    Row(
        modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(vertical = Tokens.Space.xs),
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically,
    ) {
        MachineChip("全部机器", total, selected.isEmpty(), onClick = { onSelect("") })
        machines.forEach { (machine, count) ->
            val on = machine.id == selected
            MachineChip(machine.displayName, count, on, onClick = { onSelect(if (on) "" else machine.id) }, icon = machine.deviceIcon, online = machine.online)
        }
    }
}

internal val AgentMachineGroup.headingCounts: List<Pair<Status, Int>>
    get() = listOf(AgentState.NeedsYou, AgentState.Working, AgentState.Unread).map { it.designStatus to (stateCounts[it] ?: 0) }

internal val AgentListGroup.headingCounts: List<Pair<Status, Int>>
    get() = listOf(AgentState.NeedsYou, AgentState.Working, AgentState.Unread).map { state -> state.designStatus to rows.count { it.state == state } }
