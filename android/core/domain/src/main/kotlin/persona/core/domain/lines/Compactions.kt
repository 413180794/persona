package persona.core.domain.lines

import java.math.BigDecimal
import java.math.RoundingMode

/**
 * A compaction as the chat shows it (docs/assistant.md「手动压缩」): one divider where the context was compacted, with
 * how it came about ([trigger]: `manual`, `auto`, `idle`; null when unknown) and the context's size before it. [notes]
 * are the notices folded into it, in transcript order.
 */
data class Compaction(val trigger: String? = null, val preTokens: Long? = null, val notes: List<String> = emptyList()) {
    /** 「以上内容已压缩 · 手动 · 压缩前 18.1 万 token」, leaving out what is not known. */
    val label: String get() = listOfNotNull(
        "以上内容已压缩",
        when (trigger) { "manual" -> "手动"; "auto" -> "自动"; "idle" -> "空闲时"; else -> null },
        preTokens?.takeIf { it > 0 }?.let { "压缩前 ${tokenCount(it)} token" },
    ).joinToString(" · ")
}

/** What the hub writes for a compaction boundary without its own `compact` (Codex), and for the summary after it. */
const val CompactedNotice = "上下文已压缩"
const val ContinuedNotice = "已从压缩的上下文继续"
/** A /compact sent without instructions (the app's 压缩上下文, the node's idle compaction): a notice, not a message. */
const val CompactCommandNotice = "压缩上下文"

/** 181000 → 「18.1 万」, 150000 → 「15 万」; under ten thousand the number itself. */
fun tokenCount(tokens: Long): String = if (tokens < 10_000) tokens.toString()
    else BigDecimal(tokens).divide(BigDecimal(10_000)).setScale(1, RoundingMode.HALF_UP).stripTrailingZeros().toPlainString() + " 万"

private fun LineItem.isNotice(text: String) =
    kind == "notice" && task == null && item.task == null && item.wake != true && this.text == text
private fun LineItem.isBoundary() = kind == "notice" && (item.compact != null || isNotice(CompactedNotice))
/** Claude Code's own line after a compaction ("Compacted …", "Compacted while idle …"). */
private fun LineItem.isCompactedNote() = kind == "notice" && task == null && item.wake != true &&
    text.orEmpty().startsWith("Compacted")
/** Items that draw nothing and so do not part a boundary from its notes. */
private val silent = setOf("tool_result", "turn_duration", "unknown")

/**
 * A compaction is one item: the boundary keeps the notices that came with it as [Compaction.notes] — the /compact
 * before or after it (「压缩上下文」), the summary it carried on with (「已从压缩的上下文继续」) and Claude Code's line about
 * it — and they are no rows of their own. A summary whose boundary is on an older page stands in for it. Ids, texts and
 * the order of everything else stay as they were.
 */
fun foldCompactions(items: List<LineItem>): List<LineItem> {
    if (items.none { it.isBoundary() || it.isNotice(ContinuedNotice) }) return items
    val out = ArrayList<LineItem>(items.size)
    var open: Int? = null // the boundary in `out` the notes after it still join
    var held: LineItem? = null // a 压缩上下文 waiting for the boundary after it
    fun release() { held?.let(out::add); held = null }
    fun join(note: LineItem, idle: Boolean = false) {
        val at = open ?: return
        val mark = out[at].compaction ?: Compaction()
        out[at] = out[at].copy(compaction = mark.copy(notes = mark.notes + note.text.orEmpty(),
            trigger = if (idle) "idle" else mark.trigger))
    }
    for (it in items) {
        when {
            it.isBoundary() -> {
                val notes = listOfNotNull(held?.text) + (it.text ?: CompactedNotice)
                held = null
                out += it.copy(compaction = Compaction(it.item.compact?.trigger?.takeIf(String::isNotBlank),
                    it.item.compact?.preTokens, notes))
                open = out.lastIndex
            }
            it.isNotice(CompactCommandNotice) -> if (open != null) join(it) else { release(); held = it }
            it.isNotice(ContinuedNotice) -> if (open != null) join(it) else {
                release()
                out += it.copy(compaction = Compaction(notes = listOf(it.text.orEmpty())))
                open = out.lastIndex
            }
            it.isCompactedNote() && open != null -> join(it, idle = it.text.orEmpty().startsWith("Compacted while idle"))
            it.kind in silent -> out += it
            else -> {
                release()
                open = null
                out += it
            }
        }
    }
    release()
    return out
}
