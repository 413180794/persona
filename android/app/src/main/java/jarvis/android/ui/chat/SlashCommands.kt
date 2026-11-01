package jarvis.android.ui.chat

/**
 * Jarvis's quick messages under short "/" words (iOS SlashCommand / PromptItems.commands): in the "+" menu one is sent
 * at once; typed "/" lists them over the composer, and picking one puts its message in the box.
 */
data class SlashCommand(val title: String, val word: String, val detail: String, val text: String) {
    companion object {
        val all = listOf(
            SlashCommand("继续", "/继续", "让它接着做", "继续"),
            SlashCommand("压缩上下文 /compact", "/compact", "压缩上下文", "/compact"),
            SlashCommand("查看上下文 /context", "/context", "查看上下文", "/context"),
            SlashCommand("提交并推送", "/提交", "按规范提交并推送", "把当前改动按规范提交并推送"),
            SlashCommand("汇报进展", "/汇报", "汇报进展、还剩什么", "简要汇报一下目前的进展、还剩什么"),
        )

        /** /clear: a new Claude session in the same pane (the conversation stays in history). Not a quick message. */
        const val CLEAR = "/clear"

        /** What the "/" picker lists for the draft: everything for a bare "/", else the words starting with it. */
        fun matching(draft: String, clear: Boolean): List<SlashCommand> {
            if (!draft.startsWith("/") || draft.any { it == ' ' || it == '\n' }) return emptyList()
            val list = all + if (clear) listOf(SlashCommand("清空上下文 /clear", CLEAR, "清空上下文，开新会话", CLEAR)) else emptyList()
            return list.filter { it.word.startsWith(draft, ignoreCase = true) }
        }
    }
}
