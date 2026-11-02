package jarvis.android.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.DOT_SEP
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.GroupRow
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PageLoading
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.media.MediaShare

/**
 * 会话信息 (iOS SessionInfoSheet): who and where at the top, then what it runs, how far it has got and where its
 * files are (GET …/info); copying the session id or the resume command last.
 */
@Composable
fun SessionInfoSheet(
    hub: Hub, key: SessionKey, session: Session?, machineName: String?, onDismiss: () -> Unit,
    onOpenTodo: ((String) -> Unit)? = null,
) {
    val ctx = LocalContext.current
    var info by remember { mutableStateOf<ApiClient.SessionInfo?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(key) { runCatching { hub.api.sessionInfo(key) }.onSuccess { info = it }.onFailure { error = it.message ?: "读不到会话信息" } }
    fun copy(text: String, toast: String) { MediaShare.copyText(ctx, text, "会话"); Toasts.show(toast, ToastStyle.SUCCESS) }

    JSheet(onDismiss, title = "会话信息", onClose = onDismiss) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            Column(Modifier.fillMaxWidth().padding(horizontal = JSpace.page + JSpace.xs, vertical = JSpace.xs), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
                Text(session?.displayTitle ?: "会话", style = JTheme.type.title, color = JTheme.colors.label, maxLines = 2, overflow = TextOverflow.Ellipsis)
                Text(
                    listOfNotNull(session?.agentState?.label(), machineName ?: key.machineId, session?.spaceName?.takeIf { it.isNotBlank() })
                        .joinToString(DOT_SEP),
                    style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis,
                )
            }
            session?.todo?.let { todo ->
                SectionHeader("待办")
                GroupCard {
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable { onOpenTodo?.invoke(todo.id); onDismiss() }
                            .padding(horizontal = JSpace.l, vertical = JSpace.m),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Icon(JIcons.todo, null, Modifier.size(JSize.iconS), tint = JTheme.colors.secondaryText)
                        Spacer(Modifier.width(JSpace.m))
                        Column(Modifier.weight(1f)) {
                            Text("待办", style = JTheme.type.caption, color = JTheme.colors.secondaryText)
                            Text(
                                todo.title,
                                style = JTheme.type.bodyStrong,
                                color = JTheme.colors.label,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis
                            )
                        }
                        Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = JTheme.colors.tertiaryText)
                    }
                }
            }
            val i = info
            when {
                i != null -> {
                    SectionHeader("运行")
                    GroupCard {
                        Field("模型", i.model); RowDivider()
                        Field("启动器", i.launcher ?: session?.launcher); RowDivider()
                        Field(agentName(session), i.version); RowDivider()
                        Field("权限", SessionModes.modeLabel(i.permissionMode)); RowDivider()
                        Field("思考强度", SessionModes.effortLabel(i.effort))
                    }
                    SectionHeader("用量")
                    GroupCard {
                        Field("上下文", tokens(i.contextTokens) + " tokens")
                        if (i.compactions > 0) { RowDivider(); Field("压缩", "${i.compactions} 次") }
                        RowDivider(); Field("对话", "${i.turns} 轮")
                        RowDivider(); Field("共生成", tokens(i.outputTokens) + " tokens")
                        jarvis.android.data.parseTime(i.startedAt)?.let { t ->
                            RowDivider()
                            Field("开始", java.time.format.DateTimeFormatter.ofPattern("M月d日 HH:mm").format(t.atZone(java.time.ZoneId.systemDefault())))
                        }
                    }
                    SectionHeader("位置")
                    GroupCard {
                        Field("目录", i.cwd ?: session?.cwd, mono = true)
                        i.gitBranch?.takeIf { it.isNotBlank() }?.let { RowDivider(); Field("分支", it, mono = true) }
                    }
                    SectionHeader("拷贝")
                    GroupCard {
                        CopyRow("session id", i.sessionId) { copy(i.sessionId, "已拷贝 session id") }
                        // Only a session in a terminal can be resumed there (not an agent runtime's).
                        if (session?.canTerminal == true) {
                            RowDivider()
                            val resume = resumeCommand(session, i.cwd ?: session.cwd, session.sourceThreadId ?: i.sessionId)
                            CopyRow("恢复命令", resume) { copy(resume, "已拷贝恢复命令") }
                        }
                    }
                }
                error != null -> ErrorLine(error!!, Modifier.padding(JSpace.page), centered = true)
                else -> PageLoading(Modifier.fillMaxWidth().padding(vertical = JSpace.xxl))
            }
        }
    }
}

/** A fact: its name, the value at the end (selectable; code-like ones in the code face). */
@Composable
private fun Field(label: String, value: String?, mono: Boolean = false) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.tapTarget).padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.l),
    ) {
        Text(label, style = JTheme.type.body, color = JTheme.colors.label, maxLines = 1)
        SelectionContainer(Modifier.weight(1f)) {
            Text(
                value?.takeIf { it.isNotBlank() } ?: "—", style = if (mono) JTheme.type.code else JTheme.type.body, color = JTheme.colors.secondaryText,
                textAlign = TextAlign.End, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

/** Something to copy: what it is, the text itself under it, a tap copies it. */
@Composable
private fun CopyRow(title: String, text: String, onCopy: () -> Unit) {
    GroupRow(title, subtitle = text, onClick = onCopy, chevron = false,
        trailing = { Icon(JIcons.copy, "拷贝", Modifier.size(JSize.iconXS), tint = JTheme.colors.accent) })
}

/** The coding agent in the pane: Claude Code unless the session says Codex / TRAEX. */
private fun agentName(s: Session?) = if (s?.agent == "codex" || s?.agent == "traex") s.agentLabel else "Claude Code"

/** What brings this conversation back in a terminal on that computer (iOS SessionInfoSheet.resume). */
internal fun resumeCommand(s: Session?, cwd: String, id: String): String {
    val cmd = when {
        s?.isCodex == true -> "codex resume $id"
        s?.agent == "traex" -> "traex resume $id"
        else -> "claude --resume $id"
    }
    return "cd ${jarvis.android.data.quotedPath(cwd)} && $cmd"
}

/** 950, 12.3k, 1.20M (iOS SessionInfoSheet.tokens). */
internal fun tokens(n: Long): String = when {
    n >= 1_000_000 -> "%.2fM".format(n / 1_000_000.0)
    n >= 1_000 -> "%.1fk".format(n / 1_000.0)
    else -> "$n"
}
