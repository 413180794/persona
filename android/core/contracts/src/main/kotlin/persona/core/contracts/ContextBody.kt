package persona.core.contracts

/**
 * What a reader may take as the end of a `<persona-context>` block: any case, spaces
 * around the slash and before `>`. The same pattern as the hub's `prompts.ContextBody` and the web's `contextBody`.
 */
val contextClosingTag = ProtocolCompatibility.contextClosingTag

/**
 * [text] made safe to go inside a context block: nothing left in it closes the block, so what follows cannot sit
 * outside it as the user's own words (an inbox item handed to the AI comes from someone else, #3831). Closing tags are
 * removed, and one that the removal itself puts together (`</persona-</persona-context>context>`) has its `<` written as
 * `&lt;`, which joins nothing. Two passes, linear in the text; the same output as the hub and the web.
 */
fun contextBody(text: String): String =
    text.replace(contextClosingTag, "").replace(contextClosingTag) { "&lt;" + it.groupValues[1] }
