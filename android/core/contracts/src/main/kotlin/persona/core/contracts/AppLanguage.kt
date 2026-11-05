package persona.core.contracts

import java.util.Locale

/**
 * The languages the app's text comes in (docs/i18n.md): Simplified Chinese, English and Japanese. [System] follows the
 * phone: any Chinese (Traditional too) shows Simplified Chinese, Japanese shows Japanese, everything else English.
 * [tag] is the BCP 47 tag the hub gets.
 */
enum class AppLanguage(val tag: String?) {
    System(null), ZhHans("zh-Hans"), En("en"), Ja("ja");

    companion object {
        /** The language a locale's text is shown in; null for a language the app does not have. */
        fun of(locale: Locale): AppLanguage? = when (locale.language) {
            "zh" -> ZhHans
            "ja" -> Ja
            "en" -> En
            else -> null
        }

        /**
         * The language for the phone's preferred [locales]: only the first counts (docs/i18n.md「选哪种语言」), so
         * French then Japanese shows English; English too when there is none.
         */
        fun resolve(locales: List<Locale>): AppLanguage = locales.firstOrNull()?.let(::of) ?: En

        /** A stored or system-reported tag; an empty or unknown one is [System]. */
        fun fromTag(tag: String?): AppLanguage =
            tag?.takeIf { it.isNotBlank() }?.let { of(Locale.forLanguageTag(it)) } ?: System
    }
}

/**
 * The language the app shows right now, resolved (never [AppLanguage.System]); the hub client sends it as
 * `Accept-Language` with every request and as `client_hello.locale` (docs/i18n.md「协议」). Set by the app at start
 * and whenever 设置 › 语言 or the phone's language changes; [changes] tells the socket to say hello again.
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
