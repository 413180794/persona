package persona.feature.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
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
        JListPage(title = "会话分诊", onBack = onBack) { item { JNotice("体验模式不分诊会话。") } }
        return
    }
    val scope = rememberCoroutineScope()
    var switches by remember { mutableStateOf<TriageSwitches?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var model by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(triage) {
        switches = try { triage.switches() } catch (e: CancellationException) { throw e } catch (e: Exception) {
            error = e.message ?: "读不到 hub 上的设置"; null
        }
    }
    LaunchedEffect(models) {
        model = try { models?.purpose("agent.triage")?.let { d -> d.purpose.summaryParts(d.providers).joinToString("，").ifEmpty { "未配置" } } }
        catch (e: CancellationException) { throw e } catch (_: Exception) { null }
    }
    fun change(next: TriageSwitches) {
        val before = switches
        switches = next
        busy = true
        scope.launch {
            try { switches = triage.put(next); error = null }
            catch (e: CancellationException) { throw e }
            catch (e: Exception) { switches = before; error = "没改成：${e.message ?: "hub 没有回应"}" }
            finally { busy = false }
        }
    }
    val sw = switches
    val on = sw?.enabled == true
    JListPage(title = "会话分诊", onBack = onBack) {
        error?.let { message -> item("error") { JNotice(message, status = Status.NeedsYou) } }
        item("enabled") {
            JGroupItem(GroupPosition.of(0, 2), Modifier.padding(top = Tokens.Space.m)) {
                JSwitchRow("会话分诊", on, { v -> sw?.let { change(it.copy(enabled = v)) } }, Modifier.testTag("triage-enabled"),
                    leadingIcon = PhIcons.ListChecks, enabled = sw != null && !busy)
            }
        }
        item("model") {
            JGroupItem(GroupPosition.of(1, 2)) {
                JRow("分诊模型", leadingIcon = PhIcons.Brain, subtitle = model, subtitleLines = 1, onClick = onModel)
            }
        }
        item("about") {
            SettingsCaption("Agent 每停下一轮，hub 用分诊模型读它最后的回复（每轮一次），在列表里写出做完了什么或在等什么，做完的不再算完成未看。",
                Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
        }
        item("actions-header") { JSectionHeader("hub 替你做") }
        item("decide") {
            JGroupItem(GroupPosition.of(0, 3)) {
                JSwitchRow("要你决定的开卡片", sw?.decide == true, { v -> sw?.let { change(it.copy(decide = v)) } },
                    Modifier.testTag("triage-decide"), leadingIcon = PhIcons.Question, enabled = on && !busy)
            }
        }
        item("continue") {
            JGroupItem(GroupPosition.of(1, 3)) {
                JSwitchRow("出错自动继续", sw?.autoContinue == true, { v -> sw?.let { change(it.copy(autoContinue = v)) } },
                    Modifier.testTag("triage-continue"), leadingIcon = PhIcons.ArrowsClockwise, enabled = on && !busy)
            }
        }
        item("wake") {
            JGroupItem(GroupPosition.of(2, 3)) {
                JSwitchRow("到点叫醒", sw?.wake == true, { v -> sw?.let { change(it.copy(wake = v)) } },
                    Modifier.testTag("triage-wake"), leadingIcon = PhIcons.ClockCountdown, enabled = on && !busy)
            }
        }
        item("actions-about") {
            SettingsCaption("模型服务临时出错（503 等）后自动发「继续」，最多 3 次；在等部署、CI 或另一个 Agent 的，到点叫醒它自己去查。这两项会让 Agent 用它自己的模型再跑一轮。推代码、部署、给别人发消息这类事 hub 只会问你，不会自己做。",
                Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
        }
    }
}
