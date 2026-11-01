package jarvis.android.ui.machines

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Error
import androidx.compose.material.icons.outlined.Circle
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.DetectedEngine
import jarvis.android.data.EngineSelection
import jarvis.android.data.Engines
import jarvis.android.data.Hub
import jarvis.android.data.Machine
import jarvis.android.settings.setEngine
import jarvis.android.ui.settings.KitActionRow
import jarvis.android.ui.settings.KitGroup
import jarvis.android.ui.settings.KitNoticeRow
import jarvis.android.ui.settings.KitPage
import jarvis.android.ui.settings.kitColors
import jarvis.android.ui.settings.kitToast
import kotlinx.coroutines.launch

/**
 * Which terminal engine a machine uses (docs/engines.md), as on iOS `EngineSelectionView`: after pairing ([pairing],
 * with 暂时跳过 and a way on when saving fails) and from 设置 › 电脑 › 机器 › the machine. [fallback] is the machine as
 * the pairing check saw it, until the new hub's list has it.
 */
@Composable
fun EngineScreen(hub: Hub, machineId: String, onDone: () -> Unit, pairing: Boolean = false, fallback: Machine? = null) {
    val c = kitColors
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val machines by hub.machines.collectAsState()
    val machine = machines.firstOrNull { it.id == machineId } ?: fallback?.takeIf { it.id == machineId }
    val scope = rememberCoroutineScope()
    val selection = remember(hub, machineId, machine != null) {
        machine?.let { EngineSelection(it, pairing, put = { id, e -> hub.api.setEngine(id, e) }, updated = hub::applyMachine) }
    }
    LaunchedEffect(machine) { machine?.let { selection?.synchronize(it) } }

    KitPage(if (pairing) "选择引擎" else "引擎", onBack = { if (selection?.saving != true) onDone() }) {
        if (machine == null || selection == null) {
            item(key = "missing") {
                KitGroup(Modifier.padding(top = 20.dp)) {
                    row { KitNoticeRow("暂时读不到这台机器", if (pairing) "配对已完成，稍后可以在设置 › 电脑 › 机器里选择引擎。" else null) }
                    if (pairing) row { KitActionRow("先继续", onClick = onDone) }
                }
            }
            return@KitPage
        }
        if (pairing) {
            item(key = "title") {
                Column(Modifier.fillMaxWidth().padding(horizontal = 32.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text("这台电脑用哪个引擎", style = MaterialTheme.typography.headlineSmall, color = c.label)
                    Text(machine.displayName, fontSize = 15.sp, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
        item(key = "options") {
            KitGroup(header = "引擎", footer = Engines.footer(machine)) {
                Engines.options(machine).forEach { found ->
                    row {
                        EngineOption(
                            found, machine, selected = selection.selected == found.id,
                            enabled = !selection.saving && Engines.canSelect(found.id, machine),
                        ) { selection.choose(found.id) }
                    }
                }
            }
        }
        selection.error?.let { err ->
            item(key = "error") {
                KitGroup(Modifier.padding(top = 20.dp)) {
                    row {
                        KitNoticeRow(err, if (pairing) "配对已完成，可以先继续，稍后在设置 › 电脑 › 机器里修改。" else null,
                            tint = c.danger, icon = Icons.Default.Error)
                    }
                }
            }
        }
        item(key = "actions") {
            KitGroup(Modifier.padding(top = 20.dp)) {
                row {
                    KitActionRow(
                        if (pairing) "确认并继续" else "保存",
                        onClick = {
                            scope.launch {
                                if (selection.save(machineId)) {
                                    if (!pairing) kitToast(ctx, if (machine.online) "已切换引擎" else "已保存，连上后生效")
                                    onDone()
                                }
                            }
                        },
                        enabled = !selection.saving && Engines.canSelect(selection.selected, machine),
                        trailing = if (selection.saving) ({ CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp) }) else null,
                    )
                }
                if (pairing) row {
                    KitActionRow(if (selection.error == null) "暂时跳过" else "先继续", onClick = onDone, enabled = !selection.saving)
                }
            }
        }
    }
}

/** One engine: the hub's name, where its sessions live, the detection line and the hub's note; a check when chosen. */
@Composable
private fun EngineOption(found: DetectedEngine, machine: Machine, selected: Boolean, enabled: Boolean, onClick: () -> Unit) {
    val c = kitColors
    Row(
        Modifier.fillMaxWidth()
            .semantics { this.selected = selected }
            .clickable(enabled = enabled, role = Role.RadioButton, onClick = onClick)
            .heightIn(min = 52.dp)
            .padding(horizontal = 16.dp, vertical = 10.dp)
            .alpha(if (enabled) 1f else 0.45f),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(found.name.ifBlank { found.id }, fontSize = 16.sp, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Engines.detail(found.id)?.let { Text(it, fontSize = 13.sp, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            Text(Engines.detection(found.id, machine), fontSize = 12.sp, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
            found.note?.takeIf { it.isNotBlank() }?.let {
                Text(it, fontSize = 12.sp, lineHeight = 16.sp, color = c.secondaryLabel, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        Icon(
            if (selected) Icons.Default.CheckCircle else Icons.Outlined.Circle, if (selected) "已选" else null,
            tint = if (selected) c.accent else c.tertiaryLabel, modifier = Modifier.size(22.dp),
        )
    }
}
