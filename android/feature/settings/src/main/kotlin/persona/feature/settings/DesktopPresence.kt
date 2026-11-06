package persona.feature.settings

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import persona.core.contracts.HubDesktop
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/** "Persona on Mac is signed in" (iOS `DesktopDevice.bannerTitle`): one line however many there are. */
@Composable
internal fun desktopsTitle(list: List<HubDesktop>): String =
    if (list.size > 1) pluralStringResource(R.plurals.settings_desktops_banner_n, list.size, list.size) else stringResource(R.string.settings_desktops_banner)

/**
 * The settings list's row while a desktop app is connected (iOS `DesktopPresenceRow`, after WeChat's line over its
 * chats): which ones, "Mute phone" beside it when on. It opens [DesktopsPage].
 */
@Composable
internal fun DesktopPresenceRow(desktops: List<HubDesktop>, muted: Boolean, selected: Boolean, position: GroupPosition, onClick: () -> Unit) {
    JGroupItem(position) {
        JRow(desktopsTitle(desktops), leadingIcon = PhIcons.Laptop, selected = selected, onClick = onClick,
            subtitle = desktops.joinToString(stringResource(R.string.settings_providers_list_sep)) { it.name.ifBlank { "Mac" } },
            value = stringResource(R.string.settings_desktops_mute).takeIf { muted })
    }
}

/**
 * "Mute phone" at the top of Settings › Notifications (iOS `DesktopPresenceSheet`'s switch): while a desktop app is connected this phone
 * gets no "Needs you" / "Done" pushes. Only from a hub that keeps the setting.
 */
internal fun LazyListScope.desktopMuteItems(controller: DeviceSettingsState, desktops: List<HubDesktop>, onDesktops: (() -> Unit)?) {
    item("desktops-header") { JSectionHeader(stringResource(R.string.settings_desktops_header)) }
    item("desktops-mute") {
        val state by controller.state.collectAsState()
        val scope = rememberCoroutineScope()
        val prefs = state.preferences ?: return@item
        val muted = prefs.muteWhileDesktop ?: return@item
        val rows = if (onDesktops != null && desktops.isNotEmpty()) 2 else 1
        Column {
            JGroupItem(GroupPosition.of(0, rows)) {
                JSwitchRow(stringResource(R.string.settings_desktops_mute), muted, { scope.launch { controller.savePreferences(prefs.copy(muteWhileDesktop = it)) } },
                    leadingIcon = PhIcons.BellSimple, enabled = !state.busy && !state.needsCleanup)
            }
            if (rows == 2) JGroupItem(GroupPosition.of(1, 2)) {
                JRow(desktopsTitle(desktops), leadingIcon = PhIcons.Laptop, value = desktops.size.toString(), onClick = onDesktops)
            }
        }
    }
    item("desktops-note") {
        SettingsCaption(stringResource(R.string.settings_desktops_mute_note), Modifier.padding(horizontal = Tokens.Space.xs, vertical = Tokens.Space.s))
    }
}

/**
 * Signed-in computers (iOS `DesktopPresenceSheet`): which desktop apps are signed in and since when, "Mute phone", and "Sign out" for each
 * (it revokes that computer's device token; the hub closes its connection and it has to pair again).
 */
@Composable
internal fun DesktopsPage(controller: DeviceSettingsState, desktopsFlow: StateFlow<List<HubDesktop>>?, available: Boolean, onBack: () -> Unit) {
    val desktops = desktopsFlow?.collectAsState()?.value.orEmpty()
    val state by controller.state.collectAsState()
    val scope = rememberCoroutineScope()
    var signingOut by remember { mutableStateOf<HubDesktop?>(null) }
    LaunchedEffect(controller) { if (available && state.preferences == null) controller.loadPreferences() }
    JListPage(title = stringResource(R.string.settings_desktops_title), onBack = onBack) {
        if (!available) item("none") { JEmptyState(stringResource(R.string.settings_notify_prefs_unavailable), stringResource(R.string.settings_check_pairing_and_hub)) }
        else {
            state.preferences?.muteWhileDesktop?.let { muted ->
                item("mute") {
                    JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.s)) {
                        JSwitchRow(stringResource(R.string.settings_desktops_mute), muted, { on ->
                            state.preferences?.let { scope.launch { controller.savePreferences(it.copy(muteWhileDesktop = on)) } }
                        }, leadingIcon = PhIcons.BellSimple, enabled = !state.busy && !state.needsCleanup)
                    }
                }
                item("mute-note") { SettingsCaption(stringResource(R.string.settings_desktops_mute_note), Modifier.padding(horizontal = Tokens.Space.xs, vertical = Tokens.Space.s)) }
            }
            if (desktops.isEmpty()) item("empty") {
                JEmptyState(stringResource(R.string.settings_desktops_empty), stringResource(R.string.settings_desktops_empty_detail), icon = PhIcons.Laptop)
            }
            desktops.forEachIndexed { index, d ->
                item("d-$index-${d.device}") {
                    JSectionHeader(d.name.ifBlank { "Mac" })
                    JGroupItem(GroupPosition.of(0, if (d.device.isEmpty()) 1 else 2)) {
                        JRow(d.name.ifBlank { "Mac" }, leadingIcon = PhIcons.Laptop,
                            value = d.since?.let { persona.core.contracts.parseTime(it) }?.let { stringResource(R.string.settings_desktops_since, agoText(androidx.compose.ui.platform.LocalResources.current, it)) })
                    }
                    if (d.device.isNotEmpty()) JGroupItem(GroupPosition.of(1, 2)) {
                        JRow(stringResource(R.string.settings_desktops_sign_out), destructive = true, enabled = !state.busy, chevron = false, onClick = { signingOut = d })
                    } else SettingsCaption(stringResource(R.string.settings_desktops_pairing_token), Modifier.padding(horizontal = Tokens.Space.xs, vertical = Tokens.Space.s))
                }
            }
        }
        state.message?.let { message -> item("message") { JNotice(message.text(), Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou) } }
    }
    signingOut?.let { d ->
        JDialog(onDismiss = { signingOut = null }, title = stringResource(R.string.settings_desktops_sign_out_title, d.name.ifBlank { "Mac" }),
            text = stringResource(R.string.settings_desktops_sign_out_text), destructive = true, confirmEnabled = !state.busy,
            confirm = stringResource(R.string.settings_desktops_sign_out_confirm) to { signingOut = null; scope.launch { controller.revokeCredential(d.device) }; Unit })
    }
}
