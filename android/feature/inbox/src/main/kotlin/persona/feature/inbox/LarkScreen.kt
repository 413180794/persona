package persona.feature.inbox

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
                catch (_: Exception) { chatList.holdPosition(); error = "最新消息加载失败，请重试" }
            }
        },
        loadOlder = {
            try { repository.loadOlder(route.feedId) }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { error = "更早的消息加载失败，请重试" }
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
            catch (_: Exception) { error = "消息加载失败，请重试" }
        }
    }
    LaunchedEffect(chatList, conversation.windowRevision) {
        if (conversation.windowRevision > 0) chatList.returnToLatest()
    }
    LaunchedEffect(reply.sent) {
        if (reply.sent) {
            try { repository.latestConversation(route.feedId); chatList.returnToLatest() }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { error = "消息已发送，刷新失败，请重试" }
        }
    }
    fun retry(message: LarkMessage) {
        val clientId = message.clientId ?: return
        if (retrying[clientId] == true) return
        retrying[clientId] = true
        scope.launch {
            try {
                val result = repository.verifyReply(ReplyTarget.Lark(route.feedId), clientId)
                error = if (result == ReplyDelivery.Sent) null else "暂时无法确认是否送达，不会自动重发"
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { error = "核对未完成，请稍后再试" }
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
                        error = e.message ?: "回应失败"
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
            catch (e: Exception) { if (feedback?.failure(e) != true) error = "会话刷新失败，请重试" }
            finally { refreshing = false }
        }
    }
    if (more) JActionSheet(title = "会话操作", onDismiss = { more = false }, actions = buildList {
        navigation.mention?.let { mention -> add(JAction("跳到 @我 的消息", PhIcons.At) { jumpTo(mention) }) }
        add(JAction("刷新会话", PhIcons.ArrowClockwise, enabled = !refreshing) { refreshConversation() })
    })
    JPage(title = conversation.title, topBar = {
        ChatTopBar(title = conversation.title, onBack = navigator::back,
            statusText = if (connection == ConnectionState.Online) "飞书" else inboxTitle(connection),
            busy = connection in setOf(ConnectionState.Connecting, ConnectionState.Updating),
            onTitleClick = if (connection == ConnectionState.Online) null else ({ navigator.navigate(ConnectionSettingsRoute) }),
            actions = {
                if (record != null) JIconButton(PhIcons.CheckCircle, if (record.item.isDone) "移回收件箱" else "已处理",
                    enabled = !markingDone, onClick = {
                        markingDone = true
                        scope.launch {
                            try { repository.setDone(record.item.id, !record.item.isDone); error = null; navigator.back() }
                            catch (e: CancellationException) { throw e }
                            catch (e: Exception) { if (feedback?.failure(e, operation = true) != true) error = "操作失败，请重试" }
                            finally { markingDone = false }
                        }
                    })
                JIconButton(PhIcons.DotsThree, "更多", { more = true })
            })
    }, bottomBar = {
        ReplyComposerField(reply, composer::update, composer::send, "回复 ${conversation.title}",
            modifier = Modifier.imePadding(), accessory = {
                // Opened in place of the item's plugin page, which broke: one line with 重试 (inbox_item, #1683).
                InboxPluginNotice(route.itemId, route, Modifier)
                record?.let { InboxAiActions(onDraft = { drafting = true }, onHandOff = { handingOff = true }) }
                record?.item?.summary?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2, maxLines = 2, overflow = TextOverflow.Ellipsis) }
                if (record != null && !record.item.isDone) AgentDraftCard(record, composers, navigator, onEditingChange = { editingDraft = it },
                    repository = repository, onEdit = { drafting = true }, keepComposer = true)
                if (conversation.loading || refreshing || conversation.loadingNewer) JChip("正在加载消息", status = Status.Working)
                (error ?: conversation.error)?.let { message ->
                    JNotice(message, status = Status.NeedsYou, actionLabel = "重试", onAction = { refreshConversation() })
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
                if (messages.isEmpty() && !conversation.loading) item("empty") { JEmptyState("暂无消息", "新的消息会显示在这里。") }
                itemsIndexed(messages.asReversed(), key = { _, message -> message.listKey() }, contentType = { _, message -> if (message.system) "system" else "message" }) { reversedIndex, message ->
                    val index = messages.lastIndex - reversedIndex
                    val previous = messages.getOrNull(index - 1)
                    val boundary = startsInboxItem(message, previous, route.startPosition)
                    val date = larkDate(message.time)
                    val dateBoundary = date != null && date != previous?.let { larkDate(it.time) }
                    DisposableEffect(message.listKey()) { onDispose { bounds.remove(message.listKey()) } }
                    Column {
                        if (dateBoundary) TimelineMark(date.toString())
                        if (boundary) TimelineMark("这件事从这里开始", emphasized = true)
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
                                        error = e.message ?: "回应失败"
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
                    JButton(text = if (loadingOlder || conversation.loadingOlder) "正在加载…" else "查看更早的消息", modifier = Modifier.fillMaxWidth(), enabled = !loadingOlder && !conversation.loadingOlder, onClick = {
                        loadingOlder = true
                        scope.launch {
                            try { repository.loadOlder(route.feedId); error = null }
                            catch (e: CancellationException) { throw e }
                            catch (e: Exception) { if (feedback?.failure(e) != true) error = "更早的消息加载失败，请重试" }
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
        TimelineMark(message.parts.filterIsInstance<LarkPart.Text>().joinToString("\n") { it.text }, modifier)
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
                    JNotice(quote.text, title = quote.sender)
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
                if (message.edited) Text("已编辑", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
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
            if (message.bot) JTag("机器人")
            if (message.replyCount > 0 && onThread != null) {
                JTextButton("${message.replyCount} 条回复", onClick = onThread)
            }
            val pending = message.pendingDelivery?.pendingStatus()
            if (pending != null) {
                PendingBadge(if (retrying) PendingStatus.Verifying else pending)
                if (pending in setOf(PendingStatus.Unconfirmed, PendingStatus.NeedsReview))
                    JButton("核对结果", onClick = onRetry, enabled = message.clientId != null && !retrying,
                        kind = JButtonKind.Secondary, size = JButtonSize.Small)
            } else when (message.delivery) {
                LarkDelivery.Sending -> PendingBadge(PendingStatus.Sending)
                LarkDelivery.WaitingForNetwork -> PendingBadge(PendingStatus.Waiting)
                LarkDelivery.Failed -> {
                    PendingBadge(if (retrying) PendingStatus.Verifying else PendingStatus.Unconfirmed)
                    JButton("核对结果", onClick = onRetry, enabled = message.clientId != null && !retrying,
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
                LarkAvatar(message.senderName.ifBlank { if (message.bot) "机器人" else "信" }, message.avatar, onlineSince)
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
            AsyncImage(model = model, contentDescription = part.description,
                modifier = Modifier.fillMaxWidth().aspectRatio((part.width.toFloat() / part.height.coerceAtLeast(1)).coerceIn(.2f, 5f)),
                onError = { failed = true }, onSuccess = { failed = false })
            if (failed) Text("图片加载失败", style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink3)
        }
        is LarkPart.File -> JButton(text = if (opening) "正在打开…" else "文件：${part.name}", enabled = !opening, onClick = {
            opening = true
            scope.launch {
                try {
                    val uri = Uri.parse(repository.downloadFile(part))
                    check(uri.scheme == "content")
                    context.startActivity(Intent(Intent.ACTION_VIEW).setDataAndType(uri, part.mimeType).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION))
                } catch (e: CancellationException) { throw e }
                catch (_: Exception) { onError("文件打开失败，请重试或安装可打开此文件的应用") }
                finally { opening = false }
            }
        })
        is LarkPart.External -> Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            part.cover?.let { LarkPartContent(it, repository, onError, onOpenForward, onPlayVideo, onOpenProfile) }
            Text(listOf(when (part.kind) { "video" -> "视频"; "audio" -> "语音"; else -> "卡片" }, part.title, part.duration).filter { it.isNotBlank() }.joinToString("，"), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
            JButton(text = "在飞书中打开", onClick = {
                try {
                    val uri = Uri.parse(part.larkUrl)
                    check(uri.scheme in setOf("https", "lark", "feishu"))
                    context.startActivity(Intent(Intent.ACTION_VIEW, uri))
                } catch (_: Exception) { onError("无法打开飞书，请确认已安装飞书") }
            }, enabled = part.larkUrl.isNotBlank())
        }
    }
}
