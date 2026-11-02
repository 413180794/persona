package jarvis.feature.inbox

import android.content.Intent
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.FileProvider
import jarvis.core.contracts.api.*
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** Sizes people read: bytes below 1 KB, then KB and MB. */
internal fun readableSize(bytes: Long): String {
    val value = bytes.coerceAtLeast(0)
    return when {
        value < 1024 -> "$value 字节"
        value < 1024 * 1024 -> "${(value + 512) / 1024} KB"
        else -> java.lang.String.format(java.util.Locale.ROOT, "%.1f MB", value / 1048576.0)
    }
}

/** Mail dates arrive as RFC 3339 or RFC 1123; show them in the phone's time zone. */
internal fun mailTime(value: String, zone: java.time.ZoneId = java.time.ZoneId.systemDefault(),
                      today: java.time.LocalDate = java.time.LocalDate.now(zone)): String {
    val instant = runCatching { java.time.OffsetDateTime.parse(value).toInstant() }.getOrNull()
        ?: runCatching { java.time.ZonedDateTime.parse(value, java.time.format.DateTimeFormatter.RFC_1123_DATE_TIME).toInstant() }.getOrNull()
        ?: return value
    val time = instant.atZone(zone)
    val pattern = when {
        time.toLocalDate() == today -> "今天 HH:mm"
        time.toLocalDate() == today.minusDays(1) -> "昨天 HH:mm"
        time.year == today.year -> "M月d日 HH:mm"
        else -> "yyyy年M月d日 HH:mm"
    }
    return java.time.format.DateTimeFormatter.ofPattern(pattern).format(time)
}

@Composable
internal fun MailAttachmentRow(transport: MailTransport?, ref: MailReference, uid: Long, attachment: MailAttachment, position: GroupPosition = GroupPosition.Single) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var downloading by remember(transport?.scope, uid, attachment.id) { mutableStateOf(false) }
    var progress by remember(transport?.scope, uid, attachment.id) { mutableStateOf<Pair<Long, Long?>?>(null) }
    var failure by remember(transport?.scope, uid, attachment.id) { mutableStateOf<String?>(null) }
    val files = remember(transport?.scope, ref, uid, attachment.id) { mutableListOf<MailAttachmentFile>() }
    val viewer = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        files.forEach { it.close() }
        files.clear()
    }
    DisposableEffect(files) { onDispose { files.forEach { it.close() }; files.clear() } }
    JGroupItem(position) {
        JRow(title = attachment.filename.ifBlank { "未命名附件" },
            subtitle = attachment.mime.ifBlank { "未知类型" },
            leadingIcon = PhIcons.FileText, value = readableSize(attachment.size),
            trailing = { JIconButton(
            icon = PhIcons.ArrowDown,
            contentDescription = if (downloading) "下载中" else if (failure != null) "重试下载" else "下载并打开",
            enabled = !downloading && transport is MailAttachmentTransport && attachment.id.isNotBlank(),
            onClick = {
                downloading = true
                failure = null
                progress = 0L to null
                scope.launch {
                    try {
                        val active = transport as MailAttachmentTransport
                        val downloaded = active.downloadAttachmentFile(ref, uid, attachment) { read, total -> progress = read to total }
                        files.add(downloaded)
                        try {
                            val uri = FileProvider.getUriForFile(context, "${context.packageName}.inbox.files", downloaded.file)
                            viewer.launch(Intent(Intent.ACTION_VIEW).setDataAndType(uri, attachment.mime.ifBlank { "application/octet-stream" })
                                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION))
                        } catch (error: Exception) {
                            files.remove(downloaded)
                            downloaded.close()
                            if (error is android.content.ActivityNotFoundException) failure = "已下载，未找到可打开此附件的应用"
                            else throw error
                        }
                    } catch (e: CancellationException) { throw e }
                    catch (_: Exception) { failure = "下载或打开失败，请重试" }
                    finally { downloading = false }
                }
            }) })
        progress?.takeIf { downloading }?.let { (read, total) ->
            JNotice(if (total != null && total > 0) "已下载 ${readableSize(read)}，共 ${readableSize(total)}" else "已下载 ${readableSize(read)}", status = Status.Working)
        }
        failure?.let { JNotice(it, status = Status.NeedsYou) }
    }
}
