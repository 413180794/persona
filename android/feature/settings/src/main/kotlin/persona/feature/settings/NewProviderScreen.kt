package persona.feature.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import android.content.res.Resources
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
    val res = LocalResources.current
    var choosingModel by remember { mutableStateOf(false) }
    LaunchedEffect(model) { model.load(preferPreset) }
    BackHandler(onBack = onBack)
    val draft = state.draft
    val editable = !state.busy
    JListPage(title = stringResource(R.string.settings_src_provider_new), onBack = onBack, largeTitle = false, bottomBar = {
        Row(Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s)) {
            JButton(stringResource(R.string.settings_src_save), { scope.launch { model.save()?.let { id -> feedback?.notice(res.getString(R.string.settings_src_provider_added, draft.name.trim())); onSaved(id) } } },
                Modifier.fillMaxWidth(), loading = state.busy, enabled = state.canSave)
        }
    }) {
        if (!state.loaded) item("loading") { JLoading() }
        if (state.presets.isNotEmpty()) item("source") {
            JSegmented(listOf(JSegment(stringResource(R.string.settings_src_provider_manual)), JSegment(stringResource(R.string.settings_src_provider_from_extension))), if (draft.fromPreset) 1 else 0,
                { model.edit(draft.copy(fromPreset = it == 1)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.s))
        }
        if (state.loaded) {
            if (state.usesPreset) presetForm(res, state, editable, model::edit, model::pick, onModel = { choosingModel = true })
            else manualForm(state, editable, model::edit)
        }
        state.error?.let { error -> item("error") { JInlineStatus(error.text(), Modifier.padding(top = Tokens.Space.m), tone = InlineTone.Failed) } }
    }
    val preset = state.selected
    if (choosingModel && preset != null) JActionSheet(preset.choices.map { choice ->
        JAction(choice.label, checked = choice.id == draft.presetModel, onClick = { model.edit(draft.copy(presetModel = choice.id)) })
    }, { choosingModel = false }, title = stringResource(R.string.settings_src_provider_default_model),
        note = preset.modelsError?.let { stringResource(R.string.settings_src_provider_models_error, it) })
}

private fun androidx.compose.foundation.lazy.LazyListScope.nameField(state: NewProviderState, editable: Boolean, onEdit: (NewProviderDraft) -> Unit, example: String) {
    item("name") {
        JTextField(state.draft.name, { onEdit(state.draft.copy(name = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = stringResource(R.string.settings_src_provider_name),
            placeholder = stringResource(R.string.settings_src_provider_name_example, example), singleLine = true, enabled = editable, isError = state.nameProblem != null,
            supportingText = state.nameProblem?.text() ?: stringResource(R.string.settings_src_provider_name_hint))
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.keyField(state: NewProviderState, editable: Boolean, onEdit: (NewProviderDraft) -> Unit, hint: String?) {
    item("key") {
        JTextField(state.draft.apiKey, { onEdit(state.draft.copy(apiKey = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = "API Key",
            singleLine = true, enabled = editable, visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            supportingText = listOfNotNull(hint, stringResource(R.string.settings_src_provider_key_hint)).joinToString("\n"))
    }
}

/** 从扩展添加: the presets to pick from, the name, the key and the model. */
private fun androidx.compose.foundation.lazy.LazyListScope.presetForm(
    res: Resources, state: NewProviderState, editable: Boolean, onEdit: (NewProviderDraft) -> Unit, onPick: (String) -> Unit, onModel: () -> Unit,
) {
    val preset = state.selected ?: return
    item("presets-title") { JSectionHeader(stringResource(R.string.settings_src_provider_presets)) }
    itemsIndexed(state.presets, key = { _, it -> "preset-${it.id}" }) { index, p ->
        JGroupItem(GroupPosition.of(index, state.presets.size)) {
            JRow(p.name.ifBlank { p.id }, subtitle = listOfNotNull(p.description?.takeIf { it.isNotBlank() }, presetSubtitle(res, p)).joinToString("\n"),
                subtitleLines = 3, enabled = editable, selected = p.id == preset.id, onClick = { onPick(p.id) },
                trailing = if (p.id == preset.id) ({ JIcon(PhIcons.Check, stringResource(R.string.settings_src_provider_selected), tint = PersonaTheme.palette.accent) }) else ({}))
        }
    }
    item("presets-note") {
        SettingsCaption(stringResource(R.string.settings_src_provider_preset_note, preset.extension),
            Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
    }
    nameField(state, editable, onEdit, "jv_team")
    keyField(state, editable, onEdit, preset.keyHint?.takeIf { it.isNotBlank() })
    item("preset-model") {
        if (preset.choices.isEmpty()) {
            JTextField(state.draft.presetModel, { onEdit(state.draft.copy(presetModel = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m),
                label = stringResource(R.string.settings_src_provider_default_model), singleLine = true, enabled = editable)
        } else {
            val picked = preset.choices.firstOrNull { it.id == state.draft.presetModel }
            JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
                JRow(stringResource(R.string.settings_src_provider_default_model), value = picked?.label ?: state.draft.presetModel.ifEmpty { stringResource(R.string.settings_src_provider_extension_default) }, enabled = editable, onClick = onModel)
            }
        }
    }
    item("preset-model-note") {
        SettingsCaption(preset.modelsError?.takeIf { it.isNotBlank() }?.let { stringResource(R.string.settings_src_provider_models_error, it) }
            ?: stringResource(R.string.settings_src_provider_model_note),
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
            JSegmented(listOf(JSegment(stringResource(R.string.settings_src_provider_kind_launcher)), JSegment(stringResource(R.string.settings_src_provider_kind_api))), if (api) 1 else 0,
                { if (editable) onEdit(draft.copy(kind = if (it == 1) "api" else "launcher")) }, Modifier.fillMaxWidth())
            if (api) JSegmented(listOf(JSegment("Messages"), JSegment("Responses")), if (draft.api == "responses") 1 else 0,
                { if (editable) onEdit(draft.copy(api = if (it == 1) "responses" else "messages")) }, Modifier.fillMaxWidth())
            SettingsCaption(stringResource(if (api) R.string.settings_src_provider_api_note else R.string.settings_src_provider_launcher_note),
                Modifier.padding(horizontal = rowPadding))
        }
    }
    item("base-url") {
        JTextField(draft.baseUrl, { onEdit(draft.copy(baseUrl = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = "Base URL",
            placeholder = "https://…", singleLine = true, enabled = editable, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            supportingText = stringResource(R.string.settings_src_provider_base_url_hint))
    }
    keyField(state, editable, onEdit, null)
    item("model") {
        JTextField(draft.model, { onEdit(draft.copy(model = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = stringResource(R.string.settings_src_provider_default_model),
            singleLine = true, enabled = editable, supportingText = stringResource(R.string.settings_src_provider_model_note))
    }
    if (!api) {
        item("small-model") {
            JTextField(draft.smallModel, { onEdit(draft.copy(smallModel = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m),
                label = stringResource(R.string.settings_src_provider_small_model), placeholder = stringResource(R.string.settings_src_provider_same_as_above), singleLine = true, enabled = editable)
        }
        item("subagent-model") {
            JTextField(draft.subagentModel, { onEdit(draft.copy(subagentModel = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m),
                label = stringResource(R.string.settings_src_provider_subagent_model), placeholder = stringResource(R.string.settings_src_provider_same_as_above), singleLine = true, enabled = editable,
                supportingText = stringResource(R.string.settings_src_provider_subagent_hint))
        }
        item("args") {
            JTextField(draft.args, { onEdit(draft.copy(args = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = stringResource(R.string.settings_src_provider_args),
                placeholder = stringResource(R.string.settings_src_provider_args_example), singleLine = true, enabled = editable,
                supportingText = stringResource(R.string.settings_src_provider_args_hint))
        }
    }
}
