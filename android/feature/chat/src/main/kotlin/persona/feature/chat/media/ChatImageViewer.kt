package persona.feature.chat.media

import android.content.Context
import android.graphics.Bitmap
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import persona.core.designsystem.ImageEditor
import persona.core.designsystem.ImageViewer
import persona.core.designsystem.LocalAppFeedback
import persona.core.designsystem.ViewerAction
import persona.core.designsystem.ViewerImage
import persona.core.designsystem.decodePicture
import persona.core.designsystem.icons.PhIcons
import persona.feature.chat.ComposerAttachment
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * The image viewer of a conversation: [ImageViewer] with 保存, 编辑 (only with [onEdited]), 分享 and 拷贝.
 *
 * 编辑 opens the markup editor on the current picture; 完成 makes it a photo for the composer (long side 1600, JPEG
 * 75, in `cache/chat-captures/`), hands it to [onEdited], says 「已加到输入框，补一句话就能发」 and closes the viewer
 * through [onDismiss]. With [photoLimitReached] (the composer already holds 4 photos) 编辑 says 「一次最多 4 张」
 * instead. Results of 保存 / 分享 / 拷贝 show inside the viewer, which stays open.
 */
@Composable
fun ChatImageViewer(
    images: List<ViewerImage>,
    initialIndex: Int,
    onDismiss: () -> Unit,
    /** null: no 编辑 button. Gets the marked-up photo as a composer attachment; the viewer closes after. */
    onEdited: ((ComposerAttachment) -> Unit)? = null,
    /** The composer already holds 4 photos: 编辑 says 「一次最多 4 张」 instead of opening the editor. */
    photoLimitReached: Boolean = false,
) {
    val context = LocalContext.current
    val feedback = LocalAppFeedback.current
    val media = remember(context) { AndroidChatMedia(context) }
    var editing by remember { mutableStateOf<EditRequest?>(null) }
    val edited by rememberUpdatedState(onEdited)
    val dismiss by rememberUpdatedState(onDismiss)
    val limitReached by rememberUpdatedState(photoLimitReached)

    // Runs as the 编辑 action: the action stays busy while the editor is open, and what it returns shows in the viewer.
    suspend fun edit(bytes: ByteArray): String? {
        val source = try {
            withContext(Dispatchers.Default) { decodePicture(bytes, MediaShare.EditedMaxSide, atMost = false, software = true) }
        } catch (e: CancellationException) {
            throw e
        } catch (_: Throwable) {
            return Unreadable
        }
        val request = EditRequest(source)
        editing = request
        try {
            val result = request.result.await() ?: return null
            // The composer may have filled up while the editor was open.
            if (limitReached) return PhotoLimit
            val attachment = try {
                media.writeEdited(result)
            } catch (e: CancellationException) {
                throw e
            } catch (_: Throwable) {
                return NotAttached
            }
            edited?.invoke(attachment)
            // The viewer closes right away, so this one goes to the app's own notices.
            feedback?.notice(Attached)
            dismiss()
            return null
        } finally {
            if (editing === request) editing = null
        }
    }

    val editAction: suspend (ByteArray) -> String? = { bytes -> edit(bytes) }
    val actions = chatViewerActions(media, { limitReached }, editAction.takeIf { onEdited != null })
    ImageViewer(images, initialIndex, onDismiss, actions)
    editing?.let { request ->
        key(request) {
            ImageEditor(request.source, onCancel = { request.result.complete(null) }, onDone = { request.result.complete(it) })
        }
    }
}

/** One open editor: the picture it works on and what 完成 / 取消 gave (null: cancelled). */
private class EditRequest(val source: Bitmap) {
    val result = CompletableDeferred<Bitmap?>()
}

/** What the viewer's actions need from the phone; [AndroidChatMedia] does it for real, tests fake it. */
internal interface ChatMedia {
    suspend fun save(bytes: ByteArray): Boolean
    suspend fun share(bytes: ByteArray): Boolean
    suspend fun copy(bytes: ByteArray): Boolean
}

internal class AndroidChatMedia(private val context: Context) : ChatMedia {
    override suspend fun save(bytes: ByteArray) = MediaShare.saveImage(context, bytes)
    override suspend fun share(bytes: ByteArray) = MediaShare.shareImage(context, bytes)
    override suspend fun copy(bytes: ByteArray) = MediaShare.copyImage(context, bytes)
    suspend fun writeEdited(picture: Bitmap): ComposerAttachment = MediaShare.writeEditedPhoto(context, picture)
}

internal const val Saved = "已保存到相册"
internal const val SaveFailed = "保存失败"
internal const val ShareFailed = "分享失败"
internal const val Copied = "已拷贝图片"
internal const val CopyFailed = "拷贝失败"
internal const val PhotoLimit = "一次最多 4 张"
internal const val Attached = "已加到输入框，补一句话就能发"
internal const val Unreadable = "这张图片没法编辑"
internal const val NotAttached = "没能放进输入框，请重试"

/**
 * The viewer's actions in order: 保存, 编辑 (when [edit] is given), 分享, 拷贝. Each returns the notice the viewer
 * shows; a successful share shows none (the share sheet is the answer). 编辑 checks [limitReached] first.
 */
internal fun chatViewerActions(
    media: ChatMedia,
    limitReached: () -> Boolean,
    edit: (suspend (ByteArray) -> String?)?,
): List<ViewerAction> = buildList {
    add(ViewerAction("保存", PhIcons.ArrowDown) { _, bytes -> if (media.save(bytes)) Saved else SaveFailed })
    if (edit != null) add(ViewerAction("编辑", PhIcons.PencilSimple) { _, bytes -> if (limitReached()) PhotoLimit else edit(bytes) })
    add(ViewerAction("分享", PhIcons.ShareNetwork) { _, bytes -> if (media.share(bytes)) null else ShareFailed })
    add(ViewerAction("拷贝", PhIcons.Copy) { _, bytes -> if (media.copy(bytes)) Copied else CopyFailed })
}
