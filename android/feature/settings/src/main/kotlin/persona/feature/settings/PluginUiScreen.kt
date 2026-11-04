package persona.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import persona.core.contracts.GroupKeys
import persona.core.contracts.api.AgentLauncher
import persona.core.contracts.api.MachineManagement
import persona.core.contracts.api.PluginManagement
import persona.core.contracts.api.PluginUiContext
import persona.core.contracts.api.PluginUiPlace
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

// The places a plugin's View is drawn (docs/plugins.md「声明式 UI」): a page of its own (plugin_page, from the
// plugin's page in 设置 › 插件; nav_page, from the「插件」section of the settings home) and a section on a launcher's
// page (launcher_section). The UI slots of the settings home and a machine's page are in PluginSlots.kt.

/** 设置 › 插件 › one › a page it draws, or a nav page opened from the settings home. */
@Composable
fun PluginUiPage(plugins: PluginManagement?, pluginId: String, uiId: String, title: String, onBack: () -> Unit) {
    if (plugins == null) {
        JListPage(title = title, onBack = onBack) { item { JNotice("体验模式没有 hub 的插件。") } }
        return
    }
    val model = remember(plugins, pluginId, uiId) { PluginViewModel(plugins, pluginId, uiId) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    val handle = rememberPluginView(model)
    LaunchedEffect(model) { model.load() }
    // A view with refresh_after is drawn again while this page is on screen (docs/plugins.md「UI 插槽」「刷新」).
    KeepFresh(model)
    val view = state.view
    JListPage(title = view?.title?.takeIf { it.isNotBlank() } ?: title, onBack = onBack, largeTitle = false,
        refreshing = state.loading && view != null, onRefresh = { scope.launch { model.load() } }) {
        viewStatus("page", state, onRetry = { scope.launch { model.load() } })
        if (view != null && view.sections.none { it.items.any(PluginViewText::drawable) }) item("page-empty") {
            JEmptyState("这里还没有内容", "插件现在没有要显示的东西，过一会儿再下拉刷新看看。", icon = PhIcons.PuzzlePiece)
        }
        pluginViewItems("page", state, handle)
        item("page-end") { Spacer(Tokens.Space.xxl) }
    }
}

/** Loading, an error with 重试 when there is nothing to show yet, or the error above what is shown. */
private fun LazyListScope.viewStatus(key: String, state: PluginViewState, onRetry: () -> Unit) {
    when {
        state.view == null && state.error != null -> item("$key-error") {
            JNotice(state.error, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou, actionLabel = "重试", onAction = onRetry)
        }
        state.view == null && !state.unsupported -> item("$key-loading") { JLoading() }
        state.error != null -> item("$key-error") {
            JInlineStatus(state.error, Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed)
        }
    }
}

@Composable
private fun Spacer(height: androidx.compose.ui.unit.Dp) = androidx.compose.foundation.layout.Spacer(Modifier.padding(top = height))

/** The pages a plugin draws, listed on its page: a row each into it. */
internal fun LazyListScope.pluginPages(pages: List<PluginUiPlace>, onOpen: (PluginUiPlace) -> Unit) {
    if (pages.isEmpty()) return
    item("pages-title") { JSectionHeader("页面") }
    itemsIndexed(pages, key = { _, it -> "page-${it.id}" }) { index, page ->
        JGroupItem(GroupPosition.of(index, pages.size)) {
            JRow(page.title.ifBlank { page.id }, leadingIcon = PhIcons.SquaresFour, onClick = { onOpen(page) })
        }
    }
}

/**
 * A launcher of a machine (opened from its row on the machine's page): what it runs, and the sections plugins draw
 * for its gateway. A section whose plugin says the gateway is not its own is left out without a word.
 */
@Composable
fun LauncherPage(machines: MachineManagement?, plugins: PluginManagement?, machineId: String, name: String, onBack: () -> Unit) {
    val scope = rememberCoroutineScope()
    var launcher by remember(machineId, name) { mutableStateOf<AgentLauncher?>(null) }
    var launcherError by remember(machineId, name) { mutableStateOf<String?>(null) }
    LaunchedEffect(machines, machineId, name) {
        machines ?: return@LaunchedEffect
        try {
            launcher = machines.launchers(machineId).firstOrNull { it.name == name }
            launcherError = if (launcher == null) "这台机器上没有这个启动器了" else null
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { launcherError = "读不到启动器：${failureReason(error)}" }
    }
    // The plugin calls go to the hub, which knows its machines by their own ids (no group prefix).
    val context = remember(machineId, name) { PluginUiContext(launcher = name, machine = GroupKeys.raw(machineId)) }
    val places = remember(plugins, context) { plugins?.let { PluginPlacesModel(it, "launcher_section", context) } }
    val placesState = places?.state?.collectAsState()?.value ?: PluginPlacesState()
    val models = remember(plugins, placesState.places) {
        plugins?.let { api -> placesState.places.orEmpty().map { place -> place to PluginViewModel(api, place, context) } }.orEmpty()
    }
    LaunchedEffect(places) { places?.load() }
    val sections = models.map { (place, model) ->
        key(place.plugin, place.id) {
            LaunchedEffect(model) { model.load() }
            Triple(place, model.state.collectAsState().value, rememberPluginView(model))
        }
    }
    fun reload() = scope.launch {
        places?.load()
        models.forEach { (_, model) -> launch { model.load() } }
    }
    JListPage(title = name, onBack = onBack, largeTitle = false,
        refreshing = placesState.loading || sections.any { it.second.loading && it.second.view != null }, onRefresh = { reload() }) {
        item("launcher") {
            JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.s)) {
                JRow(name, leadingIcon = PhIcons.RocketLaunch, subtitle = "启动器")
            }
        }
        launcherError?.let { item("launcher-error") { JInlineStatus(it, Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed) } }
        launcher?.let { l ->
            val rows = listOfNotNull(
                launcherModel(l)?.let { "模型" to it },
                PluginViewText.host(l.baseUrl)?.let { "网关" to it },
                "类型" to if (l.agentType == "codex") "Codex" else "Claude Code",
            )
            item("about-title") { JSectionHeader("启动器") }
            itemsIndexed(rows, key = { _, it -> "about-${it.first}" }) { index, (label, value) ->
                JGroupItem(GroupPosition.of(index, rows.size)) { JRow(label, value = value) }
            }
        }
        if (plugins != null) {
            placesState.error?.let { error ->
                item("places-error") {
                    Column(Modifier.padding(top = Tokens.Space.l), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        JInlineStatus(error, tone = InlineTone.Failed)
                        JButton("重试", { scope.launch { places?.load() } }, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary,
                            enabled = !placesState.loading)
                    }
                }
            }
            sections.forEach { (place, state, handle) ->
                if (state.unsupported) return@forEach
                val key = "ui-${place.plugin}-${place.id}"
                // The heading waits for the view: a plugin that turns out not to know this gateway never shows one.
                if (state.view == null && state.error == null) return@forEach
                item("$key-title") { JSectionHeader(state.view?.title?.takeIf { it.isNotBlank() } ?: place.title.ifBlank { place.plugin }) }
                viewStatus(key, state, onRetry = { scope.launch { models.firstOrNull { it.first == place }?.second?.load() } })
                pluginViewItems(key, state, handle)
            }
            if (placesState.places == null && placesState.loading || sections.any { it.second.view == null && it.second.loading }) {
                item("places-loading") { JLoading() }
            }
        }
        item("end") { Spacer(Tokens.Space.xxl) }
    }
}

/** "claude-opus-4" and "anthropic/claude-opus-4" both read as the last part of the path. */
private fun launcherModel(launcher: AgentLauncher): String? = launcher.model?.takeIf { it.isNotBlank() }?.substringAfterLast('/')
