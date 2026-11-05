package persona.feature.terminal

import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.terminal.*
import java.util.Base64
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class TerminalCriticalPathTest {
    @Test fun resumesFramesRecoversGapsReleasesControlAndBatchesWheel() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        Dispatchers.setMain(dispatcher)
        try {
            val transport = FakeTerminal()
            val model = TerminalViewModel(SessionKey("machine", "session"), transport, dispatcher)
            val run = launch { model.run() }
            runCurrent()
            assertTrue(transport.subscriptions.isEmpty())
            model.grid(TerminalSize(40, 20)); advanceTimeBy(250); runCurrent()
            assertEquals(TerminalSize(40, 20), transport.subscriptions.last().size)
            transport.events.emit(TerminalControlEvent("machine", "session", "active")); runCurrent()
            fun frame(seq: Int, full: Boolean, text: String, epoch: String = "epoch") = TerminalEvent("machine", "session",
                epoch, seq.toLong(), full, 40, 20, Base64.getEncoder().encodeToString(text.toByteArray()))
            transport.events.emit(frame(1, true, "中A")); runCurrent()
            assertEquals(2, model.state.value.lines[0][0].width)
            transport.events.emit(frame(2, false, "B")); runCurrent()
            transport.connection.value = ConnectionState.Connecting
            advanceTimeBy(250); runCurrent()
            model.type("不得发送"); assertTrue(transport.inputs.isEmpty())
            transport.connection.value = ConnectionState.Online
            advanceTimeBy(250); runCurrent()
            assertEquals("epoch", transport.subscriptions.last().epoch)
            assertEquals(2, transport.subscriptions.last().after)
            transport.events.emit(TerminalControlEvent("machine", "session", "active")); runCurrent()
            transport.events.emit(frame(4, false, "lost")); runCurrent()
            assertNull(transport.subscriptions.last().epoch)
            assertEquals(TerminalControl.Active, model.state.value.control)
            transport.events.emit(frame(10, true, "new", "replacement")); runCurrent()
            transport.events.emit(TerminalControlEvent("machine", "session", "active")); runCurrent()
            val at = TerminalCellPosition(7, 8)
            TerminalWheel(20f, at).move(65f, model::wheel)
            assertEquals(listOf(3), transport.inputs.map { it.scroll }) // one message for the move, not one a line
            assertTrue(transport.inputs.all { it.at == at })
            model.watch(); model.press("enter"); runCurrent()
            assertEquals(1, transport.inputs.size)
            // Watching, keys go through /keys instead of the stream.
            assertEquals(listOf(FakeTerminal.Keys(listOf("enter"), "")), transport.keys)
            transport.events.emit(TerminalControlEvent("machine", "session", "busy")); runCurrent()
            assertEquals(TerminalControl.Busy, model.state.value.control)
            model.takeOver(true); assertTrue(transport.subscriptions.last().takeover)
            transport.events.emit(TerminalControlEvent("machine", "session", "taken")); runCurrent()
            model.type("只看时走 keys"); runCurrent(); assertEquals(1, transport.inputs.size)
            assertEquals(FakeTerminal.Keys(emptyList(), "只看时走 keys"), transport.keys.last())
            run.cancelAndJoin()
            assertEquals(1, transport.unsubscribes)
        } finally { Dispatchers.resetMain() }
    }

    @Test fun compositionCommitsOnceAndModifiersCoverTapLockAndHold() {
        val sent = mutableListOf<String>()
        val marked = mutableListOf<String>()
        val input = TerminalComposition(sent::add, marked::add) {}
        input.compose("zhong"); input.compose("zhongwen")
        assertTrue(sent.isEmpty())
        input.commit("中文"); input.finish()
        assertEquals(listOf("中文"), sent)
        input.compose("ni"); input.delete(1); assertEquals("n", input.marked)
        input.compose("你"); input.finish(); assertEquals(listOf("中文", "你"), sent)
        input.compose("不得发送"); input.clear(); input.finish(); assertEquals(2, sent.size)
        val out = mutableListOf<String>()
        val keyed = TerminalComposition({ out += "text:$it" }, {}, { out += "key:$it" })
        keyed.commit("ls"); keyed.commit("\n")
        assertEquals(listOf("text:ls", "key:enter"), out); out.clear()
        keyed.commit("a\tb"); assertEquals(listOf("text:a", "key:tab", "text:b"), out); out.clear()
        keyed.commit("x\r\ny"); assertEquals(listOf("text:x", "key:enter", "text:y"), out)
        val modifiers = TerminalModifiers()
        val ctrl = TerminalModifiers.Key.CTRL
        modifiers.tap(ctrl); assertEquals("ctrl+c", modifiers.apply("c"))
        modifiers.pressed(); assertEquals(ModifierState.OFF, modifiers[ctrl])
        modifiers.tap(ctrl); modifiers.tap(ctrl, quick = true); modifiers.pressed()
        assertEquals(ModifierState.LOCKED, modifiers[ctrl])
        modifiers.tap(ctrl); modifiers.hold(ctrl); assertEquals("\u0003", TerminalKeyBytes.encode(modifiers.apply("c")))
        modifiers.pressed(); modifiers.release(ctrl); assertEquals(ModifierState.OFF, modifiers[ctrl])
        assertEquals("\u001b[200~一\r二\u001b[201~", TerminalKeyBytes.text("一\n二"))
        val decoded = ServerEvent.decode("""{"type":"terminal_control","machine_id":"machine","session_id":"session","state":"active"}""")
        assertTrue(decoded is TerminalControlEvent)
    }
}

internal class FakeTerminal : TerminalTransport {
    override val connection = MutableStateFlow(ConnectionState.Online)
    override val features = MutableStateFlow(setOf("terminal_stream", "terminal_control", "terminal_scroll", "terminal_click"))
    override val events = MutableSharedFlow<ServerEvent>()
    data class Subscription(val epoch: String?, val after: Int, val size: TerminalSize?, val takeover: Boolean)
    data class Input(val text: String, val scroll: Int, val at: TerminalCellPosition?)
    data class Keys(val keys: List<String>, val text: String)
    val subscriptions = mutableListOf<Subscription>()
    val inputs = mutableListOf<Input>()
    val keys = mutableListOf<Keys>()
    var unsubscribes = 0
    override fun subscribe(key: SessionKey, epoch: String?, after: Int, size: TerminalSize?, takeover: Boolean): Boolean {
        subscriptions += Subscription(epoch, after, size, takeover); return true
    }
    override fun unsubscribe(key: SessionKey) { unsubscribes++ }
    override fun input(key: SessionKey, text: String, size: TerminalSize?, scroll: Int, at: TerminalCellPosition?, click: TerminalCellPosition?): Boolean {
        inputs += Input(text, scroll, at); return true
    }
    override suspend fun screen(key: SessionKey): TerminalScreen { screens++; return TerminalScreen("只读屏幕") }
    /** Holds each /keys request until completed; [failKeys] makes them fail. */
    var gate: kotlinx.coroutines.CompletableDeferred<Unit>? = null
    var failKeys = false
    var screens = 0
    override suspend fun keys(key: SessionKey, keys: List<String>, text: String) {
        this.keys += Keys(keys, text)
        gate?.await()
        if (failKeys) throw java.io.IOException("hub unreachable")
    }
}
