package persona.feature.chat

/** How long the chat waits after /clear for the new session in the same pane before pointing at the list (iOS: 20 s). */
internal const val CLEAR_WAIT_MILLIS = 20_000L

/**
 * Persona's quick messages under short "/" words (iOS SlashCommand, the same five): the "+" sheet sends one at once; a
 * draft that is a single "/" word lists them above the field, and picking one puts its message in the box to send.
 */
internal data class SlashCommand(val title: String, val word: String, val detail: String, val text: String) {
    val clear: Boolean get() = word == CLEAR

    companion object {
        val all = listOf(
            SlashCommand("继续", "/继续", "让它接着做", "继续"),
            SlashCommand("压缩上下文 /compact", "/compact", "压缩上下文", "/compact"),
            SlashCommand("查看上下文 /context", "/context", "查看上下文", "/context"),
            SlashCommand("提交并推送", "/提交", "按规范提交并推送", "把当前改动按规范提交并推送"),
            SlashCommand("汇报进展", "/汇报", "汇报进展、还剩什么", "简要汇报一下目前的进展、还剩什么"),
        )

        /** A new Claude session in the same pane, the conversation kept in history: asks first, never a quick message. */
        const val CLEAR = "/clear"
        val clearCommand = SlashCommand("清空上下文 /clear", CLEAR, "清空上下文，开新会话", CLEAR)

        /** What the "/" list offers for [draft]: everything for a bare "/", else the words starting with it (any case). */
        fun matching(draft: String, clear: Boolean): List<SlashCommand> {
            if (!draft.startsWith("/") || draft.any { it == ' ' || it == '\n' }) return emptyList()
            val list = all + if (clear) listOf(clearCommand) else emptyList()
            return list.filter { it.word.startsWith(draft, ignoreCase = true) }
        }

        /**
         * The list the composer shows: [matching], except once the draft is exactly the one quick message it matches
         * (picked, or typed out): there is nothing left to choose. /clear stays, so picking it can still ask first.
         */
        fun suggestions(draft: String, clear: Boolean): List<SlashCommand> =
            matching(draft, clear).takeUnless { it.singleOrNull()?.let { only -> !only.clear && only.word.equals(draft, ignoreCase = true) } == true }.orEmpty()
    }
}
