package persona.feature.settings

import androidx.compose.material3.Text
import androidx.compose.foundation.layout.*
import androidx.compose.ui.Modifier
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import persona.core.designsystem.*
import persona.core.contracts.api.DeviceDisconnectSummary
import persona.core.contracts.api.LockScreenTest
import persona.core.contracts.api.SettingsDiagnostics
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import androidx.annotation.StringRes
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import kotlin.math.roundToLong

@Composable
internal fun LockScreenTestSection(diagnostics: SettingsDiagnostics?, onCheckSettings: () -> Unit) {
    val scope = rememberCoroutineScope()
    val preferences = LocalContext.current.getSharedPreferences("settings-readiness", 0)
    var test by remember { mutableStateOf<LockScreenTest?>(null) }
    var summary by remember { mutableStateOf<DeviceDisconnectSummary?>(null) }
    var message by remember { mutableStateOf<Int?>(null) }
    var busy by remember { mutableStateOf(false) }
    var resumed by remember { mutableStateOf(false) }
    var feedback by remember { mutableStateOf<String?>(null) }
    suspend fun refresh() {
        try {
            test = diagnostics?.lockScreenTest()
            feedback = test?.id?.let { preferences.getString("lock-test-$it", null) }
            summary = diagnostics?.disconnectSummary()
            message = null
        } catch (error: CancellationException) { throw error }
        catch (_: Exception) { message = R.string.settings_lock_test_read_failed }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { resumed = true; scope.launch { refresh() } }
    LifecycleEventEffect(Lifecycle.Event.ON_PAUSE) { resumed = false }
    LaunchedEffect(test?.id, test?.status, resumed) {
        while (resumed && test?.status in setOf("scheduled", "sent")) { delay(3_000); refresh() }
    }
    JCard(Modifier.padding(top = Tokens.Space.l), status = when (test?.status) {
        "received" -> if (test?.notificationShown == true) Status.Unread else Status.NeedsYou
        "scheduled", "sent" -> Status.Working
        null -> Status.Unknown
        else -> Status.NeedsYou
    }) {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            Text(stringResource(R.string.settings_lock_test_title), style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
            Action(stringResource(if (busy) R.string.settings_lock_test_scheduling else R.string.settings_lock_test_start), enabled = diagnostics != null && !busy && test?.status != "scheduled") {
                scope.launch {
                    busy = true
                    try {
                        checkNotNull(diagnostics).scheduleLockScreenTest()
                        refresh()
                        message = R.string.settings_lock_test_scheduled
                    } catch (error: CancellationException) { throw error }
                    catch (_: Exception) { message = R.string.settings_lock_test_schedule_failed }
                    finally { busy = false }
                }
            }
            test?.let { record ->
                if (record.status == "sent") PendingBadge(PendingStatus.Unconfirmed)
                Text(stringResource(lockTestStatus(record)), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
                SettingsCaption(stringResource(R.string.settings_lock_test_due, localTime(record.dueAt, currentLocale).orEmpty()))
                record.sentAt?.let { SettingsCaption(stringResource(R.string.settings_lock_test_sent, localTime(it, currentLocale).orEmpty())) }
                record.receivedAt?.let { SettingsCaption(stringResource(R.string.settings_lock_test_received, localTime(it, currentLocale).orEmpty())) }
                if (record.status == "received") SettingsCaption(stringResource(when (record.notificationShown) {
                    true -> R.string.settings_lock_test_shown
                    false -> R.string.settings_lock_test_not_shown
                    null -> R.string.settings_lock_test_shown_unknown
                }))
                if (record.status != "scheduled") {
                    Action(stringResource(R.string.settings_lock_test_worked)) {
                        feedback = LockTestFeedback.Worked
                        preferences.edit().putString("lock-test-${record.id}", feedback).apply()
                    }
                    Action(stringResource(R.string.settings_lock_test_failed)) {
                        feedback = LockTestFeedback.Failed
                        preferences.edit().putString("lock-test-${record.id}", feedback).apply()
                        onCheckSettings()
                    }
                }
            }
            if (test == null) JEmptyState(stringResource(R.string.settings_lock_test_none), stringResource(R.string.settings_lock_test_none_detail))
            feedback?.let { SettingsCaption(LockTestFeedback.text(it)) }
            summary?.let {
                SettingsCaption(disconnectSummaryText(it))
                SettingsCaption(stringResource(R.string.settings_disconnects_note))
            }
            message?.let { JNotice(stringResource(it)) }
            Action(stringResource(R.string.settings_lock_test_refresh), enabled = !busy) { scope.launch { refresh() } }
        }
    }
}

@StringRes internal fun lockTestStatus(test: LockScreenTest): Int = when (test.status) {
    "scheduled" -> R.string.settings_lock_test_status_scheduled
    "sent" -> R.string.settings_lock_test_status_sent
    "received" -> R.string.settings_lock_test_status_received
    "unavailable" -> R.string.settings_lock_test_status_unavailable
    "interrupted" -> R.string.settings_lock_test_status_interrupted
    else -> R.string.settings_lock_test_status_unknown
}

/** What the user said about a lock screen test, kept per test as a code (older builds kept the sentence itself). */
internal object LockTestFeedback {
    const val Worked = "worked"
    const val Failed = "failed"

    @Composable fun text(value: String): String = when (value) {
        Worked -> stringResource(R.string.settings_lock_test_feedback_worked)
        Failed -> stringResource(R.string.settings_lock_test_feedback_failed)
        else -> value
    }
}

/** The hub's 24-hour disconnect summary, as "Checklist" and "Connection diagnostics" show it. */
@Composable
internal fun disconnectSummaryText(summary: DeviceDisconnectSummary): String {
    val seconds = LocalizedFormats.number(summary.longestGapSeconds.roundToLong(), currentLocale)
    return pluralStringResource(R.plurals.settings_disconnects_summary, summary.disconnects, summary.disconnects, seconds)
}
