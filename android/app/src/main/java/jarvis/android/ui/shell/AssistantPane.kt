package jarvis.android.ui.shell

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import jarvis.android.data.Hub
import jarvis.android.data.InboxBucket
import jarvis.android.data.InboxItem
import jarvis.android.data.Interaction
import jarvis.android.data.SessionKey
import jarvis.android.ui.ChatScreen
import jarvis.android.ui.FocusComposer
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ComposerActions
import jarvis.android.ui.chat.ShellComposer
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.interactionIcon

/**
 * The 助手 page of the shell: the conversation on screen ([AssistantModel.current]), or — for a new one — the empty
 * page with only the composer and what needs you over it ([ShellHome]).
 */
@Composable
fun AssistantPane(
    hub: Hub, model: AssistantModel, focusComposer: Boolean, onFocusTaken: () -> Unit,
    onOpenSession: (SessionKey) -> Unit, onOpenInbox: () -> Unit, onError: (String) -> Unit,
    onVoice: (() -> Unit)? = null,
) {
    val current = model.current
    if (current != null && model.pending == null && model.machine.isNotEmpty()) {
        ChatScreen(
            hub, SessionKey(model.machine, current), onBack = {}, assistant = true, inShell = true,
            focusComposer = focusComposer, onFocusTaken = onFocusTaken, onVoice = onVoice,
        )
    } else {
        ShellHome(hub, model, focusComposer = focusComposer && model.state != null, onFocusTaken = onFocusTaken,
            onOpenSession = onOpenSession, onOpenInbox = onOpenInbox, onVoice = onVoice)
    }
}

/**
 * A new conversation (iOS `ShellHome`, like ChatGPT's empty chat): nothing but the composer, and over it what needs
 * you right now — the agents waiting on you, then the inbox's 需要你 — one quiet line each. Nothing to do, nothing
 * shown. While the conversation spins up, its first message and a typing line, so sending feels instant.
 */
@Composable
fun ShellHome(
    hub: Hub, model: AssistantModel, focusComposer: Boolean, onFocusTaken: () -> Unit,
    onOpenSession: (SessionKey) -> Unit, onOpenInbox: () -> Unit, onVoice: (() -> Unit)?,
) {
    val focus = remember { FocusRequester() }
    FocusComposer(focusComposer, focus, onFocusTaken)
    // Kept outside the page (Drafts): a fold moves this page to another layout.
    var draft by jarvis.android.ui.chat.Drafts.of(jarvis.android.ui.chat.Drafts.NEW_CONVERSATION)
    val dictate = jarvis.android.ui.chat.rememberDictation(
        onHeard = { heard -> draft = jarvis.android.ui.chat.withDictation(draft, heard) },
        onError = { jarvis.android.ui.chat.Toasts.show(it, jarvis.android.ui.chat.ToastStyle.ERROR) },
    )
    val pending = model.pending
    val top = WindowInsets.statusBars.asPaddingValues().calculateTopPadding() + JSize.shellBar
    Column(Modifier.fillMaxSize().background(JTheme.colors.page).imePadding().navigationBarsPadding()) {
        Column(
            Modifier.weight(1f).fillMaxWidth().verticalScroll(rememberScrollState()).padding(top = top).padding(horizontal = JSpace.page),
            verticalArrangement = Arrangement.spacedBy(JSpace.xl),
        ) {
            if (pending != null) {
                Row(Modifier.fillMaxWidth().padding(top = JSpace.m)) {
                    Spacer(Modifier.weight(1f).widthIn(min = JSize.avatarXL))
                    Text(
                        pending, style = JTheme.type.body, color = JTheme.colors.label,
                        modifier = Modifier.widthIn(max = JSize.box300).background(JTheme.shell.userBubble, RoundedCornerShape(JSize.iconXS))
                            .padding(horizontal = JSpace.l, vertical = JSpace.m),
                    )
                }
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                    CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.lineMedium, color = JTheme.colors.secondaryText)
                    Text("Jarvis 正在准备…", style = JTheme.type.secondary, color = JTheme.colors.secondaryText, maxLines = 1)
                }
            }
        }
        Column(Modifier.fillMaxWidth().padding(horizontal = JSpace.m).padding(bottom = JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
            if (pending == null) {
                (model.startError ?: model.loadError)?.let { e ->
                    Row(Modifier.padding(horizontal = JSpace.l), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                        Icon(JIcons.error, null, Modifier.size(JSize.markS), tint = JTheme.colors.danger)
                        Text(e, style = JTheme.type.caption, color = JTheme.colors.danger, maxLines = 3)
                    }
                }
                ShellNeedsYou(hub, onOpenSession, onOpenInbox)
            }
            ShellComposer(
                draft, { draft = it }, "问 Jarvis", canSend = draft.isNotBlank() && pending == null && model.state != null,
                actions = ComposerActions(send = { model.start(draft); draft = "" }, voice = onVoice, dictate = dictate),
                focus = focus, enabled = pending == null,
            )
        }
    }
}

/**
 * What needs you, over the empty composer (iOS `ShellNeedsYou`): the agents waiting on you, then the inbox's 需要你,
 * newest first — three at most, then where the rest are. A tap opens it where it lives.
 */
@Composable
fun ShellNeedsYou(hub: Hub, onOpenSession: (SessionKey) -> Unit, onOpenInbox: () -> Unit) {
    val pending by hub.interactions.collectAsState()
    val inbox by hub.inbox.collectAsState()
    val byMachine by hub.sessions.collectAsState()
    val assistantKeys = byMachine.values.flatten().filter { it.assistant }.map { it.key }.toSet()
    val agents = pending.filter { it.sessionKey !in assistantKeys && hub.session(it.sessionKey)?.let(hub::isMuted) != true }
    val mail = inbox.filter { it.bucket == InboxBucket.Action }.sortedByDescending { it.updatedAt ?: "" }
    val lines: List<Any> = agents + mail
    if (lines.isEmpty()) return
    Column(Modifier.fillMaxWidth()) {
        lines.take(SHOWN).forEach { line ->
            when (line) {
                is Interaction -> NeedsLine(interactionIcon(line), JTheme.needsYou.dot, line.agentName(), line.headline) { onOpenSession(line.sessionKey) }
                is InboxItem -> NeedsLine(if (line.source == "lark") JIcons.feishuChats else JIcons.mail, JTheme.colors.secondaryText, line.title, line.summary ?: line.preview.orEmpty()) { onOpenInbox() }
            }
        }
        if (lines.size > SHOWN) NeedsLine(JIcons.inbox, JTheme.colors.secondaryText, null, "还有 ${lines.size - SHOWN} 件在收件箱", onOpenInbox)
    }
}

private const val SHOWN = 3

private fun Interaction.agentName(): String = sessionTitle?.takeIf { it.isNotBlank() } ?: cwd?.substringAfterLast('/')?.takeIf { it.isNotBlank() } ?: "Agent"

/** ChatGPT's rows: one line of large gray text, the name a step darker to find it at a glance. */
@Composable
private fun NeedsLine(icon: androidx.compose.ui.graphics.vector.ImageVector, tint: androidx.compose.ui.graphics.Color, who: String?, what: String, onClick: () -> Unit) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.box56).clickable(onClick = onClick).padding(horizontal = JSpace.l),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Box(Modifier.size(JSize.icon), contentAlignment = Alignment.Center) { Icon(icon, null, Modifier.size(JSize.iconS), tint = tint) }
        Text(
            buildAnnotatedString {
                if (who != null) { withStyle(SpanStyle(color = c.label.copy(alpha = 0.75f))) { append(who) }; append("  ") }
                withStyle(SpanStyle(color = c.secondaryText)) { append(what.replace('\n', ' ')) }
            },
            style = JTheme.type.body, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
        )
    }
}
