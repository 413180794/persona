package persona.feature.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.DetectedEngine
import persona.core.contracts.Machine
import persona.core.contracts.api.MachineManagement
import persona.core.contracts.api.SessionsRepository
import persona.core.designsystem.*
import persona.core.domain.engines.Engines
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/**
 * Which terminal engine a machine uses (docs/engines.md), as on iOS `EngineSelectionView`. Right after pairing
 * ([pairing]) an engine must be chosen: no back, no skip, and a failed save stays on the step; from the machine page
 * it is an ordinary page that saves and goes back.
 */
@Composable
fun EngineSelectionScreen(
    machineId: String,
    sessions: SessionsRepository,
    management: MachineManagement?,
    pairing: Boolean,
    onDone: () -> Unit,
    onBack: (() -> Unit)? = null,
) {
    val machines by sessions.machines.collectAsState()
    val machine = machines.firstOrNull { it.id == machineId }
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    // Built once the machine is known; pushes about it keep the picker in step until the user picks something.
    val model = remember(machineId, management, machine != null) {
        if (machine == null || management == null) null
        else EngineSelectionModel(machine, put = management::setEngine, applied = sessions::applyMachine)
    }
    LaunchedEffect(machine) { machine?.let { model?.synchronize(it) } }
    val state = model?.state?.collectAsState()?.value ?: EngineSelectionState()
    var reading by remember(machineId) { mutableStateOf(machine == null) }
    LaunchedEffect(machineId, reading) {
        if (!reading) return@LaunchedEffect
        try { sessions.refresh() } catch (cancelled: CancellationException) { throw cancelled } catch (_: Exception) { }
        reading = false
    }
    // Pairing: system back does not skip the step (2026-11-01, the user).
    BackHandler(enabled = pairing) { }
    BackHandler(enabled = !pairing && state.saving) { }
    val ready = machine != null && model != null
    JListPage(title = if (pairing) "选择引擎" else "引擎", onBack = if (pairing) null else onBack, largeTitle = false, bottomBar = if (!ready) null else ({
        Row(Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s)) {
            JButton(if (pairing) "确认并继续" else "保存", {
                val target = machine ?: return@JButton
                scope.launch {
                    if (model?.save(target.id) == true) {
                        if (!pairing) feedback?.notice(Engines.saved(target))
                        onDone()
                    }
                }
            }, Modifier.fillMaxWidth(), loading = state.saving,
                enabled = !state.saving && machine != null && Engines.canSelect(state.selected, machine))
        }
    })) {
        if (pairing) item("heading") {
            Column(Modifier.padding(vertical = Tokens.Space.l), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                Text("这台电脑用哪个引擎", style = PersonaTheme.type.largeTitle, color = PersonaTheme.palette.ink)
                Text(machine?.displayName ?: machineId, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        if (machine == null || model == null) {
            if (reading) item("reading") { JLoading(text = "正在读这台机器") }
            else item("missing") {
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                    JNotice(if (management == null) "体验模式不能修改引擎。" else if (pairing)
                        "配对已完成，稍后可以在 Agents 的机器监控里选择引擎。" else "hub 上暂时没有这台机器。",
                        title = "暂时读不到这台机器", status = Status.NeedsYou)
                    if (management != null) JButton("重试", { reading = true }, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary)
                    if (pairing) JButton("先继续", onDone, Modifier.fillMaxWidth())
                }
            }
            return@JListPage
        }
        if (!pairing) item("title") { JSectionHeader(machine.displayName) }
        val options = Engines.options(machine)
        itemsIndexed(options, key = { _, it -> "engine-${it.id}" }) { index, found ->
            JGroupItem(GroupPosition.of(index, options.size)) {
                EngineOption(found, machine, selected = state.selected == found.id,
                    enabled = !state.saving && Engines.canSelect(found.id, machine)) { model.choose(found.id) }
            }
        }
        item("footer") { SettingsCaption(Engines.footer(machine), Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s)) }
        state.error?.let { error ->
            item("error") {
                Column(Modifier.padding(top = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    JInlineStatus(error, tone = InlineTone.Failed)
                    if (pairing) SettingsCaption("必须选一个引擎才能继续，检查网络后再点一次确认。")
                }
            }
        }
    }
}

/** One engine: its name, where its sessions live, whether it was found there and the hub's note; a radio when chosen. */
@Composable
private fun EngineOption(found: DetectedEngine, machine: Machine, selected: Boolean, enabled: Boolean, onClick: () -> Unit) {
    JRow(onClick = onClick, enabled = enabled, chevron = false, selected = false) {
        JRadio(selected, null, enabled = enabled)
        Spacer(Modifier.width(Tokens.Space.m))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
            Text(found.name.ifBlank { found.id }, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Engines.detail(found.id)?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            Text(Engines.detection(found.id, machine), style = PersonaTheme.type.caption,
                color = if (found.available && machine.online) PersonaTheme.palette.positive else PersonaTheme.palette.ink3, maxLines = 1)
            found.note?.takeIf { it.isNotBlank() }?.let {
                Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.cautionText, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}
