package persona.feature.inbox

/** Account-scoped preferences for retained recalls and message edit history. */
data class LarkMessageDisplay(
    val accountId: String = "",
    val antiRecall: Boolean = false,
    val showEditHistory: Boolean = false,
) {
    /** Summaries preserve the text supplied by the message source. */
    fun summary(text: String): String = text

    companion object {
        val Hidden = LarkMessageDisplay()
    }
}
