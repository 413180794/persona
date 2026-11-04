package persona.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import persona.core.contracts.api.HubPlugin
import persona.core.contracts.api.PluginDetail
import persona.core.contracts.api.PluginField
import persona.core.contracts.api.PluginManagement
import persona.core.contracts.api.PluginSecret
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.LocalDetailKey
import persona.core.navigation.PluginRoute
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull

/**
 * 设置 › 插件 (docs/plugins.md「App：设置 › 插件」): every plugin of the hub's (built in, from hub.json, installed on
 * the hub's machine), its state, and the way to its page. Plugins are installed on the hub's machine only.
 */
@Composable
internal fun PluginsPage(plugins: PluginManagement?, onBack: () -> Unit, onPlugin: (String) -> Unit) {
    if (plugins == null) {
        JListPage(title = "插件", onBack = onBack) { item { JNotice("体验模式没有 hub 的插件。") } }
        return
    }
    val model = remember(plugins) { PluginListModel(plugins) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    LaunchedEffect(model) { model.load() }
    val list = state.plugins
    val detail = LocalDetailKey.current
    JListPage(title = "插件", onBack = onBack, refreshing = state.loading && list != null, onRefresh = { scope.launch { model.load() } }) {
        state.error?.let { error ->
            item("error") {
                JNotice(error, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou,
                    actionLabel = "重试", onAction = { scope.launch { model.load() } })
            }
        }
        if (list == null && state.loading) item("loading") { JLoading() }
        if (list != null && list.isEmpty()) item("empty") {
            JEmptyState("没有插件", "插件在 hub 所在的机器上用 personad plugin install 安装，装好后在这里开关和设置。", icon = PhIcons.PuzzlePiece)
        }
        if (!list.isNullOrEmpty()) {
            item("top") { JSectionHeader("这台 hub 的插件", count = list.size) }
            itemsIndexed(list, key = { _, it -> "plugin-${it.id}" }) { index, plugin ->
                JGroupItem(GroupPosition.of(index, list.size)) {
                    JRow(plugin.name.ifBlank { plugin.id }, subtitle = PluginText.state(plugin), leadingIcon = PhIcons.PuzzlePiece,
                        trailing = { PluginStateMark(plugin) }, chevron = true,
                        selected = detail == PluginRoute(plugin.id), onClick = { onPlugin(plugin.id) })
                }
            }
            item("footer") {
                SettingsCaption("插件只能在 hub 所在的机器上安装、升级和卸载（personad plugin），这里可以开关、改设置、填密钥。",
                    Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.m))
            }
        }
    }
}

/** The mark beside a plugin's state: the style's status dot, or a warning in the danger colour when it is broken. */
@Composable
private fun PluginStateMark(plugin: HubPlugin?) {
    when {
        plugin == null -> JStatusDot(Status.Unknown)
        PluginText.isProblem(plugin) -> JIcon(PhIcons.WarningCircle, PluginText.state(plugin), tint = PersonaTheme.palette.danger.dot)
        else -> JStatusDot(PluginText.status(plugin))
    }
}

/**
 * 设置 › 插件 › one: what it is and how it is doing, its switch, its settings as a form drawn from its schema, its
 * secrets (set here, never shown again), what it contributes and what it may do. A plugin written in hub.json is only
 * looked at here.
 */
@Composable
fun PluginPage(plugins: PluginManagement?, pluginId: String, onBack: () -> Unit) {
    if (plugins == null) {
        JListPage(title = "插件", onBack = onBack) { item { JNotice("体验模式没有 hub 的插件。") } }
        return
    }
    val model = remember(plugins, pluginId) { PluginModel(plugins, pluginId) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    var editingSecret by remember { mutableStateOf<PluginSecret?>(null) }
    var choosing by remember { mutableStateOf<Pair<PluginField, List<String>>?>(null) }
    LaunchedEffect(model) { model.load() }
    val detail = state.detail
    val hasForm = detail != null && !detail.readOnly && detail.form.isNotEmpty()
    JListPage(title = detail?.plugin?.name?.ifBlank { null } ?: "插件", onBack = onBack, largeTitle = false,
        refreshing = state.loading && detail != null, onRefresh = { scope.launch { model.load() } },
        bottomBar = if (!hasForm) null else ({
            Row(Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s)) {
                JButton("保存", { scope.launch { if (model.save()) feedback?.notice("已保存，插件会重新启动") } }, Modifier.fillMaxWidth(),
                    loading = state.saving, enabled = state.canSave)
            }
        })) {
        header(pluginId, state, onRetry = { scope.launch { model.load() } })
        if (detail != null) {
            if (detail.readOnly) item("read-only") {
                JNotice("这个插件写在 hub.json 里：开关和设置在 hub 所在的机器上改。", Modifier.padding(top = Tokens.Space.m))
            } else {
                item("enabled") {
                    JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
                        JSwitchRow("启用", detail.plugin.enabled ?: true, { on -> scope.launch { model.setEnabled(on) } }, enabled = !state.busy)
                    }
                }
                item("enabled-note") { Note("停用后它不再运行，提供的连接器等也一起关掉；设置和密钥留着。") }
            }
            if (hasForm) form(detail, state, model, onChoose = { field, path -> choosing = field to path })
            if (!detail.readOnly && detail.secrets.isNotEmpty()) secrets(detail, state, onEdit = { editingSecret = it })
            contributions(detail)
            permissions(detail)
            about(detail)
        }
    }
    choosing?.let { (field, path) ->
        val current = state.value(path)
        JActionSheet(listOf(JAction(PluginText.placeholder(field), checked = current == null, onClick = { model.edit(path, null) })) +
            field.enum.orEmpty().map { choice ->
                JAction(choice.label, checked = current == choice.value, onClick = { model.edit(path, choice.value) })
            }, { choosing = null }, title = PluginText.fieldTitle(field))
    }
    editingSecret?.let { secret ->
        PluginSecretSheet(secret, busy = state.busy, onDismiss = { editingSecret = null },
            save = { value -> model.setSecret(secret.name, value) }, clear = { model.clearSecret(secret.name) })
    }
}

@Composable
private fun Note(text: String) = SettingsCaption(text, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))

private fun LazyListScope.header(id: String, state: PluginState, onRetry: () -> Unit) {
    val plugin = state.detail?.plugin
    item("header") {
        JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.s)) {
            JRow(plugin?.name?.ifBlank { null } ?: id, subtitle = plugin?.let(PluginText::stateLine) ?: if (state.loading) "正在读取…" else "读不到",
                leadingIcon = PhIcons.PuzzlePiece, trailing = { if (plugin == null && state.loading) JSpinner() else PluginStateMark(plugin) })
        }
    }
    plugin?.description?.takeIf { it.isNotBlank() }?.let { item("description") { Note(it) } }
    plugin?.needsSetup?.takeIf { it.isNotEmpty() }?.let { needs ->
        item("needs") { JInlineStatus("还缺：" + needs.joinToString("、"), Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Caution) }
    }
    plugin?.error?.takeIf { it.isNotBlank() }?.let { error ->
        item("plugin-error") { JInlineStatus(error, Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed) }
    }
    state.error?.let { error ->
        item("error") {
            Column(Modifier.padding(top = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JInlineStatus(error, tone = InlineTone.Failed)
                if (plugin == null) JButton("重试", onRetry, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, enabled = !state.loading)
            }
        }
    }
}

private fun LazyListScope.form(detail: PluginDetail, state: PluginState, model: PluginModel, onChoose: (PluginField, List<String>) -> Unit) {
    val plain = detail.form.filter { it.type != "object" }
    if (plain.isNotEmpty()) {
        item("form-title") { JSectionHeader("设置") }
        plain.forEach { field -> item("field-${field.name}") { PluginFieldBlock(field, listOf(field.name), state, model, onChoose) } }
    }
    item("form-note") { Note("改完点下面的「保存」；保存后插件会重新启动。留空的项用插件的默认值。") }
    detail.form.filter { it.type == "object" }.forEach { group ->
        item("group-${group.name}") { JSectionHeader(group.label) }
        group.fields.orEmpty().forEach { field ->
            item("field-${group.name}.${field.name}") { PluginFieldBlock(field, listOf(group.name, field.name), state, model, onChoose) }
        }
        group.description?.takeIf { it.isNotBlank() }?.let { item("group-note-${group.name}") { Note(it) } }
    }
}

/**
 * One field of a plugin's form, by its type: a text field, a number, a switch, a choice, a list of texts or of
 * choices. Its description and the hub's word on it below.
 */
@Composable
private fun PluginFieldBlock(field: PluginField, path: List<String>, state: PluginState, model: PluginModel, onChoose: (PluginField, List<String>) -> Unit) {
    val title = PluginText.fieldTitle(field)
    val error = state.fieldError(path)
    val enabled = !state.saving
    val description = field.description?.takeIf { it.isNotBlank() }
    Column(Modifier.padding(top = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        when {
            field.type == "boolean" -> {
                val on = (state.value(path) as? JsonPrimitive)?.booleanOrNull ?: (field.fallback as? JsonPrimitive)?.booleanOrNull ?: false
                JGroupItem(GroupPosition.Single) {
                    JSwitchRow(title, on, { model.edit(path, JsonPrimitive(it)) }, subtitle = description, enabled = enabled)
                }
            }
            field.type == "array" -> PluginListField(field, title, path, state, model, enabled)
            field.enum != null -> {
                val current = state.value(path)
                val label = field.enum.orEmpty().firstOrNull { it.value == current }?.label
                    ?: (current as? JsonPrimitive)?.contentOrNull ?: PluginText.placeholder(field)
                JGroupItem(GroupPosition.Single) {
                    JRow(title, subtitle = description, value = label, enabled = enabled, onClick = { onChoose(field, path) })
                }
            }
            field.type == "integer" || field.type == "number" -> {
                val key = path.joinToString(".")
                val text = state.numberText[key] ?: (state.value(path) as? JsonPrimitive)?.contentOrNull.orEmpty()
                JTextField(text, { model.editNumber(path, it, field.type == "integer") }, Modifier.fillMaxWidth(), label = title,
                    placeholder = PluginText.placeholder(field), supportingText = error ?: description, singleLine = true, enabled = enabled,
                    isError = error != null,
                    keyboardOptions = KeyboardOptions(keyboardType = if (field.type == "integer") KeyboardType.Number else KeyboardType.Decimal))
            }
            else -> {
                val text = (state.value(path) as? JsonPrimitive)?.contentOrNull.orEmpty()
                JTextField(text, { model.edit(path, it.takeIf { v -> v.isNotEmpty() }?.let(::JsonPrimitive)) }, Modifier.fillMaxWidth(),
                    label = title, placeholder = PluginText.placeholder(field), supportingText = error ?: description, singleLine = true,
                    enabled = enabled, isError = error != null,
                    keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None,
                        keyboardType = when (field.format) { "uri" -> KeyboardType.Uri; "email" -> KeyboardType.Email; else -> KeyboardType.Text }))
            }
        }
        // Text and number fields carry their description and error themselves (supportingText); a switch or a choice
        // has its description in its row, a list below it.
        val textField = field.type != "boolean" && field.type != "array" && field.enum == null
        if (field.type == "array") description?.takeIf { error == null }?.let { FieldSupport(it, isError = false) }
        if (!textField) error?.let { FieldSupport(it, isError = true) }
    }
}

/** A field's title above its control, set like [JTextField]'s label. */
@Composable
private fun FieldLabel(text: String) = Text(text, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2, maxLines = 2)

/** The line under a field, set like [JTextField]'s supporting text. */
@Composable
private fun FieldSupport(text: String, isError: Boolean) =
    Text(text, style = PersonaTheme.type.caption, color = if (isError) PersonaTheme.palette.danger.text else PersonaTheme.palette.ink3)

/** A list of texts (each removable, more added below), or of choices (a switch each). */
@Composable
private fun PluginListField(field: PluginField, title: String, path: List<String>, state: PluginState, model: PluginModel, enabled: Boolean) {
    var adding by remember(path) { mutableStateOf("") }
    val items = ((state.value(path) ?: field.fallback) as? JsonArray).orEmpty().mapNotNull { (it as? JsonPrimitive)?.contentOrNull }
    fun store(list: List<String>) = model.edit(path, list.takeIf { it.isNotEmpty() }?.let { l -> JsonArray(l.map(::JsonPrimitive)) })
    fun add() {
        val value = adding.trim()
        if (value.isEmpty()) return
        store(items + value)
        adding = ""
    }
    FieldLabel(title)
    val choices = field.items?.enum
    if (choices != null) {
        Column {
            choices.forEachIndexed { index, choice ->
                val value = (choice.value as? JsonPrimitive)?.contentOrNull.orEmpty()
                JGroupItem(GroupPosition.of(index, choices.size)) {
                    JSwitchRow(choice.label, value in items, { on -> store(items.filter { it != value } + listOfNotNull(value.takeIf { on })) }, enabled = enabled)
                }
            }
        }
        return
    }
    if (items.isNotEmpty()) Column {
        items.forEachIndexed { index, item ->
            JGroupItem(GroupPosition.of(index, items.size)) {
                JRow(item, trailing = {
                    JIconButton(PhIcons.X, "删除 $item", { store(items.filterIndexed { i, _ -> i != index }) }, enabled = enabled, raised = false)
                })
            }
        }
    }
    JTextField(adding, { adding = it }, Modifier.fillMaxWidth(), placeholder = "添加一项", singleLine = true, enabled = enabled,
        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, imeAction = ImeAction.Done),
        keyboardActions = KeyboardActions(onDone = { add() }),
        trailing = { JIconButton(PhIcons.Plus, "添加", ::add, enabled = enabled && adding.isNotBlank(), raised = false) })
}

private fun LazyListScope.secrets(detail: PluginDetail, state: PluginState, onEdit: (PluginSecret) -> Unit) {
    item("secrets-title") { JSectionHeader("密钥") }
    itemsIndexed(detail.secrets, key = { _, it -> "secret-${it.name}" }) { index, secret ->
        JGroupItem(GroupPosition.of(index, detail.secrets.size)) {
            JRow(secret.label, leadingIcon = PhIcons.Key, value = PluginText.secretState(secret), enabled = !state.busy && !state.saving,
                onClick = { onEdit(secret) })
        }
    }
    item("secrets-note") { Note("密钥加密存在 hub 上，只交给这个插件；存好后不再显示，只能换新的或清除。") }
}

private fun LazyListScope.contributions(detail: PluginDetail) {
    val points = detail.plugin.contributes.orEmpty().filterValues { it.isNotEmpty() }.toSortedMap().toList()
    if (points.isEmpty()) return
    item("contributes-title") { JSectionHeader("提供") }
    itemsIndexed(points, key = { _, it -> "point-${it.first}" }) { index, (point, ids) ->
        JGroupItem(GroupPosition.of(index, points.size)) { JRow(PluginText.point(point), value = ids.joinToString("、")) }
    }
}

private fun LazyListScope.permissions(detail: PluginDetail) {
    if (detail.permissions.isEmpty()) return
    item("permissions-title") { JSectionHeader("它可以") }
    itemsIndexed(detail.permissions, key = { _, it -> "permission-${it.key}" }) { index, permission ->
        JGroupItem(GroupPosition.of(index, detail.permissions.size)) {
            JRow(permission.title, subtitle = permission.detail?.takeIf { it.isNotBlank() }, subtitleLines = 4)
        }
    }
    item("permissions-note") { Note("安装时你同意过这些；新版本要得更多时，升级前会再问一次。") }
}

private fun LazyListScope.about(detail: PluginDetail) {
    val plugin = detail.plugin
    val rows = listOfNotNull(
        plugin.version?.takeIf { it.isNotBlank() }?.let { "版本" to it },
        detail.previousVersion?.takeIf { it.isNotBlank() }?.let { "上一个版本" to it },
        PluginText.source(plugin.source)?.let { "来源" to it },
        detail.publisher?.takeIf { it.isNotBlank() }?.let { "发布者" to it },
    )
    val origin = detail.origin?.takeIf { it.isNotBlank() }
    val homepage = detail.homepage?.takeIf { it.startsWith("https://") || it.startsWith("http://") }
    val count = rows.size + listOfNotNull(origin, homepage).size
    if (count == 0 && plugin.warnings.isNullOrEmpty()) return
    item("about-title") { JSectionHeader("关于") }
    rows.forEachIndexed { index, (label, value) ->
        item("about-$label") { JGroupItem(GroupPosition.of(index, count)) { JRow(label, value = value) } }
    }
    // Where it was installed from is long (a path or an address): it goes under its label, not beside it.
    origin?.let { item("about-origin") { JGroupItem(GroupPosition.of(rows.size, count)) { JRow("安装自", subtitle = it, subtitleLines = 3) } } }
    homepage?.let { url ->
        item("about-homepage") {
            val uri = LocalUriHandler.current
            JGroupItem(GroupPosition.of(count - 1, count)) {
                JRow("主页", leadingIcon = PhIcons.ArrowSquareOut, onClick = { runCatching { uri.openUri(url) } })
            }
        }
    }
    plugin.warnings.orEmpty().forEachIndexed { index, warning ->
        item("warning-$index") { JInlineStatus(warning, Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Caution) }
    }
}

/** Sets one of a plugin's secrets: typed in hidden, saved to the hub, never shown again; one that is set can be cleared. */
@Composable
private fun PluginSecretSheet(
    secret: PluginSecret, busy: Boolean, onDismiss: () -> Unit,
    save: suspend (String) -> Boolean, clear: suspend () -> Boolean,
) {
    var value by remember(secret.name) { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    JSheet(onDismiss, title = secret.label, dismissible = !busy) {
        Column(Modifier.padding(vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            JTextField(value, { value = it }, Modifier.fillMaxWidth(), placeholder = if (secret.set) "输入新的值替换" else "输入", singleLine = true,
                enabled = !busy, visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password))
            SettingsCaption(secret.description?.takeIf { it.isNotBlank() } ?: "存在 hub 上，只交给这个插件。")
            JButton("保存", { scope.launch { if (save(value)) onDismiss() } }, Modifier.fillMaxWidth(), loading = busy,
                enabled = !busy && value.isNotBlank())
            if (secret.set) JButton("清除", { scope.launch { if (clear()) onDismiss() } }, Modifier.fillMaxWidth(),
                kind = JButtonKind.Destructive, enabled = !busy)
        }
    }
}
