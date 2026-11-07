package persona.core.contracts.api

import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.Serializable

// Settings › Translation (docs/android/android.md "Translation", the iOS Translator.swift, the web's lib/translate.ts): when it is on, content not
// in the target language is translated by the hub's translation model: mail (on by default) and the Feishu chats picked
// in settings (none by default: there are too many messages).

/** A Feishu chat whose messages are translated; its name is kept for the settings list. */
@Serializable
data class TranslateChat(val id: String, val name: String)

/** One target the hub can translate into. */
data class TranslateLanguage(val code: String, val name: String)

data class TranslationPrefs(
    val enabled: Boolean = false,
    val target: String = DEFAULT_TARGET,
    val mail: Boolean = true,
    val chats: List<TranslateChat> = emptyList(),
) {
    /** Mail gets translated now. */
    val translatesMail: Boolean get() = enabled && mail

    /** Messages of this Feishu chat get translated now. */
    fun translates(chat: String?): Boolean = enabled && chat != null && chats.any { it.id == chat }

    /** What the hub keeps for this device, so it translates new mail as it comes in. */
    val hubCopy: HubTranslatePrefs get() = HubTranslatePrefs(enabled, target, mail)

    companion object {
        const val DEFAULT_TARGET = "zh-Hans"

        /** The targets in `server/internal/assistantwire/inbox.go` (`TranslateLanguages`), in the order the other apps list them. */
        val languages: List<TranslateLanguage> = listOf(
            TranslateLanguage("zh-Hans", "简体中文"), TranslateLanguage("zh-Hant", "繁體中文"), TranslateLanguage("en", "English"), // i18n-ignore: language autonyms
            TranslateLanguage("ja", "日本語"), TranslateLanguage("ko", "한국어"), TranslateLanguage("fr", "Français"), // i18n-ignore: language autonyms
            TranslateLanguage("de", "Deutsch"), TranslateLanguage("es", "Español"), TranslateLanguage("ru", "Русский"),
            TranslateLanguage("pt", "Português"),
        )

        fun languageName(code: String): String = languages.firstOrNull { it.code == code }?.name ?: code

        /** A stored target the hub no longer knows falls back to the default. */
        fun knownTarget(code: String?): String = code?.takeIf { c -> languages.any { it.code == c } } ?: DEFAULT_TARGET
    }
}

/** The `translate` field of a device's prefs on the hub (`PUT /api/v1/devices/{id}/prefs`). */
@Serializable
data class HubTranslatePrefs(val enabled: Boolean = false, val target: String = "", val mail: Boolean = false)

/** Settings › Translation on this phone: kept on the device (not per hub, like iOS), sent to the paired hub (the selected group's). */
interface TranslationSettings {
    val prefs: StateFlow<TranslationPrefs>
    fun update(transform: (TranslationPrefs) -> TranslationPrefs)
}

/** A Feishu chat to pick for translation (the pinned ones, then the recent ones). */
data class LarkChatChoice(val id: String, val name: String, val avatar: String? = null, val isGroup: Boolean = true)

/** A mail's translation (`POST /api/v1/translate/mail`): the hub reads the message itself. */
data class MailTranslation(val subject: String, val text: String, val clipped: Boolean = false)

/** The hub's translation of a mail item, done as it came in (`translations[target]` on the inbox item). */
data class ItemTranslation(val subject: String, val preview: String)

/** The manager's translations (`server/internal/assistant/inbox_translate.go`), relayed by the hub. */
interface TranslationTransport {
    /** `POST /api/v1/translate {texts, target}`: short texts in order, "" where the model gave nothing. */
    suspend fun translate(texts: List<String>, target: String): List<String>

    /** `POST /api/v1/translate/mail {account_id, uid, mailbox, target}`. */
    suspend fun translateMail(account: String, uid: Long, mailbox: String, target: String): MailTranslation
}
