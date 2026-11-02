package jarvis.android.ui.lark

import jarvis.android.data.Hub
import jarvis.android.data.HubException
import jarvis.android.lark.LarkCache
import jarvis.android.lark.LarkChatRules
import jarvis.android.lark.LarkMessage
import jarvis.android.lark.LarkMessagesPage
import jarvis.android.lark.LarkPosition
import jarvis.android.lark.larkMessages
import jarvis.android.lark.larkReact
import jarvis.android.lark.larkRead
import jarvis.android.lark.larkSend
import jarvis.android.lark.togglingReaction
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.File

/** What a chat is opened with (iOS `LarkConversation` from the inbox item, `InboxStore.larkConversation`). */
data class LarkChatInfo(
    val feedId: String,
    val name: String = "",
    val isGroup: Boolean = true,
    val isTopic: Boolean = false,
    /** Unread messages from others when it was opened (the 「N 条新消息」 count); 0 when the item is done. */
    val unread: Int = 0,
    /** The first new message: the chat opens there (around it); null opens at the latest. */
    val focus: String? = null,
    /** The unread @me: 有人@你 jumps to it. */
    val mentionPosition: String? = null,
)

/** What the chat page shows (iOS `LarkChatView` state). */
data class LarkChatState(
    val messages: List<LarkMessage> = emptyList(),
    val hasOlder: Boolean = false,
    val hasNewer: Boolean = false,
    val loading: Boolean = true,
    val loadingOlder: Boolean = false,
    val error: String? = null,
    val sending: Boolean = false,
    /** The read position the chat was opened at: 以下为新消息 sits above the first message after it. Fixed for the visit. */
    val anchor: String? = null,
    /** 有人@你: the position of the unseen mention. */
    val mention: String? = null,
    /** Positions that have been on screen. */
    val seen: Set<String> = emptySet(),
)

/** Where the list should move to (the page consumes it). */
sealed interface LarkScroll {
    data class ToMessage(val id: String, val animate: Boolean = false) : LarkScroll
    data class Bottom(val animate: Boolean) : LarkScroll
}

/**
 * One Feishu chat, opened like Feishu (iOS `LarkChatView`): at its first new message ([LarkChatInfo.focus], the page
 * around it; never the latest page for an unread chat) under a 以下为新消息 line, or at the latest; newer pages as the
 * reader scrolls down; read state reported only for what reached the screen; 回到最新 / 有人@你; pushes from the hub's
 * `lark` event. The last page is cached on disk (under [cacheDir]) so the chat paints at once. [scope] is the page's.
 */
class LarkChatModel(
    private val hub: Hub, val info: LarkChatInfo, private val scope: CoroutineScope, cacheDir: File,
    /** Lives past the page (the app's): read reports go out even as the page closes. */
    private val outlive: CoroutineScope = scope,
) {
    private val api get() = hub.api
    private val cache = LarkCache(File(cacheDir, "lark-chats"))
    private val _state = MutableStateFlow(LarkChatState())
    val state: StateFlow<LarkChatState> = _state.asStateFlow()
    private val scrolls = Channel<LarkScroll>(Channel.CONFLATED)
    val scrollRequests = scrolls.receiveAsFlow()

    private var olderCursor: String? = null
    private var newerCursor: String? = null
    private var loadingNewer = false
    private var readCursor: String? = null
    private var reportJob: Job? = null
    private val refreshLock = Mutex()
    private var started = false

    private val isTopic get() = info.isTopic
    /** The read position before the first new message. */
    private val readBefore: String? get() = info.focus?.let(LarkPosition::previous)

    fun start() {
        if (started) return
        started = true
        LarkMedia.loadCatalog(api)
        scope.launch {
            paintCached()
            load()
        }
    }

    /** The last messages seen here paint at once (offline too); an unread chat only when the cache reaches its first new one. */
    private suspend fun paintCached() {
        if (_state.value.messages.isNotEmpty()) return
        val cached = cache.page(info.feedId)?.takeIf { it.isNotEmpty() } ?: return
        val focus = info.focus
        if (focus != null && LarkPosition.less(focus, cached.first().position)) return
        olderCursor = cached.first().position
        newerCursor = cached.last().position
        _state.update { it.copy(messages = cached, hasOlder = true, loading = false) }
        val read = readBefore
        if (read != null) {
            _state.update { it.copy(anchor = read) }
            cached.firstOrNull { LarkPosition.less(read, it.position) }?.let { scrolls.trySend(LarkScroll.ToMessage(it.id)) }
        } else scrolls.trySend(LarkScroll.Bottom(false))
    }

    private fun apply(r: LarkMessagesPage) {
        if (r.hasNewer != true) scope.launch { cache.save(info.feedId, r.messages) }
        olderCursor = r.olderCursor ?: r.messages.firstOrNull()?.position
        newerCursor = r.newerCursor ?: r.messages.lastOrNull()?.position
        _state.update { it.copy(messages = r.messages, hasOlder = r.hasOlder, hasNewer = r.hasNewer ?: false, error = null, loading = false) }
    }

    private suspend fun load() {
        try {
            val focus = info.focus
            val read = readBefore
            if (focus != null && read != null && !isTopic) {
                readCursor = read
                val mention = info.mentionPosition?.takeIf { LarkPosition.less(read, it) }
                _state.update { it.copy(anchor = read, mention = mention) }
                apply(api.larkMessages(info.feedId, around = focus))
                val target = _state.value.messages.firstOrNull { LarkPosition.less(read, it.position) }
                scrolls.trySend(if (target != null) LarkScroll.ToMessage(target.id) else LarkScroll.Bottom(false))
            } else {
                apply(api.larkMessages(info.feedId, topic = isTopic))
                readCursor = _state.value.messages.lastOrNull()?.position
                scrolls.trySend(LarkScroll.Bottom(false))
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(error = errorText(e), loading = false) }
        }
    }

    fun retry() {
        _state.update { it.copy(loading = true, error = null) }
        scope.launch { load() }
    }

    /** The latest page again: new messages append, changed ones (cards, reactions, recalls) update in place. How many were new. */
    suspend fun refreshLatest(): Int = refreshLock.withLock {
        if (_state.value.hasNewer) return 0
        val r = try {
            api.larkMessages(info.feedId, topic = isTopic)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            return 0
        }
        val (merged, fresh) = LarkChatRules.mergeLatest(_state.value.messages, r.messages)
        if (merged != _state.value.messages) _state.update { it.copy(messages = merged, error = null, loading = false) }
        if (fresh > 0) {
            newerCursor = merged.lastOrNull()?.position
            scope.launch { cache.save(info.feedId, merged) }
        }
        fresh
    }

    fun loadOlder() {
        val s = _state.value
        val cursor = olderCursor ?: s.messages.firstOrNull()?.position ?: return
        if (s.loadingOlder || !s.hasOlder) return
        _state.update { it.copy(loadingOlder = true) }
        scope.launch {
            try {
                val r = api.larkMessages(info.feedId, before = cursor, topic = isTopic)
                olderCursor = r.olderCursor ?: r.messages.firstOrNull()?.position
                _state.update { it.copy(messages = LarkChatRules.prependOlder(it.messages, r.messages), hasOlder = r.hasOlder, loadingOlder = false) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(loadingOlder = false) }
                Toasts.show("没读到更早的消息：${errorText(e)}", ToastStyle.ERROR)
            }
        }
    }

    fun loadNewer() {
        val s = _state.value
        val cursor = newerCursor ?: s.messages.lastOrNull()?.position ?: return
        if (loadingNewer || !s.hasNewer) return
        loadingNewer = true
        scope.launch {
            try {
                val r = api.larkMessages(info.feedId, after = cursor)
                newerCursor = r.newerCursor ?: r.messages.lastOrNull()?.position ?: cursor
                _state.update { it.copy(messages = LarkChatRules.appendNewer(it.messages, r.messages), hasNewer = r.hasNewer ?: false) }
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
            } finally {
                loadingNewer = false
            }
        }
    }

    /** 回到最新: the latest page; the new-messages line goes. */
    fun jumpToLatest() {
        scope.launch {
            if (_state.value.hasNewer) {
                runCatching { api.larkMessages(info.feedId, topic = isTopic) }.getOrNull()?.let {
                    apply(it)
                    _state.update { s -> s.copy(anchor = null) }
                }
            }
            scrolls.trySend(LarkScroll.Bottom(true))
        }
    }

    /** 有人@你: to the mention, loading the page around it when it is not here. */
    fun jumpToMention() {
        val target = _state.value.mention ?: return
        _state.value.messages.firstOrNull { it.position == target }?.let {
            scrolls.trySend(LarkScroll.ToMessage(it.id, animate = true))
            return
        }
        scope.launch {
            val r = runCatching { api.larkMessages(info.feedId, around = target) }.getOrNull() ?: return@launch
            apply(r)
            _state.value.messages.firstOrNull { it.position == target }?.let { scrolls.trySend(LarkScroll.ToMessage(it.id)) }
        }
    }

    /** Toggles my reaction at once, then tells Feishu; a failure puts the message back. */
    fun react(m: LarkMessage, key: String) {
        val s = _state.value
        val i = s.messages.indexOfFirst { it.id == m.id }
        if (i < 0) return
        val me = s.messages.lastOrNull { it.isMine }?.senderName ?: "我"
        val (next, remove) = s.messages[i].togglingReaction(key, me)
        _state.update { it.copy(messages = it.messages.map { x -> if (x.id == m.id) next else x }) }
        scope.launch {
            try {
                api.larkReact(m.id, key, remove)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Toasts.show("没回应上：${errorText(e)}", ToastStyle.ERROR)
                _state.update { it.copy(messages = it.messages.map { x -> if (x.id == m.id) m else x }) }
            }
        }
    }

    /** Messages reached the screen: once rows stop arriving, everything up to the newest one seen is reported. */
    fun markSeen(positions: Collection<String>) {
        val s = _state.value
        val new = positions.filter { it !in s.seen }
        if (new.isEmpty()) return
        _state.update { it.copy(seen = it.seen + new) }
        if (isTopic) return
        reportJob?.cancel()
        reportJob = scope.launch {
            delay(REPORT_AFTER_MS)
            flushRead()
        }
    }

    /** Reports what was on screen past the last report (not tied to the page: leaving the chat must not lose it). */
    fun flushRead() {
        if (isTopic) return
        val s = _state.value
        val report = LarkChatRules.readReport(s.messages, s.seen, readCursor) ?: return
        readCursor = report.position
        outlive.launch { runCatching { api.larkRead(info.feedId, report) } }
    }

    /** Sends [text] as the user; true when it went out (the composer clears). */
    suspend fun send(text: String): Boolean {
        val t = text.trim()
        if (t.isEmpty() || _state.value.sending) return false
        _state.update { it.copy(sending = true) }
        return try {
            api.larkSend(info.feedId, t)
            scope.launch {
                delay(SETTLE_AFTER_SEND_MS)
                if (_state.value.hasNewer) jumpToLatest() else { refreshLatest(); scrolls.trySend(LarkScroll.Bottom(true)) }
            }
            true
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Toasts.show("没发出去：${errorText(e)}", ToastStyle.ERROR)
            false
        } finally {
            _state.update { it.copy(sending = false) }
        }
    }

    /** The page is gone: its work stops (a read report already on its way still goes out on [outlive]). */
    fun close() {
        flushRead()
        scope.cancel()
    }

    private fun errorText(e: Exception): String = (e as? HubException)?.message ?: e.message ?: "加载失败"

    private companion object {
        /** Rows that keep arriving (a fling) are reported together. */
        const val REPORT_AFTER_MS = 400L
        /** lark-connector takes a moment before a sent message is in the chat's latest page. */
        const val SETTLE_AFTER_SEND_MS = 800L
    }
}
