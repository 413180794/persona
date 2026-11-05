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
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
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
    placeholder: String = stringResource(R.string.chat_composer_placeholder),
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
    val resources = LocalResources.current
    val photosOnly = stringResource(R.string.chat_composer_photos_only)
    SideEffect { state.photosOnlyText = photosOnly }
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
            if (uris.size > allowed) onNotice(resources.getString(R.string.chat_composer_photo_limit))
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
                    onNotice(resources.getString(R.string.chat_composer_attachment_unreadable))
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
            onNotice(resources.getString(if (error is ActivityNotFoundException) R.string.chat_composer_no_camera_app else R.string.chat_composer_camera_failed))
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
        try { action() } catch (_: Exception) { onNotice(resources.getString(R.string.chat_composer_picker_failed)) }
    }

    state.attachments.forEach { attachment ->
        key(attachment.id) {
            LaunchedEffect(attachment.id, attachment.error, service) {
                if (attachment.uploadState == DraftUploadState.Invalid) return@LaunchedEffect
                if (!withContext(Dispatchers.IO) { attachmentReadable(context, attachment) }) {
                    val reason = resources.getString(R.string.chat_composer_attachment_gone)
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
                    val reason = (e as? ComposerOperationException)?.message ?: resources.getString(R.string.chat_composer_upload_failed)
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
    val extensionPrompts = if (enabled) LocalExtensionPrompts.current else emptyList()
    val quick = ExtensionPromptText.matching(extensionPrompts, state.value.text)
    // Picking a quick message puts its words in the box to send; /clear empties the box and asks first.
    fun pick(command: SlashCommand) {
        state.beginEdit()
        if (command.clear) { state.value = TextFieldValue(); confirmClear = true }
        else state.value = TextFieldValue(command.text, TextRange(command.text.length))
    }

    ComposerFrame(
        leading = {
            JIconButton(PhIcons.Plus, stringResource(R.string.chat_composer_add), { menu = true }, enabled = !sending && enabled)
            leading?.invoke(this)
        },
        trailing = {
            // Nothing to send: the mic where send will be, and while the agent works a stop to its right.
            when {
                hasContent -> SendButton(onClick = onSend, enabled = enabled && state.ready && !sending, loading = sending)
                working && canInterrupt -> {
                    dictate?.let { JIconButton(PhIcons.Microphone, stringResource(R.string.chat_composer_dictate), it, enabled = !sending) }
                    SendButton(onClick = {
                        if (service != null) {
                            interruptPending = true
                            scope.launch {
                                try { service.interrupt(session); onNotice(resources.getString(R.string.chat_composer_interrupt_requested)) }
                                catch (e: CancellationException) { throw e }
                                catch (_: Exception) { onNotice(resources.getString(R.string.chat_composer_interrupt_unknown)) }
                            }
                        }
                    }, enabled = !sending && !interruptPending && service != null, kind = SendKind.Stop)
                }
                dictate != null -> JIconButton(PhIcons.Microphone, stringResource(R.string.chat_composer_dictate), dictate, enabled = !sending)
                else -> SendButton(onClick = onSend, enabled = false, loading = sending)
            }
        },
        accessory = {
            if (state.oldVersion) Text(stringResource(R.string.chat_composer_old_version), style = PersonaTheme.type.caption,
                color = PersonaTheme.palette.cautionText)
            ComposerAttachmentPreviews(state, service, sending, enabled)
            accessory?.invoke(this)
            modeHint?.let { JChip(it, icon = PhIcons.SlidersHorizontal) }
            if (working && hasContent && enabled) Text(stringResource(R.string.chat_composer_will_queue), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            // A draft that is one "/" word: the quick messages it can be, right above the field.
            ComposerSuggestions(quick.map { ComposerSuggestion(it.title, it.extensionName, key = "${it.extension}/${it.id}", flexible = true) },
                onPick = { picked -> quick.firstOrNull { "${it.extension}/${it.id}" == picked.key }?.let { state.fillQuickPrompt(it) } },
                modifier = Modifier.padding(vertical = Tokens.Space.xs))
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
                    if (state.value.text.isEmpty()) Text(if (working && enabled) stringResource(R.string.chat_composer_placeholder_working) else placeholder,
                        style = PersonaTheme.type.body, color = PersonaTheme.palette.ink3, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    field()
                }
            })
    }
    if (menu) ChatActionsSheet(onDismiss = { menu = false }, title = stringResource(R.string.chat_composer_menu_title), actions = buildList {
        add(JAction(stringResource(R.string.chat_composer_photos), enabled = service != null && state.attachments.count { it.image } < 4) {
            launchPicker { photos.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }
        })
        add(JAction(stringResource(R.string.chat_composer_take_photo), enabled = service != null && state.attachments.count { it.image } < 4) { takePhoto() })
        add(JAction(stringResource(R.string.chat_composer_files), enabled = service != null) { launchPicker { files.launch(arrayOf("*/*")) } })
        // Quick messages go out at once; /clear asks first, and only while nothing is running or being cleared.
        extensionPrompts.forEach { prompt -> add(JAction(prompt.title) { menu = false; state.fillQuickPrompt(prompt) }) }
        SlashCommand.all(resources).forEach { command -> add(JAction(command.title) { menu = false; onCommand(command.text) }) }
        if (canClear && !working) add(JAction(SlashCommand.clearCommand(resources).title, destructive = true) { menu = false; confirmClear = true })
    })
    if (confirmClear) JDialog(onDismiss = { confirmClear = false }, title = stringResource(R.string.chat_composer_clear_title),
        text = stringResource(R.string.chat_composer_clear_text, agentName),
        confirm = stringResource(R.string.chat_composer_clear) to { confirmClear = false; onCommand(SlashCommand.CLEAR) }, destructive = true)
    if (cameraDenied) JDialog(onDismiss = { cameraDenied = false }, title = stringResource(R.string.chat_composer_camera_title),
        text = stringResource(R.string.chat_composer_camera_text, appLabel(context)),
        confirm = stringResource(R.string.chat_composer_open_settings) to { cameraDenied = false; openAppSettings(context) })
}
