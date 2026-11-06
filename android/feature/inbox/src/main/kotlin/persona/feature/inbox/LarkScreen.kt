package persona.feature.inbox

import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.res.pluralStringResource

import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.contracts.api.ConnectionState
import persona.core.navigation.ConnectionSettingsRoute
import androidx.compose.ui.platform.LocalDensity
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.interaction.collectIsDraggedAsState
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.platform.testTag
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.currentStateAsState
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil3.compose.AsyncImage
import coil3.request.ImageRequest
import kotlinx.serialization.json.JsonObject
import persona.core.chatlist.*
import persona.core.navigation.Navigator
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** The host sets this while an in-app sheet or overlay covers the conversation. */
val LocalLarkReadOccluded = staticCompositionLocalOf { false }

/** The account''s thread, read with the chat''s).
    val openImage: (LarkPart.Image) -> Unit = remember { { image -> viewer = larkGallery(larkImages(messages), image) } }
    thread?.let { root -> CompositionLocalProvider(LocalLarkImageOpener provides openImage) {
        LarkChatThreadSheet(root, repository, shownDisplay, onDismiss = { thread = null })
    } }
    profilePerson?.let { person ->
        LarkProfileSheet(person, repository, onDismiss = { profilePerson = null })
    }
    reactionPickerMessage?.let { msg ->
        LarkReactionPicker(
            message = msg,
            onPick = { key ->
                scope.launch {
                    try {
                        repository.react(msg.id, key, remove = false)
                        repository.refreshConversation(route.feedId)
                    } catch (e: Exception) {
                        error = e.message ?: res.getString(R.string.inbox_lark_react_failed)
                    }
                }
            },
            onDismiss = { reactionPickerMessage = null },
            customEmojis = customEmojis
        )
    }
    forwardedDetail?.let { fwd ->
        CompositionLocalProvider(LocalLarkImageOpener provides openImage) { LarkForwardedSheet(part = fwd, onDismiss = { forwardedDetail = null }) }
    }
    videoPlayerUrl?.let { url ->
        LarkVideoPlayerDialog(url = url, onDismiss = { videoPlayerUrl = null })
    }
    viewer?.let { (images, index) ->
        val context = LocalContext.current
        val description = stringResource(R.string.inbox_image)
        val pictures = remember(images, description) { images.mapIndexed { i, image -> larkViewerImage(context, image, i, description) } }
        ImageViewer(pictures, index, onDismiss = { viewer = null })
    }
    // AI 草拟 writes the reply to the item (sent with POST …/reply, confirmed first); the composer below stays the
    // user''s extension page, which broke: one line with 重试 (inbox_item, #1683).
                InboxExtensionNotice(route.itemId, route, Modifier)
                record?.let { InboxAiActions(onDraft = { drafting = true }, onHandOff = { handingOff = true }) }
                record?.item?.summary?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2, maxLines = 2, overflow = TextOverflow.Ellipsis) }
                if (record != null && !record.item.isDone) AgentDraftCard(record, composers, navigator, onEditingChange = { editingDraft = it },
                    repository = repository, onEdit = { drafting = true }, keepComposer = true)
                if (conversation.loading || refreshing || conversation.loadingNewer) JChip(stringResource(R.string.inbox_lark_loading_messages), status = Status.Working)
                (error ?: conversation.failure?.let { stringResource(it.text) } ?: conversation.error)?.let { message ->
                    JNotice(message, status = Status.NeedsYou, actionLabel = stringResource(R.string.inbox_retry), onAction = { refreshConversation() })
                }
            })
    }) { padding -> CompositionLocalProvider(LocalLarkMessageDisplay provides shownDisplay) {
        val density = LocalDensity.current
        val topInset = with(density) { padding.calculateTopPadding().toPx() }
        val bottomInset = with(density) { padding.calculateBottomPadding().toPx() }
        Box(Modifier.fillMaxSize()) { CompositionLocalProvider(LocalLarkImageOpener provides openImage) {
            ChatList(state = chatList, modifier = Modifier.fillMaxSize().onGloballyPositioned {
                val box = it.boundsInWindow()
                // Floating bars occlude messages even when the list extends underneath them.
                viewport = Rect(box.left, (box.top + topInset).coerceAtMost(box.bottom), box.right,
                    (box.bottom - bottomInset).coerceAtLeast(box.top + topInset).coerceAtMost(box.bottom))
            }, contentPadding = PaddingValues(start = pagePadding, end = pagePadding,
                top = padding.calculateTopPadding() + Tokens.Space.m, bottom = padding.calculateBottomPadding() + Tokens.Space.m)) {
                if (messages.isEmpty() && !conversation.loading) item("empty") { JEmptyState(stringResource(R.string.inbox_lark_empty_title), stringResource(R.string.inbox_lark_empty_text)) }
                itemsIndexed(messages.asReversed(), key = { _, message -> message.listKey() }, contentType = { _, message -> if (message.system) "system" else "message" }) { reversedIndex, message ->
                    val index = messages.lastIndex - reversedIndex
                    val previous = messages.getOrNull(index - 1)
                    val boundary = startsInboxItem(message, previous, route.startPosition)
                    val date = larkDate(message.time)
                    val dateBoundary = date != null && date != previous?.let { larkDate(it.time) }
                    DisposableEffect(message.listKey()) { onDispose { bounds.remove(message.listKey()) } }
                    Column {
                        if (dateBoundary) TimelineMark(LocalizedFormats.formatter(LocalizedFormats.DATE, currentLocale).format(date))
                        if (boundary) TimelineMark(stringResource(R.string.inbox_lark_starts_here), emphasized = true)
                        LarkBubble(message, showsSender(message, previous, boundary || dateBoundary), repository, onlineSince,
                            modifier = Modifier.onGloballyPositioned { bounds[message.listKey()] = it.boundsInWindow() },
                            retrying = retrying[message.clientId] == true, onRetry = { retry(message) }, onError = { error = it },
                            onThread = { thread = message },
                            onProfile = { profilePerson = it },
                            onReact = { msg, key, isMine ->
                                scope.launch {
                                    try {
                                        repository.react(msg.id, key, remove = isMine)
                                        repository.refreshConversation(route.feedId)
                                    } catch (e: Exception) {
                                        error = e.message ?: res.getString(R.string.inbox_lark_react_failed)
                                    }
                                }
                            },
                            onOpenReactionPicker = { reactionPickerMessage = it },
                            onOpenForward = { forwardedDetail = it },
                            onPlayVideo = { videoPlayerUrl = it },
                            feedId = route.feedId,
                            readMark = LarkReadMark.of(readStates[message.id], conversation.isGroup).takeIf { message.mine },
                            onAddPin = if (changingPin) null else ({ addingPin = it }))
                    }
                }
                if (conversation.hasOlder) item("older") {
                    JButton(text = stringResource(if (loadingOlder || conversation.loadingOlder) R.string.inbox_lark_loading else R.string.inbox_lark_older), modifier = Modifier.fillMaxWidth(), enabled = !loadingOlder && !conversation.loadingOlder, onClick = {
                        loadingOlder = true
                        scope.launch {
                            try { repository.loadOlder(route.feedId); error = null }
                            catch (e: CancellationException) { throw e }
                            catch (e: Exception) { if (feedback?.failure(e) != true) error = res.getString(R.string.inbox_lark_older_failed) }
                            finally { loadingOlder = false }
                        }
                    })
                }
            }
            val newestVisible by remember(chatList) { derivedStateOf {
                messages.lastOrNull { message ->
                    bounds[message.listKey()]?.let { box -> box.width > 0 && box.height > 0 && viewport.overlaps(box) } == true
                }
            } }
            val unreadMessages = liveMessages.filter { (it.position ?: Long.MIN_VALUE) > (readPosition ?: Long.MIN_VALUE) }
            if (!chatList.followsEnd) JumpToLatest(timeline.newMessageCount(unreadMessages, newestVisible),
                onClick = chatList::returnToLatest,
                modifier = Modifier.align(Alignment.BottomEnd).padding(end = Tokens.Space.l,
                    bottom = padding.calculateBottomPadding() + Tokens.Space.s))
        } }
    } }
}

@Composable
internal fun LarkBubble(
    original: LarkMessage,
    showSender: Boolean,
    repository: InboxRepository,
    onlineSince: Long?,
    modifier: Modifier = Modifier,
    retrying: Boolean,
    onRetry: () -> Unit,
    onError: (String) -> Unit,
    onThread: (() -> Unit)? = null,
    onProfile: ((LarkPersonRef) -> Unit)? = null,
    onReact: ((LarkMessage, String, Boolean) -> Unit)? = null,
    onOpenReactionPicker: ((LarkMessage) -> Unit)? = null,
    onOpenForward: ((LarkPart.MergeForward) -> Unit)? = null,
    onPlayVideo: ((String) -> Unit)? = null,
    feedId: String? = null,
    /** The read receipt beside a message of mine, once there is one. */
    readMark: LarkReadMark? = null,
    /** 添加 Pin, where the chat can pin. */
    onAddPin: ((LarkMessage) -> Unit)? = null,
) {
    val display = LocalLarkMessageDisplay.current
    val message = remember(original, display) { original.shown(display) }
    // 防撤回 shows what the Lark gateway kept of a recalled message, marked 已撤回; otherwise it is one line.
    if (message.system && !(message.recalled && display.showsBody(message))) {
        TimelineMark(if (message.recalled) stringResource(R.string.inbox_lark_recalled)
            else message.parts.filterIsInstance<LarkPart.Text>().joinToString("\n") { it.text }, modifier)
        return
    }
    // A recalled message 防撤回 kept takes no reaction, reply or card write (docs/protocol.md, `retained`), as on iOS.
    val react = onReact.takeUnless { message.recalled }
    val openReactionPicker = onOpenReactionPicker.takeUnless { message.recalled }
    var menuExpanded by remember { mutableStateOf(false) }
    var showingHistory by remember { mutableStateOf(false) }
    val clipboard = LocalClipboardManager.current
    if (showingHistory) LarkEditHistorySheet(message, onDismiss = { showingHistory = false })

    val body: @Composable ColumnScope.() -> Unit = {
        Box {
            Column(
                verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs),
                modifier = Modifier.combinedClickable(
                    onClick = {},
                    onLongClick = { menuExpanded = true }
                )
            ) {
                message.quote?.let { quote ->
                    JNotice(if (quote.recalled) stringResource(R.string.inbox_lark_recalled)
                        else display.summary(quote.text, stringResource(R.string.inbox_lark_brave_climb_placeholder)), title = quote.sender)
                }
                message.parts.forEach { part ->
                    LarkPartContent(
                        part = if (message.recalled && part is LarkPart.Card) part.copy(messageId = null) else part,
                        repository = repository,
                        onError = onError,
                        onOpenForward = onOpenForward,
                        onPlayVideo = onPlayVideo,
                        onOpenProfile = { uid -> onProfile?.invoke(LarkPersonRef(uid, uid, null, feedId)) }
                    )
                }
                if (message.braveClimb) Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs), verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.testTag("lark.braveClimb.decoded")) {
                    JIcon(PhIcons.ShieldCheck, null, size = Tokens.Space.m, tint = PersonaTheme.palette.ink3)
                    Text(stringResource(R.string.inbox_lark_brave_climb_decoded), style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3, maxLines = 1)
                }
                if (message.recalled) Text(stringResource(R.string.inbox_lark_recalled_mark), style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
                // 编辑历史 on and old versions kept: 已编辑 opens them.
                if (message.edited) {
                    if (display.showsHistory(message)) Text(stringResource(R.string.inbox_lark_edited), style = PersonaTheme.type.micro,
                        color = PersonaTheme.palette.ink2, modifier = Modifier.clickable(role = androidx.compose.ui.semantics.Role.Button,
                            onClickLabel = stringResource(R.string.inbox_lark_edit_history)) { showingHistory = true })
                    else Text(stringResource(R.string.inbox_lark_edited), style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
                }
            }

            LarkMessageActionMenu(
                message = message,
                expanded = menuExpanded,
                onDismiss = { menuExpanded = false },
                onReact = react?.let { r -> { key -> r(message, key, false) } },
                onOpenPicker = openReactionPicker?.let { open -> { open(message) } },
                onOpenProfile = if (message.senderId.isNotBlank() && onProfile != null) ({
                    onProfile(LarkPersonRef(message.senderId, message.senderName, message.avatar, feedId))
                }) else null,
                onAddPin = if (onAddPin != null && message.canPin && !message.recalled && message.delivery == LarkDelivery.Sent && message.pendingDelivery == null)
                    ({ onAddPin(message) }) else null,
                onCopy = {
                    val textToCopy = message.parts.filterIsInstance<LarkPart.Text>().joinToString("\n") { it.text }
                        .ifBlank { message.parts.filterIsInstance<LarkPart.Code>().joinToString("\n") { it.code } }
                    clipboard.setText(AnnotatedString(textToCopy))
                }
            )
        }
    }
    val footer: @Composable () -> Unit = {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            // 设置 › 翻译: under the bubble, as on iOS.
            LarkTranslationLine(message, feedId)
            if (message.reactions.isNotEmpty() && react != null && openReactionPicker != null) {
                LarkReactionRow(
                    reactions = message.reactions,
                    onToggleReaction = { key, isMine -> react(message, key, isMine) },
                    onOpenPicker = { openReactionPicker(message) }
                )
            }
            if (message.mine) Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
                readMark?.let { LarkReadMarkView(it) }
                Text(larkTime(message.time, currentLocale), style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
            }
            if (message.bot) JTag(stringResource(R.string.inbox_bot))
            if (message.replyCount > 0 && onThread != null && !message.recalled) {
                JTextButton(pluralStringResource(R.plurals.inbox_lark_replies, message.replyCount.toQuantity(), message.replyCount), onClick = onThread)
            }
            val pending = message.pendingDelivery?.pendingStatus()
            if (pending != null) {
                PendingBadge(if (retrying) PendingStatus.Verifying else pending)
                if (pending in setOf(PendingStatus.Unconfirmed, PendingStatus.NeedsReview))
                    JButton(stringResource(R.string.inbox_check_result), onClick = onRetry, enabled = message.clientId != null && !retrying,
                        kind = JButtonKind.Secondary, size = JButtonSize.Small)
            } else when (message.delivery) {
                LarkDelivery.Sending -> PendingBadge(PendingStatus.Sending)
                LarkDelivery.WaitingForNetwork -> PendingBadge(PendingStatus.Waiting)
                LarkDelivery.Failed -> {
                    PendingBadge(if (retrying) PendingStatus.Verifying else PendingStatus.Unconfirmed)
                    JButton(stringResource(R.string.inbox_check_result), onClick = onRetry, enabled = message.clientId != null && !retrying,
                        kind = JButtonKind.Secondary, size = JButtonSize.Small)
                }
                LarkDelivery.Sent -> Unit
            }
        }
    }
    if (message.mine) UserBubbleFrame(modifier, footer = footer, own = false, content = body)
    else IncomingBubbleFrame(modifier, author = message.senderName.takeIf { showSender },
        time = larkTime(message.time, currentLocale).takeIf { showSender }, avatar = {
            Box(Modifier.clickable {
                if (message.senderId.isNotBlank() && onProfile != null) {
                    onProfile(LarkPersonRef(message.senderId, message.senderName, message.avatar, feedId))
                }
            }) {
                LarkAvatar(message.senderName.ifBlank { stringResource(if (message.bot) R.string.inbox_bot else R.string.inbox_lark_avatar_fallback) }, message.avatar, onlineSince)
            }
        }, footer = footer, content = body)
}

@Composable
internal fun LarkPartContent(
    part: LarkPart,
    repository: InboxRepository,
    onError: (String) -> Unit = {},
    onOpenForward: ((LarkPart.MergeForward) -> Unit)? = null,
    onPlayVideo: ((String) -> Unit)? = null,
    onOpenProfile: ((String) -> Unit)? = null,
) {
    val context = LocalContext.current
    val res = LocalResources.current
    val scope = rememberCoroutineScope()
    var opening by remember { mutableStateOf(false) }
    when (part) {
        is LarkPart.Text -> {
            // A company-feed share is a card while the hub reads the feed (`lark_moments`); else the plain link.
            val postId = (larkNativeLink(part.text, LocalLarkFeatures.current) as? LarkNativeLink.MomentsPost)?.postId
            if (postId != null) {
                val open = LocalLarkLinkOpener.current
                LarkMomentsCard(postId = postId, url = part.text, repository = repository, onClick = {
                    open?.invoke(part.text, part.source)
                })
            } else {
                LarkRichInlineText(part.text, source = part.source, onOpenProfile = onOpenProfile)
            }
        }
        is LarkPart.Code -> LarkCodeBlock(part.code, part.language)
        is LarkPart.Audio -> LarkVoiceView(part)
        is LarkPart.Video -> LarkVideoView(part, onPlay = { onPlayVideo?.invoke(part.ref) })
        is LarkPart.Sticker -> LarkStickerView(part)
        is LarkPart.MergeForward -> LarkMergeForwardCard(part, onClick = { onOpenForward?.invoke(part) })
        is LarkPart.Card -> LarkInteractiveCard(part, repository, onError)
        is LarkPart.Image -> {
            var failed by remember(part.model) { mutableStateOf(false) }
            val authenticated = part.model as? AuthenticatedLarkImage
            val bytes by produceState<ByteArray?>(null, authenticated?.cacheKey) {
                if (authenticated != null) {
                    try { value = authenticated.read() }
                    catch (e: CancellationException) { throw e }
                    catch (_: Exception) { failed = true }
                }
            }
            val model = if (authenticated == null) part.model else bytes?.let {
                ImageRequest.Builder(context).data(it).memoryCacheKey(authenticated.cacheKey).build()
            }
            val open = LocalLarkImageOpener.current
            AsyncImage(model = model, contentDescription = part.description.ifBlank { stringResource(R.string.inbox_image) },
                modifier = Modifier.fillMaxWidth().aspectRatio((part.width.toFloat() / part.height.coerceAtLeast(1)).coerceIn(.2f, 5f))
                    .then(if (open != null) Modifier.clickable(role = Role.Image, onClickLabel = stringResource(R.string.inbox_lark_view_image)) { open(part) } else Modifier),
                onError = { failed = true }, onSuccess = { failed = false })
            if (failed) Text(stringResource(R.string.inbox_lark_image_failed), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink3)
        }
        is LarkPart.File -> JButton(text = if (opening) stringResource(R.string.inbox_lark_opening)
            else stringResource(R.string.inbox_lark_file, part.name.ifBlank { stringResource(R.string.inbox_lark_file_untitled) }), enabled = !opening, onClick = {
            opening = true
            scope.launch {
                try {
                    val uri = Uri.parse(repository.downloadFile(part))
                    check(uri.scheme == "content")
                    context.startActivity(Intent(Intent.ACTION_VIEW).setDataAndType(uri, part.mimeType).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION))
                } catch (e: CancellationException) { throw e }
                catch (_: Exception) { onError(res.getString(R.string.inbox_lark_file_failed)) }
                finally { opening = false }
            }
        })
        is LarkPart.External -> Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            part.cover?.let { LarkPartContent(it, repository, onError, onOpenForward, onPlayVideo, onOpenProfile) }
            Text(listOf(stringResource(when (part.kind) { "video" -> R.string.inbox_lark_video; "audio" -> R.string.inbox_lark_audio; else -> R.string.inbox_lark_card }),
                part.title, part.duration).filter { it.isNotBlank() }.joinToString(stringResource(R.string.inbox_lark_part_separator)), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
            JButton(text = stringResource(R.string.inbox_open_in_lark), onClick = {
                try {
                    val uri = Uri.parse(part.larkUrl)
                    check(uri.scheme in setOf("https", "lark", "feishu"))
                    context.startActivity(Intent(Intent.ACTION_VIEW, uri))
                } catch (_: Exception) { onError(res.getString(R.string.inbox_lark_cannot_open)) }
            }, enabled = part.larkUrl.isNotBlank())
        }
    }
}

/** A count for a plural lookup: the quantity rules take an Int. */
internal fun Long.toQuantity(): Int = coerceIn(0L, Int.MAX_VALUE.toLong()).toInt()
