package jarvis.android.ui.chat

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.ui.ChatRow
import jarvis.android.ui.FoldedRow
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChipLook
import jarvis.android.ui.design.IconAction
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.media.MediaShare

/**
 * A model API error in a chat (iOS APIErrorInfo), read from the text Claude Code writes when it gives up
 * ("API Error: 502 …", an answer) or from the hub's notice for a retry ("API 错误：Overloaded（正在重试）"): why, in
 * words, the status, and whether it is likely to pass by itself; the ids and the raw text fold under it.
 */
data class ApiErrorInfo(
    val status: Int?,
    /** One line, in words. */
    val reason: String,
    /** Likely to pass if tried again (5xx, rate limits, timeouts); false when someone has to act (login, a bad
     *  request); null when the text does not say. */
    val transient: Boolean?,
    val requestId: String?,
    val session: String?,
    val raw: String,
) {
    /** Two errors are "the same" for folding when these match. */
    val signature: String get() = "${status ?: 0}|$reason"

    companion object {
        private val statusRe = Regex("""^\s*(\d{3})\b""")
        /** One short sentence, then the error ("Failed to authenticate. API Error:"), not prose that mentions one. */
        private val lead = Regex("""^[A-Z][A-Za-z ]{2,40}\.\s+API Error:""")
        private val requestRe = Regex("""(?i)\b(?:req(?:uest)?(?:[ _-]?id)?)[:=\s"]+([A-Za-z0-9_-]{8,})""")
        private val sessionRe = Regex("""(?i)\bsession(?:[ _-]?id)?[:=\s"]+([A-Za-z0-9_:-]{6,})""")

        fun parse(text: String?): ApiErrorInfo? {
            val t = text?.trim() ?: return null
            val body = when {
                t.startsWith("API Error:") -> t.removePrefix("API Error:")
                t.startsWith("API 错误：") -> t.removePrefix("API 错误：")
                // Claude Code leads with a sentence for some: "Failed to authenticate. API Error: 401 …".
                else -> lead.find(t)?.let { t.substring(it.range.last + 1) } ?: return null
            }
            val status = statusRe.find(body)?.groupValues?.get(1)?.toIntOrNull()
            val (reason, transient) = explain(body.lowercase(), status)
            return ApiErrorInfo(
                status, reason, transient,
                requestRe.find(body)?.groupValues?.get(1), sessionRe.find(body)?.groupValues?.get(1), t,
            )
        }

        /** What went wrong, as a person would say it, and whether it is likely to pass (the iOS wording). */
        private fun explain(s: String, status: Int?): Pair<String, Boolean?> = when {
            "no available gpu" in s -> "推理服务暂时没有可用的 GPU" to true
            "overloaded" in s || status == 529 -> "模型服务太忙，过载了" to true
            status == 401 || status == 403 || "authentication" in s || "/login" in s || "login expire" in s ->
                "登录过期了，要在电脑上重新 /login" to false
            status == 429 || "rate limit" in s || "rate_limit" in s -> "请求太多，被限流了" to true
            "timed out" in s || "timeout" in s -> "请求超时了" to true
            "connection" in s -> "连不上模型服务" to true
            status == 400 -> "请求被拒绝：参数或上下文有问题" to false
            status != null && status in 500..599 -> "模型服务暂时不可用" to true
            else -> "模型请求失败" to if ("temporary" in s || "try again" in s) true else null
        }

        private val retryWords = setOf("继续", "重试", "再试一次", "continue", "retry", "go on")

        /** A bare "继续" between two of the same error: the user retrying, folded into the error's count. */
        fun isRetry(row: FoldedRow): Boolean {
            val u = (row as? FoldedRow.Plain)?.row as? ChatRow.User ?: return false
            return u.item.text.orEmpty().trim().lowercase() in retryWords
        }
    }
}

/** A visible answer or notice that is an API error, with how many times in a row it came. */
data class ApiErrorLine(val row: FoldedRow.Plain, val info: ApiErrorInfo, val count: Int)

/** One drawn line of the chat: a folded row as it is, or an API error card standing for one or more of them. */
sealed interface ChatItem {
    data class Row(val row: FoldedRow) : ChatItem
    data class Error(val error: ApiErrorLine) : ChatItem
}

/**
 * Answers and notices that are API errors become cards; the same error again right after, with nothing but a retry
 * ("继续") between, folds into the first with a count (iOS ChatLines.foldAPIErrors).
 */
fun foldApiErrors(rows: List<FoldedRow>): List<ChatItem> {
    val out = ArrayList<ChatItem>(rows.size)
    for (r in rows) {
        val item = ((r as? FoldedRow.Plain)?.row as? ChatRow.Assistant)?.item ?: ((r as? FoldedRow.Plain)?.row as? ChatRow.Notice)?.item
        val info = item?.let { ApiErrorInfo.parse(it.text) }
        if (info == null) { out += ChatItem.Row(r); continue }
        val last = out.indexOfLast { it is ChatItem.Error }
        val prev = (out.getOrNull(last) as? ChatItem.Error)?.error
        if (prev != null && prev.info.signature == info.signature &&
            out.subList(last + 1, out.size).all { it is ChatItem.Row && ApiErrorInfo.isRetry(it.row) }
        ) {
            out.subList(last + 1, out.size).clear()
            out[last] = ChatItem.Error(prev.copy(info = info, count = prev.count + 1))
            continue
        }
        out += ChatItem.Error(ApiErrorLine(r as FoldedRow.Plain, info, 1))
    }
    return out
}

/**
 * The card for an API error (iOS APIErrorCard): the reason with a warning mark and chips (the status, 临时故障 /
 * 要处理, 连续 N 次); tapped, the ids to copy and the raw text; 重试 sends 继续 when [retry] is given.
 */
@Composable
fun ApiErrorCard(e: ApiErrorLine, retry: (() -> Unit)?) {
    val c = JTheme.colors
    val ctx = LocalContext.current
    var open by rememberSaveable(e.row.key) { mutableStateOf(false) }
    val shape = RoundedCornerShape(JRadius.card)
    val info = e.info
    fun copy(s: String) { MediaShare.copyText(ctx, s, "错误"); Toasts.show("已复制", ToastStyle.SUCCESS) }
    Column(
        Modifier.fillMaxWidth().padding(vertical = JSpace.xs).clip(shape)
            .background(c.danger.copy(alpha = 0.06f)).border(JSize.hairline * 2, c.danger.copy(alpha = 0.22f), shape)
            .clickable { open = !open }.padding(JSpace.m)
            .semantics { contentDescription = "API 错误：${info.reason}" },
        verticalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
            Icon(JIcons.error, null, Modifier.size(JSize.markL), tint = c.danger)
            Text(info.reason, style = JTheme.type.bodyStrong, color = c.label, modifier = Modifier.weight(1f))
            Icon(if (open) JIcons.collapse else JIcons.expand, null, Modifier.size(JSize.markM), tint = c.tertiaryText)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            info.status?.let { Chip("$it", look = ChipLook.Tint(c.danger)) }
            when (info.transient) {
                true -> Chip("临时故障")
                false -> Chip("要处理", look = ChipLook.Tint(c.caution.copy(alpha = 0.14f), c.cautionText))
                null -> {}
            }
            if (e.count > 1) Chip("连续 ${e.count} 次")
        }
        if (open) {
            info.requestId?.let { Field("请求 ID", it) { copy(it) } }
            info.session?.let { Field("Session", it) { copy(it) } }
            e.row.row.let { (it as? ChatRow.Assistant)?.item ?: (it as? ChatRow.Notice)?.item }?.time?.let { t ->
                Field("时间", java.time.format.DateTimeFormatter.ofPattern("M月d日 HH:mm:ss").format(t.atZone(java.time.ZoneId.systemDefault())), null)
            }
            SelectionContainer {
                Text(
                    info.raw, style = JTheme.type.code, color = c.label,
                    modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.control)).background(c.codeBackground).padding(JSpace.m),
                )
            }
            PillButton("复制原文", { copy(info.raw) }, kind = ButtonKind.Neutral, icon = JIcons.copy)
        }
        retry?.let { r -> PillButton("重试", r, kind = ButtonKind.Tinted, icon = JIcons.retry) }
    }
}

/** An id under the card: its name, the id in the code face, copy at the end. */
@Composable
private fun Field(name: String, value: String, onCopy: (() -> Unit)?) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
        Text(name, style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1, modifier = Modifier.width(JSize.box64))
        Text(value, style = JTheme.type.code, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        onCopy?.let { IconAction(JIcons.copy, "复制", it, tint = JTheme.colors.accent, size = JSize.iconXS) }
    }
}
