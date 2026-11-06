package persona.feature.terminal

import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.terminal.TerminalGrid
import java.util.Base64
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test

/** What the port from the old Android app added: keys while watching, the kept choice and font, the kept rows. */
@OptIn(ExperimentalCoroutinesApi::class)
class TerminalPortTest {
    private fun frame(seq: Int, full: Boolean, text: String, cols: Int = 20, rows: Int = 6, epoch: String = "epoch") =
        TerminalEvent("machine", "session", epoch, seq.toLong(), full, cols, rows, Base64.getEncoder().encodeToString(text.toByteArray()))

    private fun screen(from: Int) = buildString { append("\u001B[2J"); for (r in 0 until 6) append("\u001B[${r + 1};1Hline ${from + r}") }

    private fun test(body: suspend TestScope.(TestDispatcher) -> Unit) = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        Dispatchers.setMain(dispatcher)
        try { body(dispatcher) } finally { Dispatchers.resetMain() }
    }

    @Test fun keysWhileWatchingGoThroughKeysOneRequestAtATime() = test { dispatcher ->
        val transport = FakeTerminal().apply { features.value = emptySet() } // an old hub: the screen is read once a second
        val model = TerminalViewModel(SessionKey("machine", "session"), transport, dispatcher)
        val run = launch { model.run() }
        runCurrent()
        assertEquals(1, transport.screens)
        transport.gate = CompletableDeferred()
        model.type("a"); runCurrent()
        // Typed while that request is on its way: one request, text first, then the keys.
        model.type("bc"); model.press("enter"); model.type("d"); model.type("e"); model.press("backspace")
        transport.gate!!.complete(Unit); runCurrent()
        assertEquals(listOf(FakeTerminal.Keys(emptyList(), "a"), FakeTerminal.Keys(listOf("enter"), "bc"), FakeTerminal.Keys(emptyList(), "d")),
            transport.keys)
        assertTrue(transport.inputs.isEmpty())
        // The screen is read again right away, so the echo shows (not a second later).
        advanceTimeBy(260); runCurrent()
        assertEquals(2, transport.screens)
        // Modifiers apply to keys sent this way too.
        model.modifier(persona.core.terminal.TerminalModifiers.Key.CTRL, "tap"); model.type("c"); runCurrent()
        assertEquals(FakeTerminal.Keys(listOf("ctrl+c"), ""), transport.keys.last())
        run.cancelAndJoin()
    }

    @Test fun theControlCKeyIgnoresTheModifiersThatAreOn() = test { dispatcher ->
        val transport = FakeTerminal().apply { features.value = emptySet() }
        val model = TerminalViewModel(SessionKey("machine", "session"), transport, dispatcher)
        val run = launch { model.run() }
        runCurrent()
        // Alt is on for the next key: ⌃C still sends ctrl+c alone, and the next key gets the Alt.
        model.modifier(persona.core.terminal.TerminalModifiers.Key.ALT, "tap")
        model.press("ctrl+c"); runCurrent()
        assertEquals(FakeTerminal.Keys(listOf("ctrl+c"), ""), transport.keys.last())
        model.press("enter"); advanceTimeBy(260); runCurrent()
        assertEquals(FakeTerminal.Keys(listOf("alt+enter"), ""), transport.keys.last())
        run.cancelAndJoin()
    }

    @Test fun aFailedKeyRequestDropsWhatWasWaitingAndSaysSo() = test { dispatcher ->
        val transport = FakeTerminal().apply { features.value = emptySet() }
        val model = TerminalViewModel(SessionKey("machine", "session"), transport, dispatcher)
        val notices = mutableListOf<TerminalNotice>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { model.notices.collect { notices += it } }
        val run = launch { model.run() }
        runCurrent()
        transport.gate = CompletableDeferred(); transport.failKeys = true
        model.type("x"); runCurrent()
        model.press("enter"); model.type("y")
        transport.gate!!.complete(Unit); runCurrent()
        assertEquals(1, transport.keys.size) // an Enter after lost text would land out of context
        assertEquals(listOf(TerminalNotice.KeysNotSent), notices)
        // Offline nothing is typed at all.
        transport.connection.value = ConnectionState.Connecting; advanceTimeBy(260); runCurrent()
        model.type("z"); runCurrent()
        assertEquals(1, transport.keys.size)
        run.cancelAndJoin()
    }

    @Test fun theChoiceToOnlyWatchIsKeptButNotTheOneOfTheBusyPrompt() = test { dispatcher ->
        val transport = FakeTerminal()
        val preferences = MemoryTerminalPreferences(drive = false)
        val model = TerminalViewModel(SessionKey("machine", "session"), transport, dispatcher, preferences)
        assertFalse(model.state.value.drive)
        val run = launch { model.run() }
        model.grid(TerminalSize(40, 20)); advanceTimeBy(260); runCurrent()
        assertNull(transport.subscriptions.last().size) // watching, as chosen last time
        model.takeOver(); runCurrent()
        assertEquals(TerminalSize(40, 20), transport.subscriptions.last().size)
        assertTrue(preferences.drive)
        model.watch()
        assertFalse(preferences.drive)
        model.takeOver(); runCurrent()
        transport.events.emit(TerminalControlEvent("machine", "session", "busy")); runCurrent()
        model.watch(remember = false) // "Take over the terminal?" -> "Read-only": this time only
        assertTrue(preferences.drive)
        assertTrue(model.state.value.drive)
        run.cancelAndJoin()
    }

    @Test fun drivingKeepsWhatScrollsOffAndWatchingDropsIt() = test { dispatcher ->
        val transport = FakeTerminal()
        val model = TerminalViewModel(SessionKey("machine", "session"), transport, dispatcher)
        val run = launch { model.run() }
        model.grid(TerminalSize(20, 6)); advanceTimeBy(260); runCurrent()
        transport.events.emit(TerminalControlEvent("machine", "session", "active")); runCurrent()
        transport.events.emit(frame(1, true, screen(1))); runCurrent()
        transport.events.emit(frame(2, false, screen(3))); runCurrent() // herdr redraws every row two lines up
        val driven = model.state.value
        assertEquals((1..8).map { "line $it" }, driven.lines.map(TerminalGrid::plain))
        assertEquals(2, driven.screenStart)
        assertEquals((1..8).joinToString("\n") { "line $it" }, model.screenText())
        model.watch(); runCurrent()
        transport.events.emit(frame(1, true, "\u001B[2J\u001B[1;1H$ ls\u001B[2;1Ha\u001B[?25l", cols = 30, rows = 10, epoch = "mirror")); runCurrent()
        val mirrored = model.state.value
        assertEquals(0, mirrored.screenStart)
        // Mirrored, the pane's empty rows under its last text are not drawn: the view can follow the last line.
        assertEquals(listOf("$ ls", "a"), mirrored.lines.map(TerminalGrid::plain))
        assertEquals("$ ls\na", model.screenText())
        run.cancelAndJoin()
    }

    @Test fun fontSizeStepsInTheIosRangeAndIsKept() {
        assertEquals(15.5f, TerminalFontSize.next(15f, true))
        assertEquals(14.5f, TerminalFontSize.next(15f, false))
        assertEquals(20f, TerminalFontSize.next(20f, true))
        assertEquals(7f, TerminalFontSize.next(7f, false))
        assertEquals(13.5f, TerminalFontSize.valid(13.3f))
        assertEquals(20f, TerminalFontSize.valid(99f))
        assertEquals(15f, TerminalFontSize.valid(Float.NaN))
        assertEquals("15", TerminalFontSize.label(15f))
        assertEquals("12.5", TerminalFontSize.label(12.5f))
        val preferences = MemoryTerminalPreferences()
        val model = TerminalViewModel(SessionKey("machine", "session"), FakeTerminal(), preferences = preferences)
        assertEquals(15f, model.state.value.fontSize)
        model.font(true)
        assertEquals(15.5f, model.state.value.fontSize)
        assertEquals(15.5f, preferences.fontSize)
        val smallest = TerminalViewModel(SessionKey("machine", "session"), FakeTerminal(), preferences = MemoryTerminalPreferences(fontSize = 7f))
        smallest.font(false)
        assertEquals(7f, smallest.state.value.fontSize)
    }
}
