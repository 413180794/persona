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
import persona.core.contracts.api.HubExtension
import persona.core.contracts.api.ExtensionDetail
import persona.core.contracts.api.ExtensionField
import persona.core.contracts.api.ExtensionManagement
import persona.core.contracts.api.ExtensionSecret
import persona.core.contracts.api.ExtensionUiPlace
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.LocalDetailKey
import persona.core.navigation.ExtensionMarketRoute
import persona.core.navigation.ExtensionRoute
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull

/**
 * 设置 › 扩展 (docs/extensions.md「App：设置 › 扩展」): every extension of the hub's (built in, from hub.json, installed on
 * the hub's machine), its state, and the way to its page. Extensions are installed on the hub's machine only.
 */
@Composable
internal fun ExtensionsPage(extensions: ExtensionManagement?, onBack: () -> Unit, onExtension: (String) -> Unit, onMarket: (() -> Unit)? = null) {
    if (extensions == null) {
        JListPage(title = "扩展", onBack = onBack) { item { JNotice("体验模式没有 hub 的扩展。") } }
        return
    }
    val model = remember(extensions) { ExtensionListModel(extensions) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    LaunchedEffect(model) { model.load() }
    val list = state.extensions
    val detail = LocalDetailKey.current
    JListPage(title = "扩展", onBack = onBack, refreshing = state.loading && list != null, onRefresh = { scope.launch { model.load() } }) {
        state.error?.let { error ->
            item("error") {
                JNotice(error, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou,
                    actionLabel = "重试", onAction = { scope.launch { model.load() } })
            }
        }
        // 扩展市场 only when the hub has markets (docs/extensions.md「扩展市场」): the one way a phone installs an extension.
        if (state.markets && onMarket != null) item("market") {
            JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
                JRow("扩展市场", subtitle = "浏览、安装和升级市场里的扩展", leadingIcon = PhIcons.Cube,
                    selected = detail == ExtensionMarketRoute, onClick = onMarket)
            }
        }
        if (list == null && state.loading) item("loading") { JLoading() }
        if (list != null && list.isEmpty()) item("empty") {
            JEmptyState("没有扩展", "扩展在 hub 所在的机器上用 personad extension install 安装，装好后在这里开关和设置。", icon = PhIcons.PuzzlePiece)
        }
        if (!list.isNullOrEmpty()) {
            item("top") { JSectionHeader("这台 hub 的扩展", count = list.size) }
            itemsIndexed(list, key = { _, it -> "plugin-${it.id}" }) { index, extension ->
                JGroupItem(GroupPosition.of(index, list.size)) {
                    JRow(extension.name.ifBlank { extension.id }, subtitle = ExtensionText.state(extension), leadingIcon = PhIcons.PuzzlePiece,
                        trailing = { ExtensionStateMark(extension) }, chevron = true,
                        selected = detail == ExtensionRoute(extension.id), onClick = { onExtension(extension.id) })
                }
            }
            item("footer") {
                SettingsCaption(if (state.markets && onMarket != null) "扩展市场里的扩展可以在这里安装、升级和卸载；别的扩展在 hub 所在的机器上（personad extension）装。这里可以开关、改设置、填密钥。"
                    else "扩展只能在 hub 所在的机器上安装、升级和卸载（personad extension），这里可以开关、改设置、填密钥。",
                    Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.m))
            }
        }
    }
}

/** The mark beside an extension's state: the style's status dot, or a warning in the danger colour when it is broken. */
@Composable
private fun ExtensionStateMark(extension: HubExtension?) {
    when {
        extension == null -> JStatusDot(Status.Unknown)
        ExtensionText.isProblem(extension) -> JIcon(PhIcons.WarningCircle, ExtensionText.state(extension), tint = PersonaTheme.palette.danger.dot)
        else -> JStatusDot(ExtensionText.status(extension))
    }
}

/**
 * 设置 › 扩展 › one: what it is and how it is doing, its switch, its settings as a form drawn from its schema, its
 * secrets (set here, never shown again), what it contributes and what it may do. An extension written in hub.json is only
 * looked at here.
 */
@Composable
fun ExtensionPage(
    extensions: ExtensionManagement?, extensionId: String, onBack: () -> Unit, onPage: ((ExtensionUiPlace) -> Unit)? = null,
    /** Opens one of the channels it contributes (设置 › 渠道); without it the line is left out. */
    onChannel: ((String) -> Unit)? = null,
) {
    if (extensions == null) {
        JListPage(title = "扩展", onBack = onBack) { item { JNotice("体验模式没有 hub 的扩展。") } }
        return
    }
    val model = remember(extensions, extensionId) { ExtensionModel(extensions, extensionId) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    var editingSecret by remember { mutableStateOf<ExtensionSecret?>(null) }
    var choosing by remember { mutableStateOf<Pair<ExtensionField, List<String>>?>(null) }
    LaunchedEffect(model) { model.load() }
    val detail = state.detail
    val hasForm = detail != null && !detail.readOnly && detail.form.isNotEmpty()
    JListPage(title = detail?.extension?.name?.ifBlank { null } ?: "扩展", onBack = onBack, largeTitle = false,
        refreshing = state.loading && detail != null, onRefresh = { scope.launch { model.load() } },
        bottomBar = if (!hasForm) null else ({
            Row(Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s)) {
                JButton("保存", { scope.launch { if (model.save()) feedback?.notice("已保存，扩展会重新启动") } }, Modifier.fillMaxWidth(),
                    loading = state.saving, enabled = state.canSave)
            }
        })) {
        header(extensionId, state, onRetry = { scope.launch { model.load() } })
        if (detail != null && onChannel != null) channelLink(detail, onChannel)
        if (detail != null) {
            if (detail.readOnly) item("read-only") {
                JNotice("这个扩展写在 hub.json 里：开关和设置在 hub 所在的机器上改。", Modifier.padding(top = Tokens.Space.m))
            } else {
                item("enabled") {
                    JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
                        JSwitchRow("启用", detail.extension.enabled ?: true, { on -> scope.launch { model.setEnabled(on) } }, enabled = !state.busy)
                    }
                }
                item("enabled-note") { Note("停用后它不再运行，提供的连接器等也一起关掉；设置和密钥留着。") }
            }
            if (onPage != null) extensionPages(detail.ui, onPage)
            if (hasForm) form(detail, state, model, onChoose = { field, path -> choosing = field to path })
            if (!detail.readOnly && detail.secrets.isNotEmpty()) secrets(detail, state, onEdit = { editingSecret = it })
            contributions(detail, state.busy || state.saving) { item, on -> scope.launch { model.setContribution(item, on) } }
            sandbox(detail.extension.sandbox)
            permissions(detail)
            about(detail)
        }
    }
    choosing?.let { (field, path) ->
        val current = state.value(path)
        JActionSheet(listOf(JAction(ExtensionText.placeholder(field), checked = current == null, onClick = { model.edit(path, null) })) +
            field.enum.orEmpty().map { choice ->
                JAction(choice.label, checked = current == choice.value, onClick = { model.edit(path, choice.value) })
            }, { choosing = null }, title = ExtensionText.fieldTitle(field))
    }
    editingSecret?.let { secret ->
        ExtensionSecretSheet(secret, busy = state.busy, onDismiss = { editingSecret = null },
            save = { value -> model.setSecret(secret.name, value) }, clear = { model.clearSecret(secret.name) })
    }
}

@Composable
private fun Note(text: String) = SettingsCaption(text, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))

private fun LazyListScope.header(id: String, state: ExtensionState, onRetry: () -> Unit) {
    val extension = state.detail?.extension
    item("header") {
        JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.s)) {
            JRow(extension?.name?.ifBlank { null } ?: id, subtitle = extension?.let(ExtensionText::stateLine) ?: if (state.loading) "正在读取…" else "读不到",
                leadingIcon = PhIcons.PuzzlePiece, trailing = { if (extension == null && state.loading) JSpinner() else ExtensionStateMark(extension) })
        }
    }
    extension?.description?.takeIf { it.isNotBlank() }?.let { item("description") { Note(it) } }
    extension?.let(ExtensionText::runNote)?.let { item("run-note") { Note(it) } }
    extension?.needsSetup?.takeIf { it.isNotEmpty() }?.let { needs ->
        item("needs") { JInlineStatus("还缺：" + needs.joinToString("、"), Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Caution) }
    }
    extension?.error?.takeIf { it.isNotBlank() }?.let { error ->
        item("plugin-error") { JInlineStatus(error, Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed) }
    }
    state.error?.let { error ->
        item("error") {
            Column(Modifier.padding(top = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JInlineStatus(error, tone = InlineTone.Failed)
                if (extension == null) JButton("重试", onRetry, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, enabled = !state.loading)
            }
        }
    }
}

/**
 * An extension that is a way of talking to Persona (it contributes a `channel`, docs/channels.md「界面」) says where that is
 * set up: binding, its switch and where its messages go are on the channel's page; this page keeps working as before.
 */
private fun LazyListScope.channelLink(detail: ExtensionDetail, onChannel: (String) -> Unit) {
    val id = detail.extension.contributes.orEmpty()["channel"]?.firstOrNull { it.isNotBlank() } ?: return
    item("channel-link") {
        JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
            JRow("这是一个对话渠道", subtitle = "在 设置 › 渠道 里设置", leadingIcon = PhIcons.ChatsCircle, onClick = { onChannel(id) })
        }
    }
}

private fun LazyListScope.form(detail: ExtensionDetail, state: ExtensionState, model: ExtensionModel, onChoose: (ExtensionField, List<String>) -> Unit) {
    val plain = detail.form.filter { it.type != "object" }
    if (plain.isNotEmpty()) {
        item("form-title") { JSectionHeader("设置") }
        plain.forEach { field -> item("field-${field.name}") { ExtensionFieldBlock(field, listOf(field.name), state, model, onChoose) } }
    }
    item("form-note") { Note("改完点下面的「保存」；保存后扩展会重新启动。留空的项用扩展的默认值。") }
    detail.form.filter { it.type == "object" }.forEach { group ->
        item("group-${group.name}") { JSectionHeader(group.label) }
        group.fields.orEmpty().forEach { field ->
            item("field-${group.name}.${field.name}") { ExtensionFieldBlock(field, listOf(group.name, field.name), state, model, onChoose) }
        }
        group.description?.takeIf { it.isNotBlank() }?.let { item("group-note-${group.name}") { Note(it) } }
    }
}

/**
 * One field of an extension's form, by its type: a text field, a number, a switch, a choice, a list of texts or of
 * choices. Its description and the hub's word on it below.
 */
@Composable
private fun ExtensionFieldBlock(field: ExtensionField, path: List<String>, state: ExtensionState, model: ExtensionModel, onChoose: (ExtensionField, List<String>) -> Unit) {
    val title = ExtensionText.fieldTitle(field)
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
            field.type == "array" -> ExtensionListField(field, title, path, state, model, enabled)
            field.enum != null -> {
                val current = state.value(path)
                val label = field.enum.orEmpty().firstOrNull { it.value == current }?.label
                    ?: (current as? JsonPrimitive)?.contentOrNull ?: ExtensionText.placeholder(field)
                JGroupItem(GroupPosition.Single) {
                    JRow(title, subtitle = description, value = label, enabled = enabled, onClick = { onChoose(field, path) })
                }
            }
            field.type == "integer" || field.type == "number" -> {
                val key = path.joinToString(".")
                val text = state.numberText[key] ?: (state.value(path) as? JsonPrimitive)?.contentOrNull.orEmpty()
                JTextField(text, { model.editNumber(path, it, field.type == "integer") }, Modifier.fillMaxWidth(), label = title,
                    placeholder = ExtensionText.placeholder(field), supportingText = error ?: description, singleLine = true, enabled = enabled,
                    isError = error != null,
                    keyboardOptions = KeyboardOptions(keyboardType = if (field.type == "integer") KeyboardType.Number else KeyboardType.Decimal))
            }
            else -> {
                if (field.name == "triage_rules" && path.size == 1) TriageTemplatePicker(enabled) { model.edit(path, JsonPrimitive(it)) }
                val text = (state.value(path) as? JsonPrimitive)?.contentOrNull.orEmpty()
                JTextField(text, { model.edit(path, it.takeIf { v -> v.isNotEmpty() }?.let(::JsonPrimitive)) }, Modifier.fillMaxWidth(),
                    label = title, placeholder = ExtensionText.placeholder(field), supportingText = error ?: description, singleLine = field.name != "triage_rules",
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
private fun ExtensionListField(field: ExtensionField, title: String, path: List<String>, state: ExtensionState, model: ExtensionModel, enabled: Boolean) {
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

private fun LazyListScope.secrets(detail: ExtensionDetail, state: ExtensionState, onEdit: (ExtensionSecret) -> Unit) {
    item("secrets-title") { JSectionHeader("密钥") }
    itemsIndexed(detail.secrets, key = { _, it -> "secret-${it.name}" }) { index, secret ->
        JGroupItem(GroupPosition.of(index, detail.secrets.size)) {
            JRow(secret.label, leadingIcon = PhIcons.Key, value = ExtensionText.secretState(secret), enabled = !state.busy && !state.saving,
                onClick = { onEdit(secret) })
        }
    }
    item("secrets-note") { Note("密钥加密存在 hub 上，只交给这个扩展；存好后不再显示，只能换新的或清除。") }
}

private fun LazyListScope.contributions(
    detail: ExtensionDetail, busy: Boolean,
    change: (persona.core.contracts.api.ExtensionContribution, Boolean) -> Unit,
) {
    detail.contributions?.takeIf { it.isNotEmpty() }?.let { list ->
        item("contributes-title") { JSectionHeader("提供") }
        itemsIndexed(list, key = { _, it -> "point-${it.point}/${it.id}" }) { index, contribution ->
            JGroupItem(GroupPosition.of(index, list.size)) {
                if (contribution.canDisable && !detail.readOnly) JSwitchRow(
                    contribution.title?.ifBlank { null } ?: contribution.id, contribution.enabled,
                    { change(contribution, it) }, subtitle = ExtensionText.point(contribution.point), enabled = !busy,
                ) else JRow(contribution.title?.ifBlank { null } ?: contribution.id,
                    subtitle = ExtensionText.point(contribution.point) + when (contribution.via) {
                        "connector" -> " · 跟随连接器开关，在连接器设置里修改"
                        "config" -> " · 跟随扩展设置里的开关"
                        else -> " · 只读"
                    },
                    value = if (contribution.enabled) "启用" else "停用")
            }
        }
        item("contributes-note") { Note("关掉一项不会减少扩展的权限；其余贡献照常，修改后扩展会重新启动。") }
        return
    }
    val points = detail.extension.contributes.orEmpty().filterValues { it.isNotEmpty() }.toSortedMap().toList()
    if (points.isEmpty()) return
    item("contributes-title") { JSectionHeader("提供") }
    itemsIndexed(points, key = { _, it -> "point-${it.first}" }) { index, (point, ids) ->
        JGroupItem(GroupPosition.of(index, points.size)) { JRow(ExtensionText.point(point), value = ids.joinToString("、")) }
    }
}

private fun LazyListScope.permissions(detail: ExtensionDetail) {
    if (detail.permissions.isEmpty()) return
    item("permissions-title") { JSectionHeader("它可以") }
    itemsIndexed(detail.permissions, key = { _, it -> "permission-${it.key}" }) { index, permission ->
        JGroupItem(GroupPosition.of(index, detail.permissions.size)) {
            JRow(permission.title, subtitle = permission.detail?.takeIf { it.isNotBlank() }, subtitleLines = 4)
        }
    }
    item("permissions-note") { Note("安装时你同意过这些；新版本要得更多时，升级前会再问一次。") }
}

private fun LazyListScope.about(detail: ExtensionDetail) {
    val extension = detail.extension
    val rows = listOfNotNull(
        extension.version?.takeIf { it.isNotBlank() }?.let { "版本" to it },
        detail.previousVersion?.takeIf { it.isNotBlank() }?.let { "上一个版本" to it },
        ExtensionText.source(extension.source)?.let { "来源" to it },
        detail.publisher?.takeIf { it.isNotBlank() }?.let { "发布者" to it },
    )
    val origin = detail.origin?.takeIf { it.isNotBlank() }
    val homepage = detail.homepage?.takeIf { it.startsWith("https://") || it.startsWith("http://") }
    val count = rows.size + listOfNotNull(origin, homepage).size
    if (count == 0 && extension.warnings.isNullOrEmpty()) return
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
    extension.warnings.orEmpty().forEachIndexed { index, warning ->
        item("warning-$index") { JInlineStatus(warning, Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Caution) }
    }
}

/** Sets one of an extension's secrets: typed in hidden, saved to the hub, never shown again; one that is set can be cleared. */
@Composable
private fun ExtensionSecretSheet(
    secret: ExtensionSecret, busy: Boolean, onDismiss: () -> Unit,
    save: suspend (String) -> Boolean, clear: suspend () -> Boolean,
) {
    var value by remember(secret.name) { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    JSheet(onDismiss, title = secret.label, dismissible = !busy) {
        Column(Modifier.padding(vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            JTextField(value, { value = it }, Modifier.fillMaxWidth(), placeholder = if (secret.set) "输入新的值替换" else "输入", singleLine = true,
                enabled = !busy, visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password))
            SettingsCaption(secret.description?.takeIf { it.isNotBlank() } ?: "存在 hub 上，只交给这个扩展。")
            JButton("保存", { scope.launch { if (save(value)) onDismiss() } }, Modifier.fillMaxWidth(), loading = busy,
                enabled = !busy && value.isNotBlank())
            if (secret.set) JButton("清除", { scope.launch { if (clear()) onDismiss() } }, Modifier.fillMaxWidth(),
                kind = JButtonKind.Destructive, enabled = !busy)
        }
    }
}
