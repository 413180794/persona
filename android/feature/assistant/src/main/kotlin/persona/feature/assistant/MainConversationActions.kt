package persona.feature.assistant

import persona.core.contracts.SessionCompaction

/** Hub features behind the main conversation's 压缩上下文 and 新建主对话 (docs/assistant.md, #1727). */
internal const val CompactFeature = "assistant_compact"
internal const val NewMainFeature = "assistant_main_new"

/** The main conversation's menu: which of its actions the hub offers. */
internal data class MainActions(val compact: Boolean, val newMain: Boolean) {
    companion object {
        fun of(features: Set<String>) = MainActions(CompactFeature in features, NewMainFeature in features)
    }
}

/** What the line over the main conversation says about a compaction. */
internal sealed interface CompactionLine {
    val text: String
    data object Running : CompactionLine { override val text = "正在压缩上下文…" }
    data object Done : CompactionLine { override val text = "已压缩" }
    data class Failed(val error: String) : CompactionLine {
        override val text = if (error.isBlank()) "压缩失败" else "压缩失败：$error"
    }
}

/**
 * The compaction line of the conversation on screen, from the session's `compaction` as updates come in. A compaction
 * is known by when it started. Its end is announced (已压缩, 压缩失败) only when this screen saw it run or asked for it
 * here: one that ended before the screen opened is old news, however often the screen opens again.
 */
internal data class CompactionStatus(
    /** Compactions seen running here, whose end is still to announce. */
    private val watched: Set<String> = emptySet(),
    /** Asked for here and not ended yet: the compaction that was there when asking ("" for none) is not it. */
    private val askedOver: String? = null,
    val line: CompactionLine? = null,
) {
    fun observe(compaction: SessionCompaction?): CompactionStatus {
        if (compaction == null) return if (line == CompactionLine.Running && askedOver == null) copy(line = null) else this
        val key = compaction.startedAt.orEmpty()
        return when {
            compaction.running -> copy(watched = watched + key, askedOver = null, line = CompactionLine.Running)
            key in watched || (askedOver != null && key != askedOver) -> copy(watched = watched - key, askedOver = null,
                line = if (compaction.failed) CompactionLine.Failed(compaction.error.orEmpty()) else CompactionLine.Done)
            askedOver != null -> this // the one asked for has not shown up yet
            // An end already announced or from before: nothing new; a line still saying 正在压缩 has nothing behind it.
            line == CompactionLine.Running -> copy(line = null)
            else -> this
        }
    }

    /** The user asked for a compaction; [current] is the session's compaction at that moment. */
    fun asked(current: SessionCompaction?): CompactionStatus =
        copy(askedOver = current?.startedAt.orEmpty(), line = CompactionLine.Running)

    /** Asking failed: back to what the session says. */
    fun refused(current: SessionCompaction?): CompactionStatus =
        copy(askedOver = null, line = null).observe(current)

    /** 已压缩 goes by itself, 压缩失败 when the user closes it; 正在压缩 stays while it runs. */
    fun dismissed(): CompactionStatus = if (line == CompactionLine.Running) this else copy(line = null)
}

/** How long 已压缩 stays over the conversation. */
internal const val CompactionDoneMillis = 3_000L

/** 新建主对话's confirmation, as the dialog words it. */
internal const val NewMainTitle = "新建主对话？"
internal fun newMainText(today: java.time.LocalDate): String =
    "当前的主对话会归档，改名为「旧主对话 · 至 ${today.monthValue}月${today.dayOfMonth}日」，以后仍能找到。" +
        "发出的下一条消息会开始新的主对话。"
internal const val CarrySummaryTitle = "带上旧对话的摘要"
internal const val CarrySummaryHint = "没压缩过的只带最近几条，想带完整摘要可以先压缩"

/** What to tell the user after 新建主对话 went through. */
internal fun newMainNotice(carried: Boolean, asked: Boolean): String = when {
    !asked -> "已新建主对话"
    carried -> "已新建主对话，会带上旧对话的摘要"
    else -> "已新建主对话，旧对话没有能带过来的内容"
}
