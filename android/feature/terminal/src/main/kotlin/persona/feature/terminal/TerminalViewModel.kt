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
)

class TerminalViewModel(val key: SessionKey, private val transport: TerminalTransport,
                        private val parserDispatcher: CoroutineDispatcher = Dispatchers.Default) : ViewModel() {
    private val _state = MutableStateFlow(TerminalUiState())
    val state = _state.asStateFlow()
    private val frames = TerminalFrames()
    private val modifiers = TerminalModifiers()
    private var phone: TerminalSize? = null
    private var wanted = true
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
                _state.update { it.copy(online = online) }
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
                        val lines = withContext(parserDispatcher) { TerminalGrid.rows(screen.text) }
                        if (version == revision) _state.update { it.copy(lines = lines, cols = lines.maxOfOrNull { row -> row.size } ?: 0,
                            rows = lines.size, cursor = null, loaded = true,
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

    fun watch() {
        wanted = false; revision++; resize?.cancel(); frames.forget(); appliedPhone = null
        _state.update { it.copy(control = TerminalControl.Watching, marked = "", note = null) }
        subscribed = false; retryAt = 0
        if (running && _state.value.online) subscribe()
    }
    fun takeOver(force: Boolean = false) {
        if (!running || !_state.value.online) return
        wanted = true; revision++; frames.forget(); retryAt = 0
        subscribe(force)
    }
    fun retry() { subscribed = false; retryAt = 0; pollAt = 0; failures = 0 }
    fun marked(text: String) { _state.update { it.copy(marked = text) } }
    fun modifier(key: TerminalModifiers.Key, action: String, quick: Boolean = false) {
        when (action) { "tap" -> modifiers.tap(key, quick); "hold" -> modifiers.hold(key); "release" -> modifiers.release(key) }
        publishModifiers()
    }
    fun type(text: String) {
        if (!canType()) return
        // Keep a multiline paste atomic so bracketed-paste boundaries cannot be interleaved.
        if (!modifiers.active) { send(TerminalKeyBytes.text(text)); return }
        if (text.any { it == '\n' || it == '\r' }) send(TerminalKeyBytes.text(text))
        else text.codePoints().toArray().forEach { cp ->
            when (val event = modifiers.typed(String(Character.toChars(cp)))) {
                is TerminalInputEvent.Text -> send(event.text)
                is TerminalInputEvent.Key -> TerminalKeyBytes.encode(event.key)?.let(::send)
            }
            modifiers.pressed()
        }
        modifiers.pressed(); publishModifiers()
    }
    fun press(key: String) {
        if (!canType()) return
        TerminalKeyBytes.encode(modifiers.apply(key))?.let(::send)
        modifiers.pressed(); publishModifiers()
    }
    fun hardware(key: String) { if (canType()) TerminalKeyBytes.encode(key)?.let(::send) }
    fun wheel(lines: Int, at: TerminalCellPosition) {
        if (lines != 0 && canType() && "terminal_scroll" in transport.features.value &&
            !transport.input(key, scroll = lines, at = at)) disconnectedInput()
    }
    fun click(at: TerminalCellPosition) {
        if (canType() && "terminal_click" in transport.features.value && !transport.input(key, click = at)) disconnectedInput()
    }
    private fun canType() = running && _state.value.online && _state.value.control == TerminalControl.Active &&
        transport.connection.value == ConnectionState.Online
    private fun send(text: String) { if (!transport.input(key, text)) disconnectedInput() }
    private fun disconnectedInput() {
        _state.update { it.copy(control = TerminalControl.Watching, note = "连接已断开，刚才的输入没有发出") }; subscribed = false
    }
    private fun publishModifiers() { _state.update { it.copy(modifiers = TerminalModifiers.Key.entries.associateWith { key -> modifiers[key] }) } }

    private fun subscribe(force: Boolean = false) {
        if ("terminal_stream" !in transport.features.value || unsupported) return
        val drive = wanted && "terminal_control" in transport.features.value
        val size = if (drive) phone ?: return else null
        if (transport.subscribe(key, frames.epoch, frames.seq, size, force)) {
            subscribed = true; appliedPhone = size
            _state.update {
                val control = if (!drive) TerminalControl.Watching
                    else if (it.control == TerminalControl.Active) TerminalControl.Active else TerminalControl.Starting
                it.copy(streaming = true, control = control,
                    note = when (control) { TerminalControl.Active -> null; TerminalControl.Starting -> "正在接管…"; else -> "只看" })
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
        val result = withContext(parserDispatcher) { frames.apply(frame) }
        if (version != revision) { frames.forget(); return }
        when (result) {
            TerminalFrames.Result.Gap -> { subscribed = false; subscribe() }
            TerminalFrames.Result.Duplicate -> Unit
            TerminalFrames.Result.Applied -> {
                failures = 0
                _state.update { it.copy(lines = frames.lines, cols = frames.cols, rows = frames.rows,
                    cursor = frames.cursor, loaded = true,
                    note = if (it.control == TerminalControl.Active) null else it.note) }
            }
        }
    }
    private fun control(event: TerminalControlEvent) {
        if (!subscribed) return
        when (event.state) {
            "active" -> if (wanted) _state.update { it.copy(control = TerminalControl.Active, note = null) }
            "busy" -> { wanted = false; _state.update { it.copy(control = TerminalControl.Busy, note = "电脑上另一个程序正在使用这个终端") } }
            else -> {
                wanted = false; resize?.cancel()
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
