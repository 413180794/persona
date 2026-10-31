package jarvis.android.shortcuts

import jarvis.android.data.HubException
import jarvis.android.data.Interaction
import jarvis.android.data.Resolution
import jarvis.android.data.parseTime
import java.io.IOException
import java.time.Instant

/** What the shortcuts say and do (iOS `IntentText` / `IntentFailure` / `IntentPicker`), pure so tests pin it. */
object ShortcutText {
    /** Oldest first: the one that has been waiting longest (docs/intents.md「选择规则」). */
    fun pending(interactions: List<Interaction>): Interaction? = interactions.minByOrNull { parseTime(it.createdAt) ?: Instant.MAX }

    /** What 批准 sends, or null when this kind needs the screen (iOS `ApproveFirstIntent`). */
    fun resolution(ix: Interaction): Resolution? = when (ix.kind) {
        "permission" -> Resolution(allow = true)
        "plan" -> Resolution(approve = true, mode = "bypassPermissions") // 「批准并自动执行」, as on iOS
        else -> null
    }

    /** The confirmation line before approving. */
    fun ask(ix: Interaction): String = when (ix.kind) {
        "permission" -> "允许 " + (ix.summary?.takeIf { it.isNotBlank() }?.let { excerpt(it, 120) } ?: ix.toolName ?: "这个工具") + "？"
        "plan" -> "批准计划并自动执行？" + (ix.plan?.takeIf { it.isNotBlank() }?.let { "\n「${excerpt(plain(it), 120)}」" } ?: "")
        else -> needsApp(ix.kind)
    }

    fun done(ix: Interaction): String = when (ix.kind) {
        "permission" -> ix.toolName?.takeIf { it.isNotEmpty() }?.let { "已允许 $it" } ?: "已允许"
        "plan" -> "已批准计划，开始执行"
        else -> needsApp(ix.kind)
    }

    fun needsApp(kind: String): String = when (kind) {
        "question" -> "这是个提问，需要打开 Jarvis 回答。"
        "screen" -> "终端在等你输入，需要打开 Jarvis。"
        else -> "需要打开 Jarvis 处理。"
    }

    const val NOT_PAIRED = "还没配对。先打开 Jarvis 扫 `jarvisd hub pair` 的二维码。"
    const val NO_PENDING = "没有等你处理的事。"

    fun failure(e: Throwable): String {
        if (e is IOException) return "连不上 hub，确认手机能访问 hub（同一个网络或 VPN）。"
        val he = e as? HubException ?: return e.message ?: "出错了"
        return when (he.status) {
            401, 403 -> "配对已失效，打开 Jarvis 重新配对。"
            410 -> "这个已经处理过了。"
            else -> he.message
        }
    }

    /** Markdown heading and quote marks off each line, so an excerpt reads as prose. */
    fun plain(markdown: String): String =
        markdown.lineSequence().joinToString("\n") { it.trimStart().trimStart('#', '>').trimStart() }

    /** Collapses whitespace and clips. */
    fun excerpt(text: String, limit: Int = 60): String {
        val flat = text.split(Regex("\\s+")).filter { it.isNotEmpty() }.joinToString(" ")
        return if (flat.length <= limit) flat else flat.take(limit) + "…"
    }
}
