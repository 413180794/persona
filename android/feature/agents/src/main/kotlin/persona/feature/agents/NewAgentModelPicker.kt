package persona.feature.agents

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import persona.core.contracts.api.AgentModel
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/**
 * 模型: what the launcher that will run starts with, picked from the list its agent shows in /model (the sheet).
 * Nothing when that launcher has no list (TRAEX, a runtime's, older hubs); starting never waits for it.
 */
internal fun LazyListScope.newAgentModelItems(state: NewAgentState, editable: Boolean, onOpen: () -> Unit, onRetry: () -> Unit) {
    val models = state.models
    if (state.modelLauncher.isEmpty() || models.machineId != state.machineId || models.launcher != state.modelLauncher) return
    val list = state.modelList
    if (list == null && !models.loading && models.error == null) return
    item("model") {
        JSectionHeader("模型")
        JGroupItem(GroupPosition.Single) {
            when {
                list?.locked != null -> JRow("跟随启动器", enabled = false, chevron = false,
                    trailing = { JIcon(PhIcons.LockSimple, "不能选择", tint = PersonaTheme.palette.ink3) })
                list != null -> JRow(state.pickedModel?.label ?: "默认",
                    subtitle = state.pickedModel.let { picked -> if (picked != null) modelDetail(picked) else defaultModelDetail(state) },
                    enabled = editable, onClick = onOpen, trailing = if (models.loading) ({ JSpinner() }) else null)
                models.loading -> JRow("正在读取模型…", subtitle = "「${state.modelLauncher}」的模型列表", chevron = false, trailing = { JSpinner() })
                else -> JRow("读不到模型列表", subtitle = "${models.error}\n不影响开始，会用默认模型", subtitleLines = 4, chevron = false,
                    trailing = { JButton("重试", onRetry, enabled = editable, kind = JButtonKind.Secondary, size = JButtonSize.Small) })
            }
        }
        list?.locked?.let {
            Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2,
                modifier = Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
        }
    }
}

/**
 * The list behind the 模型 row: 默认 first, then every model the launcher's agent offers, searchable by name, id, the
 * model an alias stands for and description. Picking one closes it; 刷新 asks the CLI again.
 */
@Composable
internal fun NewAgentModelSheet(
    state: NewAgentState,
    onPick: (String) -> Unit,
    onRefresh: () -> Unit,
    onDismiss: () -> Unit,
    initialQuery: String = "",
) {
    val list = state.modelList
    if (list == null || list.locked != null) {
        LaunchedEffect(Unit) { onDismiss() }
        return
    }
    var query by remember { mutableStateOf(initialQuery) }
    val shown = remember(list, query) { list.models.filter { !it.isDefault && it.matches(query) } }
    // Results start from the top: the list would otherwise keep the row it was at, and 默认 coming back in front of
    // it once the query is cleared would stay above the viewport.
    val scroll = rememberLazyListState()
    LaunchedEffect(query) { scroll.scrollToItem(0) }
    JSheet(onDismiss = onDismiss, title = "模型") {
        Text("「${state.modelLauncher}」里 ${list.agent.ifBlank { state.agentType }.agentLabel()} 的模型，和在终端里输入 /model 看到的一样。只用于这次启动。",
            style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
        Spacer(Modifier.height(Tokens.Space.m))
        JSearchField(query, { query = it }, "搜索名称或 ID")
        Spacer(Modifier.height(Tokens.Space.m))
        LazyColumn(Modifier.fillMaxWidth().weight(1f), scroll, contentPadding = PaddingValues(bottom = Tokens.Space.l)) {
            state.models.error?.let { error -> item("error") { JInlineStatus("没有刷新：$error", tone = InlineTone.Failed) } }
            if (query.isBlank()) item("default") {
                JGroupItem(GroupPosition.Single) {
                    ModelChoice("默认", defaultModelDetail(state), null, state.model.isEmpty()) { onPick(""); onDismiss() }
                }
            }
            item("all") {
                JSectionHeader(if (query.isBlank()) "全部模型" else "搜索结果", count = shown.size, action = {
                    if (state.models.loading) JSpinner() else JTextButton("刷新", onRefresh, icon = PhIcons.ArrowsClockwise)
                })
            }
            if (shown.isEmpty()) item("none") {
                JEmptyState("没有找到", "换个名称或 ID 试试。", icon = PhIcons.MagnifyingGlass)
            }
            itemsIndexed(shown, key = { _, model -> model.id }) { index, model ->
                JGroupItem(GroupPosition.of(index, shown.size)) {
                    ModelChoice(model.label, modelDetail(model), model.description, model.id == state.model) { onPick(model.id); onDismiss() }
                }
            }
        }
    }
}

@Composable
private fun ModelChoice(title: String, detail: String?, description: String?, selected: Boolean, onClick: () -> Unit) {
    JRow(title, subtitle = listOfNotNull(detail, description?.takeIf { it.isNotBlank() }).joinToString("\n").ifEmpty { null },
        subtitleLines = 3, chevron = false, onClick = onClick,
        trailing = if (selected) ({ JIcon(PhIcons.Check, "已选", tint = PersonaTheme.palette.ink) }) else null)
}

/** A model's second line: its id when the name is another word for it, and the model an alias stands for. */
internal fun modelDetail(model: AgentModel): String? {
    val id = model.id.takeUnless { it.equals(model.label, ignoreCase = true) }
    val resolved = model.resolved?.takeIf { it.isNotBlank() && !it.equals(model.label, true) && it != model.id }
    return when {
        id != null && resolved != null -> "$id → $resolved"
        else -> id ?: resolved
    }
}

/** What runs with no pick: 模型配置's model, else the one the agent defaults to. */
internal fun defaultModelDetail(state: NewAgentState): String? {
    if (state.configuredModel.isNotEmpty()) return "模型配置：${state.configuredModel}"
    val default = state.modelList?.models?.firstOrNull { it.isDefault } ?: return null
    return default.resolved?.takeIf { it.isNotBlank() } ?: default.label
}

/** Whether [query] is in the model's name, id, resolved model or description. */
internal fun AgentModel.matches(query: String): Boolean {
    val q = query.trim()
    return q.isEmpty() || listOf(name, id, resolved.orEmpty(), description.orEmpty()).any { it.contains(q, ignoreCase = true) }
}

/** The thinking efforts offered: 跟随启动器, then the levels the model that will run takes (all when not known). */
internal fun effortChoices(efforts: List<String>?): List<Pair<String, String>> =
    effortLevels.filter { (id, _) -> id.isEmpty() || efforts == null || id in efforts }

/** For captures (debug builds): steps the 新建会话 page takes once loaded, see `NewAgentPreview.steps`. */
object NewAgentPreview {
    /**
     * Comma separated: `launcher:<name>` picks a launcher, `effort:<level>` an effort, `model:<id>` a model, and
     * `models` or `models:<query>` opens the model list (searching for the query).
     */
    @Volatile var steps: String? = null
}
