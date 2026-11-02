package jarvis.android.di

import android.app.Application
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import jarvis.android.JarvisApp
import jarvis.core.contracts.api.*
import jarvis.android.data.ScopedChatStorage
import jarvis.feature.chat.ChatAppBindings
import kotlinx.coroutines.flow.*
import javax.inject.Inject
import javax.inject.Singleton

/** Sessions, chats, requests and hub preferences over every hub group (MergedServices.kt). */
@Module
@InstallIn(SingletonComponent::class)
object ChatDataModule {
    @Provides @Singleton fun preferences(stacks: GroupStacks): HubPreferences = mergedPreferences(stacks)
    @Provides @Singleton fun sessions(stacks: GroupStacks): SessionsRepository = mergedSessions(stacks)
    @Provides @Singleton fun chats(owner: ChatDataOwner, stacks: GroupStacks): ChatRepository = mergedChats(stacks, owner.storage)
    @Provides @Singleton fun interactions(stacks: GroupStacks): InteractionCenter = mergedInteractions(stacks)
}

/** The chat storage facade and the selected group's chat services (what the hub-level pages, the assistant, use). */
@Singleton
class ChatDataOwner @Inject constructor(application: Application, runtime: ConnectionRuntime, stacks: GroupStacks) {
    private val app = application as JarvisApp
    val storage = ScopedChatStorage(runtime, app.scope)
    val wiring: StateFlow<ChatAppBindings?> = stacks.selected.map { it?.bindings }.stateIn(app.scope, SharingStarted.Eagerly, null)
}
