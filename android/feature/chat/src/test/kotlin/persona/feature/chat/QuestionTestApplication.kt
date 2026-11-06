package persona.feature.chat

import android.app.Application
import dagger.hilt.internal.GeneratedComponent
import dagger.hilt.internal.GeneratedComponentManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import persona.core.contracts.*
import persona.core.contracts.api.*

/** Supplies the real question editor with isolated in-memory repositories. */
class QuestionTestApplication : Application(), GeneratedComponentManager<Any> {
    private val chats = object : ChatRepository {
        override fun observe(session: SessionKey) = flowOf(ChatSnapshot())
        override suspend fun loadOlder(session: SessionKey) = error("Not used by question editor")
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int): List<Item> = error("Not used by question editor")
    }
    private val interactions = object : InteractionCenter {
        override val pending = MutableStateFlow(emptyList<Interaction>())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = error("Resolutions belong to the test callback")
    }
    private val store by lazy { RequestDraftStore(this, chats, interactions) }
    private val component = object : RequestEditorServices, GeneratedComponent {
        override fun requestDrafts() = store
        override fun chats() = chats
    }
    override fun generatedComponent(): Any = component
}
