package persona.feature.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import persona.core.contracts.api.ConnectorsManagement
import persona.core.contracts.api.LarkMessageDisplay
import persona.core.designsystem.*
import persona.core.hub.HubFailure

/**
 * 设置 › 飞书 › 风灵月影 (iOS `LarkMessageDisplaySettingsView`): 防撤回, 编辑历史 and, from an extension that has it,
 * [removed]编码, shared with lark-connector's web for the signed-in account. A switch changes from what it was read as; anything
 * unclear reads the settings again instead of guessing.
 */
@Composable
internal fun LarkMessageDisplayPage(connectors: ConnectorsManagement?, onBack: () -> Unit) {
    val scope = rememberCoroutineScope()
    val res = LocalResources.current
    var snapshot by remember { mutableStateOf<LarkMessageDisplay?>(null) }
    var loading by remember { mutableStateOf(true) }
    var saving by remember { mutableStateOf(false) }
    var problem by remember { mutableStateOf<String?>(null) }

    fun load() {
        val api = connectors ?: run { loading = false; problem = res.getString(R.string.settings_src_display_unreadable); return }
        scope.launch {
            loading = true
            try {
                val next = api.larkMessageDisplay()
                if (next.accountId.isBlank()) throw IllegalStateException("No account")
                snapshot = next; problem = null
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) {
                snapshot = null
                problem = res.getString(if ((e as? HubFailure)?.status == 404) R.string.settings_src_display_update else R.string.settings_src_display_unreadable)
            } finally { loading = false }
        }
    }
    fun set(key: String, value: Boolean) {
        val api = connectors ?: return
        val current = snapshot ?: return
        if (saving || loading || current.value(key) == null) return
        scope.launch {
            saving = true
            try {
                val next = api.setLarkMessageDisplay(current, key, value)
                if (next.accountId.isBlank()) throw IllegalStateException("No account")
                snapshot = next; problem = null
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) {
                snapshot = null
                problem = res.getString(when ((e as? HubFailure)?.status) {
                    409 -> R.string.settings_src_display_changed
                    403 -> R.string.settings_src_display_forbidden
                    404 -> R.string.settings_src_display_update_extension
                    else -> R.string.settings_src_display_unconfirmed
                })
            } finally { saving = false }
        }
    }
    LaunchedEffect(connectors) { load() }
    BackHandler(onBack = onBack)
    val policy = snapshot ?: LarkMessageDisplay.Hidden
    val enabled = snapshot != null && !loading && !saving
    JListPage(title = stringResource(R.string.settings_src_display_title), onBack = onBack) {
        item("anti-recall") {
            JGroupItem(GroupPosition.Single) {
                JSwitchRow(stringResource(R.string.settings_src_display_anti_recall), policy.antiRecall, { set("anti_recall", it) }, enabled = enabled)
            }
        }
        item("anti-recall-caption") { SettingsCaption(stringResource(R.string.settings_src_display_anti_recall_caption), Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s)) }
        item("history") {
            JGroupItem(GroupPosition.Single) {
                JSwitchRow(stringResource(R.string.settings_src_display_history), policy.showEditHistory, { set("show_edit_history", it) }, enabled = enabled)
            }
        }
        item("history-caption") { SettingsCaption(stringResource(R.string.settings_src_display_history_caption), Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s)) }
        policy.braveClimb?.let { climb ->
            item("climb") {
                JGroupItem(GroupPosition.Single) {
                    JSwitchRow(stringResource(R.string.settings_src_display_climb), climb, { set("brave_climb_codec", it) }, enabled = enabled)
                }
            }
            item("climb-caption") { SettingsCaption(stringResource(R.string.settings_src_display_climb_caption), Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s)) }
        }
        if (loading || saving) item("busy") { JLoading(text = stringResource(if (saving) R.string.settings_src_display_saving else R.string.settings_src_display_reading)) }
        problem?.let { p ->
            item("problem") {
                JNotice(p, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou,
                    actionLabel = stringResource(R.string.settings_src_display_reload), onAction = ::load)
            }
        }
        item("footer") { SettingsCaption(stringResource(R.string.settings_src_display_footer), Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s)) }
    }
}
