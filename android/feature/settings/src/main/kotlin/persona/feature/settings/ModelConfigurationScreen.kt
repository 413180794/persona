package persona.feature.settings

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import persona.core.contracts.api.ModelConfiguration
import persona.core.contracts.api.ModelPurpose
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.LocalDetailKey
import persona.core.navigation.ModelPurposeRoute
import kotlinx.coroutines.launch

/**
 * 设置 › 模型配置 (docs/model-config.md): each AI use of the hub with what is in effect for it. The hub owns the
 * catalog; a row opens that use's page.
 */
@Composable
internal fun ModelConfigurationPage(models: ModelConfiguration?, onBack: () -> Unit, onPurpose: (String) -> Unit) {
    if (models == null) {
        JListPage(title = "模型配置", onBack = onBack) { item { JNotice("体验模式没有 hub 的模型配置。") } }
        return
    }
    val model = remember(models) { ModelCatalogModel(models) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    var query by remember { mutableStateOf("") }
    LaunchedEffect(model) { model.load() }
    val catalog = state.catalog
    val groups = catalog?.let { modelPurposeGroups(it.purposes, it.providers, query) }.orEmpty()
    val detail = LocalDetailKey.current
    JListPage(title = "模型配置", onBack = onBack, refreshing = state.loading && catalog != null, onRefresh = { scope.launch { model.load() } },
        actions = { JIconButton(PhIcons.ArrowClockwise, "刷新", { scope.launch { model.load() } }, enabled = !state.loading) }) {
        item("search") {
            Column(Modifier.padding(top = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JSearchField(query, { query = it }, "搜索用途或模型")
                SettingsCaption("各功能下一次运行使用这里的配置，不打断正在工作的任务。连接与凭据在模型供应商中管理。",
                    Modifier.padding(horizontal = rowPadding))
            }
        }
        when {
            state.unsupported -> item("unsupported") {
                JNotice("这个 hub 还不支持统一模型配置，请先更新服务器。", Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou)
            }
            state.error != null -> item("error") {
                JNotice(state.error.orEmpty(), Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou,
                    actionLabel = "重试", onAction = { scope.launch { model.load() } })
            }
        }
        if (catalog == null && state.loading) item("loading") { JLoading() }
        if (catalog != null && groups.isEmpty()) item("empty") {
            JEmptyState(if (query.isBlank()) "hub 没有列出用途" else "没有匹配的用途", "试试其他名称或模型。", icon = PhIcons.MagnifyingGlass)
        }
        groups.forEach { (group, purposes) ->
            item("group-$group") { JSectionHeader(group, count = purposes.size) }
            itemsIndexed(purposes, key = { _, it -> "purpose-${it.id}" }) { index, purpose ->
                JGroupItem(GroupPosition.of(index, purposes.size)) {
                    JRow(purpose.displayName, subtitle = purposeSubtitle(purpose, requireNotNull(catalog)), subtitleLines = 3,
                        selected = detail == ModelPurposeRoute(purpose.id), onClick = { onPurpose(purpose.id) })
                }
            }
        }
    }
}

/** One AI use: what is in effect, and its own provider, model and effort (blank: inherit). */
@Composable
fun ModelPurposePage(models: ModelConfiguration?, purposeId: String, onBack: () -> Unit) {
    if (models == null) {
        JListPage(title = "模型配置", onBack = onBack) { item { JNotice("体验模式没有 hub 的模型配置。") } }
        return
    }
    val model = remember(models, purposeId) { ModelPurposeModel(models, purposeId) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    var choosingProvider by remember { mutableStateOf(false) }
    LaunchedEffect(model) { model.load() }
    fun save(restore: Boolean) {
        scope.launch { model.save(restore)?.let { notice -> feedback?.notice(notice); onBack() } }
    }
    val purpose = state.purpose
    JListPage(title = purpose?.displayName ?: "模型配置", onBack = onBack, largeTitle = false, bottomBar = if (purpose == null) null else ({
        Row(Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s)) {
            JButton("保存", { save(false) }, Modifier.fillMaxWidth(), loading = state.busy, enabled = !state.busy && !state.conflict)
        }
    })) {
        if (purpose == null && state.busy) item("loading") { JLoading() }
        if (purpose != null) {
            effective(purpose, state)
            form(purpose, state, onProvider = { choosingProvider = true }, onEdit = model::edit, onRestore = { save(true) })
        }
        state.error?.let { error ->
            item("error") {
                Column(Modifier.padding(top = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    JInlineStatus(error, tone = InlineTone.Failed)
                    if (state.conflict || purpose == null) JButton(if (state.conflict) "重新加载（丢弃编辑）" else "重试",
                        { scope.launch { model.load() } }, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, enabled = !state.busy)
                }
            }
        }
    }
    if (choosingProvider && purpose != null) JActionSheet(providerOptions(purpose, state.detail?.providers.orEmpty(), state.draft.provider).map { option ->
        JAction(option.label, checked = option.id == state.draft.provider, onClick = { model.edit(state.draft.choosingProvider(option.id)) })
    }, { choosingProvider = false }, title = "模型供应商")
}

private fun androidx.compose.foundation.lazy.LazyListScope.effective(purpose: ModelPurpose, state: ModelPurposeState) {
    val providers = state.detail?.providers.orEmpty()
    val rows = listOfNotNull(
        "供应商" to (purpose.effective.provider?.let { id -> providers.firstOrNull { it.id == id }?.displayName ?: id } ?: "未配置"),
        "模型" to (purpose.effective.model?.takeIf { it.isNotBlank() } ?: "供应商默认"),
        purpose.effective.effort?.takeIf { it.isNotBlank() }?.let { "思考强度" to it },
        purpose.status?.takeIf { it.isNotBlank() }?.let { "状态" to it },
    )
    item("effective-title") { JSectionHeader("当前生效") }
    itemsIndexed(rows, key = { _, it -> "effective-${it.first}" }) { index, (label, value) ->
        JGroupItem(GroupPosition.of(index, rows.size)) { JRow(label, value = value) }
    }
    purpose.inheritedFrom?.takeIf { it.isNotBlank() }?.let { from ->
        item("effective-note") { SettingsCaption("继承来源：$from", Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s)) }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.form(
    purpose: ModelPurpose, state: ModelPurposeState, onProvider: () -> Unit,
    onEdit: (persona.core.contracts.api.ModelConfigurationDraft) -> Unit, onRestore: () -> Unit,
) {
    val draft = state.draft
    val editable = !state.busy
    item("form-title") { JSectionHeader("用途配置") }
    if (purpose.supports("provider")) item("provider") {
        val label = providerOptions(purpose, state.detail?.providers.orEmpty(), draft.provider).firstOrNull { it.id == draft.provider }?.label
        JGroupItem(GroupPosition.Single) { JRow("模型供应商", value = label ?: "跟随默认", enabled = editable, onClick = onProvider) }
    }
    if (purpose.supports("model")) item("model") {
        JTextField(draft.model, { onEdit(draft.copy(model = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = "具体模型",
            placeholder = (if (purpose.kind == "plugin") "跟随插件默认" else null) ?: state.provider?.model ?: purpose.effective.model ?: "跟随默认",
            singleLine = true, enabled = editable)
    }
    if (showsEffort(purpose, state.provider)) item("effort") {
        JTextField(draft.effort, { onEdit(draft.copy(effort = it)) }, Modifier.fillMaxWidth().padding(top = Tokens.Space.m), label = "思考强度",
            placeholder = purpose.effective.effort ?: "跟随默认", singleLine = true, enabled = editable)
    }
    item("form-note") {
        SettingsCaption("留空恢复继承。保存只修改配置，不发起模型请求；会话的临时选择仍可覆盖默认值。",
            Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.m))
    }
    if (purpose.kind == "plugin") item("plugin") { JNotice("只选择这个插件提供的模型；不修改插件自己的供应商或凭据。", title = "插件模型") }
    item("restore") {
        JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
            JRow("恢复继承", leadingIcon = PhIcons.ArrowUUpLeft, enabled = editable && !state.conflict, chevron = false, onClick = onRestore)
        }
    }
}
