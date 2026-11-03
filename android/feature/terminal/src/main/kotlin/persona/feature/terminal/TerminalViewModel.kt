package persona.feature.terminal

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.terminal.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*

enum class TerminalControl { Watching, Starting, Active, Busy }
data class TerminalUiState(
    val lines: List<TermRow> = emptyList(), val cols: Int = 0, val rows: Int = 0,
    val cursor: TerminalCursor? = null, val control: TerminalControl = TerminalControl.Watching,
    val loaded: Boolean = false, val online: Boolean = false, val note: String? = null,
    val marked: String = "", val modifiers: Map<TerminalModifiers.Key, ModifierState> = emptyMap(),
    val streaming: Boolean = false,
    /** Where the live screen starts in [lines]: the rows above it scrolled off while the phone drove the terminal. */
    val screenStart: Int = 0,
    /** Driving it, a drag is herdr's mouse wheel (feature terminal_scroll); without it a drag scrolls what the phone kept. */
    val wheel: Boolean = false,
    /** The terminal's font size in dp ([TerminalFontSize]). */
    val fontSize: Float = TerminalFontSize.DEFAULT,
    /** What the user chose last: 接管 or 只读 (kept on the phone, [TerminalPreferences.drive]). */
    val drive: Boolean = true,
) {
    /** The phone drives the pane at its own size (or is about to): keys go straight into the stream. */
    val driving: Boolean get() = control == TerminalControl.Active && online
}

class TerminalViewModel(val key: SessionKey, private val transport: TerminalTransport,
                        private val parserDispatcher: CoroutineDispatcher = Dispatchers.Default,
                        private val preferences: TerminalPreferences = MemoryTerminalPreferences()) : ViewModel() {
    private val _state = MutableStateFlow(TerminalUiState(fontSize = preferences.fontSize, drive = preferences.drive))
    val state = _state.asStateFlow()
    private val _notices = MutableSharedFlow<String>(extraBufferCapacity = 4)
    /** One-off messages for the shell's feedback (a key that did not go out); the page's notice is the stream's state. */
    val notices = _notices.asSharedFlow()
    private val frames = TerminalFrames()
    private val modifiers = TerminalModifiers()
    private val legacyKeys = KeyBatcher()
    private var legacySending: Job? = null
    private var phone: TerminalSize? = null
    private var wanted = preferences.drive
    private var subscribed = false
    private var running = false
    private var unsupported = false
    private var resize: Job? = null
    private var retryAt = 0L
    private var failures = 0
    private var pollAt = 0L
    private var revision = 0
    private var appliedPhone: TerminalSize? = null
    private val now get() = System.nanoTime() / 1_000_000

    /** The destination runs this only while STARTED; cancellation releases control before returning. */
    suspend fun run(): Unit = coroutineScope {
        check(!running)
        running = true
        val receiver = launch(start = CoroutineStart.UNDISPATCHED) {
            transport.events.collect { event ->
                when (event) {
                    is TerminalEvent -> if (event.machineId == key.machineId && event.sessionId == key.sessionId) receive(event)
                    is TerminalControlEvent -> if (event.machineId == key.machineId && event.sessionId == key.sessionId) control(event)
                    is Hello -> { subscribed = false; unsupported = false; retryAt = 0 }
                    else -> Unit
                }
            }
        }
        try {
            while (isActive) {
                val online = transport.connection.value == ConnectionState.Online
                _state.update { it.copy(online = online, wheel = "terminal_scroll" in transport.features.value) }
                if (!online) {
                    subscribed = false
                    _state.update { it.copy(control = TerminalControl.Watching, note = "连不上 hub，显示的是上次画面") }
                } else if ("terminal_stream" in transport.features.value && !unsupported) {
                    if (!subscribed && now >= retryAt) subscribe()
                } else if (now >= pollAt) {
                    val version = revision
                    _state.update { it.copy(control = TerminalControl.Watching, streaming = false) }
                    try {
                        val screen = transport.screen(key)
                        val lines = withContext(parserDispatcher) {
                            TerminalGrid.rows(screen.text).let { rows -> rows.take(TerminalText.shownRows(rows)) }
                        }
                        if (version == revision) _state.update { it.copy(lines = lines, cols = lines.maxOfOrNull { row -> row.size } ?: 0,
                            rows = lines.size, cursor = null, screenStart = 0, loaded = true,
                            note = if (screen.status == "offline") "没有终端：恢复会话后就能看到" else "只看 · 当前电脑不支持实时接管") }
                        pollAt = now + 1000
                    } catch (e: CancellationException) { throw e }
                    catch (_: Exception) { failed("读取终端失败，正在重试"); pollAt = retryAt }
                }
                delay(250)
            }
        } finally {
            receiver.cancel()
            resize?.cancel(); revision++
            transport.unsubscribe(key)
            running = false; subscribed = false; appliedPhone = null
            // The pane goes back to the computer's size: what scrolled off at the phone's size goes with it.
            frames.forgetScrollback()
            TerminalModifiers.Key.entries.forEach { modifiers[it] = ModifierState.OFF }
            _state.update { it.copy(control = TerminalControl.Watching, marked = "", modifiers = emptyMap()) }
        }
    }

    fun grid(size: TerminalSize) {
        if (phone == size) return
        phone = size
        resize?.cancel()
        if (appliedPhone == null) return
        resize = viewModelScope.launch {
            delay(250)
            if (running && subscribed && wanted && _state.value.control in listOf(TerminalControl.Active, TerminalControl.Starting)) {
                if (transport.input(key, size = size)) appliedPhone = size else disconnectedInput()
            }
        }
    }

    /**
     * Only watch: mirror the pane at the computer's size. [remember] keeps the choice for the next terminal (the 只读
     * of the top bar); the 只读 of the 「接管终端？」 prompt is for this time only.
     */
    fun watch(remember: Boolean = true) {
        if (remember) remember(false)
        wanted = false; revision++; resize?.cancel(); frames.forget(); frames.forgetScrollback(); appliedPhone = null
        _state.update { it.copy(control = TerminalControl.Watching, marked = "", note = null) }
        subscribed = false; retryAt = 0
        if (running && _state.value.online) subscribe()
    }
    fun takeOver(force: Boolean = false) {
        if (!running || !_state.value.online) return
        remember(true)
        wanted = true; revision++; frames.forget(); retryAt = 0
        subscribe(force)
    }
    fun retry() { subscribed = false; retryAt = 0; pollAt = 0; failures = 0; _state.update { it.copy(note = null) } }
    fun marked(text: String) { _state.update { it.copy(marked = text) } }
    /** A size step up or down, kept on the phone; the view lays out the new grid and a driven pane follows it. */
    fun font(up: Boolean) {
        val size = TerminalFontSize.next(_state.value.fontSize, up)
        if (size == _state.value.fontSize) return
        preferences.fontSize = size
        _state.update { it.copy(fontSize = size) }
    }
    fun modifier(key: TerminalModifiers.Key, action: String, quick: Boolean = false) {
        when (action) { "tap" -> modifiers.tap(key, quick); "hold" -> modifiers.hold(key); "release" -> modifiers.release(key) }
        publishModifiers()
    }
    fun type(text: String) {
        if (!canType()) return
        // Keep a multiline paste atomic so bracketed-paste boundaries cannot be interleaved.
        if (!modifiers.active || text.any { it == '\n' || it == '\r' }) emit(TerminalInputEvent.Text(text))
        else text.codePoints().toArray().forEach { cp ->
            emit(modifiers.typed(String(Character.toChars(cp))))
            modifiers.pressed()
        }
        modifiers.pressed(); publishModifiers()
    }
    fun press(key: String) {
        if (!canType()) return
        // ⌃C is a key of its own (iOS, the old app): the modifiers that are on neither add to it nor get used up by it.
        if (key == "ctrl+c") { emit(TerminalInputEvent.Key(key)); return }
        emit(TerminalInputEvent.Key(modifiers.apply(key)))
        modifiers.pressed(); publishModifiers()
    }
    fun hardware(key: String) { if (canType()) emit(TerminalInputEvent.Key(key)) }
    fun wheel(lines: Int, at: TerminalCellPosition) {
        if (lines != 0 && canDrive() && "terminal_scroll" in transport.features.value &&
            !transport.input(key, scroll = lines, at = at)) disconnectedInput()
    }
    fun click(at: TerminalCellPosition) {
        if (canDrive() && "terminal_click" in transport.features.value && !transport.input(key, click = at)) disconnectedInput()
    }

    /** What the 选择文本 page shows: the rows kept while driving and the screen, as plain text. */
    fun screenText(): String = TerminalText.plain(_state.value.lines)

    /** Typing works whenever the hub is reachable: into the stream while driving, through `/keys` otherwise. */
    private fun canType() = running && _state.value.online && transport.connection.value == ConnectionState.Online
    private fun canDrive() = canType() && _state.value.control == TerminalControl.Active

    private fun emit(event: TerminalInputEvent) {
        if (canDrive()) when (event) {
            is TerminalInputEvent.Text -> send(TerminalKeyBytes.text(event.text))
            is TerminalInputEvent.Key -> TerminalKeyBytes.encode(event.key)?.let(::send)
        } else legacy(event)
    }
    private fun send(text: String) { if (!transport.input(key, text)) disconnectedInput() }

    /**
     * Watching the pane or reading its screen, keys go through `POST …/keys` (every hub and node has it): one request
     * on the way at a time, what is typed meanwhile rides in the next ([KeyBatcher]: text first, then keys; a
     * backspace takes back text not sent yet). The screen is read again right after, so the echo shows.
     */
    private fun legacy(event: TerminalInputEvent) {
        legacyKeys.push(event)
        if (legacySending?.isActive == true) return
        legacySending = viewModelScope.launch {
            while (true) {
                val request = legacyKeys.take() ?: break
                try {
                    transport.keys(key, request.keys, request.text)
                    pollAt = 0
                } catch (e: CancellationException) { throw e }
                catch (_: Exception) {
                    // Later keys would land out of context (an Enter after lost text): drop them with it.
                    while (legacyKeys.take() != null) Unit
                    _notices.tryEmit("按键没有发出去，请再试一次")
                }
            }
        }
    }
    private fun disconnectedInput() {
        _state.update { it.copy(control = TerminalControl.Watching, note = "连接已断开，刚才的输入没有发出") }; subscribed = false
    }
    private fun publishModifiers() { _state.update { it.copy(modifiers = TerminalModifiers.Key.entries.associateWith { key -> modifiers[key] }) } }
    private fun remember(drive: Boolean) {
        if (preferences.drive != drive) preferences.drive = drive
        _state.update { it.copy(drive = drive) }
    }

    private fun subscribe(force: Boolean = false) {
        if ("terminal_stream" !in transport.features.value || unsupported) return
        val drive = wanted && "terminal_control" in transport.features.value
        val size = if (drive) phone ?: return else null
        if (transport.subscribe(key, frames.epoch, frames.seq, size, force)) {
            subscribed = true; appliedPhone = size
            _state.update {
                val control = if (!drive) TerminalControl.Watching
                    else if (it.control == TerminalControl.Active) TerminalControl.Active else TerminalControl.Starting
                // Taking over and watching by choice say so in the title, not in a notice above the terminal: a notice
                // coming and going changes the terminal's height, and a driven pane lays itself out again each time.
                it.copy(streaming = true, control = control, note = null)
            }
        }
    }
    private suspend fun receive(frame: TerminalEvent) {
        if (!subscribed) return
        if (frame.error != null) {
            subscribed = false; unsupported = frame.unsupported; frames.forget()
            _state.update { it.copy(control = TerminalControl.Watching) }
            failed(if (frame.error?.contains("not running") == true) "没有终端：恢复会话后就能看到" else "终端流已停止，正在重试")
            return
        }
        val version = revision
        val keep = _state.value.control in listOf(TerminalControl.Active, TerminalControl.Starting)
        val result = withContext(parserDispatcher) { frames.apply(frame, keep) }
        if (version != revision) { frames.forget(); return }
        when (result) {
            TerminalFrames.Result.Gap -> { subscribed = false; subscribe() }
            TerminalFrames.Result.Duplicate -> Unit
            TerminalFrames.Result.Applied -> {
                failures = 0
                val view = frames.view()
                // Mirrored, the pane's empty rows under its last text are not drawn: the view follows the last line.
                val shown = if (keep) view.lines else view.lines.take(TerminalText.shownRows(view.lines, view.cursor?.row))
                _state.update { it.copy(lines = shown, cols = view.cols, rows = view.rows, screenStart = view.screenStart,
                    cursor = view.cursor, loaded = true,
                    note = if (it.control == TerminalControl.Active) null else it.note) }
            }
        }
    }
    private fun control(event: TerminalControlEvent) {
        if (!subscribed) return
        when (event.state) {
            "active" -> if (wanted) _state.update { it.copy(control = TerminalControl.Active, note = null) }
            "busy" -> { wanted = false; frames.forgetScrollback(); _state.update { it.copy(control = TerminalControl.Busy, note = "电脑上另一个程序正在使用这个终端") } }
            else -> {
                wanted = false; resize?.cancel(); frames.forgetScrollback()
                _state.update { it.copy(control = TerminalControl.Watching, marked = "",
                    note = if (event.state == "unsupported") "这台电脑只能看，不能接管" else "终端已被接管，现在只能看") }
            }
        }
    }
    private fun failed(note: String) {
        failures++
        retryAt = now + minOf(15_000L, 1000L shl minOf(failures - 1, 4))
        _state.update { it.copy(note = note, loaded = true) }
    }
}
