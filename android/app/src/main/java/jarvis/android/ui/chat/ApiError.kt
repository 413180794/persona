package jarvis.android.ui.chat

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.material.icons.filled.Warning
import androidx.compose.ui.unit.dp
import jarvis.android.ui.ChatRow
import jarvis.android.ui.FoldedRow

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
@androidx.compose.runtime.Composable
fun ApiErrorCard(e: ApiErrorLine, retry: (() -> Unit)?) {
    val c = androidx.compose.material3.MaterialTheme.colorScheme
    val ctx = androidx.compose.ui.platform.LocalContext.current
    var open by androidx.compose.runtime.saveable.rememberSaveable(e.row.key) { androidx.compose.runtime.mutableStateOf(false) }
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(14.dp)
    val info = e.info
    fun copy(s: String) { jarvis.android.ui.media.MediaShare.copyText(ctx, s, "错误"); Toasts.show("已复制", ToastStyle.SUCCESS) }
    androidx.compose.foundation.layout.Column(
        androidx.compose.ui.Modifier.fillMaxWidth().padding(vertical = 4.dp)
            .background(c.error.copy(alpha = 0.06f), shape).border(1.dp, c.error.copy(alpha = 0.22f), shape)
            .clickable { open = !open }.padding(12.dp)
            .semantics { contentDescription = "API 错误：${info.reason}" },
        verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(8.dp),
    ) {
        androidx.compose.foundation.layout.Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
            androidx.compose.material3.Icon(androidx.compose.material.icons.Icons.Default.Warning, null, tint = c.error,
                modifier = androidx.compose.ui.Modifier.size(18.dp))
            androidx.compose.foundation.layout.Spacer(androidx.compose.ui.Modifier.width(8.dp))
            androidx.compose.material3.Text(info.reason, style = androidx.compose.material3.MaterialTheme.typography.titleSmall)
        }
        androidx.compose.foundation.layout.Row(horizontalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(6.dp)) {
            info.status?.let { Chip("$it", error = true) }
            when (info.transient) { true -> Chip("临时故障"); false -> Chip("要处理"); null -> {} }
            if (e.count > 1) Chip("连续 ${e.count} 次")
        }
        if (open) {
            info.requestId?.let { Field("请求 ID", it) { copy(it) } }
            info.session?.let { Field("Session", it) { copy(it) } }
            e.row.row.let { (it as? jarvis.android.ui.ChatRow.Assistant)?.item ?: (it as? jarvis.android.ui.ChatRow.Notice)?.item }?.time?.let { t ->
                Field("时间", java.time.format.DateTimeFormatter.ofPattern("M月d日 HH:mm:ss").format(t.atZone(java.time.ZoneId.systemDefault())), null)
            }
            androidx.compose.foundation.text.selection.SelectionContainer {
                androidx.compose.material3.Text(
                    info.raw, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace,
                    style = androidx.compose.material3.MaterialTheme.typography.bodySmall,
                    modifier = androidx.compose.ui.Modifier.fillMaxWidth().background(c.surfaceVariant, androidx.compose.foundation.shape.RoundedCornerShape(8.dp)).padding(10.dp),
                )
            }
            androidx.compose.material3.TextButton(onClick = { copy(info.raw) }) { androidx.compose.material3.Text("复制原文", maxLines = 1) }
        }
        retry?.let { r ->
            androidx.compose.material3.FilledTonalButton(onClick = r) { androidx.compose.material3.Text("重试", maxLines = 1) }
        }
    }
}

@androidx.compose.runtime.Composable
private fun Chip(text: String, error: Boolean = false) {
    val c = androidx.compose.material3.MaterialTheme.colorScheme
    androidx.compose.material3.Text(
        text, style = androidx.compose.material3.MaterialTheme.typography.labelSmall, color = if (error) c.error else c.onSurfaceVariant,
        modifier = androidx.compose.ui.Modifier.background(if (error) c.error.copy(alpha = 0.12f) else c.surfaceVariant, androidx.compose.foundation.shape.RoundedCornerShape(50))
            .padding(horizontal = 8.dp, vertical = 3.dp),
    )
}

@androidx.compose.runtime.Composable
private fun Field(name: String, value: String, onCopy: (() -> Unit)?) {
    androidx.compose.foundation.layout.Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
        androidx.compose.material3.Text(name, style = androidx.compose.material3.MaterialTheme.typography.labelMedium,
            color = androidx.compose.material3.MaterialTheme.colorScheme.onSurfaceVariant, modifier = androidx.compose.ui.Modifier.width(64.dp))
        androidx.compose.material3.Text(value, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace, maxLines = 1,
            overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis, style = androidx.compose.material3.MaterialTheme.typography.bodySmall,
            modifier = androidx.compose.ui.Modifier.weight(1f))
        onCopy?.let { androidx.compose.material3.TextButton(onClick = it) { androidx.compose.material3.Text("复制", maxLines = 1) } }
    }
}
