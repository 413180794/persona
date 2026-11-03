package persona.feature.chat

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.BackgroundTask
import persona.core.contracts.LoopInfo
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.DeliveryState
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.resultUnknown
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

@Composable
internal fun ChatStatusSheet(
    state: StatusLineState, session: Session?, messages: List<OutboxMessage>, busyIds: Set<String>,
    onAction: ((OutboxMessage, DeliveryAction) -> Unit)?, onDismiss: () -> Unit,
    replacement: Session?, onOpenReplacement: (() -> Unit)?,
    onConnectionDetails: (() -> Unit)?, terminal: (() -> Unit)?,
    recordedDuration: Long?, notice: String?,
    recovery: ChatStatusRecovery, machine: Machine?, draftSaveError: String?, readOnly: Boolean,
    onOpenSession: ((SessionKey) -> Unit)?,
) {
    val clipboard = LocalClipboardManager.current
    JSheet(onDismiss = onDismiss, title = "会话状态") {
        LazyColumn(contentPadding = PaddingValues(bottom = Tokens.Space.m),
            verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            item {
                StatusLine(state)
                recordedDuration?.takeIf { it >= 0 }?.let {
                    Text("本轮用时 ${it / 60} 分 ${it % 60} 秒", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                }
                if (notice != null) Text(notice, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                recovery.notice?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
            }
            if (draftSaveError != null) item {
                JGroupItem(GroupPosition.Single) {
                    Column(Modifier.fillMaxWidth().padding(Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        JRow("草稿保存失败", leadingIcon = PhIcons.WarningCircle)
                        Text(draftSaveError.ifBlank { "未能写入本机存储。" }, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink)
                        Text("请先复制输入内容，避免离开后丢失。", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                    }
                }
            }
            if (state.ended || state.replaced || state.machineOffline) item {
                JGroupItem(GroupPosition.Single) {
                    Column(Modifier.fillMaxWidth().padding(Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        val updated = if (state.machineOffline) machine?.lastSeen else session?.updatedAt
                        Text(updated?.takeIf(String::isNotBlank)?.let { "上次更新：$it" } ?: "暂无上次更新时间",
                            style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                        if (replacement != null && onOpenReplacement != null)
                            JButton("打开新会话", onOpenReplacement, enabled = !recovery.busy)
                        if ((state.ended || state.replaced) && session != null) {
                            if (!readOnly && !session.assistant) {
                                val restored = recovery.restored
                                if (restored != null && onOpenSession != null)
                                    JButton("打开已恢复的会话", { onOpenSession(restored) })
                                else if (restored == null) JButton(
                                    if (recovery.resumeUnknown) "核对恢复结果" else "恢复会话",
                                    { recovery.resume(session) },
                                    enabled = recovery.available && machine?.online == true &&
                                        (recovery.resumeUnknown || (session.cwd.isNotBlank() && !session.launcher.isNullOrBlank())),
                                    loading = recovery.busy)
                                if (!recovery.available) Text("恢复服务暂不可用，可复制命令到电脑上运行。", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                                else if (session.launcher.isNullOrBlank() || session.cwd.isBlank())
                                    Text("缺少原启动器或目录，请到电脑上恢复。", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                            }
                            StatusSecondaryButton("复制恢复命令", { clipboard.setText(AnnotatedString(resumeCommand(session))) })
                        }
                        if (state.machineOffline) {
                            Text("电脑恢复连接后会自动更新状态。", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                            if (!readOnly) StatusSecondaryButton(
                                if (recovery.reconnectRequested) "刷新电脑状态" else "重连这台电脑",
                                { recovery.reconnect(session?.machineId ?: machine!!.id) },
                                enabled = recovery.available && (session != null || machine != null), loading = recovery.busy)
                            if (!readOnly && !recovery.available) Text("重连服务暂不可用。", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                            if (onConnectionDetails != null) StatusSecondaryButton("连接诊断", onConnectionDetails)
                        }
                    }
                }
            }
            items(messages, key = { it.clientMessageId }) { message ->
                JGroupItem(GroupPosition.Single) {
                    Column(Modifier.fillMaxWidth().padding(Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        Text(composerMessagePreview(message.text), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink)
                        OutboxStatus(message, message.clientMessageId in busyIds,
                            onAction = onAction?.let { action -> { choice -> action(message, choice) } })
                        if (message.state.resultUnknown) {
                            Text(when (message.state) {
                                DeliveryState.Verifying -> "正在自动核对，在对话记录里查找这条消息。"
                                DeliveryState.NeedsConfirmation -> "自动核对未能确认是否送达，请核对电脑上的对话记录。"
                                else -> "尚未确认是否送达，请先核对对话记录。"
                            }, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                            Text("没把握时先不要重发，避免电脑收到两遍。", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                            if (onAction != null) {
                                StatusSecondaryButton("确认已送达", { onAction(message, DeliveryAction.ConfirmDelivered) },
                                    Modifier.fillMaxWidth(), enabled = message.clientMessageId !in busyIds)
                                StatusSecondaryButton("确认没送达，保留输入", { onAction(message, DeliveryAction.ConfirmNotDelivered) },
                                    Modifier.fillMaxWidth(), enabled = message.clientMessageId !in busyIds)
                            }
                        }
                    }
                }
            }
            val queued = session?.queued.orEmpty()
            if (queued.isNotEmpty()) item { JSectionHeader("排队中", count = queued.size) }
            itemsIndexed(queued) { index, message ->
                JGroupItem(GroupPosition.of(index, queued.size)) {
                    JRow(message, leadingIcon = PhIcons.Clock, subtitle = "等待处理")
                }
            }
            session?.loop?.let { loop -> item {
                JGroupItem(GroupPosition.Single) {
                    JRow("循环任务", subtitle = loopSummary(loop).replace(" · ", "，"), leadingIcon = PhIcons.ArrowsClockwise)
                    loop.prompt?.takeIf { it.isNotBlank() }?.let {
                        Text(it, Modifier.padding(Tokens.Space.m), style = PersonaTheme.type.callout,
                            color = PersonaTheme.palette.ink, maxLines = 3, overflow = TextOverflow.Ellipsis)
                    }
                }
            } }
            val tasks = session?.background.orEmpty()
            if (tasks.isNotEmpty()) item { JSectionHeader("后台任务", count = tasks.size) }
            itemsIndexed(tasks) { index, task ->
                JGroupItem(GroupPosition.of(index, tasks.size)) {
                    JRow(backgroundTitle(task), leadingIcon = PhIcons.ListChecks)
                }
            }
            item {
                if (terminal != null) StatusSecondaryButton("打开终端", terminal)
                StatusSecondaryButton("关闭", onDismiss)
            }
        }
    }
}

/** "循环 · 每 30 分钟 · 下次 14:05": the cadence the hub reports, in the phone's time zone. */
internal fun loopSummary(loop: LoopInfo, zone: java.time.ZoneId = java.time.ZoneId.systemDefault()): String {
    val every = loop.intervalSeconds?.takeIf { it > 0 }?.let { seconds ->
        when {
            seconds % 3600 == 0 -> "每 ${seconds / 3600} 小时"
            seconds % 60 == 0 -> "每 ${seconds / 60} 分钟"
            else -> "每 $seconds 秒"
        }
    } ?: loop.cron?.takeIf { it.isNotBlank() }?.let { "按计划 $it" }
    val next = loop.nextAt?.let { runCatching { java.time.Instant.parse(it) }.getOrNull() }
        ?.let { "下次 " + java.time.format.DateTimeFormatter.ofPattern("HH:mm").format(it.atZone(zone)) }
    return listOfNotNull("循环", every, next).joinToString(" · ")
}

/** A background task by what it is for; the command's first line when it has no description. */
internal fun backgroundTitle(task: BackgroundTask): String =
    task.description?.takeIf { it.isNotBlank() }
        ?: task.command?.lineSequence()?.firstOrNull { it.isNotBlank() }?.trim()
        ?: task.id.ifBlank { "后台任务" }

@Composable
private fun StatusSecondaryButton(
    text: String, onClick: () -> Unit, modifier: Modifier = Modifier,
    enabled: Boolean = true, loading: Boolean = false,
) = JButton(text, onClick, modifier, kind = JButtonKind.Secondary, enabled = enabled, loading = loading)
