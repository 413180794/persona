package jarvis.android.ui.media

import jarvis.android.data.Item
import jarvis.android.data.SessionKey

/** One image for the viewer: a chat item ([sessionKey] + [itemId]), a hub URL ([url]) or inline base64 [data]. */
data class ViewerImage(
    val sessionKey: SessionKey? = null,
    val itemId: String? = null,
    val url: String? = null,
    val data: String? = null,
    val mime: String? = null,
)

/** The image viewer: swipe through [images] from [index]; [canEdit] offers 编辑 (the result goes to the chat's composer). */
data class ImageViewerArgs(val images: List<ViewerImage>, val index: Int = 0, val canEdit: Boolean = false)

/** The viewer's handle on a chat image: inline bytes when the item has them, else fetched by id. */
fun imageOf(key: SessionKey, item: Item): ViewerImage =
    ViewerImage(sessionKey = key, itemId = item.id, data = item.image?.inlineData, mime = item.image?.mime)

/** Every image of the conversation in order, for the viewer to page through (iOS `chatImages`). */
fun chatImages(key: SessionKey, items: List<Item>): List<ViewerImage> =
    items.filter { it.kind == "image" && it.image != null }.map { imageOf(key, it) }
