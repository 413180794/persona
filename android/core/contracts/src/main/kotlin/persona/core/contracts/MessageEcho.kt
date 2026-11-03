package persona.core.contracts

import java.time.Instant

/**
 * Finds a message sent from the phone in the agent's transcript. Claude Code keeps its own id for the user's message
 * (not the client message id), so the transcript's copy is found by its words: the same rules as iOS
 * (`PendingMessage.arrived` / `same` in apple/Shared/ChatStore.swift).
 */
object MessageEcho {
    /** The computer's clock may be behind the phone's by this much. */
    const val CLOCK_SLACK_MILLIS = 120_000L

    private val dropped = listOf(
        Regex("<attached_images>[\\s\\S]*?</attached_images>"),
        Regex("<[\\\\/]*pasted_content[^>]*>"),
        Regex("\\[Image #\\d+]"),
        Regex("\\s+"),
    )

    /** The words the hub adds where it cut a long text for the page (`…（已截断，共 N 字节）`). */
    private const val CUT_MARK = "\n…（已截断"

    /** Comparable form: the phone's image note, pasted wrappers and Claude's "[Image #21]" placeholders removed, no whitespace. */
    fun normalize(text: String): String = dropped.fold(text) { t, pattern -> pattern.replace(t, "") }

    /**
     * The transcript's copy of a message sent from here: equal once normalized, or one containing the other (Claude
     * adds placeholders, the phone adds notes). Containing only counts for longer texts, or a pending "好" would be
     * taken for any message with 好 in it.
     */
    fun same(a: String, b: String): Boolean {
        val x = normalize(a)
        val y = normalize(b)
        if (x.isEmpty() || y.isEmpty()) return false
        return x == y || (minOf(x.length, y.length) >= 8 && (x.contains(y) || y.contains(x)))
    }

    /**
     * [item] is the message [text] written at [createdAt] (epoch millis): a user message that arrived after it was
     * written (with [CLOCK_SLACK_MILLIS] for the computer's clock), with its words — when the page cut a long one, its
     * start. An older message with the same words (a second "继续") is not it.
     */
    fun arrived(text: String, createdAt: Long, item: Item): Boolean {
        if (item.role != "user" || item.kind != "text") return false
        val at = item.ts?.let { runCatching { Instant.parse(it).toEpochMilli() }.getOrNull() }
        if (at != null && at < createdAt - CLOCK_SLACK_MILLIS) return false
        val words = item.text.orEmpty()
        if (item.fullSize == null) return same(words, text)
        val start = normalize(words.substringBeforeLast(CUT_MARK))
        return start.length >= 8 && normalize(text).startsWith(start)
    }
}
