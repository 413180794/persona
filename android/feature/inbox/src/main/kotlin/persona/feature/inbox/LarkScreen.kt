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
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.currentStateAsState
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
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

@Composable
fun LarkScreen(repository: InboxRepository, route: LarkRoute, navigator: Navigator) {
    val inbox by repository.snapshot.collectAsStateWithLifecycle()
    val source = inbox.contentSource
    key(source, route) {
        val opening by produceState<Pair<LarkReadingStore?, LarkListNavigation>?>(null, source) {
            val store = source?.let { repository.readingStore() }
            val navigation = try { source?.let { larkListNavigation(it.transport, route.feedId) } ?: LarkListNavigation() }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { LarkListNavigation() }
            value = store to navigation
        }
        LarkLinkHost(repository) {
            val loaded = opening
            if (loaded == null) Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { JLoading() }
            else key(source?.scope, route) {
                val readingKey = source?.let { ChatReadingKey(it.scope, "lark:${route.feedId}") }
                val store = loaded.first
                var target by remember {
                    mutableStateOf(larkListTarget(loaded.second, route.startPosition,
                        readingKey?.let { store?.read(it) }, readingKey?.let { store?.around(it) }))
                }
                key(target) {
                    LarkConversationScreen(repository, route, navigator, target, loaded.second, readingKey, store) { position ->
                        target = LarkListTarget(position, position, request = target.request + 1)
                    }
                }
            }
        }
    }
}

@Composable
private fun LarkConversationScreen(repository: InboxRepository, route: LarkRoute, navigator: Navigator,
    target: LarkListTarget, navigation: LarkListNavigation, readingKey: ChatReadingKey?, store: LarkReadingStore?,
    jumpTo: (Long) -> Unit,
) {
    val flow = remember(repository, route.feedId, target.around) { repository.conversation(route.feedId, target.around) }
    val conversation by flow.collectAsStateWithLifecycle(LarkConversation(loading = true))
    val inbox by repository.snapshot.collectAsStateWithLifecycle()
    val record = inbox.records.firstOrNull { it.item.id == route.itemId }
    val feedback = persona.core.designsystem.LocalAppFeedback.current
    val scope = rememberCoroutineScope()
    val composers = rememberReplyComposers(repository)
    val composer = composers.get(ReplyTarget.Lark(route.feedId))
    val reply by composer.state.collectAsStateWithLifecycle()
    val chatList = key(conversation.windowRevision) { rememberChatListState(readingKey, store) }
    val scroll = chatList.list
    val positioned = chatList.restored
    val timeline = remember(target, conversation.windowRevision) { larkListWindow() }
    var timelineVersion by remember(target, conversation.windowRevision) { mutableIntStateOf(0) }
    val liveMessages by rememberUpdatedState(conversation.messages)
    val displayed = remember(conversation.messages, timelineVersion, timeline) { timeline.items(conversation.messages) }
    SideEffect { store?.observe(displayed) }
    var thread by remember(route.feedId) { mutableStateOf<LarkMessage?>(null) }
    var more by remember { mutableStateOf(false) }
    var profilePerson by remember { mutableStateOf<LarkPersonRef?>(null) }
    var reactionPickerMessage by remember { mutableStateOf<LarkMessage?>(null) }
    var forwardedDetail by remember { mutableStateOf<LarkPart.MergeForward?>(null) }
    var videoPlayerUrl by remember { mutableStateOf<String?>(null) }
    val customEmojis by produceState<JsonObject?>(null, repository) {
        value = runCatching { repository.emojis() }.getOrNull()
    }
    var editingDraft by remember { mutableStateOf(false) }
    var drafting by remember { mutableStateOf(false) }
    var handingOff by remember { mutableStateOf(false) }
    var readPosition by remember { mutableStateOf(navigation.readPosition) }
    var error by remember { mutableStateOf<String?>(null) }
    val res = LocalResources.current
    var markingDone by remember { mutableStateOf(false) }
    var refreshing by remember { mutableStateOf(false) }
    var loadingOlder by remember { mutableStateOf(false) }
    val retrying = remember { mutableStateMapOf<String, Boolean>() }
    val messages by rememberUpdatedState(displayed)
    val latestConversation by rememberUpdatedState(conversation)
    val lifecycle by LocalLifecycleOwner.current.lifecycle.currentStateAsState()
    val focused = LocalWindowInfo.current.isWindowFocused
    val occluded = LocalLarkReadOccluded.current || thread != null || more || editingDraft || drafting || handingOff ||
        profilePerson != null || reactionPickerMessage != null || forwardedDetail != null || videoPlayerUrl != null ||
        chatList.programmaticScroll
    val dragged by scroll.interactionSource.collectIsDraggedAsState()
    var interacted by remember(route) { mutableStateOf(false) }
    var viewport by remember { mutableStateOf(Rect.Zero) }
    val bounds = remember(route) { mutableStateMapOf<String, Rect>() }
    LaunchedEffect(dragged) { if (dragged) interacted = true }
    val entry = larkListEntry(displayed, target.entry)
    ChatListEffects(chatList, displayed.asReversed().map { it.listKey() },
        ready = !conversation.loading, entry = entry, canFollowEnd = !conversation.hasNewer,
        hasOlder = conversation.hasOlder, loadingOlder = conversation.loadingOlder || loadingOlder,
        onFreeze = { if (liveMessages.isNotEmpty()) timeline.freeze(liveMessages); timelineVersion++ },
        onFollow = {
            timeline.follow(); timelineVersion++
            if (latestConversation.hasNewer) scope.launch {
                try { repository.latestConversation(route.feedId) }
                catch (e: CancellationException) { throw e }
                catch (_: Exception) { chatList.holdPosition(); error = res.getString(R.string.inbox_lark_latest_failed) }
            }
        },
        loadOlder = {
            try { repository.loadOlder(route.feedId) }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { error = res.getString(R.string.inbox_lark_older_failed) }
        })

    LaunchedEffect(repository, route, chatList, positioned, lifecycle, focused, occluded, conversation.verified, messages) {
        snapshotFlow {
            if (!larkReadAllowed(lifecycle == Lifecycle.State.RESUMED, focused, positioned, occluded,
                    scroll.isScrollInProgress, conversation.verified)) emptyList()
            else larkSeen(messages, bounds, viewport)
        }.distinctUntilChanged().collectLatest { visible ->
            if (visible.isNotEmpty()) {
                delay(450)
                try {
                    repository.markVisible(route.feedId, visible)
                    visible.mapNotNull { it.position }.maxOrNull()?.let { read ->
                        readPosition = maxOf(readPosition ?: Long.MIN_VALUE, read)
                    }
                }
                catch (e: CancellationException) { throw e }
                catch (_: Exception) { /* Retry on the next viewport or verified-window change. */ }
            }
        }
    }
    LaunchedEffect(repository, route, chatList, positioned, interacted, conversation.hasNewer, conversation.loadingNewer, messages, lifecycle) {
        if (lifecycle != Lifecycle.State.RESUMED) return@LaunchedEffect
        snapshotFlow {
            val keys = scroll.layoutInfo.visibleItemsInfo.map { it.key }.toSet()
            positioned && interacted && !chatList.programmaticScroll &&
                messages.lastOrNull()?.let { it.listKey() in keys } == true
        }.distinctUntilChanged().collect { atNewerEdge ->
            if (atNewerEdge && latestConversation.hasNewer && !latestConversation.loadingNewer) try {
                repository.loadNewer(route.feedId)
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { error = res.getString(R.string.inbox_lark_messages_failed) }
        }
    }
    LaunchedEffect(chatList, conversation.windowRevision) {
        if (conversation.windowRevision > 0) chatList.returnToLatest()
    }
    LaunchedEffect(reply.sent) {
        if (reply.sent) {
            try { repository.latestConversation(route.feedId); chatList.returnToLatest() }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { error = res.getString(R.string.inbox_lark_sent_refresh_failed) }
        }
    }
    fun retry(message: LarkMessage) {
        val clientId = message.clientId ?: return
        if (retrying[clientId] == true) return
        retrying[clientId] = true
        scope.launch {
            try {
                val result = repository.verifyReply(ReplyTarget.Lark(route.feedId), clientId)
                error = if (result == ReplyDelivery.Sent) null else res.getString(R.string.inbox_lark_unconfirmed)
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { error = res.getString(R.string.inbox_lark_check_unfinished) }
            finally { retrying.remove(clientId) }
        }
    }
    thread?.let { root -> LarkThreadSheet(root, repository, onDismiss = { thread = null }) }
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
        LarkForwardedSheet(part = fwd, onDismiss = { forwardedDetail = null })
    }
    videoPlayerUrl?.let { url ->
        LarkVideoPlayerDialog(url = url, onDismiss = { videoPlayerUrl = null })
    }
    // AI 草拟 writes the reply to the item (sent with POST …/reply, confirmed first); the composer below stays the
    // user's own words for the chat, sent as typed.
    if (drafting && record != null) InboxDraftSheet(repository, record,
        composers.get(ReplyTarget.Inbox(record.item.id), record.agent?.takeIf { it.state == "drafted" }?.draft.orEmpty()),
        navigator, onDismiss = { drafting = false })
    if (handingOff && record != null) InboxHandOffSheet(repository, record, navigator, onDismiss = { handingOff = false })
    val connection by repository.connectionState.collectAsStateWithLifecycle(ConnectionState.Connecting)
    val onlineSince = rememberOnlineSince(connection)
    fun refreshConversation() {
        if (refreshing) return
        refreshing = true
        scope.launch {
            try { repository.refreshConversation(route.feedId); error = null }
            catch (e: CancellationException) { throw e }
            catch (e: Exception) { if (feedback?.failure(e) != true) error = res.getString(R.string.inbox_lark_refresh_failed) }
            finally { refreshing = false }
        }
    }
    if (more) JActionSheet(title = stringResource(R.string.inbox_lark_actions), onDismiss = { more = false }, actions = buildList {
        navigation.mention?.let { mention -> add(JAction(res.getString(R.string.inbox_lark_jump_mention), PhIcons.At) { jumpTo(mention) }) }
        add(JAction(res.getString(R.string.inbox_lark_refresh), PhIcons.ArrowClockwise, enabled = !refreshing) { refreshConversation() })
    })
    val title = conversation.title.ifBlank { stringResource(R.string.inbox_lark_conversation) }
    JPage(title = title, topBar = {
        ChatTopBar(title = title, onBack = navigator::back,
            statusText = if (connection == ConnectionState.Online) stringResource(R.string.inbox_source_lark) else inboxTitle(connection),
            busy = connection in setOf(ConnectionState.Connecting, ConnectionState.Updating),
            onTitleClick = if (connection == ConnectionState.Online) null else ({ navigator.navigate(ConnectionSettingsRoute) }),
            actions = {
                if (record != null) JIconButton(PhIcons.CheckCircle, stringResource(if (record.item.isDone) R.string.inbox_move_back else R.string.inbox_done),
                    enabled = !markingDone, onClick = {
                        markingDone = true
                        scope.launch {
                            try { repository.setDone(record.item.id, !record.item.isDone); error = null; navigator.back() }
                            catch (e: CancellationException) { throw e }
                            catch (e: Exception) { if (feedback?.failure(e, operation = true) != true) error = res.getString(R.string.inbox_failed_retry) }
                            finally { markingDone = false }
                        }
                    })
                JIconButton(PhIcons.DotsThree, stringResource(R.string.inbox_more), { more = true })
            })
    }, bottomBar = {
        ReplyComposerField(reply, composer::update, composer::send, stringResource(R.string.inbox_lark_reply_to, title),
            modifier = Modifier.imePadding(), accessory = {
                // Opened in place of the item's extension page, which broke: one line with 重试 (inbox_item, #1683).
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
    }) { padding ->
        val density = LocalDensity.current
        val topInset = with(density) { padding.calculateTopPadding().toPx() }
        val bottomInset = with(density) { padding.calculateBottomPadding().toPx() }
        Box(Modifier.fillMaxSize()) {
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
                            feedId = route.feedId)
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
        }
    }
}

@Composable
internal fun LarkBubble(
    message: LarkMessage,
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
) {
    if (message.system) {
        TimelineMark(if (message.recalled) stringResource(R.string.inbox_lark_recalled)
            else message.parts.filterIsInstance<LarkPart.Text>().joinToString("\n") { it.text }, modifier)
        return
    }
    var menuExpanded by remember { mutableStateOf(false) }
    val clipboard = LocalClipboardManager.current

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
                    JNotice(if (quote.recalled) stringResource(R.string.inbox_lark_recalled) else quote.text, title = quote.sender)
                }
                message.parts.forEach { part ->
                    LarkPartContent(
                        part = part,
                        repository = repository,
                        onError = onError,
                        onOpenForward = onOpenForward,
                        onPlayVideo = onPlayVideo,
                        onOpenProfile = { uid -> onProfile?.invoke(LarkPersonRef(uid, uid, null, feedId)) }
                    )
                }
                if (message.edited) Text(stringResource(R.string.inbox_lark_edited), style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
            }

            LarkMessageActionMenu(
                message = message,
                expanded = menuExpanded,
                onDismiss = { menuExpanded = false },
                onReact = { key -> onReact?.invoke(message, key, false) },
                onOpenPicker = { onOpenReactionPicker?.invoke(message) },
                onOpenProfile = if (message.senderId.isNotBlank() && onProfile != null) ({
                    onProfile(LarkPersonRef(message.senderId, message.senderName, message.avatar, feedId))
                }) else null,
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
            if (message.reactions.isNotEmpty() && onReact != null && onOpenReactionPicker != null) {
                LarkReactionRow(
                    reactions = message.reactions,
                    onToggleReaction = { key, isMine -> onReact(message, key, isMine) },
                    onOpenPicker = { onOpenReactionPicker(message) }
                )
            }
            if (message.mine) Text(larkTime(message.time), style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
            if (message.bot) JTag(stringResource(R.string.inbox_bot))
            if (message.replyCount > 0 && onThread != null) {
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
    if (message.mine) UserBubbleFrame(modifier, footer = footer, content = body)
    else IncomingBubbleFrame(modifier, author = message.senderName.takeIf { showSender },
        time = larkTime(message.time).takeIf { showSender }, avatar = {
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
            AsyncImage(model = model, contentDescription = part.description.ifBlank { stringResource(R.string.inbox_image) },
                modifier = Modifier.fillMaxWidth().aspectRatio((part.width.toFloat() / part.height.coerceAtLeast(1)).coerceIn(.2f, 5f)),
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
