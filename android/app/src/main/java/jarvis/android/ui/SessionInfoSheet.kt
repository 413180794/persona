package jarvis.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.media.MediaShare

/**
 * 会话信息 (iOS SessionInfoSheet): who and where at the top, then what it runs, how far it has got and where its
 * files are (GET …/info); copying the session id or the resume command last.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SessionInfoSheet(hub: Hub, key: SessionKey, session: Session?, machineName: String?, onDismiss: () -> Unit) {
    val ctx = LocalContext.current
    var info by remember { mutableStateOf<ApiClient.SessionInfo?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(key) { runCatching { hub.api.sessionInfo(key) }.onSuccess { info = it }.onFailure { error = it.message ?: "读不到会话信息" } }
    fun copy(text: String, toast: String) { MediaShare.copyText(ctx, text, "会话"); Toasts.show(toast, ToastStyle.SUCCESS) }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(
            Modifier.navigationBarsPadding().padding(horizontal = 20.dp).padding(bottom = 16.dp).verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(session?.displayTitle ?: "会话", style = MaterialTheme.typography.titleLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(
                listOfNotNull(session?.agentState?.label(), machineName ?: key.machineId, session?.spaceName?.takeIf { it.isNotBlank() })
                    .joinToString(" · "),
                style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            val i = info
            when {
                i != null -> {
                    Group("运行")
                    Field("模型", i.model)
                    Field("启动器", i.launcher ?: session?.launcher)
                    Field(agentName(session), i.version)
                    Field("权限", SessionModes.modeLabel(i.permissionMode))
                    Field("思考强度", SessionModes.effortLabel(i.effort))
                    Group("用量")
                    Field("上下文", tokens(i.contextTokens) + " tokens")
                    if (i.compactions > 0) Field("压缩", "${i.compactions} 次")
                    Field("对话", "${i.turns} 轮")
                    Field("共生成", tokens(i.outputTokens) + " tokens")
                    jarvis.android.data.parseTime(i.startedAt)?.let { t ->
                        Field("开始", java.time.format.DateTimeFormatter.ofPattern("M月d日 HH:mm").format(t.atZone(java.time.ZoneId.systemDefault())))
                    }
                    Group("位置")
                    Field("目录", i.cwd ?: session?.cwd, mono = true)
                    i.gitBranch?.takeIf { it.isNotBlank() }?.let { Field("分支", it, mono = true) }
                    HorizontalDivider(Modifier.padding(vertical = 8.dp))
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedButton({ copy(i.sessionId, "已拷贝 session id") }) { Text("拷贝 session id", maxLines = 1) }
                        if (session?.canTerminal == true) OutlinedButton({ copy(resumeCommand(session, i.cwd ?: session.cwd, session.sourceThreadId ?: i.sessionId), "已拷贝恢复命令") }) {
                            Text("拷贝恢复命令", maxLines = 1)
                        }
                    }
                    SelectionContainer {
                        Text(i.sessionId, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                error != null -> Text(error!!, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                else -> CircularProgressIndicator(Modifier.padding(24.dp).align(Alignment.CenterHorizontally))
            }
        }
    }
}

@Composable
private fun Group(title: String) {
    Text(title, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(top = 12.dp))
}

@Composable
private fun Field(label: String, value: String?, mono: Boolean = false) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.Top) {
        Text(label, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.width(16.dp))
        SelectionContainer(Modifier.weight(1f)) {
            Text(
                value?.takeIf { it.isNotBlank() } ?: "—", style = MaterialTheme.typography.bodyMedium, textAlign = TextAlign.End,
                fontFamily = if (mono) FontFamily.Monospace else null, maxLines = 2, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
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
