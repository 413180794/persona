package persona.feature.files

import android.content.ClipData
import android.content.Context
import android.content.Intent
import androidx.core.content.FileProvider
import persona.core.contracts.api.SessionFileContent
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.util.Base64

/** Shares files of the files pane from this module's cache folder (`files-share/`), read-only. */
class FilesShareProvider : FileProvider()

internal object FileShare {
    /** Text this long goes as a file: a chooser carries its extras through a binder transaction that caps near 1 MB. */
    private const val INLINE_TEXT = 100_000

    /** Whether ⋯ offers 分享: text, or bytes that are whole (a cut image or PDF would not open). */
    fun canShare(file: SessionFileContent) = file.text != null || (file.data != null && !file.truncated)

    /** Shares [file] as text, or its bytes as a file through [FilesShareProvider]; false when it cannot be shared. */
    suspend fun share(context: Context, file: SessionFileContent): Boolean {
        val text = file.text
        val send = when {
            text != null && text.length <= INLINE_TEXT -> Intent(Intent.ACTION_SEND)
                .setType("text/plain").putExtra(Intent.EXTRA_TEXT, text).putExtra(Intent.EXTRA_SUBJECT, file.name)
            else -> {
                val bytes = withContext(Dispatchers.Default) {
                    text?.toByteArray() ?: runCatching { Base64.getMimeDecoder().decode(file.data.orEmpty()) }.getOrNull()
                } ?: return false
                val copy = withContext(Dispatchers.IO) { cached(context, file.name.ifEmpty { "file" }, bytes) }
                val uri = FileProvider.getUriForFile(context, "${context.packageName}.files.share", copy)
                Intent(Intent.ACTION_SEND).setType(shareType(file)).putExtra(Intent.EXTRA_STREAM, uri)
                    .putExtra(Intent.EXTRA_SUBJECT, file.name)
                    .also { it.clipData = ClipData.newRawUri(file.name, uri) }
                    .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            }
        }
        context.startActivity(Intent.createChooser(send, "分享").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        return true
    }

    internal fun shareType(file: SessionFileContent): String {
        val mime = file.mime?.substringBefore(';')?.trim().orEmpty()
        return when {
            file.text != null -> "text/plain"
            mime.isEmpty() -> "application/octet-stream"
            else -> mime
        }
    }

    /** The one file being shared (the previous one is removed), under its own name so the receiving app shows it. */
    private fun cached(context: Context, name: String, bytes: ByteArray): File {
        val folder = File(context.cacheDir, "files-share")
        folder.deleteRecursively()
        folder.mkdirs()
        val safe = name.replace('/', '_').replace('\\', '_').ifEmpty { "file" }
        return File(folder, safe).apply { writeBytes(bytes) }
    }
}
