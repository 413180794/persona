package jarvis.feature.chat

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.ChatComposerService
import jarvis.core.contracts.api.ComposerOperationException
import jarvis.core.contracts.api.DraftUploadState
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import java.io.File
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@Composable
fun ChatComposer(
    session: SessionKey,
    state: ChatComposerState,
    working: Boolean,
    sending: Boolean,
    canClear: Boolean,
    service: ChatComposerService?,
    onSend: () -> Unit,
    onCommand: (String) -> Unit,
    onNotice: (String) -> Unit,
    // Content queued outside the text field (selected page context) also makes the draft sendable.
    extraContent: Boolean = false,
    enabled: Boolean = true,
    placeholder: String = "输入消息",
    leading: (@Composable RowScope.() -> Unit)? = null,
    accessory: (@Composable ColumnScope.() -> Unit)? = null,
    modeHint: String? = null,
    onOverlay: (Boolean) -> Unit = {},
    visible: Boolean = true,
) {
    val context = LocalContext.current
    val hasContent = state.hasContent || extraContent
    val scope = rememberCoroutineScope()
    var menu by remember { mutableStateOf(false) }
    var confirmClear by remember { mutableStateOf(false) }
    val focusRequester = remember { androidx.compose.ui.focus.FocusRequester() }
    LaunchedEffect(menu, confirmClear) { onOverlay(menu || confirmClear) }
    DisposableEffect(Unit) { onDispose { onOverlay(false) } }
    var cameraUri by rememberSaveable { mutableStateOf<String?>(null) }
    var interruptPending by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(working) { if (!working) interruptPending = false }

    fun select(uris: List<Uri>, image: Boolean) {
        scope.launch {
            val allowed = if (image) (4 - state.attachments.count { it.image }).coerceAtLeast(0) else uris.size
            if (uris.size > allowed) onNotice("最多添加 4 张照片")
            for (uri in uris.take(allowed)) {
                try {
                    val attachment = withContext(Dispatchers.IO) { selectAttachment(context, uri, image) }
                    state.beginEdit()
                    state.attachments += attachment
                    attachment.error?.let(onNotice)
                } catch (e: CancellationException) { throw e }
                catch (_: Exception) { onNotice("附件无法读取，请重新选择；输入已保留") }
            }
        }
    }
    val photos = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(4)) { select(it, true) }
    val files = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { select(it, false) }
    val camera = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { success ->
        cameraUri?.let { value ->
            if (success) select(listOf(Uri.parse(value)), true)
            else runCatching { context.contentResolver.delete(Uri.parse(value), null, null) }
        }
        cameraUri = null
    }
    fun launchPicker(action: () -> Unit) {
        menu = false
        try { action() } catch (_: Exception) { onNotice("无法打开选择器，请检查是否安装了对应应用") }
    }

    state.attachments.forEach { attachment ->
        key(attachment.id) {
            LaunchedEffect(attachment.id, attachment.error, service) {
                if (attachment.uploadState == DraftUploadState.Invalid) return@LaunchedEffect
                if (!withContext(Dispatchers.IO) { attachmentReadable(context, attachment) }) {
                    val reason = "附件已失效或读取权限已丢失，请移除后重新选择"
                    state.attachments = state.attachments.map {
                        if (it.id == attachment.id) it.copy(uploadState = DraftUploadState.Invalid, error = reason) else it
                    }
                    onNotice(reason)
                    return@LaunchedEffect
                }
                if (attachment.path != null) return@LaunchedEffect
                if (attachment.error != null || service == null) return@LaunchedEffect
                try {
                    state.attachments = state.attachments.map {
                        if (it.id == attachment.id) it.copy(uploadState = DraftUploadState.Uploading) else it
                    }
                    val path = withContext(Dispatchers.IO) {
                        val staged = stageAttachment(context, attachment)
                        try {
                            service.upload(session, if (attachment.image) "${attachment.name.substringBeforeLast('.')}.jpg" else attachment.name, staged)
                        } finally { staged.delete() }
                    }
                    if (state.attachments.any { it.id == attachment.id }) {
                        state.attachments = state.attachments.map {
                            if (it.id == attachment.id) it.copy(path = path, uploadState = DraftUploadState.Uploaded) else it
                        }
                    }
                } catch (e: CancellationException) { throw e }
                catch (e: Exception) {
                    val reason = (e as? ComposerOperationException)?.message ?: "上传失败，请重试；输入已保留"
                    state.attachments = state.attachments.map {
                        if (it.id == attachment.id) it.copy(error = reason, uploadState = DraftUploadState.Failed) else it
                    }
                }
            }
        }
    }

    LaunchedEffect(visible) { if (!visible) { menu = false; confirmClear = false } }
    if (!visible) return
    LaunchedEffect(state.focusRequest) { if (state.focusRequest > 0) focusRequester.requestFocus() }

    ComposerFrame(
        leading = {
            JIconButton(PhIcons.Plus, "添加附件和快捷指令", { menu = true }, enabled = !sending)
            leading?.invoke(this)
        },
        trailing = {
            if (working && !hasContent) SendButton(onClick = {
                if (service != null) {
                    interruptPending = true
                    scope.launch {
                        try { service.interrupt(session); onNotice("已请求中断，排队消息仍会保留") }
                        catch (e: CancellationException) { throw e }
                        catch (_: Exception) { onNotice("中断结果待确认，请查看终端后再操作") }
                    }
                }
            }, enabled = !sending && !interruptPending && service != null, kind = SendKind.Stop)
            else SendButton(onClick = onSend, enabled = hasContent && state.ready && !sending, loading = sending)
        },
        accessory = {
            if (state.oldVersion) Text("这份草稿来自会话旧版本，请核对后再发送", style = JarvisTheme.type.caption,
                color = JarvisTheme.palette.cautionText)
            ComposerAttachmentPreviews(state, service, sending)
            accessory?.invoke(this)
            modeHint?.let { JChip(it, icon = PhIcons.SlidersHorizontal) }
            if (working && hasContent) Text("会排队发送", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
        },
    ) {
        BasicTextField(state.value, { state.beginEdit(); state.value = it },
            modifier = Modifier.fillMaxWidth().heightIn(max = 160.dp).focusRequester(focusRequester)
                .onFocusChanged { state.focused = it.isFocused },
            enabled = enabled, textStyle = JarvisTheme.type.body.copy(color = JarvisTheme.palette.ink),
            cursorBrush = SolidColor(JarvisTheme.palette.ink),
            decorationBox = { field ->
                Box {
                    if (state.value.text.isEmpty()) Text(if (working) "补充说明（会排队发送）" else placeholder,
                        style = JarvisTheme.type.body, color = JarvisTheme.palette.ink3, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    field()
                }
            })
    }
    if (menu) ChatActionsSheet(onDismiss = { menu = false }, title = "附件和快捷指令", actions = buildList {
        add(JAction("照片", enabled = service != null && state.attachments.count { it.image } < 4) {
            launchPicker { photos.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }
        })
        add(JAction("拍照", enabled = service != null && state.attachments.count { it.image } < 4) {
            launchPicker {
                val folder = File(context.cacheDir, "chat-captures").apply { mkdirs() }
                val file = File.createTempFile("camera-", ".jpg", folder)
                val uri = FileProvider.getUriForFile(context, "${context.packageName}.chat.files", file)
                cameraUri = uri.toString()
                camera.launch(uri)
            }
        })
        add(JAction("文件", enabled = service != null) { launchPicker { files.launch(arrayOf("*/*")) } })
        composerCommands.forEach { (title, command) -> add(JAction(title) { menu = false; onCommand(command) }) }
        if (canClear) add(JAction("清空上下文 /clear", destructive = true, enabled = !working) { menu = false; confirmClear = true })
    })
    if (confirmClear) JDialog(onDismiss = { confirmClear = false }, title = "清空上下文？",
        text = "电脑上的 Agent 将开始新的上下文，手机里的未发送输入会保留。",
        confirm = "清空" to { confirmClear = false; onCommand("/clear") })
}

private val composerCommands = listOf(
    "继续" to "继续", "压缩上下文 /compact" to "/compact", "查看上下文 /context" to "/context",
    "提交并推送" to "把当前改动按规范提交并推送", "汇报进展" to "简要汇报一下目前的进展、还剩什么",
)
