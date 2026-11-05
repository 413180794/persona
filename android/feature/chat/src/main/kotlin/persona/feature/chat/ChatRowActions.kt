package persona.feature.chat

import androidx.compose.runtime.Stable
import androidx.compose.runtime.compositionLocalOf
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatRepository
import persona.core.designsystem.ViewerImage
import java.util.Base64

/**
 * What a row deep in the chat list can do beyond drawing itself: the page builds one and provides it around the list,
 * so the row composables keep their parameter lists (the old App's ChatEnv).
 */
@Stable
internal class ChatRowActions(
    val session: SessionKey,
    /** The whole text of an item the hub cut for the page (`full_size`). */
    val fullText: suspend (Item) -> String,
    /** Opens a path from a message or a tool card on the session's machine, at its line if any; null: paths are not offered. */
    val openPath: ((path: String, line: Int?) -> Unit)?,
    /** Which paths messages name are there on the session's machine; null: not asked (paths stay as they were). */
    val paths: PathPresence? = null,
    /** Photos sent with messages (`<attached_images>`), read from the session's machine; null: they cannot load. */
    val photos: AttachedPhotos?,
    /** Every picture of the conversation in order, for the image viewer to page through (read when it opens). */
    val pictures: () -> List<ViewerImage>,
    /** Opens the image viewer on [images] at [index]. */
    val openImages: (images: List<ViewerImage>, index: Int) -> Unit,
    /** The long-press menu of messages; null where messages have none. */
    val menu: MessageMenuState?,
    /** The one API error card that offers 重试 (the newest line, see [retryLine]); null: none does. */
    val retryLineId: String?,
    /** What 重试 does: send 「继续」. */
    val onRetry: () -> Unit,
    /** What a picture of the conversation is called to accessibility services, in the page's language. */
    val imageDescription: String = "",
) {
    /** Opens the viewer on the conversation's pictures at [item]; a picture not among them opens alone. */
    fun openPicture(item: Item) {
        val all = pictures()
        val index = all.indexOfFirst { it.key == item.id }
        if (index >= 0) openImages(all, index)
        else openImages(listOf(chatViewerImage(session, item, null, imageDescription)), 0)
    }
}

internal val LocalChatRowActions = compositionLocalOf<ChatRowActions?> { null }

/**
 * A picture of the conversation for the viewer: its bytes come inline with the page when the hub sent them, else
 * from the repository by item id (pages drop picture bytes, docs/android.md「缓存体积预算」).
 */
internal fun chatViewerImage(session: SessionKey, item: Item, repository: ChatRepository?, description: String = ""): ViewerImage {
    val image = item.image
    return ViewerImage(item.id, description, image?.width, image?.height) {
        val inline = image?.data?.takeIf { it.isNotEmpty() && image?.lazy != true }
        when {
            inline != null -> Base64.getMimeDecoder().decode(inline)
            repository != null -> repository.loadImage(session, item.id)
            else -> error("picture unavailable")
        }
    }
}

/** Every picture of the conversation in order: what the agent showed, the tools' screenshots, the photos it got. */
internal fun chatViewerImages(session: SessionKey, items: List<Item>, repository: ChatRepository?, description: String = ""): List<ViewerImage> =
    items.filter { it.kind == "image" && it.image != null }.map { chatViewerImage(session, it, repository, description) }

/** The image viewer to show: [images] from [index]. */
internal data class ViewerRequest(val images: List<ViewerImage>, val index: Int)
