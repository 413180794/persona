package persona.feature.chat

import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import persona.core.contracts.api.*
import persona.core.contracts.JsonCodec
import kotlinx.serialization.encodeToString

data class ComposerAttachment(
    val id: String,
    val name: String,
    val uri: String,
    val image: Boolean,
    val path: String? = null,
    val error: String? = null,
    val size: Long? = null,
    val uploadState: DraftUploadState = DraftUploadState.Pending,
)

data class ChatComposerDraft(
    val text: String = "", val attachments: List<ComposerAttachment> = emptyList(),
    val sessionVersion: DraftSessionVersion? = null,
)

private fun DraftAttachment.composerAttachment() = ComposerAttachment(
    id, name, uri, type == DraftAttachmentType.Image, uploadedPath, error, size, uploadState)

internal fun DraftSnapshot.composerDraft() = ChatComposerDraft(text, attachments.map { it.composerAttachment() }, sessionVersion)

internal fun ChatComposerDraft.storedAttachments() = attachments.map {
    DraftAttachment(it.id, if (it.image) DraftAttachmentType.Image else DraftAttachmentType.File,
        it.uri, it.name, it.size, when {
            it.uploadState == DraftUploadState.Invalid -> DraftUploadState.Invalid
            it.path != null -> DraftUploadState.Uploaded
            it.error != null -> DraftUploadState.Failed
            else -> it.uploadState
        }, it.path, it.error)
}

@Stable
class ChatComposerState(initial: ChatComposerDraft = ChatComposerDraft()) {
    var focused by mutableStateOf(false)
    var focusRequest by mutableIntStateOf(0)
    var value by mutableStateOf(TextFieldValue(initial.text, TextRange(initial.text.length)))
    var attachments by mutableStateOf(initial.attachments)
    var sessionVersion by mutableStateOf(initial.sessionVersion)
    var currentSessionVersion by mutableStateOf<DraftSessionVersion?>(null)
    val oldVersion get() = hasContent && (currentSessionVersion?.replaced == true || sessionVersion?.let { origin ->
        currentSessionVersion?.let(origin::differsFrom)
    } == true)
    val draft get() = ChatComposerDraft(value.text, attachments, sessionVersion)
    val ready get() = attachments.all { it.path != null && it.uploadState != DraftUploadState.Invalid }
    val hasContent get() = value.text.isNotBlank() || attachments.isNotEmpty()

    internal fun beginEdit() {
        if (!hasContent) sessionVersion = currentSessionVersion
    }

    fun insert(text: String, atSelection: Boolean) {
        beginEdit()
        val start = if (atSelection) value.selection.min.coerceIn(0, value.text.length) else value.text.length
        val end = if (atSelection) value.selection.max.coerceIn(start, value.text.length) else start
        val prefix = value.text.take(start)
        val suffix = value.text.drop(end)
        val addition = (if (prefix.isNotEmpty() && !prefix.last().isWhitespace()) " " else "") + text +
            (if (suffix.isNotEmpty() && !suffix.first().isWhitespace()) " " else "")
        value = TextFieldValue(prefix + addition + suffix, TextRange(start + addition.length))
    }

    fun message(): String {
        check(ready)
        val images = attachments.filter { it.image }.mapNotNull { it.path }
        val files = attachments.filterNot { it.image }.mapNotNull { it.path }.map(::quoteComposerPath)
        val text = (listOf(value.text) + files).filter(String::isNotBlank).joinToString(" ")
        return text + if (images.isEmpty()) "" else "\n\n<attached_images>\n${images.joinToString("\n")}\n</attached_images>"
    }

    fun clearIfUnchanged(sent: ChatComposerDraft) {
        if (draft == sent) { value = TextFieldValue(); attachments = emptyList(); sessionVersion = currentSessionVersion }
    }

    fun editMessage(text: String) {
        beginEdit()
        val paths = composerImageTags.findAll(text).flatMap { it.groupValues[1].lineSequence() }
            .map { it.trim() }.filter { it.startsWith('/') }.toList()
        val plain = text.replace(composerImageTags, "").trimEnd()
        value = TextFieldValue(plain, TextRange(plain.length))
        attachments = paths.mapIndexed { index, path ->
            ComposerAttachment("restored-$index-$path", path.substringAfterLast('/'), "", true, path)
        }
    }

    internal fun restoreMessage(text: String) {
        if (!hasContent) { editMessage(text); return }
        val restored = ChatComposerState().apply { editMessage(text) }
        val combined = listOf(value.text, restored.value.text).filter(String::isNotBlank).joinToString("\n\n")
        value = TextFieldValue(combined, TextRange(combined.length))
        attachments = attachments + restored.attachments.filter { item -> attachments.none { it.id == item.id } }
    }

    companion object {
        val Saver = listSaver<ChatComposerState, String>(
            save = { state -> listOf(state.value.text, JsonCodec.encodeToString(state.draft.storedAttachments()),
                state.sessionVersion?.let { JsonCodec.encodeToString(it) }.orEmpty()) },
            restore = { values ->
                if (values.size == 3) ChatComposerState(ChatComposerDraft(values[0],
                    JsonCodec.decodeFromString<List<DraftAttachment>>(values[1]).map { it.composerAttachment() },
                    values[2].takeIf(String::isNotEmpty)?.let { JsonCodec.decodeFromString<DraftSessionVersion>(it) }))
                else ChatComposerState(ChatComposerDraft(values.first(), values.drop(1).chunked(6).map {
                    ComposerAttachment(it[0], it[1], it[2], it[3].toBoolean(), it[4].ifEmpty { null }, it[5].ifEmpty { null })
                }))
            },
        )
    }
}

@Composable
fun rememberChatComposerState(initial: ChatComposerDraft = ChatComposerDraft()): ChatComposerState =
    rememberSaveable(saver = ChatComposerState.Saver) { ChatComposerState(initial) }

internal fun quoteComposerPath(path: String): String =
    if (path.any { it.isWhitespace() || it in "'\"\\\$`;&|<>()[]{}*?!" }) "'${path.replace("'", "'\\''")}'" else path

private val composerImageTags = Regex("<attached_images>([\\s\\S]*?)</attached_images>")
internal fun composerMessagePreview(text: String): String = text.replace(composerImageTags, "[图片]").trim()
