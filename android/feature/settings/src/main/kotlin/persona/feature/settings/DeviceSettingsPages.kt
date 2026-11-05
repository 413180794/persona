package persona.feature.settings

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.contracts.api.*
import kotlinx.coroutines.launch
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource

@Composable
internal fun DevicesPage(
    controller: DeviceSettingsState,
    available: Boolean,
    onBack: () -> Unit,
    footer: @Composable () -> Unit = {},
) {
    val state by controller.state.collectAsState()
    val scope = rememberCoroutineScope()
    var pending by remember { mutableStateOf<PairedDevice?>(null) }
    LaunchedEffect(controller) { if (available) controller.loadDevices() }
    JListPage(title = stringResource(R.string.settings_devices_title), onBack = onBack) {
        if (!available) item { JEmptyState(stringResource(R.string.settings_devices_unavailable), stringResource(R.string.settings_check_pairing_and_hub)) }
        else {
            if (state.devices == null && state.busy) item { JLoading(text = stringResource(R.string.settings_devices_loading)) }
            if (state.devices?.devices?.isEmpty() == true) item { JEmptyState(stringResource(R.string.settings_devices_empty), stringResource(R.string.settings_devices_empty_detail)) }
            items(state.devices?.devices.orEmpty(), key = { it.id }) { device ->
                val current = device.id == state.devices?.currentId
                JGroupItem(GroupPosition.Single, Modifier.padding(bottom = Tokens.Space.m)) {
                    JRow(device.name.ifBlank { stringResource(R.string.settings_devices_unnamed) }, leadingIcon = PhIcons.DeviceMobile,
                        value = if (current) stringResource(R.string.settings_devices_this) else null,
                        subtitle = stringResource(R.string.settings_devices_platform_role, device.platformLabel(current),
                            stringResource(when (device.role) { "full" -> R.string.settings_devices_role_full; "answer" -> R.string.settings_devices_role_answer; else -> R.string.settings_devices_role_unknown })))
                    SettingsCaption(if (device.lastSeen.isNullOrBlank() && !device.createdAt.isNullOrBlank()) stringResource(R.string.settings_devices_added, lastUsed(device.createdAt))
                        else stringResource(R.string.settings_devices_last_used, lastUsed(device.lastSeen)), Modifier.padding(horizontal = Tokens.Space.m))
                    JButton(stringResource(if (current) R.string.settings_devices_revoke_this else R.string.settings_devices_revoke), { pending = device }, Modifier.padding(Tokens.Space.m),
                        kind = JButtonKind.Secondary, size = JButtonSize.Small, enabled = !state.busy && !state.needsCleanup)
                }
            }
            item { Action(stringResource(if (state.busy) R.string.settings_devices_working else R.string.settings_devices_refresh), enabled = !state.busy && !state.needsCleanup) { scope.launch { controller.loadDevices() } } }
            // Revoked credentials stay listed, greyed, for reference; they cannot connect or be revoked again.
            val revoked = state.devices?.revoked.orEmpty()
            if (revoked.isNotEmpty()) item("revoked-title") { JSectionHeader(stringResource(R.string.settings_devices_revoked), count = revoked.size) }
            itemsIndexed(revoked, key = { _, device -> "revoked-${device.id}" }) { index, device ->
                JGroupItem(GroupPosition.of(index, revoked.size)) {
                    JRow(device.name.ifBlank { stringResource(R.string.settings_devices_unnamed) }, leadingIcon = PhIcons.DeviceMobile, enabled = false,
                        subtitle = stringResource(R.string.settings_devices_revoked_at, lastUsed(device.revokedAt)))
                }
            }
            if (state.devices != null) item("credentials-note") {
                SettingsCaption(stringResource(R.string.settings_devices_note),
                    Modifier.padding(vertical = Tokens.Space.m))
            }
        }
        item { footer() }
    }
    pending?.let { device ->
        val current = device.id == state.devices?.currentId
        JDialog(onDismiss = { pending = null }, title = stringResource(if (current) R.string.settings_devices_revoke_this_title else R.string.settings_devices_revoke_title),
            text = stringResource(if (current) R.string.settings_devices_revoke_this_text else R.string.settings_devices_revoke_text),
            destructive = true, confirmEnabled = !state.busy,
            confirm = stringResource(R.string.settings_devices_revoke) to { pending = null; scope.launch { controller.revoke(device) }; Unit })
    }
}

@Composable
internal fun NotificationPreferences(controller: DeviceSettingsState, available: Boolean): LazyListScope.() -> Unit {
    val state by controller.state.collectAsState()
    val scope = rememberCoroutineScope()
    LaunchedEffect(controller) { if (available) controller.loadPreferences() }
    val prefs = state.preferences?.takeIf { available }
    val enabled = !state.busy && !state.needsCleanup
    return {
        item { JSectionHeader(stringResource(R.string.settings_notify_prefs_header)) }
        if (!available) item { JEmptyState(stringResource(R.string.settings_notify_prefs_unavailable), stringResource(R.string.settings_check_pairing_and_hub)) }
        if (prefs != null) {
            item {
                JGroupItem(GroupPosition.Single) {
                    JSwitchRow(stringResource(R.string.settings_notify_needs_you), prefs.input, { scope.launch { controller.savePreferences(prefs.copy(input = it)) } },
                        leadingIcon = PhIcons.BellSimple, enabled = enabled)
                }
            }
            item { JSectionHeader(stringResource(R.string.settings_notify_done_header)) }
            DoneNotification.entries.forEachIndexed { index, mode ->
                item {
                    JGroupItem(GroupPosition.of(index, DoneNotification.entries.size)) {
                        JRow(stringResource(when (mode) { DoneNotification.Off -> R.string.settings_notify_done_off; DoneNotification.Long -> R.string.settings_notify_done_long; DoneNotification.Always -> R.string.settings_notify_done_always }),
                            enabled = enabled, chevron = false,
                            onClick = { scope.launch { controller.savePreferences(prefs.copy(done = mode)) } },
                            trailing = { JRadio(selected = prefs.done == mode, onClick = null, enabled = enabled) })
                    }
                }
            }
            item {
                Column(Modifier.padding(vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    SettingsCaption(stringResource(R.string.settings_notify_seen_note))
                    prefs.longTurnSeconds?.let { SettingsCaption(pluralStringResource(R.plurals.settings_notify_long_turn, it, it)) }
                    prefs.doneDelaySeconds?.let { SettingsCaption(pluralStringResource(R.plurals.settings_notify_done_delay, it, it)) }
                }
            }
            item {
                JGroupItem(GroupPosition.of(0, 2)) {
                    JSwitchRow(stringResource(R.string.settings_notify_live), prefs.liveActivity, { scope.launch { controller.savePreferences(prefs.copy(liveActivity = it)) } }, enabled = enabled)
                    SettingsCaption(stringResource(R.string.settings_notify_live_about),
                        Modifier.padding(horizontal = Tokens.Space.m).padding(bottom = Tokens.Space.m))
                }
            }
            item {
                JGroupItem(GroupPosition.of(1, 2)) {
                    JSwitchRow(stringResource(R.string.settings_notify_lock_details), prefs.lockDetails, { scope.launch { controller.savePreferences(prefs.copy(lockDetails = it)) } }, enabled = enabled)
                    SettingsCaption(stringResource(R.string.settings_notify_lock_details_about), Modifier.padding(horizontal = Tokens.Space.m).padding(bottom = Tokens.Space.m))
                }
            }
        }
        if (available && state.preferences == null) item {
            Action(stringResource(if (state.busy) R.string.settings_reading else R.string.settings_retry_read), enabled = !state.busy) { scope.launch { controller.loadPreferences() } }
        }
        if (state.busy && state.preferences != null) item { SettingsCaption(stringResource(R.string.settings_saving)) }
    }
}

@Composable
internal fun AboutSettingsPage() {
    val context = LocalContext.current
    val info = remember(context) { context.packageManager.getPackageInfo(context.packageName, 0) }
    JGroupItem(GroupPosition.of(0, 2)) { JRow(stringResource(R.string.settings_about_version), value = info.versionName ?: stringResource(R.string.settings_not_provided)) }
    JGroupItem(GroupPosition.of(1, 2)) { JRow(stringResource(R.string.settings_about_build), trailing = { Text("${info.longVersionCode}", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3) }) }
}

@Composable
private fun PairedDevice.platformLabel(current: Boolean): String = when {
    current -> "Android"
    platform == "android" || kind == "android" -> "Android"
    platform == "ios" || platform == "iphone" || kind == "iphone" -> "iPhone"
    platform == "watchos" || kind == "watch" -> stringResource(R.string.settings_devices_watch)
    kind == "phone" -> stringResource(R.string.settings_devices_phone_unknown)
    else -> stringResource(R.string.settings_devices_platform_unknown)
}

@Composable
private fun lastUsed(value: String?): String {
    if (value.isNullOrBlank()) return stringResource(R.string.settings_devices_no_record)
    // parseTime also reads offsets (+08:00), which Instant.parse rejects on Android 12 and 13.
    return persona.core.contracts.parseTime(value)?.let { LocalizedFormats.format(it, LocalizedFormats.DATE_TIME, currentLocale) }
        ?: stringResource(R.string.settings_devices_no_time)
}
