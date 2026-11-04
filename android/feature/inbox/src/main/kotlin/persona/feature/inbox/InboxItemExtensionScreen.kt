package persona.feature.inbox

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.rememberScrollState
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.designsystem.*
import persona.core.domain.inbox.InboxTier
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.InboxItemUiRoute
import persona.core.navigation.Navigator
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** The item's tier for its header (the extension's body does not repeat it); none before triage. */
internal fun inboxHeaderTier(record: InboxRecord): InboxTier? = record.item.triage.takeIf { record.item.isTriaged }

/**
 * The words of an item's source line, as its row in the list says them: where it comes from (the account's name when
 * there are several), what kind it is, how many messages.
 */
internal fun inboxSourceTags(record: InboxRecord): List<String> = listOfNotNull(
    record.sourceLabel.takeIf { it.isNotBlank() } ?: if (record.isLark) "飞书" else "邮件",
    kindLabel(record).takeIf { it.isNotBlank() },
    "${record.count} 条消息".takeIf { record.count > 0 },
).distinct()

/**
 * An inbox item whose body its connector's extension draws (inbox_item, #1683): the app's own header of the item (its
 * title, tier and source line, 已处理, AI 摘要, AI 草拟 / 交给 AI and the AI 分身's draft), then [body], the extension's
 * view, which does not repeat them.
 * The shell builds the body (feature:settings draws extensions' views) and opens the item's own page instead when the
 * extension cannot draw it. [title] is the view's, used only when the item has none; [loading] is the body's first read.
 */
@Composable
fun InboxItemExtensionScreen(
    repository: InboxRepository,
    route: InboxItemUiRoute,
    navigator: Navigator,
    title: String?,
    loading: Boolean,
    refreshing: Boolean,
    onRefresh: () -> Unit,
    body: LazyListScope.() -> Unit,
) {
    val inbox by repository.snapshot.collectAsStateWithLifecycle()
    val record = inbox.records.firstOrNull { it.item.id == route.itemId }
    val feedback = LocalAppFeedback.current
    val scope = rememberCoroutineScope()
    val composers = rememberReplyComposers(repository)
    var drafting by remember { mutableStateOf(false) }
    var handingOff by remember { mutableStateOf(false) }
    var markingDone by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val heading = record?.item?.title?.takeIf { it.isNotBlank() } ?: title?.takeIf { it.isNotBlank() } ?: route.title.ifBlank { "事项" }
    JListPage(title = heading, onBack = navigator::back, largeTitle = false, refreshing = refreshing, onRefresh = onRefresh,
        actions = {
            if (record != null) JIconButton(PhIcons.CheckCircle, if (record.item.isDone) "移回收件箱" else "已处理", enabled = !markingDone,
                onClick = {
                    markingDone = true
                    scope.launch {
                        try { repository.setDone(record.item.id, !record.item.isDone); error = null; navigator.back() }
                        catch (e: CancellationException) { throw e }
                        catch (e: Exception) { if (feedback?.failure(e, operation = true) != true) error = "操作失败，请重试" }
                        finally { markingDone = false }
                    }
                })
        }) {
        if (record != null) {
            item("item-source") {
                Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(bottom = Tokens.Space.s),
                    horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                    inboxHeaderTier(record)?.let { JTag(it.title, status = if (it == InboxTier.Action && !record.item.isDone) Status.NeedsYou else null) }
                    inboxSourceTags(record).forEach { JTag(it) }
                }
            }
            record.item.summary?.takeIf { it.isNotBlank() }?.let { summary ->
                item("item-summary") { JNotice(summary, Modifier.padding(bottom = Tokens.Space.s), title = "AI 摘要", icon = PhIcons.Sparkle) }
            }
            item("item-ai") { InboxAiActions(onDraft = { drafting = true }, onHandOff = { handingOff = true }) }
            if (!record.item.isDone) item("item-agent-draft") {
                AgentDraftCard(record, composers, navigator, repository = repository, onEdit = { drafting = true }, keepComposer = true)
            }
        }
        error?.let { message ->
            item("item-error") { JNotice(message, Modifier.padding(top = Tokens.Space.s), status = Status.NeedsYou) }
        }
        item("item-gap") { Spacer(Modifier.padding(top = Tokens.Space.m)) }
        if (loading) item("item-body-loading") { JLoading() }
        body()
        item("item-end") { Spacer(Modifier.padding(top = Tokens.Space.xxl)) }
    }
    if (drafting && record != null) InboxDraftSheet(repository, record,
        composers.get(ReplyTarget.Inbox(record.item.id), record.agent?.takeIf { it.state == "drafted" }?.draft.orEmpty()),
        navigator, onDismiss = { drafting = false })
    if (handingOff && record != null) InboxHandOffSheet(repository, record, navigator, onDismiss = { handingOff = false })
}
