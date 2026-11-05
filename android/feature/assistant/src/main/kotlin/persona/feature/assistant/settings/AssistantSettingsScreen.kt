package persona.feature.assistant.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dagger.hilt.android.EntryPointAccessors
import persona.core.contracts.Machine
import persona.core.contracts.SessionKey
import persona.core.contracts.api.AssistantConfig
import persona.core.contracts.api.AssistantManagement
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.ChatRoute
import persona.core.navigation.ConversationKind
import persona.core.navigation.Navigator
import persona.feature.assistant.AssistantAppHost
import persona.feature.assistant.AssistantEntryPoint
import persona.feature.assistant.LocalAssistantHost
import persona.feature.assistant.conversationTime
import persona.feature.assistant.proactive.AssistantProactiveScreen
import kotlinx.coroutines.launch

/**
 * 设置 › 助手 as a destination of its own (a wide window shows it beside the settings list). It needs the pairing's
 * assistant transport; the demo has none and says so.
 */
@Composable
fun AssistantSettingsDestination(navigator: Navigator, onBack: () -> Unit) = AssistantAppHost(navigator) {
    val host = LocalAssistantHost.current
    if (host == null) {
        JListPage(title = "助手", onBack = onBack) {
            item {
                JEmptyState("体验模式里没有助手设置", "连上自己的 hub 以后，可以在这里选 Persona 在哪台机器上运行，改关于我和记忆。",
                    icon = PhIcons.Sparkle)
            }
        }
        return@AssistantAppHost
    }
    val context = LocalContext.current.applicationContext
    val conversations = remember(context) {
        EntryPointAccessors.fromApplication(context, AssistantEntryPoint::class.java).assistantConversations()
    }
    AssistantSettingsScreen(host.management, host.machines, onBack, onMoved = { conversations.refresh() },
        // An assistant conversation belongs to the Persona tab, which shows it on its home.
        onOpenConversation = { navigator.navigate(ChatRoute(it.machineId, it.sessionId, kind = ConversationKind.Assistant)) })
}

/** Debug builds open one part of the page directly for captures (`debug.assistantSettings`); release never sets it. */
object AssistantSettingsPreview {
    @Volatile var open: String? = null
}

private enum class SettingsPart { Home, Notes, Memory, MemoryFile, Proactive }
private enum class SettingsSheet { Machine, Zone }

/**
 * Where the assistant runs (machine, directory, and what lives where; its model is 模型配置 › 主对话, #1291), what it knows about the user
 * (关于我, memory) and its time zone. Sub-pages stay inside this destination, so they open in the same pane.
 */
@Composable
internal fun AssistantSettingsScreen(
    management: AssistantManagement,
    machines: List<Machine>,
    onBack: () -> Unit,
    onMoved: suspend () -> Unit = {},
    /** Shown in place of another page (the Persona tab) rather than as a navigation destination: back closes it here. */
    inPlace: Boolean = false,
    /** Opens an assistant conversation (one a run of 主动做事 started, or a follow-up continues). */
    onOpenConversation: (SessionKey) -> Unit = {},
) {
    val context = LocalContext.current
    val moved by rememberUpdatedState(onMoved)
    // The outbox that binds the main conversation: told when a move clears it.
    val sender by rememberUpdatedState(LocalAssistantHost.current?.mainSender)
    val controller = remember(management) {
        AssistantSettingsState(management.settingsApi(), StoredTimezoneChoice(context), mainChanged = { sender?.mainChanged() }) { moved() }
    }
    val state by controller.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    val preview = remember { AssistantSettingsPreview.open.also { AssistantSettingsPreview.open = null } }
    var part by rememberSaveable { mutableStateOf(when (preview) {
        "notes" -> SettingsPart.Notes
        "memory", "memory-file", "memory-edit", "memory-delete" -> SettingsPart.Memory
        "proactive" -> SettingsPart.Proactive
        else -> SettingsPart.Home
    }) }
    var memoryName by rememberSaveable { mutableStateOf<String?>(null) }
    var sheet by remember { mutableStateOf(when (preview) {
        "machine" -> SettingsSheet.Machine
        "timezone" -> SettingsSheet.Zone
        else -> null
    }) }
    LaunchedEffect(controller) {
        controller.load()
        controller.loadMemory()
    }
    // Captures of a memory file and of the move warning wait for the data they show.
    LaunchedEffect(state.memory.entries, state.config, machines) {
        if (preview in setOf("memory-file", "memory-edit", "memory-delete") && memoryName == null) state.memory.entries?.firstOrNull()?.let {
            memoryName = it.file.name; part = SettingsPart.MemoryFile
        }
        if (preview == "move" && state.move == null && state.config != null) {
            machines.firstOrNull { it.id != state.config?.machineId }?.let { controller.requestMove(it, machines) }
        }
    }
    // Results go to the shell's notice when there is one, else stay on the page until the next action.
    LaunchedEffect(state.notice) {
        val notice = state.notice ?: return@LaunchedEffect
        if (feedback?.notice(notice.text) == true) controller.clearNotice()
    }
    BackHandler(part != SettingsPart.Home || inPlace) {
        when (part) {
            SettingsPart.Home -> onBack()
            SettingsPart.MemoryFile -> part = SettingsPart.Memory
            else -> part = SettingsPart.Home
        }
    }
    // With the shell's notice the page draws none of its own (not even for the frame before it is handed over).
    val shown = if (feedback == null) state else state.copy(notice = null)
    val machine = machines.firstOrNull { it.id == state.config?.machineId }
    val machineName = machine?.displayName ?: state.config?.machineId?.takeIf { it.isNotBlank() } ?: "助手所在的机器"
    fun refresh() {
        scope.launch {
            controller.load()
            controller.loadMemory()
        }
    }
    when (part) {
        SettingsPart.Home -> SettingsHome(shown, machine, machineName, controller.phoneZone, onBack, ::refresh,
            open = { part = it }, sheet = { sheet = it }, follow = { scope.launch { controller.setFollowPhone(it) } })
        SettingsPart.Notes -> NotesPart(shown, onBack = { part = SettingsPart.Home }) { text -> controller.saveNotes(text) }
        SettingsPart.Memory -> MemoryPart(shown, machine, machineName, onBack = { part = SettingsPart.Home },
            onRefresh = { scope.launch { controller.loadMemory() } },
            onOpen = { memoryName = it; part = SettingsPart.MemoryFile })
        SettingsPart.MemoryFile -> MemoryFilePart(shown, memoryName, preview, onBack = { part = SettingsPart.Memory },
            onSave = { name, content -> controller.saveMemory(name, content) },
            onDelete = { name -> if (controller.deleteMemory(name)) part = SettingsPart.Memory })
        // Keeps its own sub-pages and back; back on its first page returns here (the handler above).
        SettingsPart.Proactive -> AssistantProactiveScreen(onBack = { part = SettingsPart.Home }, onOpenConversation = onOpenConversation,
            management = management, machines = machines)
    }
    when (sheet) {
        SettingsSheet.Machine -> MachineSheet(machines, state.config?.machineId, onDismiss = { sheet = null }) {
            sheet = null
            controller.requestMove(it, machines)
        }
        SettingsSheet.Zone -> ZoneSheet(state.config?.timezone, controller.phoneZone, onDismiss = { sheet = null }) {
            sheet = null
            scope.launch { controller.chooseZone(it) }
        }
        null -> Unit
    }
    state.move?.let { move ->
        JDialog(onDismiss = controller::cancelMove, title = move.title, text = move.text,
            confirm = "换过去" to { scope.launch { controller.confirmMove() }; Unit },
            dismiss = "取消" to controller::cancelMove)
    }
}

@Composable
private fun Caption(text: String) {
    Text(text, Modifier.padding(top = Tokens.Space.s, start = Tokens.Space.xs, end = Tokens.Space.xs),
        style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
}

/** A group of rows, one lazy item each (design rule 5). */
private fun LazyListScope.rows(key: String, rows: List<@Composable () -> Unit>) {
    rows.forEachIndexed { index, row ->
        item("$key-$index") { JGroupItem(GroupPosition.of(index, rows.size)) { row() } }
    }
}

private fun machineIcon(machine: Machine?): PhIcon = when {
    machine == null -> PhIcons.Desktop
    machine.icon == "laptop" || machine.os == "darwin" -> PhIcons.Laptop
    machine.icon == "server" || machine.kind == "local" -> PhIcons.HardDrives
    else -> PhIcons.Desktop
}

private fun machineState(machine: Machine?): String = when {
    machine == null -> "不在机器列表里"
    machine.online && machine.kind == "local" -> "在线，hub 所在的机器"
    machine.online -> "在线"
    else -> "离线"
}

private fun zoneLabel(zone: String): String = zoneOffset(zone)?.let { "$zone（$it）" } ?: zone

@Composable
private fun SettingsHome(
    state: AssistantSettingsUiState, machine: Machine?, machineName: String, phoneZone: String,
    onBack: () -> Unit, onRefresh: () -> Unit, open: (SettingsPart) -> Unit, sheet: (SettingsSheet) -> Unit,
    follow: (Boolean) -> Unit,
) {
    val config = state.config
    val ready = config != null && !state.busy
    JListPage(
        title = "助手", onBack = onBack, refreshing = state.loading && config != null, onRefresh = onRefresh,
        summary = config?.let { "Persona 在「$machineName」上运行。" },
    ) {
        if (state.loadFailed) item("load-failed") {
            JNotice("读不到助手的设置，请检查连接后下拉重试。", status = Status.NeedsYou,
                actionLabel = "重试", onAction = onRefresh)
        }
        if (config == null && state.loading) item("loading") { JLoading(text = "正在读取") }
        state.notice?.let { notice -> item("notice") { JNotice(notice.text, status = if (notice.error) Status.NeedsYou else Status.Unknown) } }
        if (config != null) {
            item("where") { JSectionHeader("在哪运行") }
            rows("where", listOf(
                { JRow("机器", value = machineName, subtitle = machineState(machine), leadingIcon = machineIcon(machine),
                    enabled = ready, onClick = { sheet(SettingsSheet.Machine) }) },
                { JRow("工作目录", value = homeRelative(config.cwd, machine?.home).ifBlank { "~/.jarvis/assistant" },
                    leadingIcon = PhIcons.FolderSimple) },
            ))
            if (machine != null && !machine.online) item("offline") {
                JNotice("「$machineName」现在离线，连上之前 Persona 不能回复，记忆也读不到。", Modifier.padding(top = Tokens.Space.m),
                    status = Status.Offline)
            }
            item("where-caption") {
                Caption("每段对话都在这台机器的这个目录里后台运行一个 Claude Code，不进 herdr。用哪个模型在「设置 › 模型配置 › 主对话」里选。")
            }

            item("proactive") { JSectionHeader("主动做事") }
            rows("proactive", listOf(
                { JRow("定时任务、跟进和运行记录", subtitle = "到点自己开始做事、回来跟进答应你的事，做完推送到手机",
                    leadingIcon = PhIcons.Clock, onClick = { open(SettingsPart.Proactive) }) },
            ))
            item("proactive-caption") { Caption("在对话里说「每天 9 点给我早报」「一小时后提醒我」，Persona 会把它们加到这里。") }

            item("data") { JSectionHeader("数据存在哪里") }
            rows("data", listOf(
                { JRow("会话记录和记忆", value = machineName, leadingIcon = PhIcons.ChatCircle) },
                { JRow("关于我、定时任务和设置", value = "hub", leadingIcon = PhIcons.SlidersHorizontal) },
                { JRow("知识库", value = "hub 所在的机器", leadingIcon = PhIcons.BookOpen) },
            ))
            item("data-caption") { Caption("会话记录和记忆跟着 Persona 所在的机器：换一台机器，它们留在原来那台上，不会跟过去。") }

            item("you") { JSectionHeader("了解你") }
            val memoryCount = state.memory.entries?.size
            rows("you", listOf(
                { JRow("关于我", value = when {
                    !config.notes.isNullOrBlank() -> "已填写"
                    config.notes == null && config.notesEffective.isNotBlank() -> "hub 配置"
                    else -> "未填写"
                }, leadingIcon = PhIcons.UserCircle, enabled = ready, onClick = { open(SettingsPart.Notes) }) },
                { JRow("记忆", value = when {
                    memoryCount != null -> if (memoryCount == 0) "没有" else "$memoryCount 条"
                    state.memory.failed -> "读不到"
                    else -> null
                }, leadingIcon = PhIcons.Brain, onClick = { open(SettingsPart.Memory) }) },
            ))
            item("you-caption") {
                Caption("「关于我」是你告诉 Persona 的，存在 hub；「记忆」是它在对话里自己记下的，存在「$machineName」上。都可以改。")
            }

            item("zone") { JSectionHeader("时区") }
            val hubZone = config.timezone?.takeIf { it.isNotBlank() }
            val zoneRows = mutableListOf<@Composable () -> Unit>({
                JSwitchRow("跟随手机", state.followPhone, follow, subtitle = "手机现在是 ${zoneLabel(phoneZone)}",
                    leadingIcon = PhIcons.GlobeSimple, enabled = !state.busy)
            })
            if (!state.followPhone) zoneRows += {
                JRow("时区", value = hubZone ?: "未设置", subtitle = hubZone?.let { zone -> zoneName(zone) ?: zoneOffset(zone) },
                    leadingIcon = PhIcons.Clock, enabled = ready, onClick = { sheet(SettingsSheet.Zone) })
            }
            rows("zone", zoneRows)
            if (state.followPhone && hubZone != null && hubZone != phoneZone) item("zone-behind") {
                JNotice("hub 现在还按 ${zoneLabel(hubZone)} 算时间，下拉刷新会再同步一次。", Modifier.padding(top = Tokens.Space.m))
            }
            item("zone-caption") { Caption("定时任务按这个时区的时刻运行，Persona 也按它理解「今天」「明早」。") }

            item("reach") { JSectionHeader("能访问") }
            rows("reach", listOf(
                { JRow("各台电脑上的 Agent", subtitle = "看状态、读对话、发消息、开新的", leadingIcon = PhIcons.Desktop) },
                { JRow("飞书", subtitle = "消息、日历、文档、会议纪要、任务、邮件", leadingIcon = PhIcons.ChatCircle) },
                { JRow("邮箱", subtitle = "你添加的所有邮箱", leadingIcon = PhIcons.EnvelopeSimple) },
                { JRow("手机", subtitle = "在手机上问你、给你推送", leadingIcon = PhIcons.DeviceMobile) },
            ))
            item("reach-caption") { Caption("发飞书消息、发邮件、新建日程这类会替你出面的操作，都先在手机上问你。") }
        }
    }
}

@Composable
private fun NotesPart(state: AssistantSettingsUiState, onBack: () -> Unit, onSave: suspend (String) -> Boolean) {
    val config = state.config
    val scope = rememberCoroutineScope()
    var text by rememberSaveable { mutableStateOf<String?>(null) }
    var saved by rememberSaveable { mutableStateOf<String?>(null) }
    var leaving by remember { mutableStateOf(false) }
    LaunchedEffect(config) {
        if (text == null && config != null) config.notesShown().let { text = it; saved = it }
    }
    val changed = text != null && text != saved
    fun save(then: () -> Unit = {}) {
        val value = text ?: return
        scope.launch { if (onSave(value)) { saved = value; then() } }
    }
    val back = { if (changed) leaving = true else onBack() }
    BackHandler(changed) { leaving = true }
    JListPage(title = "关于我", onBack = back, bottomBar = {
        Row(Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
            JButton("保存", { save() }, Modifier.fillMaxWidth(), enabled = changed && !state.busy, loading = state.busy)
        }
    }) {
        item("intro") {
            Caption("告诉 Persona 你的情况：代码放在哪、常用哪些工具查工作数据、哪些机器或目录别碰、你习惯的汇报方式。它每次开新对话都会读到，改了以后新对话生效。")
        }
        if (config != null && config.notes == null && config.notesEffective.isNotBlank()) item("from-hub") {
            JNotice("现在 Persona 读到的是 hub 配置文件里写的内容。在这里保存以后，以这里为准。", Modifier.padding(top = Tokens.Space.m))
        }
        state.notice?.takeIf { it.error }?.let { notice -> item("notice") { JNotice(notice.text, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou) } }
        item("text") {
            JTextField(text.orEmpty(), { text = it }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = "关于我",
                placeholder = "比如：代码在 ~/code 下；周报每周五下班前给我草稿。", enabled = text != null && !state.busy,
                minLines = 12, maxLines = Int.MAX_VALUE)
        }
    }
    if (leaving) JDialog(onDismiss = { leaving = false }, title = "关于我还没保存",
        text = "离开前要保存修改吗？", confirmEnabled = !state.busy,
        confirm = "保存" to { leaving = false; save(onBack) },
        dismiss = "不保存" to { leaving = false; onBack() })
}

@Composable
private fun MemoryPart(
    state: AssistantSettingsUiState, machine: Machine?, machineName: String, onBack: () -> Unit,
    onRefresh: () -> Unit, onOpen: (String) -> Unit,
) {
    val memory = state.memory
    val entries = memory.entries.orEmpty()
    JListPage(title = "记忆", onBack = onBack, refreshing = memory.loading && memory.entries != null, onRefresh = onRefresh) {
        item("intro") {
            Caption("Persona 记住的事，一条一个文件，存在「$machineName」上，新对话开始时会读。说错的地方可以直接改，或者在对话里告诉它。")
        }
        if (memory.failed) item("failed") {
            JNotice(if (machine != null && !machine.online) "「$machineName」现在离线，连上以后才能看记忆。" else "读不到记忆，请稍后重试。",
                Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou, actionLabel = "重试", onAction = onRefresh)
        }
        if (memory.entries == null && memory.loading) item("loading") { JLoading(text = "正在读取") }
        if (memory.entries != null && entries.isEmpty() && !memory.failed) item("empty") {
            JEmptyState("还没有记忆", "Persona 会把你的偏好、纠正过它的地方记在这里。", icon = PhIcons.Brain)
        }
        if (entries.isNotEmpty()) item("entries") { JSectionHeader("记住的事", count = entries.size) }
        itemsIndexed(entries, key = { _, entry -> entry.file.name }) { index, entry ->
            JGroupItem(GroupPosition.of(index, entries.size)) {
                JRow(entry.title, subtitle = entry.description ?: entry.body.lineSequence().firstOrNull { it.isNotBlank() }?.trim(),
                    leadingIcon = PhIcons.Brain, trailing = entry.typeLabel?.let { label -> @Composable { JTag(label) } },
                    chevron = true, onClick = { onOpen(entry.file.name) })
            }
        }
        memory.index?.takeIf { entries.isNotEmpty() }?.let { index ->
            item("index") { JSectionHeader("索引") }
            item("index-row") {
                JGroupItem(GroupPosition.Single) {
                    JRow(index.name, subtitle = "Persona 自己维护的目录，删除记忆时会一起更新", leadingIcon = PhIcons.ListChecks,
                        onClick = { onOpen(index.name) })
                }
            }
        }
    }
}

@Composable
private fun MemoryFilePart(
    state: AssistantSettingsUiState, name: String?, preview: String?, onBack: () -> Unit,
    onSave: suspend (String, String) -> Boolean, onDelete: suspend (String) -> Unit,
) {
    val scope = rememberCoroutineScope()
    val entry = state.memory.entries?.firstOrNull { it.file.name == name }
        ?: state.memory.index?.takeIf { it.name == name }?.let { parseMemory(it).copy(title = "索引") }
    // The index is Persona's own table of contents: shown, not edited by hand.
    val editable = entry != null && entry.file.name != MEMORY_INDEX
    var editing by remember { mutableStateOf(preview == "memory-edit") }
    var deleting by remember { mutableStateOf(preview == "memory-delete") }
    JListPage(
        title = entry?.title ?: "记忆", onBack = onBack,
        actions = {
            if (editable) {
                JIconButton(PhIcons.PencilSimple, "编辑", { editing = true }, enabled = !state.busy)
                JIconButton(PhIcons.Trash, "删除", { deleting = true }, enabled = !state.busy, tint = PersonaTheme.palette.danger.text)
            }
        },
    ) {
        if (entry == null) {
            item("missing") { JEmptyState("这条记忆不在了", "可能已经被删除，或者 Persona 改了它的名字。", icon = PhIcons.Brain) }
            return@JListPage
        }
        item("meta") {
            Row(Modifier.padding(top = Tokens.Space.s, bottom = Tokens.Space.s), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
                verticalAlignment = Alignment.CenterVertically) {
                entry.typeLabel?.let { JTag(it) }
                conversationTime(entry.updated)?.let { Text("更新于 $it", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3) }
            }
        }
        entry.description?.let { description ->
            item("description") { Text(description, Modifier.padding(bottom = Tokens.Space.m), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2) }
        }
        item("body") {
            JCard {
                SelectionContainer {
                    Text(entry.body.ifBlank { "（空）" }, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink)
                }
            }
        }
        item("file") { Caption(entry.file.name) }
    }
    if (editing && entry != null) {
        var content by rememberSaveable(entry.file.name) { mutableStateOf(entry.file.content) }
        JSheet(onDismiss = { if (!state.busy) editing = false }, title = "编辑「${entry.title}」", dismissible = !state.busy) {
            LazyColumn(Modifier.fillMaxWidth().weight(1f, fill = false), contentPadding = PaddingValues(bottom = Tokens.Space.l),
                verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                item { Caption("开头两行 --- 之间是 Persona 用的标题、说明和类型，可以一起改。") }
                state.notice?.takeIf { it.error }?.let { notice -> item { JNotice(notice.text, status = Status.NeedsYou) } }
                item {
                    JTextField(content, { content = it }, Modifier.fillMaxWidth(), label = "记忆内容", enabled = !state.busy,
                        minLines = 8, maxLines = Int.MAX_VALUE)
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JButton("取消", { editing = false }, Modifier.weight(1f), kind = JButtonKind.Secondary, enabled = !state.busy)
                JButton("保存", { scope.launch { if (onSave(entry.file.name, content)) editing = false } }, Modifier.weight(1f),
                    enabled = !state.busy && content != entry.file.content, loading = state.busy)
            }
        }
    }
    if (deleting && entry != null) JDialog(
        onDismiss = { deleting = false }, title = "删掉这条记忆？",
        text = "Persona 以后不再记得「${entry.title}」。索引（$MEMORY_INDEX）里对应的那一行也会去掉。",
        destructive = true, confirmEnabled = !state.busy,
        confirm = "删除" to { deleting = false; scope.launch { onDelete(entry.file.name) }; Unit },
        dismiss = "取消" to { deleting = false },
    )
}

@Composable
private fun MachineSheet(machines: List<Machine>, current: String?, onDismiss: () -> Unit, onPick: (Machine) -> Unit) {
    val list = machines.distinctBy { it.id }
    JSheet(onDismiss = onDismiss, title = "在哪台机器上运行") {
        Text("记忆和会话记录在原来那台机器上，不会跟过去。", style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
        Spacer(Modifier.height(Tokens.Space.m))
        LazyColumn(Modifier.fillMaxWidth().weight(1f, fill = false), contentPadding = PaddingValues(bottom = Tokens.Space.l)) {
            if (list.isEmpty()) item { JEmptyState("还没有机器", "hub 接入电脑以后会出现在这里。", icon = PhIcons.Desktop) }
            itemsIndexed(list, key = { _, machine -> machine.id }) { index, machine ->
                JGroupItem(GroupPosition.of(index, list.size)) {
                    JRow(machine.displayName, subtitle = machineState(machine),
                        leading = { JRadio(machine.id == current, null) },
                        trailing = { JStatusDot(if (machine.online) Status.Unread else Status.Offline) },
                        chevron = false, onClick = { if (machine.id == current) onDismiss() else onPick(machine) })
                }
            }
        }
    }
}

@Composable
private fun ZoneSheet(current: String?, phoneZone: String, onDismiss: () -> Unit, onPick: (String) -> Unit) {
    var query by remember { mutableStateOf("") }
    val zones = remember { pickableZones() }
    // The zone in use and the phone's own come first; the full list (by offset) follows.
    val pinned = remember(current, phoneZone) { listOfNotNull(current?.takeIf { it.isNotBlank() }, phoneZone.takeIf { it.isNotBlank() }).distinct() }
    val shown = remember(query, pinned) { zones.filter { zoneMatches(it, query) && (query.isNotBlank() || it !in pinned) } }
    JSheet(onDismiss = onDismiss, title = "时区") {
        JSearchField(query, { query = it }, "搜索城市、时区或 UTC+8")
        Spacer(Modifier.height(Tokens.Space.m))
        LazyColumn(Modifier.fillMaxWidth().weight(1f, fill = false), contentPadding = PaddingValues(bottom = Tokens.Space.l)) {
            if (query.isBlank() && pinned.isNotEmpty()) {
                itemsIndexed(pinned, key = { _, zone -> "pinned:$zone" }) { index, zone ->
                    JGroupItem(GroupPosition.of(index, pinned.size)) { ZoneRow(zone, current, phoneZone, onPick) }
                }
                item("all") { JSectionHeader("全部时区") }
            }
            if (shown.isEmpty()) item { JEmptyState("没有找到", "换个城市名或 UTC 偏移试试。", icon = PhIcons.GlobeSimple) }
            itemsIndexed(shown, key = { _, zone -> zone }) { index, zone ->
                JGroupItem(GroupPosition.of(index, shown.size)) { ZoneRow(zone, current, phoneZone, onPick) }
            }
        }
    }
}

@Composable
private fun ZoneRow(zone: String, current: String?, phoneZone: String, onPick: (String) -> Unit) {
    JRow(zone, subtitle = listOfNotNull(zoneOffset(zone), zoneName(zone), "手机的时区".takeIf { zone == phoneZone }).joinToString("，").ifBlank { null },
        leading = { JRadio(zone == current, null) }, chevron = false, onClick = { onPick(zone) })
}

/** What 关于我 opens with: what the user saved in the app, else what the assistant is told from hub.json. */
internal fun AssistantConfig.notesShown(): String = notes ?: notesEffective
