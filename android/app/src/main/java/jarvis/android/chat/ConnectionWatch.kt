package jarvis.android.chat

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.os.SystemClock
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.data.SessionKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.transformLatest
import kotlinx.coroutines.launch

/**
 * What the title spot says about the link to the hub (docs/ios.md「连接状态」): 等待网络 (the phone has no network),
 * 连接中… (no socket, or a socket whose ping went unanswered for 5 s = a weak link), 更新中… (just connected, the open
 * chats' backlog not in yet, at most 8 s). No banners. A problem must last 1.5 s before it shows; recovery shows at once.
 * When catching up ends, the outbox goes out.
 */
object ConnectionWatch {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    private val _hasNetwork = MutableStateFlow(true)
    val hasNetwork: StateFlow<Boolean> = _hasNetwork.asStateFlow()

    private val _stalled = MutableStateFlow(false)
    /** Connected, but the last protocol ping got no pong within 5 s. */
    val stalled: StateFlow<Boolean> = _stalled.asStateFlow()

    private val _catchingUp = MutableStateFlow(false)
    val catchingUp: StateFlow<Boolean> = _catchingUp.asStateFlow()

    private val _hub = MutableStateFlow<Hub?>(null)
    val hub: StateFlow<Hub?> = _hub.asStateFlow()

    /** Chats on screen (their backlog ends 更新中…). */
    private val open = HashSet<SessionKey>()
    private val awaiting = HashSet<SessionKey>()
    private var catchUpJob: Job? = null
    private var stallJob: Job? = null
    @Volatile var lastConnectedAt: Long = 0
        private set

    /** The undebounced label; null = show the normal title. */
    fun raw(conn: ConnState?, hasNetwork: Boolean, stalled: Boolean, catchingUp: Boolean): String? = when {
        conn == null -> null
        conn == ConnState.Unauthorized -> "需要重新配对"
        conn == ConnState.Online && stalled -> "连接中…"
        conn == ConnState.Online && catchingUp -> "更新中…"
        conn == ConnState.Online -> null
        !hasNetwork -> "等待网络"
        else -> "连接中…"
    }

    /** The debounced label for the title (null = normal title). */
    @OptIn(ExperimentalCoroutinesApi::class)
    val label: StateFlow<String?> by lazy {
        var shown: String? = null
        _hub.flatMapLatest { h -> h?.conn ?: flowOf(null) }
            .let { connFlow -> combine(connFlow, _hasNetwork, _stalled, _catchingUp) { c, n, s, u -> raw(c, n, s, u) } }
            .distinctUntilChanged()
            .transformLatest { l ->
                if (!(l == null || shown != null || l == "需要重新配对")) delay(1_500)
                shown = l
                emit(l)
            }
            .stateIn(scope, SharingStarted.Eagerly, null)
    }

    fun init(ctx: Context) {
        val cm = ctx.getSystemService(ConnectivityManager::class.java) ?: return
        _hasNetwork.value = cm.activeNetwork?.let { n ->
            cm.getNetworkCapabilities(n)?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
        } ?: false
        runCatching {
            cm.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) { _hasNetwork.value = true }
                override fun onLost(network: Network) { _hasNetwork.value = cm.activeNetwork != null && cm.activeNetwork != network }
            })
        }
    }

    private var watch: Job? = null

    /** A new hub connection (JarvisApp.connect): follows its socket and its subscribe backlogs. */
    fun attach(h: Hub) {
        watch?.cancel()
        _hub.value = h
        _stalled.value = false
        _catchingUp.value = false
        watch = scope.launch {
            launch {
                var online = false
                h.conn.collect { c ->
                    val now = c == ConnState.Online
                    if (now && !online) {
                        lastConnectedAt = System.currentTimeMillis()
                        startCatchUp(h)
                        startStallWatch(h)
                    } else if (!now && online) {
                        stallJob?.cancel()
                        _stalled.value = false
                        endCatchUp(flush = false)
                    }
                    online = now
                }
            }
            launch { h.backlogs.collect { caughtUp(it, h) } }
        }
    }

    fun detach() {
        watch?.cancel()
        watch = null
        _hub.value = null
        endCatchUp(flush = false)
    }

    fun chatOpened(key: SessionKey) = synchronized(this) { open += key }
    fun chatClosed(key: SessionKey) = synchronized(this) { open -= key; awaiting -= key }

    private fun startCatchUp(h: Hub) {
        synchronized(this) {
            catchUpJob?.cancel()
            awaiting.clear()
            awaiting += open
            _catchingUp.value = awaiting.isNotEmpty()
            catchUpJob = scope.launch {
                if (awaiting.isNotEmpty()) delay(8_000) else delay(300)
                endCatchUp(flush = true, hub = h)
            }
        }
    }

    private fun caughtUp(key: SessionKey, h: Hub) {
        val done = synchronized(this) { awaiting.remove(key) && awaiting.isEmpty() }
        if (done) endCatchUp(flush = true, hub = h)
    }

    private fun endCatchUp(flush: Boolean, hub: Hub? = null) {
        synchronized(this) {
            catchUpJob?.cancel(); catchUpJob = null
            awaiting.clear()
            _catchingUp.value = false
        }
        if (flush && hub != null) Outbox.flush(hub)
    }

    /**
     * Watches OkHttp's ping / pong counters once a second: a ping unanswered for 5 s is a slow link, and only a pong
     * that comes back clears it (a late pong counts as slow too).
     */
    private fun startStallWatch(h: Hub) {
        stallJob?.cancel()
        val ws = h.socket as? okhttp3.internal.ws.RealWebSocket ?: return
        stallJob = scope.launch {
            var sent = runCatching { ws.sentPingCount() }.getOrDefault(0)
            var sentAt = SystemClock.elapsedRealtime()
            while (h.socket === ws) {
                delay(1_000)
                val s = runCatching { ws.sentPingCount() }.getOrNull() ?: break
                val r = runCatching { ws.receivedPongCount() }.getOrNull() ?: break
                val now = SystemClock.elapsedRealtime()
                if (s != sent) { sent = s; sentAt = now }
                if (r >= s) _stalled.value = false else if (now - sentAt > 5_000) _stalled.value = true
            }
            _stalled.value = false
        }
    }
}
