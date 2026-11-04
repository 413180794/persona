package persona.feature.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import persona.core.contracts.api.ProviderSetup
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.launch

/** Debug builds open 新增模型供应商 directly for captures (`debug.newProvider`: manual | preset); release never sets it. */
object NewProviderPreview {
    @Volatile var open: String? = null

    /** Taken once: the page it opens is shown, then the setting is gone. */
    fun take(): String? = open.also { open = null }
}

/**
 * 新增模型供应商 (docs/launchers.md「从扩展添加」): 自己填写 — the name, the kind (Claude Code 启动器 / 直接 API), the
 * gateway and key, the models and flags — or, when the hub's extensions offer presets, 从扩展添加: a preset, then only the
 * name, the key and maybe another of its models. [onSaved] gets the new provider's 模型配置 id.
 */
@Composable
internal fun NewProviderPage(providers: ProviderSetup, onBack: () -> Unit, onSaved: (String) -> Unit, preferPreset: Boolean = false) {
    val model = remember(providers) { NewProviderModel(providers) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    var choosingModel by remember { mutableStateOf(false) }
    LaunchedEffect(model) { model.load(preferPreset) }
    BackHandler(onBack = onBack)
    val draft = state.draft
    val editable = !state.busy
    JListPage(title = "新增模型供应商", onBack = onBack, largeTitle = false, bottomBar = {
        Row(Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s)) {
            JButton("保存", { scope.launch { model.save()?.let { id -> feedback?.notice("已新增 ${draft.name.trim()}"); onSaved(id) } } },
                Modifier.fillMaxWidth(), loading = state.busy, enabled = state.canSave)
        }
    }) {
        if (!state.loaded) item("loading") { JLoading() }
        if (state.presets.isNotEmpty()) item("source") {
            JSegmented(listOf(JSegment("自己填写"), JSegment("从扩展添加")), if (draft.fromPreset) 1 else 0,
                { model.edit(draft.copy(fromPreset = it == 1)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.s))
        }
        if (state.loaded) {
            if (state.usesPreset) presetForm(state, editable, model::edit, model::pick, onModel = { choosingModel = true })
            else manualForm(state, editable, model::edit)
        }
        state.error?.let { error -> item("error") { JInlineStatus(error, Modifier.padding(top = Tokens.Space.m), tone = InlineTone.Failed) } }
    }
    val preset = state.selected
    if (choosingModel && preset != null) JActionSheet(preset.choices.map { choice ->
        JAction(choice.label, checked = choice.id == draft.presetModel, onClick = { model.edit(draft.copy(presetModel = choice.id)) })
    }, { choosingModel = false }, title = "默认模型", note = preset.modelsError?.let { "读不到扩展的模型列表（$it），这里是它声明的。" })
}

private fun androidx.compose.foundation.lazy.LazyListScope.nameField(state: NewProviderState, editable: Boolean, onEdit: (NewProviderDraft) -> Unit, example: String) {
    item("name") {
        JTextField(state.draft.name, { onEdit(state.draft.copy(name = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = "名字",
            placeholder = "比如 $example", singleLine = true, enabled = editable, isError = state.nameProblem != null,
            supportingText = state.nameProblem ?: "字母、数字、_ 和 -；按这个名字选择供应商。")
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.keyField(state: NewProviderState, editable: Boolean, onEdit: (NewProviderDraft) -> Unit, hint: String?) {
    item("key") {
        JTextField(state.draft.apiKey, { onEdit(state.draft.copy(apiKey = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = "API Key",
            singleLine = true, enabled = editable, visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            supportingText = listOfNotNull(hint, "Key 加密存在 hub，不会返回到客户端。").joinToString("\n"))
    }
}

/** 从扩展添加: the presets to pick from, the name, the key and the model. */
private fun androidx.compose.foundation.lazy.LazyListScope.presetForm(
    state: NewProviderState, editable: Boolean, onEdit: (NewProviderDraft) -> Unit, onPick: (String) -> Unit, onModel: () -> Unit,
) {
    val preset = state.selected ?: return
    item("presets-title") { JSectionHeader("供应商") }
    itemsIndexed(state.presets, key = { _, it -> "preset-${it.id}" }) { index, p ->
        JGroupItem(GroupPosition.of(index, state.presets.size)) {
            JRow(p.name.ifBlank { p.id }, subtitle = listOfNotNull(p.description?.takeIf { it.isNotBlank() }, presetSubtitle(p)).joinToString("\n"),
                subtitleLines = 3, enabled = editable, selected = p.id == preset.id, onClick = { onPick(p.id) },
                trailing = if (p.id == preset.id) ({ JIcon(PhIcons.Check, "已选", tint = PersonaTheme.palette.accent) }) else ({}))
        }
    }
    item("presets-note") {
        SettingsCaption("地址、接入方式和模型由扩展「${preset.extension}」提供；加好以后和自己填写的一样可以编辑。",
            Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
    }
    nameField(state, editable, onEdit, "jv_team")
    keyField(state, editable, onEdit, preset.keyHint?.takeIf { it.isNotBlank() })
    item("preset-model") {
        if (preset.choices.isEmpty()) {
            JTextField(state.draft.presetModel, { onEdit(state.draft.copy(presetModel = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m),
                label = "默认模型", singleLine = true, enabled = editable)
        } else {
            val picked = preset.choices.firstOrNull { it.id == state.draft.presetModel }
            JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
                JRow("默认模型", value = picked?.label ?: state.draft.presetModel.ifEmpty { "扩展默认" }, enabled = editable, onClick = onModel)
            }
        }
    }
    item("preset-model-note") {
        SettingsCaption(preset.modelsError?.takeIf { it.isNotBlank() }?.let { "读不到扩展的模型列表（$it），这里是它声明的。" }
            ?: "各用途的具体选择在「模型配置」里统一管理；未单独指定时使用这里的默认。",
            Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
    }
}

/** 自己填写: the name, the kind, the gateway and key, the models, the flags. */
private fun androidx.compose.foundation.lazy.LazyListScope.manualForm(state: NewProviderState, editable: Boolean, onEdit: (NewProviderDraft) -> Unit) {
    val draft = state.draft
    val api = draft.kind == "api"
    nameField(state, editable, onEdit, "jv_strong")
    item("kind") {
        Column(Modifier.padding(top = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            JSegmented(listOf(JSegment("Claude Code 启动器"), JSegment("直接 API")), if (api) 1 else 0,
                { if (editable) onEdit(draft.copy(kind = if (it == 1) "api" else "launcher")) }, Modifier.fillMaxWidth())
            if (api) JSegmented(listOf(JSegment("Messages"), JSegment("Responses")), if (draft.api == "responses") 1 else 0,
                { if (editable) onEdit(draft.copy(api = if (it == 1) "responses" else "messages")) }, Modifier.fillMaxWidth())
            SettingsCaption(if (api) "hub 直接请求模型，供分拣、翻译和知识处理等用途选择。" else "在你的电脑上启动 Claude Code，供对话和 Agent 等用途选择。",
                Modifier.padding(horizontal = rowPadding))
        }
    }
    item("base-url") {
        JTextField(draft.baseUrl, { onEdit(draft.copy(baseUrl = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = "Base URL",
            placeholder = "https://…", singleLine = true, enabled = editable, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            supportingText = "填写所选协议兼容的地址。")
    }
    keyField(state, editable, onEdit, null)
    item("model") {
        JTextField(draft.model, { onEdit(draft.copy(model = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = "默认模型",
            singleLine = true, enabled = editable, supportingText = "各用途的具体选择在「模型配置」里统一管理；未单独指定时使用这里的默认。")
    }
    if (!api) {
        item("small-model") {
            JTextField(draft.smallModel, { onEdit(draft.copy(smallModel = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m),
                label = "小模型（可选）", placeholder = "默认同上", singleLine = true, enabled = editable)
        }
        item("subagent-model") {
            JTextField(draft.subagentModel, { onEdit(draft.copy(subagentModel = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m),
                label = "子 Agent 模型（可选）", placeholder = "默认同上", singleLine = true, enabled = editable,
                supportingText = "用于 workflow 等多 Agent 运行。")
        }
        item("args") {
            JTextField(draft.args, { onEdit(draft.copy(args = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = "额外参数（可选）",
                placeholder = "比如 --dangerously-skip-permissions", singleLine = true, enabled = editable,
                supportingText = "加在 claude 后面，空格分隔，每个都以 - 开头。")
        }
    }
}
