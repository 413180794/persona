package persona.feature.agents

import persona.core.designsystem.*
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import persona.core.designsystem.icons.PhIcons
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.unit.dp
import persona.core.contracts.Session
import persona.core.domain.sort.SessionRename
import persona.core.domain.sort.closeConfirmation
import persona.core.domain.sort.deleteConfirmation

internal sealed interface AgentOperationDialog {
    val session: Session
    data class Rename(override val session: Session) : AgentOperationDialog
    data class RenameSpace(override val session: Session) : AgentOperationDialog
    data class Close(override val session: Session) : AgentOperationDialog
    data class Delete(override val session: Session) : AgentOperationDialog
}

@Composable
internal fun AgentOperationDialog(dialog: AgentOperationDialog, model: AgentsViewModel, machineName: String? = null, dismiss: () -> Unit) {
    val session = dialog.session
    when (dialog) {
        // The same words as the chat's menu (core:domain closeConfirmation); long press and swipe both land here.
        is AgentOperationDialog.Close -> {
            val confirmation = session.closeConfirmation(machineName)
            JDialog(
                onDismiss = dismiss, title = confirmation.title, destructive = true, text = confirmation.message,
                confirm = "关闭 Agent" to {
                    // It may have been closed elsewhere since the dialog opened: then there is nothing left to do.
                    if (model.closable(session)) model.close(session)
                    dismiss()
                },
            )
        }
        // The same words as the chat's menu (core:domain deleteConfirmation).
        is AgentOperationDialog.Delete -> {
            val confirmation = session.deleteConfirmation(machineName)
            JDialog(
                onDismiss = dismiss, title = confirmation.title, destructive = true, text = confirmation.message,
                confirm = "删除" to {
                    // It may have started again since the dialog opened: then the hub would refuse it.
                    if (model.deletable(session)) model.delete(session)
                    dismiss()
                },
            )
        }
        else -> {
            val space = dialog is AgentOperationDialog.RenameSpace
            var name by rememberSaveable(session.key.path, space) { mutableStateOf(if (space) session.spaceName.orEmpty() else session.title) }
            var tab by rememberSaveable(session.key.path) { mutableStateOf(session.slotName.orEmpty()) }
            val hasTab = !space && !session.paneId.isNullOrBlank()
            val newName = name.trim().takeIf { it.isNotBlank() && it != (if (space) session.spaceName else session.title) }
            val newTab = SessionRename.changes(session, name, tab).tabName
            JDialog(
                onDismiss = dismiss, title = if (space) "重命名空间" else "重命名",
                text = if (space) "同时改 herdr 里这个 workspace 的名字。"
                    else "标签名同步到 herdr；会话名用 Claude 的 /rename，Mac 上 claude --resume 也看得到。",
                confirmEnabled = newName != null || newTab != null,
                confirm = "保存" to {
                    if (space) model.renameSpace(session, requireNotNull(newName)) else model.rename(session, newName, newTab)
                    dismiss()
                }, content = {
                    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        if (hasTab) JTextField(tab, { tab = it }, singleLine = true, label = "标签名（herdr tab）")
                        JTextField(name, { name = it }, singleLine = true, label = if (space) "空间名" else "会话名")
                    }
                },
            )
        }
    }
}

/**
 * The actions of a space. [onOpen] adds 打开空间 (the list offers it here instead of a separate button);
 * [compact] is the smaller trigger of list headings. A space without a herdr name can only be opened and read.
 */
@Composable
internal fun AgentSpaceMenu(session: Session, pinned: Boolean, muted: Boolean, busy: Boolean,
    onPin: () -> Unit, onMute: () -> Unit, onRename: () -> Unit, onMarkRead: (() -> Unit)? = null,
    onOpen: (() -> Unit)? = null, compact: Boolean = false) {
    val named = !session.spaceName.isNullOrBlank()
    if (!named && onOpen == null && onMarkRead == null) return
    var menu by remember { mutableStateOf(false) }
    if (compact) HeadingAction(PhIcons.DotsThree, "空间操作", onClick = { menu = true })
    else JIconButton(PhIcons.DotsThree, "空间操作", { menu = true })
    if (menu) JActionSheet(buildList {
        if (onOpen != null) add(JAction("打开空间", PhIcons.SquaresFour, onClick = onOpen))
        if (onMarkRead != null) add(JAction("全部标为已读", PhIcons.Check, onClick = onMarkRead))
        if (named) {
            add(JAction(if (pinned) "取消置顶空间" else "置顶空间", PhIcons.PushPin, enabled = !busy, onClick = onPin))
            add(JAction(if (muted) "取消屏蔽空间" else "屏蔽空间", PhIcons.Prohibit, enabled = !busy, onClick = onMute))
            add(JAction("重命名空间", PhIcons.NotePencil, enabled = !busy && session.operationWorkspaceId != null, onClick = onRename))
        }
    }, { menu = false }, title = session.spaceName?.takeIf { it.isNotBlank() } ?: "空间操作")
}
