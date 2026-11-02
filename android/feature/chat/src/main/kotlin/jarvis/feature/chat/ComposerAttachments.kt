package jarvis.feature.chat

import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.ImageDecoder
import android.net.Uri
import android.provider.OpenableColumns
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.core.contracts.api.ChatComposerService
import jarvis.core.contracts.api.ComposerOperationException
import jarvis.core.contracts.api.DraftUploadState
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import java.io.File
import java.util.UUID
import kotlin.math.roundToInt

internal const val MAX_COMPOSER_FILE_BYTES = 200L * 1024 * 1024

internal fun attachmentName(context: Context, uri: Uri): String =
    context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use {
        if (it.moveToFirst()) it.getString(0) else null
    } ?: "附件"

internal fun selectAttachment(context: Context, uri: Uri, image: Boolean): ComposerAttachment {
    val resolver = context.contentResolver
    runCatching { resolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION) }
    val name = runCatching { attachmentName(context, uri) }.getOrDefault("附件")
    val size = runCatching {
        resolver.query(uri, arrayOf(OpenableColumns.SIZE), null, null, null)?.use {
            if (it.moveToFirst() && !it.isNull(0)) it.getLong(0).takeIf { bytes -> bytes >= 0 } else null
        }
    }.getOrNull()
    val attachment = ComposerAttachment(UUID.randomUUID().toString(), name, uri.toString(), image, size = size)
    return if (attachmentReadable(context, attachment)) attachment else attachment.copy(
        uploadState = DraftUploadState.Invalid, error = "附件已失效或无法保留读取权限，请移除后重新选择")
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
                val scale = minOf(1.0, 1600.0 / maxOf(info.size.width, info.size.height))
                decoder.setTargetSize(maxOf(1, (info.size.width * scale).roundToInt()), maxOf(1, (info.size.height * scale).roundToInt()))
            }
            try { file.outputStream().use { check(bitmap.compress(Bitmap.CompressFormat.JPEG, 80, it)) } }
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
                        if (total > MAX_COMPOSER_FILE_BYTES) throw ComposerOperationException("文件不能超过 200 MB")
                        output.write(buffer, 0, read)
                    }
                }
            } ?: throw ComposerOperationException("文件无法读取，请重新选择")
        }
        return file
    } catch (error: Throwable) { file.delete(); throw error }
}

@Composable
internal fun ComposerAttachmentPreviews(state: ChatComposerState, service: ChatComposerService?, sending: Boolean) {
    if (state.attachments.isEmpty()) return
    Column(Modifier.heightIn(max = 140.dp).verticalScroll(rememberScrollState())) {
        state.attachments.forEach { attachment ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                JIcon(if (attachment.image) PhIcons.Image else PhIcons.File, null, tint = JarvisTheme.palette.ink2)
                Spacer(Modifier.width(8.dp))
                Column(Modifier.weight(1f)) {
                    Text(attachment.name, maxLines = 1, overflow = TextOverflow.Ellipsis,
                        style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink)
                    Text(attachment.error ?: when {
                        attachment.path != null -> "已上传，等待发送"
                        service == null -> "附件服务尚未接入"
                        else -> "正在上传…"
                    }, color = if (attachment.error != null) JarvisTheme.palette.danger.text else JarvisTheme.palette.ink2,
                        style = JarvisTheme.type.caption)
                }
                if (attachment.error != null && attachment.uploadState != DraftUploadState.Invalid) JTextButton("重试", {
                    state.attachments = state.attachments.map {
                        if (it.id == attachment.id) it.copy(error = null, uploadState = DraftUploadState.Pending) else it
                    }
                }, enabled = !sending && service != null)
                JIconButton(PhIcons.X, "移除附件", { state.attachments = state.attachments.filterNot { it.id == attachment.id } }, enabled = !sending)
            }
        }
    }
}
