package persona.feature.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/**
 * 设置 › 会话分诊 (docs/agent-work.md「开关」, the web's TriageSettings): when an agent stops, the hub can read its last
 * reply, say whether it is done or what it waits for, and act on it. All off until turned on here: the triage calls a
 * model on every stopped turn, and 出错自动继续 / 到点叫醒 run a turn of the agent on its own model. Saved on the hub, so
 * it is the same on every device.
 */
@Composable
internal fun TriageSettingsPage(triage: AgentTriage?, models: ModelConfiguration?, onBack: () -> Unit, onModel: () -> Unit) {
    BackHandler(onBack = onBack)
    if (triage == null) {
        JListPage(title = stringResource(R.string.settings_triage_title), onBack = onBack) { item { JNotice(stringResource(R.string.settings_triage_demo)) } }
        return
    }
    val scope = rememberCoroutineScope()
    val resources = LocalContext.current.resources
    val triageModel = remember(triage) { TriageSettingsModel(triage) }
    val state by triageModel.state.collectAsState()
    var model by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(triageModel) { triageModel.load() }
    LaunchedEffect(models) {
        model = try { models?.purpose("agent.triage")?.let { d -> d.purpose.summaryParts(d.providers).joinToString(resources.getString(R.string.settings_list_separator)).ifEmpty { resources.getString(R.string.settings_not_configured) } } }
        catch (e: CancellationException) { throw e } catch (_: Exception) { null }
    }
    fun change(next: TriageSwitches) { scope.launch { triageModel.change(next) } }
    val switches = state.switches
    val busy = state.busy
    val sw = switches
    val on = sw?.enabled == true
    JListPage(title = stringResource(R.string.settings_triage_title), onBack = onBack) {
        if (switches == null && state.loading) item("loading") { JLoading() }
        else state.readError?.takeIf { switches == null }?.let { message ->
            item("error") {
                JNotice(message.text(), status = Status.NeedsYou,
                    actionLabel = if (state.unsupported) null else stringResource(R.string.settings_retry_read),
                    onAction = if (state.unsupported) null else ({ scope.launch { triageModel.load() } }))
            }
        }
        state.saveError?.let { message -> item("save-error") { JNotice(message.text(), status = Status.NeedsYou) } }
        item("enabled") {
            JGroupItem(GroupPosition.of(0, 2), Modifier.padding(top = Tokens.Space.m)) {
                JSwitchRow(stringResource(R.string.settings_triage_title), on, { v -> sw?.let { change(it.copy(enabled = v)) } }, Modifier.testTag("triage-enabled"),
                    leadingIcon = PhIcons.ListChecks, enabled = sw != null && !busy)
            }
        }
        item("model") {
            JGroupItem(GroupPosition.of(1, 2)) {
                JRow(stringResource(R.string.settings_triage_model), leadingIcon = PhIcons.Brain, subtitle = model, subtitleLines = 1, onClick = onModel)
            }
        }
        item("about") {
            SettingsCaption(stringResource(R.string.settings_triage_about),
                Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
        }
        item("actions-header") { JSectionHeader(stringResource(R.string.settings_triage_actions_header)) }
        item("decide") {
            JGroupItem(GroupPosition.of(0, 3)) {
                JSwitchRow(stringResource(R.string.settings_triage_decide), sw?.decide == true, { v -> sw?.let { change(it.copy(decide = v)) } },
                    Modifier.testTag("triage-decide"), leadingIcon = PhIcons.Question, enabled = on && !busy)
            }
        }
        item("continue") {
            JGroupItem(GroupPosition.of(1, 3)) {
                JSwitchRow(stringResource(R.string.settings_triage_continue), sw?.autoContinue == true, { v -> sw?.let { change(it.copy(autoContinue = v)) } },
                    Modifier.testTag("triage-continue"), leadingIcon = PhIcons.ArrowsClockwise, enabled = on && !busy)
            }
        }
        item("wake") {
            JGroupItem(GroupPosition.of(2, 3)) {
                JSwitchRow(stringResource(R.string.settings_triage_wake), sw?.wake == true, { v -> sw?.let { change(it.copy(wake = v)) } },
                    Modifier.testTag("triage-wake"), leadingIcon = PhIcons.ClockCountdown, enabled = on && !busy)
            }
        }
        item("actions-about") {
            SettingsCaption(stringResource(R.string.settings_triage_actions_about),
                Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
        }
    }
}
