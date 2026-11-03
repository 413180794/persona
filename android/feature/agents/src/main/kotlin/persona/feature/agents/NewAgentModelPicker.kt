package persona.feature.agents

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
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

/** The list behind the 模型 row (the design system's [JModelSheet]): only for this start. */
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
    JModelSheet(
        note = "「${state.modelLauncher}」里 ${list.agent.ifBlank { state.agentType }.agentLabel()} 的模型，和在终端里输入 /model 看到的一样。只用于这次启动。",
        models = list.models, selected = state.model, defaultDetail = defaultModelDetail(state),
        loading = state.models.loading, error = state.models.error,
        onPick = onPick, onRefresh = onRefresh, onDismiss = onDismiss, initialQuery = initialQuery,
    )
}

/** A model's second line (the design system's [detailLine]). */
internal fun modelDetail(model: AgentModel): String? = model.detailLine()

/** What runs with no pick: 模型配置's model, else the one the agent defaults to. */
internal fun defaultModelDetail(state: NewAgentState): String? {
    if (state.configuredModel.isNotEmpty()) return "模型配置：${state.configuredModel}"
    val default = state.modelList?.models?.firstOrNull { it.isDefault } ?: return null
    return default.resolved?.takeIf { it.isNotBlank() } ?: default.label
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
