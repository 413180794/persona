package persona.core.data

import persona.core.contracts.CoreText

/**
 * How the repositories here word what they put in their state (an error line, a pending reply's status, a recalled
 * message): they hold no resources of their own, so the words come from [CoreText.words], which the app points at its
 * resources in the shown language. Unset (unit tests) it is English.
 */
internal object DataText {
    fun of(key: CoreText.Key, vararg args: Any): String = CoreText.words(CoreText(key, args.toList()))
}
