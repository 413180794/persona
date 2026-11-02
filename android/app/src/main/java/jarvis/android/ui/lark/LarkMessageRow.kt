package jarvis.android.ui.lark

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.lark.LarkChatRules
import jarvis.android.lark.LarkFormat
import jarvis.android.lark.LarkMessage
import jarvis.android.lark.LarkParts
import jarvis.android.lark.LarkReaction
import jarvis.android.lark.LarkReplyTo
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.Haptics
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.LarkChrome
import jarvis.android.ui.design.LarkIcons
import jarvis.android.ui.design.LarkSize
import jarvis.android.ui.design.hapticView
import jarvis.android.ui.media.MediaShare

/**
 * One message, laid out like lark-connector (and Feishu desktop, iOS `LarkMessageRow`): everyone's messages on the left
 * with an avatar and, in group chats, the sender's name on the first of a run; mine in the accent's soft color.
 * The avatar and the name open the profile card; a long press gives quick reactions, 表情回复 and 拷贝.
 */
@Composable
fun LarkMessageRow(message: LarkMessage, grouped: Boolean, isGroup: Boolean, feedId: String?, modifier: Modifier = Modifier, inThread: Boolean = false) {
    val m = message
    val actions = LocalLarkActions.current
    val c = JTheme.colors
    if (m.isSystem) {
        val p = m.parts?.firstOrNull()
        Box(modifier.fillMaxWidth().padding(horizontal = JSpace.xl, vertical = JSpace.xs), contentAlignment = Alignment.Center) {
            if (p != null && p.metaBool("system")) LarkSystemText(p)
            else Text(m.text, style = JTheme.type.label, color = c.secondaryText, textAlign = TextAlign.Center)
        }
        return
    }
    val showProfile: () -> Unit = {
        val id = m.senderId
        if (!id.isNullOrEmpty()) actions.openProfile?.invoke(LarkPersonRef(id, m.senderName, m.senderAvatar, feedId))
    }
    Row(modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
        if (grouped) Spacer(Modifier.width(JSize.wellM)) else {
            LarkAvatar(m.senderName, m.senderAvatar, Modifier.clip(CircleShape).clickable(role = Role.Button, onClick = showProfile)
                .semantics { contentDescription = "${m.senderName} 的资料" }, size = JSize.wellM)
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            if (!grouped && isGroup) Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                Text(m.senderName, style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false).clickable(onClick = showProfile))
                if (m.senderType == "bot") Text("机器人", style = JTheme.type.labelStrong, color = LarkChrome.blue, maxLines = 1,
                    modifier = Modifier.clip(RoundedCornerShape(JSize.bar)).background(LarkChrome.blue.copy(alpha = 0.12f)).padding(horizontal = JSpace.xs))
            }
            MessageContent(m, showProfile)
            Footer(m, inThread)
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun MessageContent(m: LarkMessage, showProfile: () -> Unit) {
    val c = JTheme.colors
    val parts = m.parts.orEmpty()
    var menu by remember { mutableStateOf(false) }
    val view = hapticView()
    val press = Modifier.combinedClickable(interactionSource = null, indication = null, onClick = {}, onLongClick = { Haptics.tap(view); menu = true })
    Box {
        when {
            m.recalled -> Text(
                if (m.isMine) "你撤回了一条消息" else "${m.senderName} 撤回了一条消息", style = JTheme.type.caption.copy(fontStyle = FontStyle.Italic),
                color = c.tertiaryText,
                modifier = Modifier.clip(RoundedCornerShape(JRadius.control)).background(c.label.copy(alpha = 0.04f)).padding(horizontal = JSpace.m, vertical = JSpace.s),
            )
            parts.isEmpty() -> Bubble(m, press) { Text(m.text.ifEmpty { "[${m.type.lowercase()}]" }, style = JTheme.type.body, color = c.label) }
            LarkParts.isBare(parts) -> Column((if (LarkParts.fillsWidth(parts)) Modifier.fillMaxWidth() else Modifier).then(press), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                m.replyTo?.let { LarkReplyQuote(it, Modifier.padding(horizontal = JSpace.xs)) }
                WithLarkMessage(m.id, parts) { LarkPartsView(parts) }
            }
            else -> Bubble(m, press) {
                Column(verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    m.replyTo?.let { LarkReplyQuote(it) }
                    WithLarkMessage(m.id, parts) { LarkPartsView(parts) }
                }
            }
        }
        MessageMenu(m, menu, onDismiss = { menu = false }, showProfile = showProfile)
    }
}

@Composable
private fun Bubble(m: LarkMessage, press: Modifier, content: @Composable () -> Unit) {
    Box(
        Modifier.widthIn(max = LarkSize.bubbleMax).clip(RoundedCornerShape(JRadius.control)).background(if (m.isMine) LarkChrome.myBubble else LarkChrome.otherBubble)
            .then(press).padding(JSpace.m),
    ) { content() }
}

/** A long press: a row of quick reactions, 表情回复, 拷贝, the sender's profile. */
@Composable
private fun MessageMenu(m: LarkMessage, open: Boolean, onDismiss: () -> Unit, showProfile: () -> Unit) {
    val actions = LocalLarkActions.current
    val ctx = LocalContext.current
    val quick by LarkMedia.quickReactions.collectAsState()
    DropdownMenu(open, onDismissRequest = onDismiss, containerColor = JTheme.colors.card) {
        val react = actions.react
        if (react != null && !m.recalled) {
            Row(Modifier.padding(horizontal = JSpace.s, vertical = JSpace.xs), horizontalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
                for (key in quick.take(4)) Box(
                    Modifier.size(JSize.tapTarget).clip(CircleShape).clickable { onDismiss(); react(m, key) }
                        .semantics { contentDescription = LarkMedia.emoji(key)?.label ?: key },
                    contentAlignment = Alignment.Center,
                ) { LarkEmojiImage(key, JSize.wellXS) }
            }
            HorizontalDivider(color = JTheme.colors.separator.copy(alpha = 0.4f))
            DropdownMenuItem(text = { Text("表情回复", maxLines = 1) }, leadingIcon = { Icon(LarkIcons.react, null) },
                onClick = { onDismiss(); actions.pickReaction?.invoke(m) })
        }
        DropdownMenuItem(text = { Text("拷贝", maxLines = 1) }, leadingIcon = { Icon(JIcons.copy, null) }, onClick = {
            onDismiss()
            MediaShare.copyText(ctx, m.text, "消息")
            Toasts.show("已拷贝", ToastStyle.SUCCESS)
        })
        if (!m.senderId.isNullOrEmpty() && actions.openProfile != null) {
            DropdownMenuItem(text = { Text("查看 ${m.senderName} 的资料", maxLines = 1, overflow = TextOverflow.Ellipsis) },
                leadingIcon = { Icon(LarkIcons.profile, null) }, onClick = { onDismiss(); showProfile() })
        }
    }
}

/** Reactions (a tap toggles mine, ☺︎ opens the picker), then 「N 条回复」, 已编辑 and the time. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Footer(m: LarkMessage, inThread: Boolean) {
    val actions = LocalLarkActions.current
    val c = JTheme.colors
    val reactions = m.reactions.orEmpty()
    val react = actions.react
    if (reactions.isNotEmpty()) FlowRow(Modifier.widthIn(max = LarkSize.reactionsMax), horizontalArrangement = Arrangement.spacedBy(JSpace.sm), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        for (r in reactions) LarkReactionChip(r, Modifier.clip(CircleShape).clickable(enabled = react != null) { react?.invoke(m, r.key) })
        if (react != null) Box(
            Modifier.width(JSize.well).height(JSize.iconL).clip(CircleShape).background(c.label.copy(alpha = 0.05f))
                .clickable { actions.pickReaction?.invoke(m) }.semantics { contentDescription = "表情回复" },
            contentAlignment = Alignment.Center,
        ) { Icon(LarkIcons.react, null, Modifier.size(JSize.markS), tint = c.secondaryText) }
    }
    val replies = m.replies
    if (replies > 0 || m.edited || m.createdAt != null) Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
        if (replies > 0 && !inThread) Row(
            Modifier.clip(RoundedCornerShape(JSpace.xs)).clickable(enabled = actions.openThread != null) { actions.openThread?.invoke(m) },
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xxs),
        ) {
            Icon(JIcons.reply, null, Modifier.size(JSize.markXS), tint = LarkChrome.blue)
            Text("$replies 条回复", style = JTheme.type.label, color = LarkChrome.blue, maxLines = 1)
        }
        if (m.edited) Text("已编辑", style = JTheme.type.label, color = c.secondaryText, maxLines = 1)
        if (m.createdAt != null) Text(LarkChatRules.clock(m.createdAt), style = JTheme.type.label, color = c.secondaryText, maxLines = 1)
    }
}

/** The message a reply quotes: a bar, the author, a line or two of it. */
@Composable
fun LarkReplyQuote(reply: LarkReplyTo, modifier: Modifier = Modifier) {
    val c = JTheme.colors
    Row(modifier.height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        Box(Modifier.width(JSize.rule).fillMaxHeight().clip(CircleShape).background(c.label.copy(alpha = 0.2f)))
        Text(if (reply.recalled) "${reply.senderName}：消息已撤回" else "${reply.senderName}：${LarkFormat.plain(reply.text)}",
            style = JTheme.type.caption, color = c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}

/** A reaction: the emoji's picture, then who (up to three names) or how many. */
@Composable
fun LarkReactionChip(reaction: LarkReaction, modifier: Modifier = Modifier) {
    val c = JTheme.colors
    val users = reaction.users.orEmpty()
    val label = LarkMedia.emoji(reaction.key)?.label ?: reaction.key
    Row(
        modifier.background(if (reaction.mine) LarkChrome.blue.copy(alpha = 0.12f) else c.label.copy(alpha = 0.05f), CircleShape)
            .padding(horizontal = JSpace.s, vertical = JSpace.xs).semantics(mergeDescendants = true) { contentDescription = "$label ${reaction.count}" },
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
    ) {
        LarkEmojiImage(reaction.key, JSize.markL)
        Text(if (users.isEmpty() || users.size > 3) "${reaction.count}" else users.joinToString("、"), style = JTheme.type.label,
            color = if (reaction.mine) LarkChrome.blue else c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Feishu's 以下为新消息 line above the first unread message. */
@Composable
fun LarkNewMessagesDivider() {
    val blue = LarkChrome.blue
    Row(Modifier.fillMaxWidth().padding(vertical = JSpace.m).semantics { heading() }, verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
        Box(Modifier.weight(1f).height(JSize.line).background(blue.copy(alpha = 0.35f)))
        Text("以下为新消息", style = JTheme.type.labelMedium, color = blue, maxLines = 1)
        Box(Modifier.weight(1f).height(JSize.line).background(blue.copy(alpha = 0.35f)))
    }
}
