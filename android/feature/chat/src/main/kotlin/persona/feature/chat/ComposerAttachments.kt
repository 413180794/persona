package persona.feature.chat

import android.content.Context
import android.content.Intent
import android.content.res.Resources
import android.graphics.Bitmap
import android.graphics.ImageDecoder
import android.net.Uri
import android.provider.OpenableColumns
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import persona.core.contracts.api.ChatComposerService
import persona.core.contracts.api.ComposerOperationException
import persona.core.contracts.api.DraftUploadState
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import java.io.File
import java.util.UUID
import kotlin.math.roundToInt
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

internal const val MAX_COMPOSER_FILE_BYTES = 200L * 1024 * 1024

/** Photos go to the computer as JPEG at this quality, the long side at most [PHOTO_MAX_SIDE] (as iOS). */
internal const val PHOTO_JPEG_QUALITY = 75
internal const val PHOTO_MAX_SIDE = 1600

/** Why a file cannot go to the computer at all: over the upload limit. Unknown sizes are counted while staging. */
internal fun oversizeReason(name: String, size: Long?, resources: Resources): String? =
    if (size != null && size > MAX_COMPOSER_FILE_BYTES) resources.getString(R.string.chat_attach_too_large, name) else null

internal fun attachmentName(context: Context, uri: Uri): String =
    context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use {
        if (it.moveToFirst()) it.getString(0) else null
    } ?: context.getString(R.string.chat_attach_default_name)

internal fun selectAttachment(context: Context, uri: Uri, image: Boolean): ComposerAttachment {
    val resolver = context.contentResolver
    runCatching { resolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION) }
    val name = runCatching { attachmentName(context, uri) }.getOrDefault(context.getString(R.string.chat_attach_default_name))
    val size = runCatching {
        resolver.query(uri, arrayOf(OpenableColumns.SIZE), null, null, null)?.use {
            if (it.moveToFirst() && !it.isNull(0)) it.getLong(0).takeIf { bytes -> bytes >= 0 } else null
        }
    }.getOrNull()
    val attachment = ComposerAttachment(UUID.randomUUID().toString(), name, uri.toString(), image, size = size)
    // A file over the limit is refused before anything is copied; retrying could not help, so it can only be removed.
    if (!image) oversizeReason(name, size, context.resources)?.let { return attachment.copy(uploadState = DraftUploadState.Invalid, error = it) }
    return if (attachmentReadable(context, attachment)) attachment else attachment.copy(
        uploadState = DraftUploadState.Invalid, error = context.getString(R.string.chat_attach_lost_permission))
}

/** App-owned camera URIs need no provider grant; external URIs must survive process death. */
internal fun attachmentReadable(context: Context, attachment: ComposerAttachment): Boolean {
    if (attachment.uri.isEmpty()) return attachment.path != null
    return runCatching {
        val uri = Uri.parse(attachment.uri)
        val granted = uri.authority == "${context.packageName}.chat.files" ||
            context.contentResolver.persistedUriPermissions.any { it.uri == uri && it.isReadPermission }
        granted && context.contentResolver.openFileDescriptor(uri, "r")?.use { true } == true
    }.getOrDefault(false)
}

/** Stage provider streams locally to guarantee Content-Length and bound memory use. */
internal fun stageAttachment(context: Context, attachment: ComposerAttachment): File {
    val uri = Uri.parse(attachment.uri)
    val directory = File(context.cacheDir, "chat-uploads").apply { mkdirs() }
    val file = File.createTempFile("upload-", if (attachment.image) ".jpg" else ".bin", directory)
    try {
        if (attachment.image) {
            val bitmap = ImageDecoder.decodeBitmap(ImageDecoder.createSource(context.contentResolver, uri)) { decoder, info, _ ->
                decoder.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
                val scale = minOf(1.0, PHOTO_MAX_SIDE.toDouble() / maxOf(info.size.width, info.size.height))
                decoder.setTargetSize(maxOf(1, (info.size.width * scale).roundToInt()), maxOf(1, (info.size.height * scale).roundToInt()))
            }
            try { file.outputStream().use { check(bitmap.compress(Bitmap.CompressFormat.JPEG, PHOTO_JPEG_QUALITY, it)) } }
            finally { bitmap.recycle() }
        } else {
            context.contentResolver.openInputStream(uri)?.use { input ->
                file.outputStream().use { output ->
                    val buffer = ByteArray(64 * 1024)
                    var total = 0L
                    while (true) {
                        val read = input.read(buffer)
                        if (read < 0) break
                        total += read
                        if (total > MAX_COMPOSER_FILE_BYTES) throw ComposerOperationException(persona.core.contracts.CoreText.plain(oversizeReason(attachment.name, total, context.resources)!!))
                        output.write(buffer, 0, read)
                    }
                }
            } ?: throw ComposerOperationException(persona.core.contracts.CoreText.plain(context.getString(R.string.chat_attach_unreadable)))
        }
        return file
    } catch (error: Throwable) { file.delete(); throw error }
}

/** How much to scale a photo so its short side covers a thumbnail of [target] pixels; never enlarges. */
internal fun thumbnailScale(width: Int, height: Int, target: Int): Double =
    if (width <= 0 || height <= 0 || target <= 0) 1.0 else minOf(1.0, target.toDouble() / minOf(width, height))

/** A small copy of a local photo for the composer, or null when it cannot be read. Call off the main thread. */
internal fun decodeThumbnail(context: Context, uri: String, target: Int): ImageBitmap? = runCatching {
    ImageDecoder.decodeBitmap(ImageDecoder.createSource(context.contentResolver, Uri.parse(uri))) { decoder, info, _ ->
        val scale = thumbnailScale(info.size.width, info.size.height, target)
        decoder.setTargetSize(maxOf(1, (info.size.width * scale).roundToInt()), maxOf(1, (info.size.height * scale).roundToInt()))
    }.asImageBitmap()
}.getOrNull()

/** The thumbnail of a photo attachment, decoded in the background; null while decoding or when there is no local copy. */
@Composable
private fun rememberThumbnail(uri: String): ImageBitmap? {
    val context = LocalContext.current
    val target = with(LocalDensity.current) { AttachmentThumbSize.roundToPx() }
    val image by produceState<ImageBitmap?>(null, uri, target) {
        // Restored from a sent message: the photo is only on the computer.
        if (uri.isNotEmpty()) value = withContext(Dispatchers.IO) { decodeThumbnail(context.applicationContext, uri, target) }
    }
    return image
}

/** What the composer shows on a photo while it is on its way to the computer. */
internal fun ComposerAttachment.thumbnailState(service: ChatComposerService?): AttachmentState = when {
    uploadState == DraftUploadState.Invalid -> AttachmentState.Invalid
    error != null -> AttachmentState.Failed
    path != null -> AttachmentState.Ready
    uploadState == DraftUploadState.Uploading -> AttachmentState.Uploading
    service == null -> AttachmentState.Waiting
    else -> AttachmentState.Uploading
}

/** Photos as a row of thumbnails (iOS); files keep one row each with their name, state, 重试 and ✕. */
@Composable
internal fun ComposerAttachmentPreviews(state: ChatComposerState, service: ChatComposerService?, sending: Boolean, enabled: Boolean = true) {
    if (state.attachments.isEmpty()) return
    fun remove(id: String) { state.attachments = state.attachments.filterNot { it.id == id } }
    fun retry(id: String) {
        state.attachments = state.attachments.map { if (it.id == id) it.copy(error = null, uploadState = DraftUploadState.Pending) else it }
    }
    val photos = state.attachments.filter { it.image }
    val files = state.attachments.filterNot { it.image }
    val canRetry = !sending && enabled && service != null
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        if (photos.isNotEmpty()) {
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                photos.forEachIndexed { index, photo ->
                    key(photo.id) {
                        // The badge stays while a send is in flight (no flicker); taps are ignored until it finishes.
                        AttachmentThumb(rememberThumbnail(photo.uri), stringResource(R.string.chat_attach_photo, index + 1), state = photo.thumbnailState(service),
                            onRemove = { if (!sending) remove(photo.id) },
                            onRetry = if (canRetry) ({ retry(photo.id) }) else null)
                    }
                }
            }
            // The thumbnails only mark a failure; why it failed is said once under them.
            photos.firstNotNullOfOrNull { it.error }?.let {
                Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.danger.text, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        if (files.isNotEmpty()) Column(Modifier.heightIn(max = 140.dp).verticalScroll(rememberScrollState())) {
            files.forEach { attachment ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    JIcon(PhIcons.File, null, tint = PersonaTheme.palette.ink2)
                    Spacer(Modifier.width(Tokens.Space.s))
                    Column(Modifier.weight(1f)) {
                        Text(attachment.name, maxLines = 1, overflow = TextOverflow.Ellipsis,
                            style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink)
                        Text(attachment.error ?: when {
                            attachment.path != null -> stringResource(R.string.chat_attach_uploaded)
                            service == null -> stringResource(R.string.chat_attach_no_service)
                            else -> stringResource(R.string.chat_attach_uploading)
                        }, color = if (attachment.error != null) PersonaTheme.palette.danger.text else PersonaTheme.palette.ink2,
                            style = PersonaTheme.type.caption)
                    }
                    if (attachment.error != null && attachment.uploadState != DraftUploadState.Invalid)
                        JTextButton(stringResource(R.string.chat_attach_retry), { retry(attachment.id) }, enabled = canRetry)
                    JIconButton(PhIcons.X, stringResource(R.string.chat_attach_remove), { remove(attachment.id) }, enabled = !sending)
                }
            }
        }
    }
}
