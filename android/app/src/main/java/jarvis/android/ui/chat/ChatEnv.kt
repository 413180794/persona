package jarvis.android.ui.chat

import androidx.compose.runtime.staticCompositionLocalOf
import jarvis.android.data.Hub
import jarvis.android.data.SessionKey
import jarvis.android.ui.files.FilesTarget
import jarvis.android.ui.media.ChatImageLoader
import jarvis.android.ui.media.ImageViewerArgs
import jarvis.android.ui.media.ViewerImage

/**
 * What a chat's rows need from the page (iOS ChatView's environment values: `openFile`, `chatImages`, `loadImage`,
 * `loadFullText`). One per chat page; the page sets the callbacks (it shows the files and image screens over itself).
 * [gallery] is refreshed as items arrive and read only when an image is tapped.
 */
class ChatEnv(val hub: Hub, val key: SessionKey) {
    /** The session's working directory (relative paths in messages are resolved against it). */
    @Volatile var cwd: String? = null

    /** Files on the session's machine can be opened (the machine is known and the session has a folder). */
    @Volatile var canOpenFiles: Boolean = true

    /** Every image of the conversation, in order (the viewer pages through all of them). */
    @Volatile var gallery: List<ViewerImage> = emptyList()

    /** Long-press menu of a message (set by the page). */
    var menu: MessageMenuState? = null

    var onOpenFiles: (FilesTarget) -> Unit = {}
    var onOpenImage: (ImageViewerArgs) -> Unit = {}

    fun openFile(path: String) {
        if (!canOpenFiles) return
        onOpenFiles(FilesTarget(key.machineId, resolvePath(path, cwd), cwd))
    }

    /** Full-screen viewer at [img], paging through the conversation's pictures (or [local] when it isn't among them). */
    fun openImage(img: ViewerImage, local: List<ViewerImage> = listOf(img)) {
        val id = ChatImageLoader.identity(img)
        val all = gallery
        val at = all.indexOfFirst { ChatImageLoader.identity(it) == id }
        onOpenImage(
            if (at >= 0) ImageViewerArgs(all, at, canEdit = true)
            else ImageViewerArgs(local, local.indexOfFirst { ChatImageLoader.identity(it) == id }.coerceAtLeast(0), canEdit = true),
        )
    }
}

val LocalChatEnv = staticCompositionLocalOf<ChatEnv?> { null }

/** A path from a message, absolute (relative ones against [cwd]; ~ stays for the hub to expand). */
fun resolvePath(path: String, cwd: String?): String = when {
    path.startsWith("/") || path.startsWith("~") || cwd.isNullOrEmpty() -> path
    else -> cwd.trimEnd('/') + "/" + path.removePrefix("./")
}
