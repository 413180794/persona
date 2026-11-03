package persona.feature.settings

import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/**
 * 设置 › hub (docs/android.md「多 hub」): the phone's hub groups. A group is hubs linked by the hub mesh; the lists
 * show every group together, the hub-level pages (Jarvis, inbox, hub settings) show the selected one. Adding a hub
 * puts it into the group its mesh says, else it starts a new group.
 */
@Composable
internal fun HubsScreen(hubs: HubGroups, onAdd: (() -> Unit)?, onBack: () -> Unit, footer: @Composable () -> Unit = {}) {
    val groups by hubs.groups.collectAsState()
    val scope = rememberCoroutineScope()
    var menu by remember { mutableStateOf<HubGroupInfo?>(null) }
    var hubMenu by remember { mutableStateOf<Pair<HubGroupInfo, HubMemberInfo>?>(null) }
    var renaming by remember { mutableStateOf<HubGroupInfo?>(null) }
    var removing by remember { mutableStateOf<HubGroupInfo?>(null) }
    var removingHub by remember { mutableStateOf<Pair<HubGroupInfo, HubMemberInfo>?>(null) }
    var busy by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    fun run(failure: String, block: suspend () -> Unit) {
        if (busy) return
        busy = true
        scope.launch {
            try { block(); message = null }
            catch (error: CancellationException) { throw error }
            catch (_: Exception) { message = failure }
            finally { busy = false }
        }
    }
    JListPage(title = "hub", onBack = onBack) {
        item {
            SettingsCaption(if (groups.size > 1) "Agents、机器和要你处理的请求把各组合在一起显示。Jarvis、收件箱和 hub 相关的设置用选中的那一组。"
                else "互相连通的几台 hub 算一组。再添加一台不相通的 hub，会单独成一组，列表合在一起显示。",
                Modifier.padding(top = Tokens.Space.m))
        }
        if (groups.isEmpty()) item { JEmptyState("还没有配对 hub", "添加一台 hub 后，这里会列出它和同组的 hub。") }
        groups.forEach { group ->
            item(key = "head-" + group.id) { JSectionHeader(group.name) }
            val rows = group.hubs.size + 1
            item(key = "state-" + group.id) {
                JGroupItem(GroupPosition.of(0, rows)) {
                    JRow(if (group.state == ConnectionState.Online) "已连接" else group.state.label(), leadingIcon = PhIcons.TreeStructure,
                        subtitle = groupStateText(group, groups.size > 1), onClick = { menu = group })
                }
            }
            group.hubs.forEachIndexed { index, hub ->
                item(key = "hub-" + group.id + "-" + hub.hubId) {
                    JGroupItem(GroupPosition.of(index + 1, rows)) {
                        JRow(hub.name, leadingIcon = PhIcons.HardDrives, subtitle = hubText(hub),
                            value = if (hub.connected) "正在使用" else null,
                            onClick = if (hub.paired && group.hubs.count { it.paired } > 1) ({ hubMenu = group to hub }) else null)
                    }
                }
            }
        }
        if (onAdd != null) item {
            JButton("添加 hub", onAdd, Modifier.fillMaxWidth().padding(top = Tokens.Space.xl), icon = PhIcons.Plus, enabled = !busy)
        }
        message?.let { item { JNotice(it, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou) } }
        item { footer() }
    }
    menu?.let { group ->
        JActionSheet(listOfNotNull(
            JAction("用于 Jarvis 和收件箱", PhIcons.Sparkle, checked = group.selected, enabled = !group.selected) {
                run("没能切换，请稍后再试。") { hubs.select(group.id) }
            }.takeIf { groups.size > 1 },
            JAction("改名", PhIcons.PencilSimple) { renaming = group },
            JAction("断开这一组", PhIcons.Trash, destructive = true, separatorBefore = true) { removing = group },
        ), onDismiss = { menu = null }, title = group.name)
    }
    hubMenu?.let { (group, hub) ->
        JActionSheet(listOf(JAction("断开这台 hub", PhIcons.Trash, destructive = true) { removingHub = group to hub }),
            onDismiss = { hubMenu = null }, title = hub.name)
    }
    renaming?.let { group ->
        var name by remember(group.id) { mutableStateOf(group.name) }
        JDialog(onDismiss = { renaming = null }, title = "给这一组改名",
            confirm = "保存" to { renaming = null; run("没能保存名字，请稍后再试。") { hubs.rename(group.id, name) } },
            confirmEnabled = name.isNotBlank()) {
            JTextField(name, { name = it }, Modifier.fillMaxWidth(), label = "名字", singleLine = true)
            SettingsCaption("只在这台手机上显示，不改 hub 自己的名字。", Modifier.padding(top = Tokens.Space.s))
        }
    }
    removing?.let { group ->
        JDialog(onDismiss = { removing = null }, title = "断开「${group.name}」？",
            text = "这一组的 ${group.hubs.count { it.paired }} 台 hub 都会断开，本机的凭据和这一组的本地数据一起清除。别的组不受影响。",
            destructive = true, confirmEnabled = !busy,
            confirm = "断开" to { removing = null; run("没能断开，请稍后再试。") { hubs.removeGroup(group.id) } })
    }
    removingHub?.let { (group, hub) ->
        JDialog(onDismiss = { removingHub = null }, title = "断开「${hub.name}」？",
            text = "清除本机保存的这台 hub 的凭据。「${group.name}」里的其他 hub 照常使用，本地数据保留。",
            destructive = true, confirmEnabled = !busy,
            confirm = "断开" to { removingHub = null; run("没能断开，请稍后再试。") { hubs.removeHub(group.id, hub.hubId) } })
    }
}

private fun groupStateText(group: HubGroupInfo, several: Boolean): String {
    val paired = group.hubs.count { it.paired }
    val mesh = group.hubs.size - paired
    val hubs = if (mesh == 0) "$paired 台 hub" else "$paired 台已配对，$mesh 台经互联发现"
    return if (several && group.selected) "$hubs，Jarvis 和收件箱在用" else hubs
}

private fun hubText(hub: HubMemberInfo): String? = when {
    !hub.paired && hub.online == false -> "经互联发现，离线"
    !hub.paired -> "经互联发现，用这一组的凭据连接"
    hub.online == false -> "离线"
    else -> null
}
