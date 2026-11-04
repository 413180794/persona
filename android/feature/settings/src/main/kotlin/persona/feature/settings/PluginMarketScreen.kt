package persona.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import persona.core.contracts.api.CatalogPlugin
import persona.core.contracts.api.PluginManagement
import persona.core.contracts.api.PluginMarket
import persona.core.contracts.api.PluginPermission
import persona.core.contracts.api.PluginPreview
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// 设置 › 插件 › 插件市场 (docs/plugins.md「插件市场」): the catalog, one plugin of it, and the markets. The models are
// in PluginMarketState.kt.

private const val SearchDelayMillis = 300L

/** The catalog: search, narrow to a market, each plugin's version, words, tags and how it stands here. */
@Composable
fun PluginMarketPage(plugins: PluginManagement?, onBack: () -> Unit, onPlugin: (CatalogPlugin) -> Unit, onMarkets: () -> Unit) {
    if (plugins == null) {
        JListPage(title = "插件市场", onBack = onBack) { item { JNotice("体验模式没有 hub 的插件。") } }
        return
    }
    val model = remember(plugins) { CatalogModel(plugins) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    // Typing waits a moment before asking the hub; the market filter asks at once.
    LaunchedEffect(model, state.query) { if (state.catalog != null) delay(SearchDelayMillis); model.load() }
    LaunchedEffect(model, state.market) { if (state.catalog != null) model.load() }
    val catalog = state.catalog
    JListPage(title = "插件市场", onBack = onBack, largeTitle = false,
        actions = { JIconButton(PhIcons.SlidersHorizontal, "管理市场", onMarkets) },
        refreshing = state.loading && catalog != null, onRefresh = { scope.launch { model.load(refresh = true) } }) {
        item("search") {
            JSearchField(state.query, model::setQuery, "搜索名字、说明、发布者、标签", Modifier.padding(top = Tokens.Space.s))
        }
        if (state.markets.size > 1) item("filter") { MarketFilter(state.markets, state.market, model::setMarket) }
        state.error?.let { error ->
            item("error") {
                JNotice(error, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou, actionLabel = "重试",
                    onAction = { scope.launch { model.load() } })
            }
        }
        marketProblems(state.markets.filter { state.market == null || it.id == state.market })
        when {
            catalog == null && state.loading -> item("loading") { JLoading() }
            catalog != null && catalog.markets.isEmpty() -> item("no-markets") {
                JEmptyState("还没有插件市场", "插件市场是一份插件的清单（一个网站、GitHub 仓库或 git 仓库）。添加一个市场后，就能在这里浏览、安装它提供的插件。",
                    icon = PhIcons.Cube, action = { JButton("添加市场", onMarkets, kind = JButtonKind.Secondary, size = JButtonSize.Small) })
            }
            catalog != null && catalog.plugins.isEmpty() -> item("empty") {
                JEmptyState(if (state.query.isBlank()) "这个市场没有插件" else "没有找到", if (state.query.isBlank()) "市场的清单里还没有插件。"
                    else "换个关键词试试；搜索会查名字、说明、发布者和标签。", icon = PhIcons.MagnifyingGlass)
            }
        }
        val list = catalog?.plugins.orEmpty()
        if (list.isNotEmpty()) {
            item("list-title") { JSectionHeader(if (state.query.isBlank()) "可以安装的插件" else "搜索结果", count = list.size) }
            itemsIndexed(list, key = { _, it -> "plugin-${it.market}-${it.id}" }) { index, plugin ->
                JGroupItem(GroupPosition.of(index, list.size)) {
                    JRow(plugin.label, subtitle = catalogSubtitle(plugin, showMarket = state.market == null && state.markets.size > 1), subtitleLines = 4,
                        leadingIcon = PhIcons.PuzzlePiece,
                        trailing = CatalogText.badge(plugin)?.let { badge -> { JTag(badge, standingTone(plugin)) } },
                        chevron = true, onClick = { onPlugin(plugin) })
                }
            }
            item("footer") {
                SettingsCaption("装之前会列出它能做什么，你同意了才装。插件在 hub 所在的机器上运行，不在沙箱里。",
                    Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.m))
            }
        }
    }
}

/** The lines under a catalog plugin: its words, how it stands (version, installed, why not), its tags and market. */
private fun catalogSubtitle(p: CatalogPlugin, showMarket: Boolean): String = listOfNotNull(
    p.description?.takeIf { it.isNotBlank() },
    CatalogText.line(p),
    listOfNotNull(p.tags.orEmpty().takeIf { it.isNotEmpty() }?.joinToString(" · ") { "#$it" },
        p.marketName?.takeIf { showMarket && it.isNotBlank() }).joinToString("　").takeIf { it.isNotBlank() },
).joinToString("\n")

internal fun standingTone(p: CatalogPlugin): Tone = when (CatalogText.standing(p)) {
    CatalogStanding.Upgradable -> Tone.Caution
    CatalogStanding.Installed, CatalogStanding.InstalledElsewhere -> Tone.Positive
    CatalogStanding.Unavailable -> Tone.Danger
    CatalogStanding.Available -> Tone.Neutral
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun MarketFilter(markets: List<PluginMarket>, selected: String?, onSelect: (String?) -> Unit) {
    FlowRow(Modifier.padding(top = Tokens.Space.m), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
        verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        JChip("全部", selected = selected == null, onClick = { onSelect(null) })
        markets.forEach { market -> JChip(market.label, selected = selected == market.id, onClick = { onSelect(market.id) }) }
    }
}

/** A market the hub could not read, or read with entries it dropped. */
private fun LazyListScope.marketProblems(markets: List<PluginMarket>) {
    markets.forEach { market ->
        market.error?.takeIf { it.isNotBlank() }?.let { error ->
            item("market-error-${market.id}") {
                JInlineStatus("${market.label}：$error", Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed)
            }
        }
    }
}

/**
 * One plugin of a market: what it is, what it may do, and the way to install, upgrade or remove it. Installing and
 * upgrading first fetch and check the package and list what it may do; nothing happens until the user agrees.
 */
@Composable
fun PluginCatalogPage(plugins: PluginManagement?, marketId: String, pluginId: String, onBack: () -> Unit, onInstalled: (String) -> Unit) {
    if (plugins == null) {
        JListPage(title = "插件", onBack = onBack) { item { JNotice("体验模式没有 hub 的插件。") } }
        return
    }
    val model = remember(plugins, marketId, pluginId) { CatalogPluginModel(plugins, marketId, pluginId) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    var removing by remember { mutableStateOf(false) }
    LaunchedEffect(model) { model.load() }
    val plugin = state.plugin
    val actions = plugin?.let(CatalogText::actions).orEmpty()
    JListPage(title = plugin?.label ?: "插件", onBack = onBack, largeTitle = false,
        refreshing = state.loading && plugin != null, onRefresh = { scope.launch { model.load() } },
        bottomBar = if (actions.isEmpty()) null else ({
            Column(Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                if (CatalogAction.Install in actions) JButton("安装", { scope.launch { model.preview() } }, Modifier.fillMaxWidth(),
                    loading = state.previewing, loadingText = "正在下载和检查…", enabled = !state.busy)
                if (CatalogAction.Upgrade in actions) JButton("升级到 ${plugin?.version.orEmpty()}", { scope.launch { model.preview() } }, Modifier.fillMaxWidth(),
                    loading = state.previewing, loadingText = "正在下载和检查…", enabled = !state.busy)
                if (CatalogAction.Uninstall in actions) JButton("卸载", { removing = true }, Modifier.fillMaxWidth(),
                    kind = JButtonKind.Destructive, size = if (actions.size > 1) JButtonSize.Small else JButtonSize.Large,
                    loading = state.acting && state.preview == null, enabled = !state.busy)
            }
        })) {
        when {
            plugin == null && state.loading -> item("loading") { JLoading() }
            plugin == null && state.error != null -> item("error") {
                JNotice(state.error!!, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou, actionLabel = "重试",
                    onAction = { scope.launch { model.load() } })
            }
            plugin == null && state.missing -> item("missing") {
                JEmptyState("市场里没有这个插件了", "市场的清单可能改了，或者这个市场已经删掉。", icon = PhIcons.Cube)
            }
        }
        if (plugin != null) catalogPluginBody(plugin, state, onOpenInstalled = { onInstalled(plugin.id) })
    }
    state.preview?.let { preview ->
        PreviewSheet(plugin, preview, busy = state.acting, onDismiss = model::dismissPreview,
            onConfirm = { scope.launch { model.confirm()?.let { feedback?.notice(it) } } })
    }
    if (removing && plugin != null) UninstallDialog(plugin, onDismiss = { removing = false }) { keep ->
        removing = false
        scope.launch { if (model.uninstall(keep)) feedback?.notice("已卸载 ${plugin.label}") }
    }
}

private fun LazyListScope.catalogPluginBody(plugin: CatalogPlugin, state: CatalogPluginState, onOpenInstalled: () -> Unit) {
    item("header") {
        JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.s)) {
            // Why it cannot be installed is said in red below; the header keeps to the version.
            val line = if (CatalogText.standing(plugin) == CatalogStanding.Unavailable) plugin.version else CatalogText.line(plugin)
            JRow(plugin.label, subtitle = line, subtitleLines = 3, leadingIcon = PhIcons.PuzzlePiece,
                trailing = CatalogText.badge(plugin)?.let { badge -> { JTag(badge, standingTone(plugin)) } })
        }
    }
    plugin.description?.takeIf { it.isNotBlank() }?.let { item("description") { Note(it) } }
    plugin.tags.orEmpty().takeIf { it.isNotEmpty() }?.let { tags -> item("tags") { Tags(tags) } }
    state.actionError?.let { error -> item("action-error") { JInlineStatus(error, Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed) } }
    state.error?.let { error -> item("error") { JInlineStatus(error, Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed) } }
    CatalogText.noActionReason(plugin)?.let { reason ->
        item("why") {
            JInlineStatus(reason, Modifier.padding(top = Tokens.Space.s),
                tone = if (CatalogText.standing(plugin) == CatalogStanding.Unavailable) InlineTone.Failed else InlineTone.Busy)
        }
    }
    if (!plugin.installed.isNullOrBlank()) item("installed") {
        JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
            JRow("这个插件的设置", leadingIcon = PhIcons.GearSix, value = "已装 ${plugin.installed}", onClick = onOpenInstalled)
        }
    }
    permissionRows("permissions", "它可以", plugin.permissions)
    item("permissions-note") { Note("安装前会再列一次，你同意了才装；升级时要的权限变多，会单独列出来再问。") }
    val points = plugin.contributes.orEmpty().filterValues { it.isNotEmpty() }.toSortedMap().toList()
    if (points.isNotEmpty()) {
        item("contributes-title") { JSectionHeader("提供") }
        itemsIndexed(points, key = { _, it -> "point-${it.first}" }) { index, (point, ids) ->
            JGroupItem(GroupPosition.of(index, points.size)) { JRow(PluginText.point(point), value = ids.joinToString("、")) }
        }
    }
    val about = listOfNotNull(
        "版本" to plugin.version,
        state.marketName?.let { "市场" to it },
        plugin.publisher?.takeIf { it.isNotBlank() }?.let { "发布者" to it },
    )
    val homepage = plugin.homepage?.takeIf { PluginViewText.isHttps(it) }
    item("about-title") { JSectionHeader("关于") }
    val count = about.size + if (homepage != null) 1 else 0
    about.forEachIndexed { index, (label, value) ->
        item("about-$label") { JGroupItem(GroupPosition.of(index, count)) { JRow(label, value = value) } }
    }
    homepage?.let { url ->
        item("about-homepage") {
            val uri = LocalUriHandler.current
            JGroupItem(GroupPosition.of(count - 1, count)) {
                JRow("主页", leadingIcon = PhIcons.ArrowSquareOut, chevron = false, onClick = { runCatching { uri.openUri(url) } })
            }
        }
    }
    item("end") { androidx.compose.foundation.layout.Spacer(Modifier.padding(top = Tokens.Space.xxl)) }
}

private fun LazyListScope.permissionRows(key: String, title: String, permissions: List<PluginPermission>) {
    if (permissions.isEmpty()) return
    item("$key-title") { JSectionHeader(title) }
    itemsIndexed(permissions, key = { _, it -> "$key-${it.key}-${it.title}" }) { index, permission ->
        JGroupItem(GroupPosition.of(index, permissions.size)) {
            JRow(permission.title, subtitle = permission.detail?.takeIf { it.isNotBlank() }, subtitleLines = 4)
        }
    }
}

@Composable
private fun Note(text: String) = SettingsCaption(text, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Tags(tags: List<String>) {
    FlowRow(Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.xs), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
        verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        tags.forEach { JTag(it) }
    }
}

/**
 * What the package may do, checked by the hub, before it is installed: an upgrade that asks for more says so first
 * and lists the new lines alone, and its button turns red. Nothing is installed without the yes.
 */
@Composable
private fun PreviewSheet(plugin: CatalogPlugin?, preview: PluginPreview, busy: Boolean, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    val name = preview.manifest?.name?.takeIf { it.isNotBlank() } ?: plugin?.label ?: preview.manifest?.id.orEmpty()
    val more = CatalogText.asksForMore(preview)
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    JSheet(onDismiss, title = if (preview.upgrade) "升级 $name" else "安装 $name", dismissible = !busy, scrollable = true) {
        Column(Modifier.padding(vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            val version = preview.manifest?.version.orEmpty()
            Text(if (preview.upgrade) "版本 ${preview.from.orEmpty()} → $version" else "版本 $version", style = type.callout, color = palette.ink2)
            if (more) {
                JInlineStatus("新版本要的权限变多了", tone = InlineTone.Failed)
                PermissionList("新增", preview.addedPermissions.orEmpty())
            }
            PermissionList(if (preview.upgrade) "升级后它可以" else "它可以", preview.permissions)
            preview.warnings.orEmpty().forEach { JInlineStatus(it, tone = InlineTone.Caution) }
            SettingsCaption("包已经下载并核对过 sha256。插件在 hub 所在的机器上运行，能读写 hub 的系统用户能读写的文件。" +
                (preview.expiresAt?.let(CatalogText::time)?.let { "这次预览到 $it（UTC）失效。" } ?: ""))
            JButton(CatalogText.confirmLabel(preview), onConfirm, Modifier.fillMaxWidth(),
                kind = if (more) JButtonKind.Destructive else JButtonKind.Primary, loading = busy, enabled = !busy)
            JButton("不装了", onDismiss, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, enabled = !busy)
        }
    }
}

@Composable
private fun PermissionList(title: String, permissions: List<PluginPermission>) {
    if (permissions.isEmpty()) return
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        Text(title, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        Column {
            permissions.forEachIndexed { index, permission ->
                JGroupItem(GroupPosition.of(index, permissions.size)) {
                    JRow(permission.title, subtitle = permission.detail?.takeIf { it.isNotBlank() }, subtitleLines = 4)
                }
            }
        }
    }
}

/** Removing stops it and deletes its settings and secrets; its data folder can stay. */
@Composable
private fun UninstallDialog(plugin: CatalogPlugin, onDismiss: () -> Unit, onConfirm: (Boolean) -> Unit) {
    var keep by remember { mutableStateOf(false) }
    JDialog(onDismiss, title = "卸载 ${plugin.label}？", text = "会停掉它，删掉它的设置和密钥；它提供的连接器也一起去掉。",
        confirm = "卸载" to { onConfirm(keep) }, destructive = true) {
        Row(Modifier.fillMaxWidth().padding(top = Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
            JCheckbox(keep, { keep = it })
            Text("保留它的数据", Modifier.padding(start = Tokens.Space.s), style = PersonaTheme.type.body, color = PersonaTheme.palette.ink)
        }
    }
}

/** 管理市场: the markets the hub reads; add (an address, a token if private), refresh, change the token, remove. */
@Composable
fun PluginMarketsPage(plugins: PluginManagement?, onBack: () -> Unit) {
    if (plugins == null) {
        JListPage(title = "管理市场", onBack = onBack) { item { JNotice("体验模式没有 hub 的插件。") } }
        return
    }
    val model = remember(plugins) { MarketsModel(plugins) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    var adding by remember { mutableStateOf(false) }
    var acting by remember { mutableStateOf<PluginMarket?>(null) }
    var tokenFor by remember { mutableStateOf<PluginMarket?>(null) }
    var removing by remember { mutableStateOf<PluginMarket?>(null) }
    val uri = LocalUriHandler.current
    LaunchedEffect(model) { model.load() }
    val markets = state.markets
    JListPage(title = "管理市场", onBack = onBack, largeTitle = false,
        actions = { JIconButton(PhIcons.Plus, "添加市场", { model.clearAddError(); adding = true }) },
        refreshing = state.loading && markets != null, onRefresh = { scope.launch { model.load() } }) {
        state.error?.let { error ->
            item("error") {
                JNotice(error, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou, actionLabel = "重试",
                    onAction = { scope.launch { model.load() } })
            }
        }
        state.actionError?.let { error -> item("action-error") { JInlineStatus(error, Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed) } }
        when {
            markets == null && state.loading -> item("loading") { JLoading() }
            markets != null && markets.isEmpty() -> item("empty") {
                JEmptyState("还没有市场", "填一个市场的地址：https://… 的网站、github:owner/repo，或 git:<地址> 的 git 仓库。私有的 GitHub 仓库要一个只读的 token。", icon = PhIcons.Cube,
                    action = { JButton("添加市场", { model.clearAddError(); adding = true }, kind = JButtonKind.Secondary, size = JButtonSize.Small) })
            }
        }
        if (!markets.isNullOrEmpty()) {
            item("title") { JSectionHeader("这台 hub 读的市场", count = markets.size) }
            markets.forEachIndexed { index, market ->
                item("market-${market.id}") {
                    JGroupItem(GroupPosition.of(index, markets.size)) {
                        JRow(market.label, subtitle = market.spec + "\n" + CatalogText.marketLine(market), subtitleLines = 3, leadingIcon = PhIcons.Cube,
                            trailing = when {
                                state.working == market.id -> ({ JSpinner() })
                                !market.error.isNullOrBlank() -> ({ JIcon(PhIcons.WarningCircle, "读不到", tint = PersonaTheme.palette.danger.dot) })
                                else -> null
                            },
                            chevron = true, enabled = state.working == null, onClick = { acting = market })
                    }
                }
            }
            // Under the group, not between its rows: each says which market it is about.
            markets.forEach { market ->
                market.error?.takeIf { it.isNotBlank() }?.let { error ->
                    item("market-error-${market.id}") {
                        JInlineStatus("${market.label}：$error", Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed)
                    }
                }
                market.warnings.orEmpty().forEachIndexed { w, warning ->
                    item("market-warning-${market.id}-$w") {
                        JInlineStatus("${market.label}：$warning", Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Caution)
                    }
                }
            }
            item("footer") {
                SettingsCaption("删掉市场后，从它装的插件留着，只是不能再从这里升级。token 加密存在 hub 上，只发给这个市场，存好后不再显示。",
                    Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.m))
            }
        }
    }
    acting?.let { market ->
        JActionSheet(listOfNotNull(
            JAction("现在重新读取", icon = PhIcons.ArrowClockwise, onClick = {
                scope.launch { if (model.refresh(market.id)) feedback?.notice("已重新读取 ${market.label}") }
            }),
            JAction(if (market.hasToken) "换 token" else "设置 token", icon = PhIcons.Key, onClick = { tokenFor = market }),
            market.homepage?.takeIf { PluginViewText.isHttps(it) }?.let { url ->
                JAction("打开主页", icon = PhIcons.ArrowSquareOut, onClick = { runCatching { uri.openUri(url) } })
            },
            JAction("删除市场", icon = PhIcons.Trash, destructive = true, separatorBefore = true, onClick = { removing = market }),
        ), { acting = null }, title = market.label, note = market.description?.takeIf { it.isNotBlank() })
    }
    if (adding) AddMarketSheet(state, onDismiss = { adding = false }) { spec, token ->
        scope.launch { if (model.add(spec, token)) { adding = false; feedback?.notice("已添加市场") } }
    }
    tokenFor?.let { market ->
        TokenSheet(market, busy = state.working == market.id, onDismiss = { tokenFor = null }) { token ->
            scope.launch {
                if (model.setToken(market.id, token)) { tokenFor = null; feedback?.notice(if (token.isBlank()) "已去掉 token" else "已换 token") }
                else tokenFor = null
            }
        }
    }
    removing?.let { market ->
        JDialog({ removing = null }, title = "删除 ${market.label}？", text = "hub 不再读这个市场。从它装的插件留着。",
            confirm = "删除" to {
                removing = null
                scope.launch { if (model.remove(market.id)) feedback?.notice("已删除市场") }
            }, destructive = true)
    }
}

@Composable
private fun AddMarketSheet(state: MarketsState, onDismiss: () -> Unit, onAdd: (String, String) -> Unit) {
    var spec by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    JSheet(onDismiss, title = "添加市场", dismissible = !state.adding) {
        Column(Modifier.padding(vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            JTextField(spec, { spec = it }, Modifier.fillMaxWidth(), label = "地址", placeholder = "https://…、github:owner/repo 或 git:…",
                singleLine = true, enabled = !state.adding,
                keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, keyboardType = KeyboardType.Uri))
            SettingsCaption("三种写法：网站 https://example.com/plugins/；GitHub 仓库 github:owner/repo；任意 git 仓库 " +
                "git:<地址>[#分支]（git@host:path.git、ssh://…、https://…）。")
            JTextField(token, { token = it }, Modifier.fillMaxWidth(), label = "token（私有的才要）", placeholder = "可以不填", singleLine = true,
                enabled = !state.adding, visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password))
            SettingsCaption("私有的 GitHub 仓库用只读的 fine-grained token（这个仓库的 Contents 读权限）；git: 市场用 hub 机器自己的 SSH 凭据，不用 token。" +
                "token 加密存在 hub 上，存好后不再显示。")
            state.addError?.let { JInlineStatus(it, tone = InlineTone.Failed) }
            JButton("添加", { onAdd(spec, token) }, Modifier.fillMaxWidth(), loading = state.adding, loadingText = "正在读取市场…",
                enabled = !state.adding && spec.isNotBlank())
        }
    }
}

@Composable
private fun TokenSheet(market: PluginMarket, busy: Boolean, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var token by remember(market.id) { mutableStateOf("") }
    JSheet(onDismiss, title = if (market.hasToken) "换 token" else "设置 token", dismissible = !busy) {
        Column(Modifier.padding(vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            JTextField(token, { token = it }, Modifier.fillMaxWidth(), placeholder = if (market.hasToken) "输入新的 token 替换" else "输入 token",
                singleLine = true, enabled = !busy, visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password))
            SettingsCaption("保存后 hub 用新的 token 重新读一次 ${market.label}。")
            JButton("保存", { onSave(token) }, Modifier.fillMaxWidth(), loading = busy, enabled = !busy && token.isNotBlank())
            if (market.hasToken) JButton("去掉 token", { onSave("") }, Modifier.fillMaxWidth(), kind = JButtonKind.Destructive, enabled = !busy)
        }
    }
}
