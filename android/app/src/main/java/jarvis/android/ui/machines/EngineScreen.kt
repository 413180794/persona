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
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.settings.KitActionRow
import jarvis.android.ui.settings.KitGroup
import jarvis.android.ui.settings.KitNoticeRow
import jarvis.android.ui.settings.KitPage
import jarvis.android.ui.settings.kitColors
import jarvis.android.ui.settings.kitToast
import kotlinx.coroutines.launch

/**
 * Which terminal engine a machine uses (docs/engines.md), as on iOS `EngineSelectionView`: after pairing ([pairing],
 * an engine must be chosen: no skip, a failed save stays on the step) and from 设置 › 电脑 › 机器 › the machine. [fallback] is the machine as
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

    // Right after pairing an engine must be chosen: no back button (system back is held by the caller too).
    KitPage(if (pairing) "选择引擎" else "引擎", onBack = if (pairing) null else ({ if (selection?.saving != true) onDone() })) {
        if (machine == null || selection == null) {
            item(key = "missing") {
                KitGroup(Modifier.padding(top = JSpace.xl)) {
                    row { KitNoticeRow("暂时读不到这台机器", if (pairing) "配对已完成，稍后可以在设置 › 电脑 › 机器里选择引擎。" else null) }
                    if (pairing) row { KitActionRow("先继续", onClick = onDone) }
                }
            }
            return@KitPage
        }
        if (pairing) {
            item(key = "title") {
                Column(Modifier.fillMaxWidth().padding(horizontal = JSpace.page + JSpace.xs, vertical = JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                    Text("这台电脑用哪个引擎", style = JTheme.type.title, color = c.label)
                    Text(machine.displayName, style = JTheme.type.secondary, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
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
                KitGroup(Modifier.padding(top = JSpace.xl)) {
                    row {
                        KitNoticeRow(err, if (pairing) "必须选一个引擎才能继续，检查网络后再点一次确认。" else null,
                            tint = c.danger, icon = JIcons.error)
                    }
                }
            }
        }
        item(key = "actions") {
            KitGroup(Modifier.padding(top = JSpace.xl)) {
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
                        trailing = if (selection.saving) ({ CircularProgressIndicator(Modifier.size(JSize.markL), strokeWidth = JSize.rule, color = c.secondaryLabel) }) else null,
                    )
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
            .heightIn(min = JSize.box50)
            .padding(horizontal = JSpace.l, vertical = JSpace.m)
            .alpha(if (enabled) 1f else 0.45f),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(found.name.ifBlank { found.id }, style = JTheme.type.body, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Engines.detail(found.id)?.let { Text(it, style = JTheme.type.caption, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            Text(Engines.detection(found.id, machine), style = JTheme.type.label, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
            found.note?.takeIf { it.isNotBlank() }?.let {
                Text(it, style = JTheme.type.label, color = c.secondaryLabel, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        Icon(
            if (selected) JIcons.success else JIcons.unchecked, if (selected) "已选" else null,
            tint = if (selected) c.accent else c.tertiaryLabel, modifier = Modifier.size(JSize.iconS),
        )
    }
}
