package jarvis.core.hub

import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.flow.first
import jarvis.core.contracts.*
import jarvis.core.contracts.api.*
import java.security.MessageDigest
import java.time.Instant
import java.util.Base64
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.*
import kotlinx.serialization.json.*
import okhttp3.*
import kotlin.random.Random

data class HubNetwork(
    val id: String,
    val available: Boolean = true,
    val homeWifi: Boolean = false,
    val localNetworkPermissionGranted: Boolean = true,
    // Include global IPv6 hosts on the same local link when permission is required.
    val localHosts: Set<String> = emptySet(),
    val transport: HubNetworkTransport = HubNetworkTransport.Other,
)

class HubConnectionConfig(
    val hubId: String?,
    val fingerprint: String?,
    val token: String,
    val endpoints: List<Endpoint>,
    val version: String,
    val deviceId: String? = null,
    val build: JsonElement? = null,
    val os: JsonObject? = null,
    val requiredFeatures: Set<String> = emptySet(),
    val clientFeatures: Set<String> = emptySet(),
    val heartbeatPolicy: HeartbeatPolicy = HeartbeatPolicy(),
    /**
     * The other hubs of this hub's group, tried after this hub's own candidates in list order (docs/android.md「多 hub」).
     * Empty for a hub without a group: everything then behaves exactly as for a single paired hub.
     */
    val members: List<HubMember> = emptyList(),
)

/** Call onReady to restore subscriptions and reconcile pending interactions after hello. */
class OkHttpHubConnection(
    private val config: HubConnectionConfig,
    initialNetwork: HubNetwork,
    val http: HubHttpClient = HubHttpClient(config.hubId, config.fingerprint, config.version,
        pins = config.members.mapNotNullTo(LinkedHashSet()) { it.fingerprint }),
    dispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val onReady: suspend (OkHttpHubConnection) -> Unit = {},
    // Android callers should inject SystemClock.elapsedRealtime, which includes deep sleep.
    private val clockMillis: () -> Long = { System.nanoTime() / 1_000_000 },
    private val webSocketFactory: WebSocket.Factory = http.client,
    /** How often a running socket looks for an answer from a candidate listed above it; shorter only in tests. */
    private val recheckMillis: Long = RECHECK_MILLIS,
    /**
     * A hub of the group refused its credential (401): it is no longer tried by this connection. [ownToken] is false
     * when the hub only rejected the borrowed group credential. Not called for a connection without [HubConnectionConfig.members].
     */
    private val onMemberRevoked: (hubId: String, ownToken: Boolean) -> Unit = { _, _ -> },
) : HubConnection, ConnectionDiagnosticsSource, AutoCloseable {
    private val scope = CoroutineScope(SupervisorJob() + dispatcher)
    private val lock = Any()
    private val selector = HubRouteSelector(clockMillis) { endpoint -> probe(endpoint) }
    @Volatile private var members: List<HubMember> = config.members
    /** Hubs that refused their credential, with the member as it was then; a changed member is tried again. */
    private val excluded = HashMap<String, HubMember>()
    @Volatile private var homeExcluded = false
    private val _memberEndpoints = MutableStateFlow<Map<String, List<Endpoint>>>(emptyMap())
    /** Candidates the other hubs of the group sent in their hello / endpoints events while connected through them. */
    val memberEndpoints = _memberEndpoints.asStateFlow()
    /** The hub a selected route belongs to; kept after the route is cleared, so a failure is charged to it. */
    @Volatile private var routeOwner: Pair<HubRoute, Owner>? = null
    @Volatile private var attemptOwner: Owner? = null
    private val _state = MutableStateFlow(ConnectionState.Connecting)
    override val state = _state.asStateFlow()
    private val _serverFeatures = MutableStateFlow<Set<String>>(emptySet())
    val serverFeatures = _serverFeatures.asStateFlow()
    /** A pairing-owned consumer for phone calls, before typed event decoding. */
    @Volatile var phoneMessageHandler: ((JsonObject) -> Boolean)? = null
    private val _events = MutableSharedFlow<ServerEvent>()
    override val events = _events.asSharedFlow()
    private val _endpoints = MutableStateFlow(config.endpoints)
    /** The pairing's candidates until the hub sends its own list (hello, endpoints events); manual ones are separate. */
    val endpoints = _endpoints.asStateFlow()
    private val _advertised = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the list the hub sent most recently: tells the hub's candidates from the pairing's. */
    val advertised = _advertised.asStateFlow()
    private val _overrides = MutableStateFlow(EndpointOverrides())
    /** Route choices made on this phone; a list from the hub never changes them. */
    val overrides = _overrides.asStateFlow()
    private val _route = MutableStateFlow<HubRoute?>(null)
    val route = _route.asStateFlow()
    private val _failure = MutableStateFlow<HubFailure?>(null)
    val failure = _failure.asStateFlow()
    private val _heartbeat = MutableStateFlow(Heartbeat(20, 15))
    val heartbeat = _heartbeat.asStateFlow()
    private val _weakNetwork = MutableStateFlow(false)
    val weakNetwork = _weakNetwork.asStateFlow()
    private val _diagnostics = MutableStateFlow(ConnectionDiagnostics())
    override val diagnostics = _diagnostics.asStateFlow()
    /** The credential for the hub the current route reaches: its own token, else the group credential. */
    val token: String get() = ownerOf(_route.value).token

    @Volatile private var network = initialNetwork
    @Volatile private var foreground = true
    @Volatile private var socket: WebSocket? = null
    @Volatile private var mailbox: Channel<Signal>? = null
    @Volatile private var closed = false
    @Volatile private var revoked = false
    private var runner: Job? = null
    private var pendingState: Job? = null
    private var stateTarget: ConnectionState? = null
    private val pingIds = AtomicLong()

    init { updateDiagnostics { it }; reconnect() }

    override suspend fun readDiagnostics(): ConnectionDiagnostics {
        val deviceId = config.deviceId ?: return diagnostics.value
        if (route.value == null) return diagnostics.value
        val path = HttpUrl.Builder().scheme("https").host("example.com")
            .addPathSegments("api/v1/devices").addPathSegment(deviceId).addPathSegment("sessions")
            .build().encodedPath
        val rows = try {
            request(path).use { response ->
                JsonCodec.parseToJsonElement(response.body!!.string()).jsonObject["sessions"] as? JsonArray
            }
        } catch (error: HubFailure) {
            if (error.status == 404) return diagnostics.value
            throw error
        }
        val sessions = rows.orEmpty().map { row ->
            val value = row.jsonObject
            fun text(key: String) = value[key]?.jsonPrimitive?.contentOrNull
            DeviceConnectionSession(text("connected_at").orEmpty(), text("disconnected_at"), text("reason"))
        }
        updateDiagnostics { it.copy(sessions = sessions) }
        return diagnostics.value
    }

    private fun updateDiagnostics(change: (ConnectionDiagnostics) -> ConnectionDiagnostics) {
        _diagnostics.update { previous ->
            val value = change(previous)
            value.copy(unmeasuredFields = buildMap {
                if (value.currentPath == null) put("currentPath", "未实测")
                if (value.fingerprint == null) put("fingerprint", "未实测")
                if (value.heartbeatSeconds == null) put("heartbeatSeconds", "未实测")
                if (value.foregroundHeartbeatSeconds == null) put("foregroundHeartbeatSeconds", "未实测")
                if (value.backgroundHeartbeatSeconds == null) put("backgroundHeartbeatSeconds", "未实测")
                if (value.pongTimeoutSeconds == null) put("pongTimeoutSeconds", "未实测")
                if (value.lastReceivedAt == null) put("lastReceivedAt", "未实测")
                if (value.lastDisconnectReason == null) put("lastDisconnectReason", "未实测")
            })
        }
    }

    private fun clearRoute() {
        _route.value = null
        updateDiagnostics { it.copy(currentPath = null, fingerprint = null, heartbeatSeconds = null, pongTimeoutSeconds = null) }
    }

    /** Races the enabled candidates in the order settings lists them; diagnostics start over for this attempt. */
    private suspend fun selectRoute(): HubRoute {
        val candidates = usableEndpoints()
        syncPins()
        updateDiagnostics { it.copy(candidates = candidates.map { endpoint ->
            ConnectionCandidate(endpoint.url, endpoint.kind, "未实测")
        }) }
        return probeRoutes(candidates)
    }

    /** Probes [candidates] in order ([HubRouteSelector]), recording each measured probe in the diagnostics. */
    private suspend fun probeRoutes(candidates: List<Endpoint>, probeTimeoutMillis: Long? = null): HubRoute =
        selector.select(candidates, probeTimeoutMillis, onProbeMeasured = { endpoint, elapsed, result ->
            updateDiagnostics { snapshot -> snapshot.copy(candidates = snapshot.candidates.map { candidate ->
                if (candidate.url != endpoint.url) candidate else candidate.copy(
                    result = endpointResultLabel(result.exceptionOrNull()?.let { HubFailure.from(it).kind }),
                    checkedAt = Instant.now().toString(), probeDurationMillis = elapsed,
                    sampleCount = 1, measurementNote = "单次探测（样本数 1），不作为性能基线",
                )
            }) }
        })

    override fun reconnect() = synchronized(lock) {
        if (closed || revoked) return@synchronized
        val previous = runner
        previous?.cancel()
        socket?.cancel()
        socket = null
        clearRoute()
        runner = scope.launch { previous?.join(); connectLoop() }
    }

    fun updateNetwork(value: HubNetwork) {
        if (network == value) return
        network = value
        reconnect()
    }

    fun updateEndpoints(value: List<Endpoint>) {
        if (value == _endpoints.value) return
        _endpoints.value = value.toList()
        reconnect()
    }

    /** Manual candidates first, then the pairing's or the hub's, deduplicated by key; disabled ones included. */
    fun candidates(overrides: EndpointOverrides = _overrides.value): List<Endpoint> =
        mergeEndpoints(overrides.manual, _endpoints.value, http.localAddressPolicy)

    /** This hub's candidates, then each other hub's of the group (disabled ones included), deduplicated by key. */
    fun allCandidates(): List<Endpoint> =
        (candidates() + members.flatMap { memberCandidates(it) }).distinctBy { endpointKey(it.url) }

    /**
     * Replaces the other hubs of the group. A hub that refused its credential stays out unless it comes back changed.
     * Starts over only when the route in use belongs to a hub no longer listed, or there is no route.
     */
    fun updateMembers(value: List<HubMember>) {
        val reconnect = synchronized(lock) {
            if (value == members) return
            members = value.toList()
            excluded.entries.removeAll { (hubId, member) -> value.none { it.hubId == hubId } || value.first { it.hubId == hubId } != member }
            http.updatePins(value.mapNotNullTo(LinkedHashSet()) { it.fingerprint })
            syncPins()
            val owner = routeOwner?.takeIf { it.first === _route.value }?.second
            _route.value == null || (owner != null && !owner.home && value.none { it.hubId == owner.hubId })
        }
        if (reconnect) reconnect()
    }

    /**
     * Applies new route choices. Without a route, or when the one in use is no longer an enabled candidate, the
     * connection starts over. A new order is only stored: the connection follows it at the next reconnect or periodic
     * recheck in [runSocket], so dragging an address never drops a working connection (user decision, 2026-11-02).
     */
    fun updateOverrides(value: EndpointOverrides) {
        val previous = _overrides.getAndUpdate { value }
        if (previous == value) return
        val active = _route.value?.endpoint?.url?.let(::endpointKey)
        if (active == null || active !in usableEndpoints(value).map { endpointKey(it.url) }) reconnect()
    }

    fun setForeground(value: Boolean) {
        if (foreground == value) return
        foreground = value
        mailbox?.trySend(Signal.Foreground)
    }

    /** Validate the live connection when the Android service is woken after sleeping. */
    fun onWake() { mailbox?.trySend(Signal.Wake) }

    override fun send(message: JsonObject): Boolean = socket?.send(message.toString()) ?: false

    /** The selected origin has already passed health identity validation. Caller owns the response. */
    suspend fun request(
        path: String, method: String = "GET", body: RequestBody? = null,
        headers: Map<String, String> = emptyMap(), buffered: Boolean = true,
        expectedRoute: HubRoute? = null,
    ): Response {
        val selected = awaitRoute()
        if (expectedRoute != null && selected !== expectedRoute) throw HubFailure(HubError.NoNetwork)
        require(path.startsWith("/api/") && !path.contains('#')) { "Expected an API path" }
        val url = selected.endpoint.baseUrl().resolve(path) ?: throw HubFailure(HubError.Protocol, retryable = false)
        require(url.encodedPath.startsWith("/api/")) { "Expected an API path" }
        val owner = ownerOf(selected)
        val request = Request.Builder().url(url).apply {
            headers.forEach { (name, value) -> header(name, value) }
        }.header("Authorization", "Bearer ${owner.token}").method(method, body).build()
        val response = http.client.newCall(request).awaitResponse(buffered)
        if (!response.isSuccessful) {
            val error = response.use { HubFailure.from(it) }
            // recordFailure owns revocation: it cancels the socket, clears the route and diagnostics. In a group only
            // the hub that refused is dropped, and the connection moves on to the others.
            if (error.kind == HubError.PairingExpired && recordFailure(error, owner)) reconnect()
            throw error
        }
        return response
    }

    /** Requests made while the first route is still being chosen (right after pairing, after a network change) wait for it. */
    private suspend fun awaitRoute(): HubRoute {
        if (_route.value == null && !revoked && !closed) withTimeoutOrNull(10_000) { _route.first { it != null } }
        return activeRoute()
    }

    internal fun activeRoute(): HubRoute = synchronized(lock) {
        if (revoked) throw _failure.value ?: HubFailure(HubError.PairingExpired, retryable = false)
        if (closed) throw HubFailure(HubError.NoNetwork, retryable = false)
        _route.value ?: throw HubFailure(HubError.NoNetwork)
    }

    private suspend fun connectLoop() {
        var backoff = 1L
        while (currentCoroutineContext().isActive && !closed && !revoked) {
            if (!network.available) {
                publishState(ConnectionState.WaitingForNetwork)
                return
            }
            publishState(ConnectionState.Connecting)
            try {
                val candidates = usableEndpoints()
                if (candidates.isEmpty() && permissionMissing()) {
                    throw HubFailure(HubError.LocalNetworkPermission, retryable = false)
                }
                attemptOwner = null
                val selected = selectRoute()
                currentCoroutineContext().ensureActive()
                val owner = ownerFor(selected.endpoint)
                routeOwner = selected to owner
                attemptOwner = owner
                _route.value = selected
                updateDiagnostics { it.copy(currentPath = "${selected.endpoint.kind} · ${selected.endpoint.url}") }
                runSocket(selected) { backoff = 1L }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                currentCoroutineContext().ensureActive()
                var failure = HubFailure.from(error)
                if (failure.kind == HubError.Timeout && permissionMissing()) {
                    failure = HubFailure(HubError.LocalNetworkPermission, retryable = false, cause = failure)
                }
                clearRoute()
                // A hub of the group refusing its credential is dropped; the next round tries the others at once.
                if (recordFailure(failure)) { backoff = 1L; continue }
                if (!failure.retryable || revoked) return
                val retryMillis = failure.retryAfterSeconds?.let { it.coerceAtMost(86400) * 1000 }
                    ?: (backoff * 1000 * Random.nextDouble(0.8, 1.0)).toLong()
                delay(retryMillis)
                backoff = (backoff * 2).coerceAtMost(20)
            }
        }
    }

    private suspend fun runSocket(selected: HubRoute, ready: () -> Unit): Unit = coroutineScope {
        val messages = Channel<Signal>(Channel.UNLIMITED)
        val activity = Channel<Unit>(Channel.CONFLATED)
        val lastReceived = AtomicLong(clockMillis())
        fun received() {
            lastReceived.set(clockMillis())
            activity.trySend(Unit)
            updateDiagnostics { it.copy(lastReceivedAt = Instant.now().toString()) }
        }
        _heartbeat.value = Heartbeat(20, 15)
        val owner = ownerOf(selected)
        val ws = webSocketFactory.newWebSocket(
            authorized(selected.endpoint.baseUrl().newBuilder().encodedPath("/api/v1/ws").build(), owner).build(),
            object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: Response) {
                    received()
                    val fingerprint = response.handshake?.peerCertificates?.firstOrNull()?.publicKey?.encoded?.let {
                        Base64.getUrlEncoder().withoutPadding().encodeToString(MessageDigest.getInstance("SHA-256").digest(it))
                    // A successful TLS upgrade has already passed SpkiTrustManager.
                    } ?: http.seenPin(selected.endpoint.baseUrl()).takeIf { members.isNotEmpty() }
                        ?: owner.fingerprint?.takeIf { selected.endpoint.baseUrl().isHttps }
                    updateDiagnostics { it.copy(fingerprint = fingerprint) }
                    messages.trySend(Signal.Open)
                }
                override fun onMessage(webSocket: WebSocket, text: String) {
                    received()
                    messages.trySend(Signal.Text(text))
                }
                override fun onMessage(webSocket: WebSocket, bytes: okio.ByteString) { received() }
                override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                    webSocket.close(code, null)
                    messages.trySend(Signal.Failed(HubFailure(HubError.Transport, "WebSocket closed ($code): $reason")))
                }
                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                    val error = response?.use { HubFailure.from(it) } ?: HubFailure.from(t)
                    messages.trySend(Signal.Failed(error))
                }
            },
        )
        socket = ws
        mailbox = messages
        _weakNetwork.value = false
        var features = emptySet<String>()
        var receivedHello = false
        var synchronizedData = false
        var pingJob: Job? = null
        val pendingPing = AtomicReference<Pair<String, CompletableDeferred<Unit>>?>(null)
        var syncing: Job? = null

        fun probe(reconnectOnTimeout: Boolean = false, immediate: Boolean = true) {
            if (pingJob?.isActive == true || "app_ping" !in features) return
            pingJob = launch {
                // OkHttp does not expose control ping callbacks. An application pong makes
                // background liveness observable without relying on hidden socket state.
                if (!immediate) delay(_heartbeat.value.interval * 1000L)
                do {
                    val id = pingIds.incrementAndGet().toString()
                    val pong = CompletableDeferred<Unit>()
                    pendingPing.set(id to pong)
                    if (!ws.send(buildJsonObject { put("type", "ping"); put("id", id) }.toString())) {
                        messages.send(Signal.Failed(HubFailure(HubError.Transport)))
                        return@launch
                    }
                    val start = clockMillis()
                    val timeout = if (foreground) 5000L else _heartbeat.value.pongTimeout * 1000L
                    val timely = withTimeoutOrNull(timeout) { pong.await(); true } == true && clockMillis() - start <= timeout
                    pendingPing.set(null)
                    messages.send(Signal.ProbeResult(timely))
                    if (!timely && reconnectOnTimeout) {
                        messages.send(Signal.Failed(HubFailure(HubError.Timeout)))
                        return@launch
                    }
                    val interval = _heartbeat.value.interval * 1000L
                    delay((interval - (clockMillis() - start)).coerceAtLeast(0))
                } while (isActive)
            }
        }

        fun remainingLifetime(): Long {
            // Legacy hubs only send control frames, which OkHttp consumes internally.
            // Keep their background behavior until application probes are supported.
            if (!foreground && "app_ping" !in features) return Long.MAX_VALUE
            val limit = if (foreground) 45_000L else
                (_heartbeat.value.interval.toLong() + _heartbeat.value.pongTimeout) * 1000L
            return limit - (clockMillis() - lastReceived.get())
        }

        fun checkAlive() {
            if (remainingLifetime() <= 0) throw HubFailure(HubError.Timeout)
        }

        val inactivity = launch {
            while (isActive) {
                val remaining = remainingLifetime()
                if (remaining <= 0) {
                    messages.send(Signal.Failed(HubFailure(HubError.Timeout)))
                    break
                }
                withTimeoutOrNull(remaining) { activity.receive() }
            }
        }
        val helloDeadline = launch {
            delay(15_000)
            if (!receivedHello) messages.send(Signal.Failed(HubFailure(HubError.Timeout)))
        }
        val recheck = launch {
            while (isActive) {
                delay(recheckMillis)
                // Only a candidate listed above the one in use can take over; the list's order is the order tried. Each
                // gets the step it has alone in the race: one that answers later would lose the race to the one in use
                // again, and the connection would start over at every recheck for nothing.
                val candidates = usableEndpoints()
                val position = candidates.indexOfFirst { endpointKey(it.url) == endpointKey(selected.endpoint.url) }
                if (position < 0) {
                    messages.send(Signal.Failed(HubFailure(HubError.Transport)))
                    break
                }
                if (position == 0) continue
                try { probeRoutes(candidates.take(position), HubRouteSelector.STEP_MILLIS) }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (_: Exception) { continue }
                // Starts over as a choice in settings does (no failure recorded, no backoff): the race picks the first that answers.
                reconnect()
                break
            }
        }
        try {
            for (signal in messages) when (signal) {
                Signal.Open -> {
                    publishState(ConnectionState.Updating)
                }
                Signal.Foreground -> {
                    activity.trySend(Unit)
                    if ("client_hello" in features) ws.send(clientHello(features).toString())
                    pingJob?.cancelAndJoin()
                    pingJob = null
                    pendingPing.set(null)
                    if (foreground) { checkAlive(); probe() }
                    else probe(immediate = false)
                }
                Signal.Wake -> {
                    checkAlive()
                    pingJob?.cancelAndJoin()
                    pingJob = null
                    pendingPing.set(null)
                    if ("app_ping" in features) probe(reconnectOnTimeout = true)
                    else if (receivedHello) throw HubFailure(HubError.Transport)
                }
                Signal.Synced -> {
                    synchronizedData = true
                    ready()
                    if (!_weakNetwork.value) {
                        _failure.value = null
                        publishState(ConnectionState.Online)
                    }
                }
                is Signal.ProbeResult -> {
                    _weakNetwork.value = !signal.timely
                    if (signal.timely && synchronizedData) {
                        _failure.value = null
                        publishState(ConnectionState.Online)
                    } else if (!signal.timely) {
                        _failure.value = HubFailure(HubError.Timeout)
                        publishState(ConnectionState.Connecting)
                    }
                }
                is Signal.Failed -> throw signal.error
                is Signal.Text -> {
                    val raw = runCatching { JsonCodec.parseToJsonElement(signal.value) as? JsonObject }.getOrNull()
                    if (raw != null && phoneMessageHandler?.invoke(raw) == true) continue
                    val event = ServerEvent.decode(signal.value) ?: continue
                    when (event) {
                        is Hello -> {
                            if (owner.hubId != null && event.hubId != owner.hubId) {
                                throw HubFailure(HubError.HubMismatch, retryable = false)
                            }
                            features = event.features.toSet()
                            _serverFeatures.value = features
                            if (!features.containsAll(config.requiredFeatures)) {
                                throw HubFailure(HubError.Incompatible, retryable = false)
                            }
                            receivedHello = true
                            helloDeadline.cancel()
                            if (event.endpoints.isNotEmpty()) {
                                if (owner.home) {
                                    _endpoints.value = event.endpoints
                                    _advertised.value = event.endpoints.mapTo(HashSet()) { endpointKey(it.url) }
                                } else _memberEndpoints.update { it + (owner.hubId!! to event.endpoints) }
                            }
                            if ("client_hello" in features) ws.send(clientHello(features).toString())
                            if (foreground) probe()
                            else probe(immediate = false)
                            if (syncing == null) syncing = launch {
                                try {
                                    withTimeoutOrNull(8000) { onReady(this@OkHttpHubConnection) }
                                    messages.send(Signal.Synced)
                                } catch (cancelled: CancellationException) { throw cancelled }
                                catch (error: Exception) { messages.send(Signal.Failed(HubFailure.from(error))) }
                            }
                        }
                        is ClientHelloAck -> if ("client_hello" in features && "heartbeat" in features &&
                            event.heartbeat.interval > 0 && event.heartbeat.pongTimeout > 0) {
                            _heartbeat.value = event.heartbeat
                            activity.trySend(Unit)
                            updateDiagnostics { it.copy(
                                heartbeatSeconds = event.heartbeat.interval, pongTimeoutSeconds = event.heartbeat.pongTimeout,
                                foregroundHeartbeatSeconds = if (foreground) event.heartbeat.interval else it.foregroundHeartbeatSeconds,
                                backgroundHeartbeatSeconds = if (!foreground) event.heartbeat.interval else it.backgroundHeartbeatSeconds,
                            ) }
                            pingJob?.cancelAndJoin()
                            pingJob = null
                            pendingPing.set(null)
                            probe(immediate = foreground)
                        }
                        is EndpointsEvent -> {
                            if (!owner.home) {
                                // Another hub's list: it never replaces this hub's own candidates or settings.
                                val hubId = owner.hubId!!
                                if (_memberEndpoints.value[hubId] != event.endpoints) {
                                    _memberEndpoints.update { it + (hubId to event.endpoints) }
                                    throw HubFailure(HubError.Transport)
                                }
                                continue
                            }
                            _events.emit(event)
                            _advertised.value = event.endpoints.mapTo(HashSet()) { endpointKey(it.url) }
                            if (_endpoints.value != event.endpoints) {
                                _endpoints.value = event.endpoints
                                throw HubFailure(HubError.Transport)
                            }
                            continue
                        }
                        is Pong -> pendingPing.get()?.takeIf { it.first == event.id }?.second?.complete(Unit)
                        else -> Unit
                    }
                    _events.emit(event)
                }
            }
        } finally {
            inactivity.cancel()
            helloDeadline.cancel()
            recheck.cancel()
            syncing?.cancel()
            pingJob?.cancel()
            ws.cancel()
            messages.close()
            activity.close()
            if (socket === ws) socket = null
            if (mailbox === messages) mailbox = null
        }
    }

    private fun clientHello(features: Set<String>) = buildJsonObject {
        put("type", "client_hello")
        put("platform", "android")
        put("version", config.version)
        config.deviceId?.let { put("device_id", it) }
        config.build?.let { put("build", it) }
        config.os?.let { put("os", it) }
        putJsonArray("features") {
            (setOf("endpoints", "ws_notify", "app_ping", "elicitation") + config.clientFeatures).forEach { add(it) }
        }
        if ("heartbeat" in features) {
            putJsonObject("heartbeat") {
                put("interval", config.heartbeatPolicy.interval(foreground, network.transport))
                put("pong_timeout", config.heartbeatPolicy.pongTimeoutSeconds)
            }
        }
    }

    private fun authorized(url: HttpUrl, owner: Owner) = Request.Builder().url(url).header("Authorization", "Bearer ${owner.token}")

    private fun permissionMissing() = network.homeWifi && !network.localNetworkPermissionGranted

    /**
     * The enabled candidates in the order settings lists them (the order tried), minus what local-network permission
     * rules out; then each other hub's of the group the same way, by its own route choices.
     */
    private fun usableEndpoints(overrides: EndpointOverrides = _overrides.value) = routes(overrides).map { it.first }

    private fun routes(overrides: EndpointOverrides = _overrides.value): List<Pair<Endpoint, Owner>> {
        val home = homeOwner()
        val own = if (homeExcluded) emptyList() else usable(candidates(overrides), overrides).map { it to home }
        val group = members.filter { synchronized(lock) { it.hubId !in excluded } }.flatMap { member ->
            val owner = Owner(member.hubId, member.fingerprint, member.token ?: config.token, member.token != null, home = false)
            usable(memberCandidates(member), member.overrides).map { it to owner }
        }
        return (own + group).distinctBy { endpointKey(it.first.url) }
    }

    private fun usable(candidates: List<Endpoint>, overrides: EndpointOverrides) =
        arrangeEndpoints(candidates, overrides).filter { endpoint ->
            if (endpointKey(endpoint.url) in overrides.disabled) false
            else if (!permissionMissing()) true else {
                val host = runCatching { endpoint.baseUrl().host }.getOrNull()
                val literal = host?.let(::literalAddress)
                endpoint.kind != "lan" && host !in network.localHosts &&
                    (literal == null || literal.isLoopbackAddress || !http.localAddressPolicy.allows(literal))
            }
        }

    /** A member's manual candidates, then what it sent while connected through it, else what the group knew of it. */
    private fun memberCandidates(member: HubMember): List<Endpoint> = mergeEndpoints(member.overrides.manual,
        _memberEndpoints.value[member.hubId]?.takeIf { it.isNotEmpty() } ?: member.endpoints, http.localAddressPolicy)

    /** Who answers at [endpoint], with which credential: this hub unless the group lists it under another hub. */
    private fun ownerFor(endpoint: Endpoint): Owner =
        routes().firstOrNull { endpointKey(it.first.url) == endpointKey(endpoint.url) }?.second ?: homeOwner()

    /**
     * Tells the client which hub's pin every TLS candidate must present (the client of a group trusts them all). All
     * candidates of every hub, disabled ones too, so a settings test is held to the same pin as the connection.
     */
    private fun syncPins() {
        if (members.isEmpty()) return http.expectPins(emptyMap())
        val pins = LinkedHashMap<HttpUrl, String>()
        fun add(endpoints: List<Endpoint>, pin: String?) = endpoints.forEach { endpoint ->
            val url = runCatching { endpoint.baseUrl() }.getOrNull() ?: return@forEach
            if (pin != null && url !in pins) pins[url] = pin
        }
        add(candidates(), config.fingerprint)
        members.forEach { add(memberCandidates(it), it.fingerprint) }
        http.expectPins(pins)
    }

    private fun ownerOf(route: HubRoute?): Owner = routeOwner?.takeIf { route != null && it.first === route }?.second ?: homeOwner()

    private fun homeOwner() = Owner(config.hubId, config.fingerprint, config.token, ownToken = true, home = true)

    /** Pinned health probes; without other hubs exactly the single-hub probe. */
    private suspend fun probe(endpoint: Endpoint): Health {
        if (members.isEmpty()) return http.health(endpoint)
        val owner = ownerFor(endpoint)
        if (endpoint.baseUrl().isHttps && owner.fingerprint == null) throw HubFailure(HubError.CertificateMismatch, retryable = false)
        return http.health(endpoint, expectedHubId = owner.hubId, expectedPin = owner.fingerprint)
    }

    /** The hub behind a route: id, pin, the credential to send and whether it is that hub's own. */
    private class Owner(val hubId: String?, val fingerprint: String?, val token: String, val ownToken: Boolean, val home: Boolean)

    /** Route legacy HTTP authentication failures through the same terminal state as live requests. */
    fun revoke() {
        if (recordFailure(HubFailure(HubError.PairingExpired, retryable = false), ownerOf(_route.value).takeIf { _route.value != null })) reconnect()
    }

    /**
     * Records [error]. Returns true when it was a group hub refusing its credential and only that hub was dropped
     * (the caller starts over with the others); false otherwise, including when the whole pairing is now revoked.
     * [refusedBy] is the hub the refused request went to; by default the one the connection last ran on.
     */
    private fun recordFailure(error: HubFailure, refusedBy: Owner? = null): Boolean {
        if (error.kind != HubError.PairingExpired || members.isEmpty()) {
            recordTerminal(error)
            return false
        }
        val owner: Owner
        val remaining: Boolean
        synchronized(lock) {
            if (revoked || closed) return false
            owner = refusedBy ?: attemptOwner ?: homeOwner()
            if (owner.home) homeExcluded = true
            else members.firstOrNull { it.hubId == owner.hubId }?.let { excluded[it.hubId] = it }
            remaining = routes().isNotEmpty()
            if (remaining) {
                _failure.value = error
                updateDiagnostics { it.copy(lastDisconnectReason = error.message) }
                socket?.cancel()
                socket = null
                clearRoute()
                publishState(if (network.available) ConnectionState.Connecting else ConnectionState.WaitingForNetwork)
            }
        }
        if (!remaining) recordTerminal(error)
        notifyRevoked(owner)
        return remaining
    }

    private fun notifyRevoked(owner: Owner) = onMemberRevoked(owner.hubId.orEmpty(), owner.ownToken)

    private fun recordTerminal(error: HubFailure) = synchronized(lock) {
        if (revoked) return@synchronized
        _failure.value = error
        updateDiagnostics { it.copy(lastDisconnectReason = error.message) }
        if (error.kind == HubError.Timeout) synchronized(lock) {
            pendingState?.cancel()
            pendingState = null
            stateTarget = null
            _state.value = ConnectionState.Connecting
        }
        when (error.kind) {
            HubError.PairingExpired -> {
                revoked = true
                runner?.cancel()
                socket?.cancel()
                socket = null
                clearRoute()
                publishState(ConnectionState.Revoked)
            }
            HubError.CertificateMismatch, HubError.HubMismatch -> publishState(ConnectionState.CertificateMismatch)
            HubError.LocalNetworkPermission -> publishState(ConnectionState.NeedsLocalNetworkPermission)
            else -> publishState(if (network.available) ConnectionState.Connecting else ConnectionState.WaitingForNetwork)
        }
    }

    private fun publishState(value: ConnectionState) = synchronized(lock) {
        if (revoked && value != ConnectionState.Revoked) return@synchronized
        if (value == ConnectionState.Revoked || value == ConnectionState.Online ||
            _state.value != ConnectionState.Online && pendingState == null) {
            pendingState?.cancel()
            pendingState = null
            stateTarget = null
            _state.value = value
        } else if (stateTarget != value) {
            pendingState?.cancel()
            stateTarget = value
            pendingState = scope.launch {
                delay(1500)
                synchronized(lock) { _state.value = value; pendingState = null; stateTarget = null }
            }
        }
    }

    override fun close() {
        synchronized(lock) {
            if (closed) return
            closed = true
            runner?.cancel()
            socket?.cancel()
            socket = null
            clearRoute()
            scope.cancel()
            http.close()
        }
    }

    companion object {
        /** How often the connection looks for an answer from a candidate listed above the one in use; configured, not measured. */
        const val RECHECK_MILLIS = 300_000L
    }

    private sealed interface Signal {
        data object Open : Signal
        data object Foreground : Signal
        data object Wake : Signal
        data object Synced : Signal
        data class ProbeResult(val timely: Boolean) : Signal
        data class Text(val value: String) : Signal
        data class Failed(val error: HubFailure) : Signal
    }
}
