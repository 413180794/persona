package jarvis.android.ui.lark

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.lark.LarkFormat
import jarvis.android.lark.LarkPalette
import jarvis.android.lark.LarkTint
import jarvis.android.lark.arr
import jarvis.android.lark.bool
import jarvis.android.lark.get
import jarvis.android.lark.int
import jarvis.android.lark.str
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.LarkChrome
import jarvis.android.ui.design.LarkIcons
import jarvis.android.ui.media.MediaShare
import kotlinx.serialization.json.JsonElement

/**
 * Cards lark-connector summarises itself (`metadata.variant`): forwarded chats, group / contact cards, meetings, calendar
 * events, votes, tasks, red packets (iOS LarkSummaryCards.swift, a port of `SummaryCardView`).
 */
@Composable
fun LarkSummaryCardView(meta: JsonElement, fallbackTitle: String) {
    val variant = meta["variant"].str ?: ""
    val title = meta["title"].str ?: fallbackTitle
    val shape = RoundedCornerShape(JRadius.card)
    Box(Modifier.fillMaxWidth().clip(shape).background(LarkChrome.cardBackground).border(JSize.hairline, LarkChrome.cardStroke, shape)) {
        when (variant) {
            "merge-forward" -> MergeForwardCard(meta, title)
            "share-group", "share-user" -> ShareCard(meta, title, variant == "share-group")
            else -> GenericSummaryCard(meta, title, variant)
        }
    }
}

/** 「聊天记录」: a few previews; a tap opens every forwarded message. */
@Composable
private fun MergeForwardCard(meta: JsonElement, title: String) {
    val actions = LocalLarkActions.current
    val c = JTheme.colors
    val previews = meta["previews"].arr.orEmpty()
    val count = meta["messageCount"].int ?: previews.size
    Column(
        Modifier.fillMaxWidth().clickable(enabled = actions.openForwarded != null) { actions.openForwarded?.invoke(title, meta) }.padding(JSpace.m),
        verticalArrangement = Arrangement.spacedBy(JSpace.sm),
    ) {
        Text(title.ifEmpty { "聊天记录" }, style = JTheme.type.secondaryStrong, color = c.label, maxLines = 2, overflow = TextOverflow.Ellipsis)
        for (p in previews.take(4)) {
            Text("${p["senderName"].str ?: ""}：${LarkFormat.plain(p["text"].str ?: "")}", style = JTheme.type.caption, color = c.secondaryText,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        HorizontalDivider(Modifier.padding(top = JSpace.xxs), color = LarkChrome.cardStroke, thickness = JSize.hairline)
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            Icon(LarkIcons.chat, null, Modifier.size(JSize.markXS), tint = c.secondaryText)
            Text("聊天记录", style = JTheme.type.label, color = c.secondaryText, modifier = Modifier.weight(1f))
            Text("$count 条", style = JTheme.type.label, color = c.secondaryText)
        }
    }
}

/** A group or contact card: picture, name, facts, eyebrow. */
@Composable
private fun ShareCard(meta: JsonElement, title: String, isGroup: Boolean) {
    val c = JTheme.colors
    val fields = meta["fields"].arr.orEmpty().mapNotNull { f ->
        val l = f["label"].str ?: return@mapNotNull null
        val v = f["value"].str?.takeIf { it.isNotEmpty() } ?: return@mapNotNull null
        "$l ${LarkFormat.fieldValue(v)}"
    }
    Column(Modifier.fillMaxWidth().padding(JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
            LarkAvatar(title, meta["imageUrl"].str, isGroup = isGroup, size = JSize.avatarM)
            Column(verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
                Text(title, style = JTheme.type.secondaryStrong, color = c.label, maxLines = 2, overflow = TextOverflow.Ellipsis)
                if (fields.isNotEmpty()) Text(fields.joinToString(" · "), style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        HorizontalDivider(color = LarkChrome.cardStroke, thickness = JSize.hairline)
        Row {
            Text(meta["eyebrow"].str ?: if (isGroup) "群名片" else "个人名片", style = JTheme.type.label, color = c.secondaryText, modifier = Modifier.weight(1f))
            if (meta["isDissolved"].bool == true) Text("已解散", style = JTheme.type.label, color = c.secondaryText)
        }
    }
}

/** Meetings, events, votes, tasks and anything else: an accent bar, a glyph, the eyebrow, title, status, facts, footer. */
@Composable
private fun GenericSummaryCard(meta: JsonElement, title: String, variant: String) {
    val c = JTheme.colors
    val ctx = LocalContext.current
    val fields = meta["fields"].arr.orEmpty().mapNotNull { f ->
        val l = f["label"].str ?: return@mapNotNull null
        val v = f["value"].str?.takeIf { it.isNotEmpty() } ?: return@mapNotNull null
        l to LarkFormat.fieldValue(v)
    }
    val accent = larkColor(
        when (variant) {
            "hongbao" -> LarkPalette.fixed("red")
            "calendar" -> LarkPalette.fixed("orange")
            "vote" -> LarkPalette.fixed("purple")
            "todo", "task" -> LarkPalette.fixed("green")
            else -> LarkTint.blue
        },
    )
    Row(Modifier.fillMaxWidth().height(IntrinsicSize.Min)) {
        Box(Modifier.width(JSize.barWide).fillMaxHeight().background(accent))
        Column(Modifier.weight(1f).padding(JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
            Row(horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
                val img = meta["imageUrl"].str
                val shape = RoundedCornerShape(JRadius.control)
                if (img != null) LarkRemoteImage(img, Modifier.size(JSize.wellL).clip(shape), size = thumbEdge(JSize.wellL), maxPx = GLYPH_PX)
                else Box(Modifier.size(JSize.wellL).clip(shape).background(accent.copy(alpha = 0.12f)), contentAlignment = Alignment.Center) {
                    Icon(LarkIcons.summary(variant), null, Modifier.size(JSize.iconXS), tint = accent)
                }
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
                    meta["eyebrow"].str?.let { Text(it, style = JTheme.type.label, color = c.secondaryText, maxLines = 1) }
                    Text(title, style = JTheme.type.secondaryStrong, color = c.label, maxLines = 3, overflow = TextOverflow.Ellipsis)
                }
                meta["status"].str?.let {
                    Text(it, style = JTheme.type.labelMedium, color = c.secondaryText, maxLines = 1,
                        modifier = Modifier.clip(CircleShape).background(c.label.copy(alpha = 0.06f)).padding(horizontal = JSpace.sm, vertical = JSpace.xxs))
                }
            }
            meta["description"].str?.takeIf { it.isNotEmpty() }?.let {
                Text(LarkFormat.stripHtml(it), style = JTheme.type.secondary, color = c.secondaryText, maxLines = 4, overflow = TextOverflow.Ellipsis)
            }
            val vote = meta["vote"]
            val options = vote["options"].arr.orEmpty()
            if (options.isNotEmpty()) {
                val multi = (vote["maxPick"].int ?: 1) > 1
                Column(verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    for (o in options) Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                        Icon(if (multi) LarkIcons.checkbox else LarkIcons.unchecked, null, Modifier.size(JSize.markM), tint = c.secondaryText)
                        Text(o["content"].str ?: "", style = JTheme.type.secondary, color = c.label)
                    }
                }
            }
            if (fields.isNotEmpty()) Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                for ((l, v) in fields) Row(horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
                    Text(l, style = JTheme.type.caption, color = c.secondaryText, maxLines = 1)
                    Text(v, style = JTheme.type.caption, color = c.label, maxLines = 3, overflow = TextOverflow.Ellipsis)
                }
            }
            meta["footer"].str?.let { Text(LarkFormat.fieldValue(it), style = JTheme.type.label, color = c.tertiaryText) }
            val number = meta["actions"].arr.orEmpty().firstOrNull { it["type"].str == "copyMeetingNumber" }?.get("value").str
            if (number != null) Row(
                Modifier.clip(CircleShape).background(c.fillChip).clickable {
                    MediaShare.copyText(ctx, number, "会议号")
                    Toasts.show("已拷贝会议号", ToastStyle.SUCCESS)
                }.padding(horizontal = JSpace.m, vertical = JSpace.sm),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
            ) {
                Icon(JIcons.copy, null, Modifier.size(JSize.markXS), tint = LarkChrome.blue)
                Text("拷贝会议号", style = JTheme.type.labelMedium, color = LarkChrome.blue, maxLines = 1)
            }
        }
    }
}

private const val GLYPH_PX = 120
