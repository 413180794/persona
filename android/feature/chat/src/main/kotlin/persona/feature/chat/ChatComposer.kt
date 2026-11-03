package persona.feature.chat

import android.Manifest
import android.content.ActivityNotFoundException
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
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatComposerService
import persona.core.contracts.api.ComposerOperationException
import persona.core.contracts.api.DraftUploadState
import persona.core.contracts.api.MessageTransport
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import java.io.File
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull

/** How long an upload waits for the hub connection to be ready again before it fails (a setting, not measured). */
internal const val UPLOAD_READY_WAIT_MILLIS = 15_000L

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
    /** Who /clear restarts, for its confirmation ("Claude 会开一个新会话…"). */
    agentName: String = "Claude",
    /** False for a session whose source does not let the phone interrupt it: no stop while it works. */
    canInterrupt: Boolean = true,
) {
    val context = LocalContext.current
    val hasContent = state.hasContent || extraContent
    val scope = rememberCoroutineScope()
    var menu by remember { mutableStateOf(false) }
    var confirmClear by remember { mutableStateOf(false) }
    var cameraDenied by remember { mutableStateOf(false) }
    val focusRequester = remember { androidx.compose.ui.focus.FocusRequester() }
    LaunchedEffect(menu, confirmClear, cameraDenied) { onOverlay(menu || confirmClear || cameraDenied) }
    DisposableEffect(Unit) { onDispose { onOverlay(false) } }
    var cameraUri by rememberSaveable { mutableStateOf<String?>(null) }
    var interruptPending by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(working) { if (!working) interruptPending = false }
    // A photo taken here waits in cache/chat-captures until it leaves this draft: removed, or sent and cleared. Only the
    // draft's own captures go; the folder is never swept (another conversation's draft may still hold one).
    LaunchedEffect(state) {
        var before = state.attachments
        snapshotFlow { state.attachments }.collect { now ->
            val gone = leftCaptures(before, now, context.packageName)
            before = now
            if (gone.isNotEmpty()) withContext(Dispatchers.IO) { gone.forEach { deleteCapture(context, it) } }
        }
    }

    fun select(uris: List<Uri>, image: Boolean, capture: Boolean = false) {
        scope.launch {
            val allowed = if (image) (4 - state.attachments.count { it.image }).coerceAtLeast(0) else uris.size
            if (uris.size > allowed) onNotice("最多添加 4 张照片")
            if (capture) withContext(Dispatchers.IO) { uris.drop(allowed).forEach { deleteCapture(context, it.toString()) } }
            for (uri in uris.take(allowed)) {
                try {
                    val attachment = withContext(Dispatchers.IO) { selectAttachment(context, uri, image) }
                    state.beginEdit()
                    state.attachments += attachment
                    attachment.error?.let(onNotice)
                } catch (e: CancellationException) { throw e }
                catch (_: Exception) {
                    if (capture) withContext(Dispatchers.IO) { deleteCapture(context, uri.toString()) }
                    onNotice("附件无法读取，请重新选择；输入已保留")
                }
            }
        }
    }
    val photos = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(4)) { select(it, true) }
    val files = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { select(it, false) }
    val camera = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { success ->
        cameraUri?.let { value ->
            if (success) select(listOf(Uri.parse(value)), true, capture = true)
            else runCatching { context.contentResolver.delete(Uri.parse(value), null, null) }
        }
        cameraUri = null
    }
    fun openCamera() {
        val folder = File(context.cacheDir, "chat-captures").apply { mkdirs() }
        val file = File.createTempFile("camera-", ".jpg", folder)
        val uri = FileProvider.getUriForFile(context, "${context.packageName}.chat.files", file)
        cameraUri = uri.toString()
        try { camera.launch(uri) }
        catch (error: Exception) {
            file.delete(); cameraUri = null
            onNotice(if (error is ActivityNotFoundException) "这台手机上没有能拍照的应用" else "无法打开相机，请稍后再试")
        }
    }
    // The app declares CAMERA (pairing scans codes), so Android starts another app's camera for it only once granted.
    val cameraPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) openCamera() else cameraDenied = true
    }
    fun takePhoto() {
        menu = false
        if (cameraAllowed(context)) openCamera()
        else try { cameraPermission.launch(Manifest.permission.CAMERA) } catch (_: Exception) { cameraDenied = true }
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
                    // Back from the camera or a picker the connection is often still catching up (the app was in the
                    // background): wait for it a little instead of failing at once with 「连接恢复后重试上传」.
                    (service as? MessageTransport)?.readyToSend?.let { ready ->
                        withTimeoutOrNull(UPLOAD_READY_WAIT_MILLIS) { ready.first { it } }
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

    LaunchedEffect(visible) { if (!visible) { menu = false; confirmClear = false; cameraDenied = false } }
    // 语音输入: what the phone's recognizer heard goes at the end of the draft, to edit before sending.
    val dictation = rememberDictation(onHeard = { state.appendDictation(it) }, onError = onNotice)
    if (!visible) return
    LaunchedEffect(state.focusRequest) { if (state.focusRequest > 0) focusRequester.requestFocus() }
    // A read-only session (enabled = false) offers nothing to type with: no +, no mic, no "/" list, no send.
    val dictate = dictation?.takeIf { enabled }
    val suggestions = if (enabled) SlashCommand.suggestions(state.value.text, canClear && !working) else emptyList()
    // Picking a quick message puts its words in the box to send; /clear empties the box and asks first.
    fun pick(command: SlashCommand) {
        state.beginEdit()
        if (command.clear) { state.value = TextFieldValue(); confirmClear = true }
        else state.value = TextFieldValue(command.text, TextRange(command.text.length))
    }

    ComposerFrame(
        leading = {
            JIconButton(PhIcons.Plus, "添加附件和快捷指令", { menu = true }, enabled = !sending && enabled)
            leading?.invoke(this)
        },
        trailing = {
            // Nothing to send: the mic where send will be, and while the agent works a stop to its right.
            when {
                hasContent -> SendButton(onClick = onSend, enabled = enabled && state.ready && !sending, loading = sending)
                working && canInterrupt -> {
                    dictate?.let { JIconButton(PhIcons.Microphone, "听写", it, enabled = !sending) }
                    SendButton(onClick = {
                        if (service != null) {
                            interruptPending = true
                            scope.launch {
                                try { service.interrupt(session); onNotice("已请求中断，排队消息仍会保留") }
                                catch (e: CancellationException) { throw e }
                                catch (_: Exception) { onNotice("中断结果待确认，请查看终端后再操作") }
                            }
                        }
                    }, enabled = !sending && !interruptPending && service != null, kind = SendKind.Stop)
                }
                dictate != null -> JIconButton(PhIcons.Microphone, "听写", dictate, enabled = !sending)
                else -> SendButton(onClick = onSend, enabled = false, loading = sending)
            }
        },
        accessory = {
            if (state.oldVersion) Text("这份草稿来自会话旧版本，请核对后再发送", style = PersonaTheme.type.caption,
                color = PersonaTheme.palette.cautionText)
            ComposerAttachmentPreviews(state, service, sending, enabled)
            accessory?.invoke(this)
            modeHint?.let { JChip(it, icon = PhIcons.SlidersHorizontal) }
            if (working && hasContent && enabled) Text("会排队发送", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            // A draft that is one "/" word: the quick messages it can be, right above the field.
            ComposerSuggestions(suggestions.map { ComposerSuggestion(it.word, it.detail, destructive = it.clear) },
                onPick = { picked -> suggestions.firstOrNull { it.word == picked.word }?.let(::pick) },
                modifier = Modifier.padding(vertical = Tokens.Space.xs))
        },
    ) {
        BasicTextField(state.value, { state.beginEdit(); state.value = it },
            modifier = Modifier.fillMaxWidth().heightIn(max = 160.dp).focusRequester(focusRequester)
                .onFocusChanged { state.focused = it.isFocused },
            enabled = enabled, textStyle = PersonaTheme.type.body.copy(color = PersonaTheme.palette.ink),
            cursorBrush = SolidColor(PersonaTheme.palette.ink),
            decorationBox = { field ->
                Box {
                    if (state.value.text.isEmpty()) Text(if (working && enabled) "补充说明（会排队发送）" else placeholder,
                        style = PersonaTheme.type.body, color = PersonaTheme.palette.ink3, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    field()
                }
            })
    }
    if (menu) ChatActionsSheet(onDismiss = { menu = false }, title = "附件和快捷指令", actions = buildList {
        add(JAction("照片", enabled = service != null && state.attachments.count { it.image } < 4) {
            launchPicker { photos.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }
        })
        add(JAction("拍照", enabled = service != null && state.attachments.count { it.image } < 4) { takePhoto() })
        add(JAction("文件", enabled = service != null) { launchPicker { files.launch(arrayOf("*/*")) } })
        // Quick messages go out at once; /clear asks first, and only while nothing is running or being cleared.
        SlashCommand.all.forEach { command -> add(JAction(command.title) { menu = false; onCommand(command.text) }) }
        if (canClear && !working) add(JAction(SlashCommand.clearCommand.title, destructive = true) { menu = false; confirmClear = true })
    })
    if (confirmClear) JDialog(onDismiss = { confirmClear = false }, title = "清空上下文？",
        text = "$agentName 会开一个新会话，这段对话留在历史里；新会话出现后页面会跳过去。手机里没发出的输入会保留。",
        confirm = "清空" to { confirmClear = false; onCommand(SlashCommand.CLEAR) }, destructive = true)
    if (cameraDenied) JDialog(onDismiss = { cameraDenied = false }, title = "需要相机权限",
        text = "拍照要用相机。可以到系统设置 › 应用 › ${appLabel(context)} › 权限里打开相机权限。",
        confirm = "去设置" to { cameraDenied = false; openAppSettings(context) })
}
