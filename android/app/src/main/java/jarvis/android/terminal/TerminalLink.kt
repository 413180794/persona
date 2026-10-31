package jarvis.android.terminal

import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.data.SessionKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * Where the terminal page's screen comes from, as the iOS app's TerminalFeed: the hub's live stream of the pane
 * (continued from the last frame after a reconnect), or — a hub, node or herdr that cannot stream — the colored
 * screen read once a second. Driving it (the default, 设置 in the page): the phone takes the terminal at its own grid
 * and keys go straight in; otherwise it mirrors the pane at the computer's size and keys go through `/keys`.
 */
class TerminalLink(private val hub: Hub, val key: SessionKey, private val scope: CoroutineScope) {
    val feed = TerminalFeed()

    enum class Mode { Stream, Poll }
    private val _mode = MutableStateFlow(Mode.Poll)
    val mode: StateFlow<Mode> = _mode.asStateFlow()
    /** Why the screen may not be current (shown over it); null: fine. */
    private val _failure = MutableStateFlow<String?>(null)
    val failure: StateFlow<String?> = _failure.asStateFlow()
    private val _loaded = MutableStateFlow(false)
    val loaded: StateFlow<Boolean> = _loaded.asStateFlow()

    @Volatile var wantsControl = true
        private set
    @Volatile private var grid: GridSize? = null
    @Volatile private var subscribed = false
    @Volatile private var unsupported = false
    @Volatile private var takeover = false
    @Volatile private var nextTry = 0L
    private var failures = 0
    private var job: Job? = null

    val canControl: Boolean get() = "terminal_control" in hub.features
    val canClick: Boolean get() = "terminal_click" in hub.features
    val canScroll: Boolean get() = "terminal_scroll" in hub.features
    val drives: Boolean get() = feed.control.value == TerminalFeed.Control.Active

    /** Runs while the page is on screen (and the app in front): keeps the stream subscribed, or polls. */
    fun start(wantsControl: Boolean) {
        this.wantsControl = wantsControl
        job?.cancel()
        job = scope.launch {
            while (isActive) {
                tick()
                delay(if (_mode.value == Mode.Poll) 1_000 else 250)
            }
        }
    }

    fun stop() {
        job?.cancel(); job = null
        if (subscribed) hub.terminalUnsubscribe(key)
        subscribed = false
        if (feed.control.value != TerminalFeed.Control.Declined) feed.setControl(TerminalFeed.Control.Off)
    }

    private suspend fun tick() {
        val live = hub.conn.value == ConnState.Online
        if (_mode.value == Mode.Poll && !unsupported && live && "terminal_stream" in hub.features) {
            if (willDrive() || !wantsControl || !canControl || grid != null) {
                _mode.value = Mode.Stream
                subscribe(continuing = false)
                return
            }
        }
        when (_mode.value) {
            Mode.Stream -> {
                if (!live) { subscribed = false; if (_failure.value == null) _failure.value = LINK_DOWN; return }
                if (_failure.value == LINK_DOWN) _failure.value = null
                if (!subscribed && System.currentTimeMillis() >= nextTry) subscribe(continuing = true)
            }
            Mode.Poll -> if (System.currentTimeMillis() >= nextTry) poll()
        }
    }

    private suspend fun poll() {
        runCatching { hub.api.screen(key, 60, ansi = true) }
            .onSuccess { r ->
                if (_mode.value != Mode.Poll) return
                feed.showPolled(TerminalGrid.rowsFromAnsi(r.text).dropLastWhile { row -> row.all { it.isBlank } })
                healthy()
            }
            .onFailure { failed(it.message ?: "读取终端失败") }
        _loaded.value = true
    }

    private fun willDrive(): Boolean {
        val c = feed.control.value
        return wantsControl && canControl && grid != null && c != TerminalFeed.Control.Declined && c != TerminalFeed.Control.Busy
    }

    private fun subscribe(continuing: Boolean) {
        subscribed = true
        val drive = willDrive()
        if (drive && feed.control.value == TerminalFeed.Control.Off) feed.setControl(TerminalFeed.Control.Starting)
        if (!continuing) feed.forgetStream()
        val g = grid
        hub.terminalSubscribe(
            key, if (continuing) feed.epochToResume else null, feed.lastSeq,
            if (drive && g != null) g.cols to g.rows else null, takeover, ::receive,
        )
        takeover = false
    }

    private fun receive(e: Hub.TerminalEvent) {
        when (e) {
            is Hub.TerminalEvent.Frame -> when (synchronized(feed) { feed.apply(e.epoch, e.seq, e.full, e.cols, e.rows, e.data) }) {
                TerminalFeed.Result.Applied -> { healthy(); _loaded.value = true }
                TerminalFeed.Result.Duplicate -> {}
                TerminalFeed.Result.Gap -> subscribe(continuing = false)
            }
            is Hub.TerminalEvent.Ended -> {
                subscribed = false
                feed.forgetStream()
                if (e.unsupported) {
                    unsupported = true
                    _mode.value = Mode.Poll
                    nextTry = 0
                    return
                }
                failed(if ("not running" in e.error) "会话没有在 herdr 里运行" else e.error)
                _loaded.value = true
                val c = feed.control.value
                if (c == TerminalFeed.Control.Active || c == TerminalFeed.Control.Starting) feed.setControl(TerminalFeed.Control.Off)
            }
            is Hub.TerminalEvent.Control -> feed.controlChanged(e.state, e.error)
            Hub.TerminalEvent.Reconnected -> if (_mode.value == Mode.Stream) subscribe(continuing = true)
        }
    }

    /** The page's grid changed (laid out, the keyboard): a terminal the phone drives takes the new size. */
    fun setGrid(g: GridSize) {
        if (g == grid || g.cols < 10 || g.rows < 4) return
        grid = g
        val c = feed.control.value
        if (subscribed && (c == TerminalFeed.Control.Active || c == TerminalFeed.Control.Starting)) hub.terminalInput(key, control = g.cols to g.rows)
    }

    /** The user said take it from the program that holds it (or take it back). */
    fun takeOver() {
        takeover = true
        feed.setControl(TerminalFeed.Control.Starting)
        if (hub.conn.value == ConnState.Online) subscribe(continuing = true)
    }

    fun watchOnly(why: String) { feed.setControl(TerminalFeed.Control.Declined, why) }

    /** 手机尺寸 / 电脑尺寸: subscribe again the other way. */
    fun setWantsControl(on: Boolean) {
        if (on == wantsControl) return
        wantsControl = on
        feed.setControl(TerminalFeed.Control.Off)
        if (subscribed && hub.conn.value == ConnState.Online) subscribe(continuing = false)
    }

    /** A key by herdr's name ("enter", "ctrl+c"): bytes into the terminal the phone drives, or `/keys` otherwise. */
    fun key(name: String) {
        if (drives) TerminalKeys.encode(name)?.let { hub.terminalInput(key, text = it) }
        else scope.launch { runCatching { hub.api.keys(key, listOf(name)) }.onFailure { failed(it.message ?: "发送失败") }; nudge() }
    }

    fun type(text: String) {
        if (text.isEmpty()) return
        if (drives) hub.terminalInput(key, text = TerminalKeys.text(text))
        else scope.launch { runCatching { hub.api.keys(key, emptyList(), text) }.onFailure { failed(it.message ?: "发送失败") }; nudge() }
    }

    fun wheel(lines: Int, at: Pair<Int, Int>) { if (lines != 0 && drives && canScroll) hub.terminalInput(key, scroll = lines, at = at) }
    fun click(at: Pair<Int, Int>) { if (drives && canClick) hub.terminalInput(key, click = at) }

    /** Read the screen again right away (after sending keys, so their echo shows). */
    private fun nudge() { if (_mode.value == Mode.Poll) nextTry = 0 }

    private fun healthy() { _failure.value = null; failures = 0 }

    private fun failed(why: String) {
        _failure.value = why
        failures += 1
        nextTry = System.currentTimeMillis() + backoffMillis(failures)
    }

    companion object {
        const val LINK_DOWN = "连不上 hub"
        /** 1, 2, 4, 8 and then every 15 seconds. */
        fun backoffMillis(failures: Int): Long = minOf(15_000L, 1_000L shl (maxOf(0, failures - 1)).coerceAtMost(4))
    }
}
