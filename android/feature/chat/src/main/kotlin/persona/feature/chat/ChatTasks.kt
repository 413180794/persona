package persona.feature.chat

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import persona.core.chatlist.ChatListState
import persona.core.chatlist.beginRecordNavigation
import persona.core.chatlist.scrollToRecord
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.TaskIndex
import persona.core.domain.lines.TaskProgressItem
import persona.core.domain.lines.TaskProgressStatus
import persona.core.contracts.Item

private data class TaskReturnPosition(val rowId: String, val offset: Int, val expanded: Set<String>)

/** One chat-header attachment owns the full page and its return affordance. */
@Composable
internal fun ChatTasks(
    index: TaskIndex,
    lines: List<ChatLine>,
    chatList: ChatListState,
    onFreeze: () -> Unit,
    onVisibilityChanged: (Boolean) -> Unit,
    onExpandFold: (Item) -> Unit,
    foldErrors: Map<String, String>,
) {
    var open by rememberSaveable { mutableStateOf(false) }
    var target by remember { mutableStateOf<TaskProgressItem?>(null) }
    var origin by remember { mutableStateOf<TaskReturnPosition?>(null) }
    var returning by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    val visible = open || target != null || returning
    val visibilityChanged by rememberUpdatedState(onVisibilityChanged)
    LaunchedEffect(visible) { visibilityChanged(visible) }
    DisposableEffect(Unit) { onDispose { visibilityChanged(false) } }

    LaunchedEffect(target, lines, returning, foldErrors) {
        if (returning) {
            val saved = origin ?: return@LaunchedEffect
            if (chatList.position.expanded != saved.expanded) {
                chatList.setExpanded(saved.expanded)
                return@LaunchedEffect
            }
            if (!chatList.scrollToRecord(saved.rowId, saved.offset)) notice = "原来的记录已不在当前列表中"
            returning = false
            origin = null
        } else {
            val task = target ?: return@LaunchedEffect
            val destination = taskRecordTarget(lines, task.sourceItemId, task.sourceSeq)
            when {
                destination?.expandFold != null -> {
                    if (destination.expandFold.id in foldErrors) {
                        notice = "相关记录载入失败，可展开工作过程后重试"
                        target = null
                    } else onExpandFold(destination.expandFold)
                }
                destination?.rowId != null -> {
                    if (!chatList.scrollToRecord(destination.rowId)) notice = "相关记录已不在当前列表中"
                    target = null
                }
                destination?.expandGroup != null -> chatList.setExpanded(chatList.position.expanded + destination.expandGroup)
                else -> { notice = "相关记录已不在当前列表中"; target = null }
            }
        }
    }
    if (index.progress.isNotEmpty()) TaskProgressBar(index.progress) { open = true }
    if (origin != null) {
        JTextButton("返回查看任务前的位置", { target = null; returning = true },
            enabled = !returning, icon = PhIcons.ArrowLeft,
            modifier = Modifier.padding(horizontal = Tokens.Space.m))
    }
    notice?.let { Text(it, Modifier.padding(horizontal = Tokens.Space.m), style = PersonaTheme.type.caption,
        color = PersonaTheme.palette.ink2) }

    if (open) JFullScreen({ open = false }) {
        TaskProgressPage(index.progress, onBack = { open = false }, onRecord = { task ->
            if (origin == null) {
                val row = chatList.list.layoutInfo.visibleItemsInfo.firstOrNull { it.index == chatList.list.firstVisibleItemIndex }
                (row?.key as? String)?.let { origin = TaskReturnPosition(it,
                    chatList.list.firstVisibleItemScrollOffset, chatList.position.expanded) }
            }
            chatList.beginRecordNavigation()
            onFreeze()
            notice = null
            target = task
            open = false
        })
    }
}

@Composable
private fun TaskProgressBar(tasks: List<TaskProgressItem>, onClick: () -> Unit) {
    val completed = tasks.count { it.status == TaskProgressStatus.Completed }
    val active = tasks.count { it.status == TaskProgressStatus.InProgress }
    Row(Modifier.fillMaxWidth().padding(horizontal = Tokens.Space.m), verticalAlignment = Alignment.CenterVertically) {
        JChip("$completed/${tasks.size} 已完成，$active 个进行中", Modifier.weight(1f),
            onClick = onClick, icon = PhIcons.ListChecks)
        JIconButton(PhIcons.CaretRight, "查看全部任务", onClick)
    }
}

@Composable
internal fun TaskProgressPage(tasks: List<TaskProgressItem>, onBack: () -> Unit, onRecord: (TaskProgressItem) -> Unit) {
    JListPage(title = "任务进度", onBack = onBack) {
        item {
            val completed = tasks.count { it.status == TaskProgressStatus.Completed }
            Text("$completed/${tasks.size} 已完成", Modifier.padding(vertical = Tokens.Space.m),
                style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
        }
        if (tasks.isEmpty()) item { JEmptyState("暂无任务", "已加载的记录里还没有任务清单。") }
        itemsIndexed(tasks, key = { _, task -> task.id }) { index, task ->
            JGroupItem(GroupPosition.of(index, tasks.size)) { TaskProgressRow(task) { onRecord(task) } }
        }
        item {
            Text("根据已加载的记录整理，可能和电脑上此刻不同", Modifier.padding(vertical = Tokens.Space.m),
                style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        }
    }
}

@Composable
private fun TaskProgressRow(task: TaskProgressItem, onRecord: () -> Unit) {
    val status = when (task.status) {
        TaskProgressStatus.Completed -> Status.Unread
        TaskProgressStatus.InProgress -> Status.Working
        TaskProgressStatus.Pending -> Status.Idle
        TaskProgressStatus.Unknown -> Status.Unknown
    }
    val label = when (task.status) {
        TaskProgressStatus.Pending -> "待开始"
        TaskProgressStatus.InProgress -> "进行中"
        TaskProgressStatus.Completed -> "已完成"
        TaskProgressStatus.Unknown -> "无结果记录"
    }
    JRow(task.subject, subtitle = task.description?.takeIf(String::isNotBlank),
        leading = { JStatusMark(status) }, value = label, subtitleLines = Int.MAX_VALUE)
    JTextButton("查看相关记录", onRecord, Modifier.padding(horizontal = Tokens.Space.s))
}
