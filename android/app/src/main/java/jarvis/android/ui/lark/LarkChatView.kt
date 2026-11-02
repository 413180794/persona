package jarvis.android.ui.lark

import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.lifecycle.compose.LifecycleStartEffect
import jarvis.android.data.Hub
import jarvis.android.lark.LarkChatRules
import jarvis.android.lark.LarkMessage
import jarvis.android.lark.LarkPalette
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ChatComposer
import jarvis.android.ui.chat.ComposerActions
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.LarkChrome
import jarvis.android.ui.design.LarkIcons
import jarvis.android.ui.design.surface
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * A Feishu chat (iOS `LarkChatView`; docs/feishu.md「保留什么」: the chat and link rendering, no other Feishu pages):
 * lark-connector's message layout, opened at the first new message under 以下为新消息 (or at the latest), newer pages as
 * the reader scrolls down, older ones at the top; 回到最新 / 有人@你; the composer sends as the user. Read state is
 * reported only for messages that reached the screen. [actions] are the page's (links, pictures, files, people…).
 */
@Composable
fun LarkChatView(hub: Hub, model: LarkChatModel, actions: LarkActions, modifier: Modifier = Modifier) {
    val state by model.state.collectAsState()
    val info = model.info
    val scope = rememberCoroutineScope()
    val messages = state.messages
    // Rows before the messages: 加载更早的消息 and the error line.
    val lead = (if (state.hasOlder) 1 else 0) + (if (state.error != null) 1 else 0)
    // Loaded ahead (a screenshot, a page shown again): the first frame is already where the chat opens.
    val first = remember { initialRow(model, lead) }
    val list = rememberLazyListState(first?.first ?: 0, first?.second ?: 0)
    var positioned by remember { mutableStateOf(first != null) }
    var draft by rememberSaveable(info.feedId) { mutableStateOf("") }
    var picking by remember { mutableStateOf<LarkMessage?>(null) }
    var thread by remember { mutableStateOf<LarkMessage?>(null) }
    var forwarded by remember { mutableStateOf<Pair<String, kotlinx.serialization.json.JsonElement>?>(null) }
    var moments by remember { mutableStateOf<Pair<String, String?>?>(null) }
    var profile by remember { mutableStateOf<LarkPersonRef?>(null) }

    LaunchedEffect(model) { model.start() }
    // Only while on screen: realtime pushes, with a poll a minute as the fallback.
    LifecycleStartEffect(model) {
        val job = scope.launch {
            launch { hub.larkChanges.filter { it.isEmpty() || info.feedId in it }.collect { follow(model, list) } }
            while (isActive) {
                delay(POLL_MS)
                follow(model, list)
            }
        }
        onStopOrDispose {
            job.cancel()
            model.flushRead()
        }
    }
    LaunchedEffect(model) {
        model.scrollRequests.collect { req ->
            delay(LAYOUT_MS) // the rows exist once the new data has been laid out
            val st = model.state.value
            val ld = (if (st.hasOlder) 1 else 0) + (if (st.error != null) 1 else 0)
            when (req) {
                is LarkScroll.Bottom -> {
                    val last = ld + st.messages.size - 1 + (if (st.hasNewer) 1 else 0)
                    if (last >= 0) { if (req.animate) list.animateScrollToItem(last) else list.scrollToItem(last, Int.MAX_VALUE / 2) }
                }
                is LarkScroll.ToMessage -> {
                    val i = st.messages.indexOfFirst { it.id == req.id }
                    if (i >= 0) { if (req.animate) list.animateScrollToItem(ld + i) else list.scrollToItem(ld + i) }
                }
            }
            positioned = true
        }
    }
    // Nothing to place (an empty chat, a failed load): show the list as it is.
    LaunchedEffect(state.loading, messages.isEmpty()) { if (!state.loading && messages.isEmpty()) positioned = true }
    // Read reports: the message rows on screen.
    LaunchedEffect(list, model) {
        snapshotFlow { if (!positioned) emptyList() else list.layoutInfo.visibleItemsInfo.mapNotNull { (it.key as? String)?.removePrefix(ROW) } }
            .distinctUntilChanged().collect { ids ->
                if (ids.isEmpty()) return@collect
                val byId = model.state.value.messages.associateBy { it.id }
                model.markSeen(ids.mapNotNull { byId[it]?.position })
            }
    }
    // The older page when the top comes into view.
    LaunchedEffect(list) {
        snapshotFlow { list.firstVisibleItemIndex }.distinctUntilChanged().collect { i ->
            if (positioned && i <= 2 && model.state.value.hasOlder) model.loadOlder()
        }
    }

    val atBottom by remember { derivedStateOf { !list.canScrollForward } }
    val unseen = remember(state.messages, state.seen, state.anchor) { LarkChatRules.unseenNew(state.messages, state.seen, state.anchor, info.unread) }
    val chatActions = remember(model, actions) {
        LarkActions(
            openUrl = actions.openUrl, openImages = actions.openImages, openFile = actions.openFile, playMedia = actions.playMedia,
            refresh = { scope.launch { model.refreshLatest() } }, openProfile = { profile = it }, react = { m, key -> model.react(m, key) },
            pickReaction = { picking = it }, openThread = { thread = it }, openForwarded = { t, meta -> forwarded = t to meta },
            openMoments = { id, link -> moments = id to link },
        )
    }
    val c = JTheme.colors

    androidx.compose.runtime.CompositionLocalProvider(LocalLarkActions provides chatActions, LocalLarkHub provides hub) {
        Column(modifier.fillMaxSize().imePadding()) {
            Box(Modifier.weight(1f).fillMaxWidth()) {
                LazyColumn(state = list, modifier = Modifier.fillMaxSize().alpha(if (positioned) 1f else 0f), contentPadding = PaddingValues(JSpace.m)) {
                    if (state.hasOlder) item(key = "older", contentType = "older") {
                        Box(Modifier.fillMaxWidth().heightIn(min = JSize.well).clickable { model.loadOlder() }, contentAlignment = Alignment.Center) {
                            if (state.loadingOlder) CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.rule, color = LarkChrome.blue)
                            else Text("加载更早的消息", style = JTheme.type.labelMedium, color = LarkChrome.blue, maxLines = 1)
                        }
                    }
                    state.error?.let { e ->
                        item(key = "error", contentType = "error") {
                            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                                ErrorLine(e, Modifier.weight(1f))
                                if (messages.isEmpty()) Text("重试", style = JTheme.type.secondaryMedium, color = LarkChrome.blue, maxLines = 1,
                                    modifier = Modifier.clip(CircleShape).clickable { model.retry() }.padding(horizontal = JSpace.m, vertical = JSpace.sm))
                            }
                        }
                    }
                    itemsIndexed(messages, key = { _, m -> ROW + m.id }, contentType = { _, m -> if (m.isSystem) "system" else "message" }) { i, m ->
                        val prev = messages.getOrNull(i - 1)
                        val cont = LarkChatRules.continues(m, prev)
                        Column {
                            if (LarkChatRules.isFirstUnread(m, prev, state.anchor)) LarkNewMessagesDivider()
                            if (LarkChatRules.startsDay(m, prev)) Text(LarkChatRules.dayLabel(m.createdAt), style = JTheme.type.labelMedium, color = c.secondaryText,
                                maxLines = 1, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth().padding(vertical = JSpace.m))
                            LarkMessageRow(m, grouped = cont, isGroup = info.isGroup, feedId = info.feedId,
                                modifier = Modifier.padding(top = if (cont) JSize.bar else JSpace.m))
                        }
                    }
                    if (state.hasNewer) item(key = "newer", contentType = "newer") {
                        LaunchedEffect(messages.size) { model.loadNewer() }
                        Box(Modifier.fillMaxWidth().heightIn(min = JSize.tapTarget), contentAlignment = Alignment.Center) {
                            CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.rule, color = c.secondaryText)
                        }
                    }
                }
                if (state.loading && messages.isEmpty()) {
                    CircularProgressIndicator(Modifier.align(Alignment.Center).size(JSize.icon), strokeWidth = JSize.rule, color = c.secondaryText)
                }
                // 有人@你
                val mention = state.mention
                androidx.compose.animation.AnimatedVisibility(mention != null && mention !in state.seen, Modifier.align(Alignment.TopEnd).padding(top = JSpace.s, end = JSpace.l),
                    enter = fadeIn() + scaleIn(), exit = fadeOut() + scaleOut()) {
                    val red = larkColor(LarkPalette.fixed("red"))
                    Row(
                        Modifier.defaultMinSize(minHeight = JSize.wellM).surface(CircleShape, floating = true).clickable { model.jumpToMention() }.padding(horizontal = JSpace.m),
                        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
                    ) {
                        Icon(JIcons.mention, null, Modifier.size(JSize.markS), tint = red)
                        Text("有人@你", style = JTheme.type.captionMedium, color = red, maxLines = 1)
                    }
                }
                // 回到最新 / N 条新消息
                androidx.compose.animation.AnimatedVisibility(positioned && (state.hasNewer || !atBottom), Modifier.align(Alignment.BottomEnd).padding(end = JSpace.l, bottom = JSpace.s),
                    enter = fadeIn() + scaleIn(), exit = fadeOut() + scaleOut()) {
                    val n = if (unseen > 99) "99+" else "$unseen"
                    Row(
                        Modifier.defaultMinSize(minWidth = JSize.wellXL, minHeight = JSize.wellXL).surface(CircleShape, floating = true)
                            .clickable { model.jumpToLatest() }.semantics { contentDescription = if (unseen > 0) "$n 条新消息，回到最新" else "回到最新" }
                            .padding(horizontal = if (unseen > 0) JSpace.m else JSpace.xxs),
                        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm, Alignment.CenterHorizontally),
                    ) {
                        Icon(LarkIcons.down, null, Modifier.size(JSize.iconXS), tint = LarkChrome.blue)
                        if (unseen > 0) Text("$n 条新消息", style = JTheme.type.captionMedium, color = LarkChrome.blue, maxLines = 1)
                    }
                }
            }
            ChatComposer(
                draft, { draft = it }, "回复 ${info.name}", canSend = !state.sending && draft.isNotBlank(),
                actions = ComposerActions(send = { scope.launch { if (model.send(draft)) draft = "" } }),
                modifier = Modifier.navigationBarsPadding().padding(start = JSpace.m, end = JSpace.m, top = JSpace.xs, bottom = JSpace.s),
                enabled = !state.sending,
            )
        }
        profile?.let { LarkProfileSheet(it) { profile = null } }
        picking?.let { m -> LarkReactionPicker(m, onPick = { model.react(m, it) }, onDismiss = { picking = null }) }
        thread?.let { m -> LarkThreadSheet(m, info.isGroup, info.feedId) { thread = null } }
        forwarded?.let { (t, meta) -> LarkForwardedSheet(t, meta) { forwarded = null } }
        moments?.let { (id, link) -> LarkMomentsSheet(id, link) { moments = null } }
    }
}

/** List rows of messages are keyed with this before the message id (the read report reads them back). */
private const val ROW = "m:"
private const val POLL_MS = 60_000L
private const val LAYOUT_MS = 16L

/** Where the list starts when the chat is loaded before the page is drawn: (row, offset), or null to wait for the load. */
private fun initialRow(model: LarkChatModel, lead: Int): Pair<Int, Int>? {
    val s = model.state.value
    if (s.messages.isEmpty()) return null
    val anchor = s.anchor
    if (anchor != null) {
        val i = s.messages.indexOfFirst { jarvis.android.lark.LarkPosition.less(anchor, it.position) }
        if (i >= 0) return (lead + i) to 0
    }
    return (lead + s.messages.size - 1 + (if (s.hasNewer) 1 else 0)) to Int.MAX_VALUE / 2
}

/** New messages arrived: follow them when the reader is at the bottom. */
private suspend fun follow(model: LarkChatModel, list: LazyListState) {
    val atBottom = !list.canScrollForward
    if (model.refreshLatest() > 0 && atBottom) {
        delay(LAYOUT_MS)
        val n = list.layoutInfo.totalItemsCount
        if (n > 0) list.animateScrollToItem(n - 1)
    }
}
