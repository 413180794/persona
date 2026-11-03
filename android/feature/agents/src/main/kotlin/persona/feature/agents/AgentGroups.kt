package persona.feature.agents

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import persona.core.contracts.GroupKeys
import persona.core.contracts.Machine
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubGroupInfo
import persona.core.designsystem.MachineChip
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PhIcons

/*
 * Hub groups in the Agents list (docs/android.md「多 hub」). The list merges every group; with more than one, each
 * computer says which group it belongs to and a row of group chips filters by group. With a single group none of
 * this is drawn, so the page looks exactly as before groups existed.
 */

/** Which group the list shows; null in the view model is every group. The primary group's [namespace] is null. */
internal data class GroupFilter(val namespace: String?)

internal fun GroupFilter?.admits(machineId: String) = this == null || GroupKeys.namespaceOf(machineId) == namespace

/** The group's name for a computer, only when there is more than one group. */
internal fun List<HubGroupInfo>.tagFor(machineId: String): String? =
    if (size > 1) GroupKeys.namespaceOf(machineId).let { namespace -> firstOrNull { it.namespace == namespace }?.name } else null

/** Computer names for rows and request cards; with more than one group a name says its group, as 「MacBook（家里）」. */
internal fun List<HubGroupInfo>.machineNames(machines: List<Machine>): Map<String, String> =
    machines.associate { machine -> machine.id to (tagFor(machine.id)?.let { "${machine.displayName}（$it）" } ?: machine.displayName) }

/** 全部 hub and each group, side by side above the computers; only drawn with more than one group. */
@Composable
internal fun GroupFilterRow(groups: List<HubGroupInfo>, counts: Map<String?, Int>, selected: GroupFilter?, onSelect: (GroupFilter?) -> Unit) {
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(vertical = Tokens.Space.xs),
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically,
    ) {
        MachineChip("全部 hub", counts.values.sum(), selected == null, onClick = { onSelect(null) })
        groups.forEach { group ->
            val on = selected != null && selected.namespace == group.namespace
            MachineChip(group.name, counts[group.namespace] ?: 0, on, onClick = { onSelect(if (on) null else GroupFilter(group.namespace)) },
                icon = PhIcons.HardDrives, online = group.state in setOf(ConnectionState.Online, ConnectionState.Updating))
        }
    }
}
