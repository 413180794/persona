package persona.feature.chat

import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import persona.core.contracts.api.ExtensionQuickPrompt

internal object ExtensionPromptText {
    fun matching(prompts: List<ExtensionQuickPrompt>, draft: String): List<ExtensionQuickPrompt> {
        if (!draft.startsWith('/') || draft.any(Char::isWhitespace)) return emptyList()
        val query = draft.drop(1)
        return prompts.filter { query.isEmpty() || it.title.contains(query, ignoreCase = true) || it.id.contains(query, ignoreCase = true) }
    }
}

internal fun ChatComposerState.fillQuickPrompt(prompt: ExtensionQuickPrompt) {
    beginEdit()
    value = TextFieldValue(prompt.text, TextRange(prompt.text.length))
    focusRequest++
}
