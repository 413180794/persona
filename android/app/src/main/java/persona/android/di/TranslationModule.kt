package persona.android.di

import android.app.Application
import android.content.Context
import android.content.SharedPreferences
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.builtins.ListSerializer
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.TranslateChat
import persona.core.contracts.api.TranslationPrefs
import persona.core.contracts.api.TranslationSettings
import persona.core.hub.HttpTranslationTransport
import persona.feature.inbox.Translator
import persona.android.PersonaApp
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object TranslationModule {
    @Provides @Singleton fun translation(app: Application): TranslationSettings =
        StoredTranslationSettings(app.getSharedPreferences(StoredTranslationSettings.FILE, Context.MODE_PRIVATE))

    /** The inbox's translations, on the selected group's hub (null while there is none, as for model configuration). */
    @Provides @Singleton fun translator(app: Application, settings: TranslationSettings, runtime: ConnectionRuntime): Translator =
        Translator(settings.prefs, transport = {
            runtime.scopedData.value?.let { HttpTranslationTransport(runtime.connectionFor(it)) }
        }, scope = (app as PersonaApp).scope)
}

/**
 * Settings › Translation kept on this phone, like the iOS app's `translate.*` defaults: not tied to a hub, so it survives
 * re-pairing (the picked chats keep their names for the list).
 */
class StoredTranslationSettings(private val stored: SharedPreferences) : TranslationSettings {
    private val current = MutableStateFlow(read())
    override val prefs: StateFlow<TranslationPrefs> = current.asStateFlow()

    override fun update(transform: (TranslationPrefs) -> TranslationPrefs) {
        current.update { old -> transform(old).let { it.copy(target = TranslationPrefs.knownTarget(it.target)) } }
        val next = current.value
        val editor = stored.edit()
            .putBoolean(ENABLED, next.enabled)
            .putString(TARGET, next.target)
            .putBoolean(MAIL, next.mail)
        if (next.chats.isEmpty()) editor.remove(CHATS) else editor.putString(CHATS, encodeChats(next.chats))
        editor.apply()
    }

    private fun read() = TranslationPrefs(
        enabled = stored.getBoolean(ENABLED, false),
        target = TranslationPrefs.knownTarget(stored.getString(TARGET, null)),
        mail = stored.getBoolean(MAIL, true),
        chats = decodeChats(stored.getString(CHATS, null)),
    )

    companion object {
        const val FILE = "persona-translate"
        private const val ENABLED = "translate.enabled"
        private const val TARGET = "translate.target"
        private const val MAIL = "translate.mail"
        private const val CHATS = "translate.larkChats"
        private val chatList = ListSerializer(TranslateChat.serializer())

        fun encodeChats(chats: List<TranslateChat>): String = JsonCodec.encodeToString(chatList, chats)

        /** Anything unreadable is no chats, never a crash at start. */
        fun decodeChats(raw: String?): List<TranslateChat> =
            raw?.let { runCatching { JsonCodec.decodeFromString(chatList, it) }.getOrNull() }.orEmpty().filter { it.id.isNotBlank() }
    }
}
