package persona.core.contracts

import java.util.Locale

/**
 * An interface language from android/i18n/<tag>.properties. Gradle generates [GeneratedLanguages] from these
 * resources: adding a language never needs another enum case or changes to the picker (docs/i18n.md).
 */
data class AppLanguage internal constructor(val tag: String?, val ownName: String, internal val matches: List<String>) {
    companion object {
        val System = AppLanguage(null, "", emptyList())
        val entries: List<AppLanguage> by lazy { listOf(System) + GeneratedLanguages.entries }
        // Named accessors keep callers and tests of the original three languages source-compatible.
        val ZhHans: AppLanguage get() = entries.first { it.tag == "zh-Hans" }
        val En: AppLanguage get() = entries.first { it.tag == "en" }
        val Ja: AppLanguage get() = entries.first { it.tag == "ja" }

        /** The language a locale's text is shown in; null when the app has no catalog for it. */
        fun of(locale: Locale): AppLanguage? = entries.firstOrNull { locale.language.lowercase(Locale.ROOT) in it.matches }

        /** Only the phone's first preferred locale counts; a missing or unsupported language falls back to English. */
        fun resolve(locales: List<Locale>): AppLanguage = locales.firstOrNull()?.let(::of) ?: En

        /** A stored or system-reported tag; an empty or unknown one is [System]. */
        fun fromTag(tag: String?): AppLanguage =
            tag?.takeIf { it.isNotBlank() }?.let { of(Locale.forLanguageTag(it.replace('_', '-'))) } ?: System
    }
}

/**
 * The language the app shows right now, resolved (never [AppLanguage.System]); the hub client sends it as
 * `Accept-Language` with every request and as `client_hello.locale` (docs/i18n.md "Protocol"). Set by the app at start
 * and whenever Settings › Language or the phone's language changes; [changes] tells the socket to say hello again.
 */
object ClientLanguage {
    private val state = kotlinx.coroutines.flow.MutableStateFlow(AppLanguage.En)
    val changes: kotlinx.coroutines.flow.StateFlow<AppLanguage> get() = state

    var current: AppLanguage
        get() = state.value
        set(value) { state.value = if (value == AppLanguage.System) AppLanguage.En else value }

    /** The language's code, `zh-Hans`, `en` or `ja`. */
    val tag: String get() = current.tag ?: "en"
}
