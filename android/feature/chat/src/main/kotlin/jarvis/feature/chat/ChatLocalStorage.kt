package jarvis.feature.chat

import jarvis.core.chatlist.ChatAnchorStore
import jarvis.core.contracts.Item
import jarvis.core.contracts.SessionKey
import kotlinx.coroutines.flow.StateFlow
import java.io.File

/** Optional local capabilities supplied by the app's pairing-scoped repository facade. */
interface ChatLocalStorage {
    val localStorage: StateFlow<ChatStorageSnapshot?>
    suspend fun imageFile(session: SessionKey, item: Item, width: Int, height: Int): File
}

data class ChatStorageSnapshot(val scope: String, val anchors: ChatAnchorStore)
