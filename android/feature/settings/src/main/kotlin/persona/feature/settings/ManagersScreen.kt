package persona.feature.settings

import android.content.ClipData
import android.content.ClipboardManager
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.api.HubManager
import persona.core.contracts.api.HubManagers
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.LocalDetailKey
import persona.core.navigation.ManagerRoute
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.Instant

/**
 * Settings › Managers (docs/managers.md): the hub's managers, each a whole assistant, where each runs and whether it is
 * connected; New manager gives an install code. The list follows the hub's `managers` pushes while it is open.
 */
@Composable
internal fun ManagersPage(
    managers: HubManagers?, onBack: () -> Unit, onManager: (String) -> Unit,
    /** False draws the time once (the pictures and the tests); countdowns move each second otherwise. */
    ticking: Boolean = true,
) {
    if (managers == null) {
        JListPage(title = stringResource(R.string.settings_managers_title), onBack = onBack) { item { JNotice(stringResource(R.string.settings_managers_demo)) } }
        return
    }
    val model = remember(managers) { ManagersModel(managers) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    var naming by remember { mutableStateOf(false) }
    LaunchedEffect(model) { model.load() }
    LaunchedEffect(model) { managers.changes.collect { model.load() } }
    val now = rememberNow(ticking = ticking && (state.code != null || state.managers.any { it.isInstalling }))
    ManagersContent(state, now, onBack, onRetry = { scope.launch { model.load() } }, onManager = onManager,
        onNew = { model.clearFailure(); naming = true }, selected = LocalDetailKey.current.let { key -> { id: String -> key == ManagerRoute(id) } })
    if (naming) NameDialog(
        title = stringResource(R.string.settings_managers_new), confirm = stringResource(R.string.settings_managers_create), initial = "",
        busy = state.busy, failure = state.failure, onDismiss = { naming = false },
        onConfirm = { name -> scope.launch { if (model.create(name)) naming = false } },
    )
    ManagerCodeSheetHost(state, now, model)
}

/** The list as drawn for [state] at [now] (the pictures draw this with fixed data). */
@Composable
internal fun ManagersContent(
    state: ManagersState, now: Instant, onBack: () -> Unit, onRetry: () -> Unit, onManager: (String) -> Unit, onNew: () -> Unit,
    selected: (String) -> Boolean = { false },
) {
    val roster = state.roster
    JListPage(title = stringResource(R.string.settings_managers_title), onBack = onBack,
        refreshing = state.loading && roster != null, onRefresh = onRetry) {
        item("hint") { ManagerNote(stringResource(R.string.settings_managers_hint)) }
        if (state.unsupported) item("unsupported") {
            JNotice(stringResource(R.string.settings_managers_unsupported), Modifier.padding(top = Tokens.Space.s), status = Status.NeedsYou)
        }
        state.error?.let { error ->
            item("error") {
                JNotice(error.text(), Modifier.padding(top = Tokens.Space.s), status = Status.NeedsYou,
                    actionLabel = stringResource(R.string.settings_src_retry), onAction = onRetry)
            }
        }
        if (roster == null && state.loading && !state.unsupported) item("loading") { JLoading() }
        val list = state.managers
        itemsIndexed(list, key = { _, it -> "manager-${it.id}" }) { index, manager ->
            JGroupItem(GroupPosition.of(index, list.size), if (index == 0) Modifier.padding(top = Tokens.Space.s) else Modifier) {
                JRow(onClick = { onManager(manager.id) }, selected = selected(manager.id)) { ManagerSummary(manager, now) }
            }
        }
        state.failure?.let { failure -> item("failure") { JInlineStatus(failure.text(), Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed) } }
        if (roster != null) item("new") {
            JButton(stringResource(R.string.settings_managers_new), onNew, Modifier.fillMaxWidth().padding(top = Tokens.Space.l),
                kind = JButtonKind.Secondary, icon = PhIcons.Plus, enabled = !state.busy)
        }
    }
}

/** A manager's name with Default, its status with the dot, and where it runs (its page has a row for that instead). */
@Composable
private fun androidx.compose.foundation.layout.RowScope.ManagerSummary(manager: HubManager, now: Instant, where: Boolean = true) {
    val res = LocalResources.current
    val palette = PersonaTheme.palette
    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            Text(manager.name.ifBlank { manager.id }, Modifier.weight(1f, fill = false), style = PersonaTheme.type.body, color = palette.ink,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (manager.isDefault) JTag(stringResource(R.string.settings_managers_default))
        }
        JStateLabel(ManagerText.statusLine(res, manager, now), ManagerText.status(manager))
        if (where) ManagerText.where(res, manager)?.let {
            Text(it, style = PersonaTheme.type.caption.copy(fontWeight = FontWeight.Normal), color = palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** What a manager's page is asking about right now. */
private enum class ManagerAsk { Rename, Move, Delete }

/**
 * Settings › Managers › one: its status, where it runs, its version and when it was last connected; Rename, Set as default, Install elsewhere
 * (New installation code while installing) and Delete (Cancel installation), each only where the hub would take it.
 */
@Composable
internal fun ManagerPage(managers: HubManagers?, managerId: String, onBack: () -> Unit, ticking: Boolean = true) {
    if (managers == null) {
        JListPage(title = stringResource(R.string.settings_managers_title), onBack = onBack) { item { JNotice(stringResource(R.string.settings_managers_demo)) } }
        return
    }
    val model = remember(managers) { ManagersModel(managers) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    var asking by remember { mutableStateOf<ManagerAsk?>(null) }
    LaunchedEffect(model) { model.load() }
    LaunchedEffect(model) { managers.changes.collect { model.load() } }
    val manager = state.manager(managerId)
    val now = rememberNow(ticking = ticking && (state.code != null || manager?.isInstalling == true))
    ManagerContent(state, managerId, now, onBack, onRetry = { scope.launch { model.load() } },
        onRename = { model.clearFailure(); asking = ManagerAsk.Rename },
        onDefault = { scope.launch { model.makeDefault(managerId) } },
        // An installing manager's new code moves nothing: no warning.
        onMove = { if (manager?.isInstalling == true) scope.launch { model.newCode(managerId) } else asking = ManagerAsk.Move },
        onDelete = { asking = ManagerAsk.Delete })
    val name = manager?.name?.ifBlank { manager.id }.orEmpty()
    when (asking) {
        ManagerAsk.Rename -> NameDialog(
            title = stringResource(R.string.settings_managers_rename), confirm = stringResource(R.string.settings_managers_save), initial = name,
            busy = state.busy, failure = state.failure, onDismiss = { asking = null },
            onConfirm = { typed -> scope.launch { if (model.rename(managerId, typed)) asking = null } },
        )
        ManagerAsk.Move -> JDialog(
            onDismiss = { asking = null }, title = stringResource(R.string.settings_managers_move),
            text = stringResource(R.string.settings_managers_move_confirm), confirmEnabled = !state.busy,
            confirm = stringResource(R.string.settings_managers_move) to { asking = null; scope.launch { model.newCode(managerId) }; Unit },
        )
        ManagerAsk.Delete -> {
            val installing = manager?.isInstalling == true
            JDialog(
                onDismiss = { asking = null },
                title = stringResource(if (installing) R.string.settings_managers_cancel_install else R.string.settings_managers_delete),
                text = stringResource(if (installing) R.string.settings_managers_cancel_install_confirm else R.string.settings_managers_delete_confirm, name),
                destructive = true, confirmEnabled = !state.busy,
                dismiss = stringResource(R.string.settings_managers_keep) to { asking = null },
                confirm = stringResource(if (installing) R.string.settings_managers_cancel_install else R.string.settings_managers_delete) to {
                    scope.launch { if (model.delete(managerId)) { asking = null; onBack() } else asking = null }; Unit
                },
            )
        }
        null -> Unit
    }
    ManagerCodeSheetHost(state, now, model)
}

/** A manager's page as drawn for [state] at [now] (the pictures draw this with fixed data). */
@Composable
internal fun ManagerContent(
    state: ManagersState, managerId: String, now: Instant, onBack: () -> Unit, onRetry: () -> Unit,
    onRename: () -> Unit, onDefault: () -> Unit, onMove: () -> Unit, onDelete: () -> Unit,
) {
    val res = LocalResources.current
    val manager = state.manager(managerId)
    JListPage(title = manager?.name?.ifBlank { manager.id } ?: stringResource(R.string.settings_managers_title), onBack = onBack, largeTitle = false,
        refreshing = state.loading && state.roster != null, onRefresh = onRetry) {
        if (state.unsupported) item("unsupported") {
            JNotice(stringResource(R.string.settings_managers_unsupported), Modifier.padding(top = Tokens.Space.s), status = Status.NeedsYou)
        }
        state.error?.let { error ->
            item("error") {
                JNotice(error.text(), Modifier.padding(top = Tokens.Space.s), status = Status.NeedsYou,
                    actionLabel = stringResource(R.string.settings_src_retry), onAction = onRetry)
            }
        }
        if (state.roster == null && state.loading) item("loading") { JLoading() }
        if (state.roster != null && manager == null) item("gone") {
            JNotice(stringResource(R.string.settings_managers_err_gone), Modifier.padding(top = Tokens.Space.s))
        }
        if (manager == null) return@JListPage
        item("header") {
            JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.s)) { JRow { ManagerSummary(manager, now, where = false) } }
        }
        val facts = listOfNotNull(
            ManagerText.where(res, manager)?.let { res.getString(R.string.settings_managers_row_where) to it },
            manager.version?.takeIf { it.isNotBlank() }?.let { res.getString(R.string.settings_managers_row_version) to it },
            ManagerText.seen(res, manager, now),
        )
        if (facts.isNotEmpty()) item("facts") {
            Column(Modifier.padding(top = Tokens.Space.l)) {
                facts.forEachIndexed { index, (label, value) ->
                    JGroupItem(GroupPosition.of(index, facts.size)) { JRow(label, value = value) }
                }
            }
        }
        val installing = manager.isInstalling
        val actions = buildList<@Composable () -> Unit> {
            if (!installing) add { JRow(stringResource(R.string.settings_managers_rename), leadingIcon = PhIcons.PencilSimple, enabled = !state.busy, onClick = onRename, chevron = false) }
            if (!installing && !manager.isDefault) add { JRow(stringResource(R.string.settings_managers_make_default), leadingIcon = PhIcons.Star, enabled = !state.busy, onClick = onDefault, chevron = false) }
            if (!manager.builtin) add {
                JRow(stringResource(if (installing) R.string.settings_managers_new_code else R.string.settings_managers_move), leadingIcon = PhIcons.Key,
                    enabled = !state.busy, onClick = onMove, chevron = false)
            }
            add {
                JRow(stringResource(if (installing) R.string.settings_managers_cancel_install else R.string.settings_managers_delete),
                    leadingIcon = if (installing) PhIcons.X else PhIcons.Trash, destructive = true,
                    enabled = !state.busy && ManagerText.whyNoDelete(manager) == null, onClick = onDelete, chevron = false)
            }
        }
        item("actions") {
            Column(Modifier.padding(top = Tokens.Space.l)) {
                actions.forEachIndexed { index, row -> JGroupItem(GroupPosition.of(index, actions.size)) { row() } }
                ManagerText.whyNoDelete(manager)?.let { ManagerNote(stringResource(it)) }
            }
        }
        state.failure?.let { failure -> item("failure") { JInlineStatus(failure.text(), Modifier.padding(top = Tokens.Space.s), tone = InlineTone.Failed) } }
    }
}

/** New manager / Rename: a name, 1 to 40 characters; the hub's refusal shows under the field. */
@Composable
private fun NameDialog(
    title: String, confirm: String, initial: String, busy: Boolean, failure: ResText?, onDismiss: () -> Unit, onConfirm: (String) -> Unit,
) {
    var name by remember { mutableStateOf(initial) }
    val valid = ManagerText.validName(name)
    JDialog(onDismiss = onDismiss, title = title, confirmEnabled = valid && !busy, confirm = confirm to { onConfirm(name) }) {
        JTextField(name, { name = it }, Modifier.fillMaxWidth(), placeholder = stringResource(R.string.settings_managers_name_placeholder),
            singleLine = true, enabled = !busy, isError = failure != null,
            supportingText = failure?.text() ?: stringResource(R.string.settings_managers_err_name).takeIf { name.isNotEmpty() && !valid })
    }
}

/** The code sheet over either page while a code is on screen. */
@Composable
private fun ManagerCodeSheetHost(state: ManagersState, now: Instant, model: ManagersModel) {
    val sheet = state.code ?: return
    val scope = rememberCoroutineScope()
    JSheet(onDismiss = model::closeCode, title = sheet.name.ifBlank { null }, scrollable = true) {
        ManagerCodeBody(sheet, now, busy = state.busy, failure = state.failure,
            onNewCode = { scope.launch { model.newCode(sheet.managerId) } }, onDone = model::closeCode)
    }
}

/**
 * Installation code: the code to copy and its countdown (a new one once it ran out), and the hub's install commands when it
 * gives them, else what to do with the code; Done.
 */
@Composable
internal fun ColumnScope.ManagerCodeBody(sheet: ManagerCodeSheet, now: Instant, busy: Boolean, failure: ResText?, onNewCode: () -> Unit, onDone: () -> Unit) {
    val palette = PersonaTheme.palette
    val code = sheet.code
    val left = ManagerText.countdown(code.expiresAt, now)
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        CopyBlock(stringResource(R.string.settings_managers_code_title), code.code)
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            if (left != null) Text(stringResource(R.string.settings_managers_expires_in, left), Modifier.weight(1f), style = PersonaTheme.type.caption, color = palette.ink3, maxLines = 1)
            else {
                Text(stringResource(R.string.settings_managers_expired), Modifier.weight(1f), style = PersonaTheme.type.caption, color = palette.danger.text, maxLines = 1)
                JButton(stringResource(R.string.settings_managers_new_code), onNewCode, kind = JButtonKind.Secondary, size = JButtonSize.Small,
                    icon = PhIcons.Key, loading = busy)
            }
        }
        failure?.let { JInlineStatus(it.text(), tone = InlineTone.Failed) }
        val commands = code.commands
        val service = commands?.service?.takeIf { it.isNotBlank() }
        val docker = commands?.docker?.takeIf { it.isNotBlank() }
        if (service == null && docker == null) {
            Text(stringResource(R.string.settings_managers_code_note), style = PersonaTheme.type.callout, color = palette.ink2)
        } else {
            service?.let { CopyBlock(stringResource(R.string.settings_managers_install_service), it, small = true) }
            docker?.let { CopyBlock(stringResource(R.string.settings_managers_install_docker), it, small = true) }
        }
        JButton(stringResource(R.string.settings_managers_done), onDone, Modifier.fillMaxWidth().padding(top = Tokens.Space.s))
    }
}

/** A label with Copy on its right, over the text in the code face. */
@Composable
private fun CopyBlock(label: String, text: String, small: Boolean = false) {
    val context = LocalContext.current
    val res = LocalResources.current
    val feedback = LocalAppFeedback.current
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            Text(label, Modifier.weight(1f), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
            JButton(stringResource(R.string.settings_managers_copy), {
                context.getSystemService(ClipboardManager::class.java)?.setPrimaryClip(ClipData.newPlainText(label, text))
                feedback?.notice(res.getString(R.string.settings_src_copied))
            }, kind = JButtonKind.Quiet, size = JButtonSize.Small, icon = PhIcons.Copy, enabled = text.isNotEmpty())
        }
        JCodeBlock(text, Modifier.fillMaxWidth(), small = small)
    }
}

@Composable
private fun ManagerNote(text: String) = SettingsCaption(text, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))

/** The time, once a second while [ticking] (a countdown on screen), else fixed at when it stopped. */
@Composable
private fun rememberNow(ticking: Boolean): Instant {
    var now by remember { mutableStateOf(Instant.now()) }
    LaunchedEffect(ticking) {
        now = Instant.now()
        while (ticking) { delay(1000); now = Instant.now() }
    }
    return now
}
