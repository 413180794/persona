package persona.feature.voice

import java.io.IOException
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** One conversation as the socket and the audio drive it, without either. */
class VoiceSessionTest {
    private val plugins = FakePlugins()
    private val audio = FakeAudio()
    private val session = VoiceSession(plugins, "voice", voice = "soft", model = "fast", audio = audio)
    private val state get() = session.state.value

    private fun live() {
        session.start()
        plugins.plugin.onOpen()
        plugins.plugin.onText(Ready)
    }

    @Test fun itTalksOnlyOnceThePluginIsReady() {
        session.start()
        assertTrue("Nothing before the socket opens", plugins.socket.texts.isEmpty())
        plugins.plugin.onOpen()
        assertEquals(listOf("""{"type":"start","model":"fast","voice":"soft"}"""), plugins.socket.texts)
        plugins.plugin.onBinary(byteArrayOf(1, 2))
        assertTrue("No sound before ready", audio.played.isEmpty())
        assertNull(audio.rates)
        assertEquals(VoiceState.Phase.Connecting, state.phase)
        plugins.plugin.onText("""{"type":"ready","input_rate":16000,"output_rate":24000}""")
        assertEquals(16000 to 24000, audio.rates)
        assertEquals(VoiceState.Phase.Live, state.phase)
        plugins.plugin.onBinary(byteArrayOf(3, 4))
        assertArrayEquals(byteArrayOf(3, 4), audio.played.single())
        plugins.plugin.onText(Ready)
        assertEquals("A second ready does not open the microphone again", 16000 to 24000, audio.rates)
    }

    @Test fun theStartFrameGoesOnceEvenWhenTheSocketOpensBeforeStartHasIt() {
        plugins.openAtOnce = true
        session.start()
        plugins.plugin.onOpen()
        assertEquals(1, plugins.socket.texts.size)
    }

    @Test fun theMicrophoneGoesUpAllTheTimeAndMutedAsSilence() {
        live()
        val microphone = audio.listener!!
        microphone.onFrame(byteArrayOf(5, 6, 7, 8, 9), 4, 0.8f)
        assertArrayEquals(byteArrayOf(5, 6, 7, 8), plugins.socket.frames.last())
        assertTrue(session.level.value > 0f)
        session.setMuted(true)
        assertTrue(state.muted)
        assertEquals("已静音", statusText(state))
        val frame = byteArrayOf(5, 6, 7, 8)
        repeat(12) { microphone.onFrame(frame, 4, 0.8f) }
        assertArrayEquals("Muted frames still go, as silence", byteArrayOf(0, 0, 0, 0), plugins.socket.frames.last())
        assertEquals(13, plugins.socket.frames.size)
        assertEquals("The orb settles while muted", 0f, session.level.value, 0f)
        session.setMuted(false)
        microphone.onFrame(byteArrayOf(1, 1), 2, 0.5f)
        assertArrayEquals(byteArrayOf(1, 1), plugins.socket.frames.last())
    }

    @Test fun captionsAndToolsComeAsTheySay() {
        live()
        plugins.plugin.onText("""{"type":"transcript","id":"u1","role":"user","text":"今天下午","final":false}""")
        plugins.plugin.onText("""{"type":"transcript","id":"u1","role":"user","text":"今天下午有什么会？","final":true}""")
        plugins.plugin.onText("""{"type":"transcript","id":"a1","role":"assistant","text":"我看一下","final":false}""")
        assertEquals(listOf(Line("u1", true, "今天下午有什么会？", true), Line("a1", false, "我看一下", false)), state.lines)
        plugins.plugin.onText("""{"type":"tool","id":"t1","label":"查日程","done":false}""")
        audio.listener!!.onSpeaking(true)
        assertEquals("查日程…", statusText(state))
        plugins.plugin.onText("""{"type":"tool","id":"t1","done":true}""")
        assertEquals("正在说，直接开口就能打断", statusText(state))
        audio.listener!!.onSpeaking(false)
        assertEquals("在听", statusText(state))
        plugins.plugin.onText("""{"type":"something_new"}""")
        assertEquals(VoiceState.Phase.Live, state.phase)
    }

    @Test fun clearAndInterruptDropWhatIsQueued() {
        live()
        plugins.plugin.onText("""{"type":"clear"}""")
        assertEquals(1, audio.clears)
        session.interrupt()
        assertEquals(2, audio.clears)
        assertEquals("""{"type":"interrupt"}""", plugins.socket.texts.last())
    }

    @Test fun anErrorIsShownAndAFatalOneEndsTheConversation() {
        live()
        plugins.plugin.onText("""{"type":"error","message":"查不到日程","fatal":false}""")
        assertEquals("查不到日程", state.error)
        assertEquals(VoiceState.Phase.Live, state.phase)
        plugins.plugin.onText("""{"type":"error","message":"模型服务不可用","fatal":true}""")
        assertEquals(VoiceState.Phase.Ended, state.phase)
        assertEquals("模型服务不可用", state.error)
        assertEquals(1000 to "bye", plugins.socket.closed)
        assertEquals(1, audio.stops)
        plugins.plugin.onClosed(1001, "")
        assertEquals("What ended it stays the reason", "模型服务不可用", state.error)
    }

    @Test fun aPluginRestartOrADropInTheMiddleSaysSo() {
        live()
        audio.listener!!.onSpeaking(true)
        plugins.plugin.onClosed(1001, "going away")
        assertEquals(VoiceText.Restarted, state.error)
        assertEquals(VoiceState.Phase.Ended, state.phase)
        assertFalse(state.speaking)

        val again = VoiceSession(plugins, "voice", null, null, FakeAudio())
        again.start()
        plugins.plugin.onOpen()
        plugins.plugin.onText(Ready)
        plugins.plugin.onFailure(IOException("Broken pipe"), null)
        assertEquals(VoiceText.Restarted, again.state.value.error)
    }

    @Test fun failingBeforeTheConversationSaysWhy() {
        session.start()
        plugins.plugin.onFailure(IOException("Expected HTTP 101 response but was '404 Not Found'"), 404)
        assertEquals("hub 上没有语音插件", state.error)
        assertEquals(VoiceState.Phase.Ended, state.phase)

        val unreachable = VoiceSession(plugins, "voice", null, null, FakeAudio())
        unreachable.start()
        plugins.plugin.onFailure(IOException("Failed to connect"), null)
        assertEquals("连不上语音服务：连不上 hub", unreachable.state.value.error)

        plugins.connectError = IOException("no route")
        val noRoute = VoiceSession(plugins, "voice", null, null, FakeAudio())
        noRoute.start()
        assertEquals("连不上语音服务：连不上 hub", noRoute.state.value.error)
        assertEquals(VoiceState.Phase.Ended, noRoute.state.value.phase)
    }

    @Test fun aMicrophoneThatWillNotOpenEndsIt() {
        audio.opens = false
        live()
        assertEquals("麦克风打不开", state.error)
        assertEquals(VoiceState.Phase.Ended, state.phase)
        assertEquals(1000 to "bye", plugins.socket.closed)
        assertEquals("The route and focus go back", 1, audio.stops)
    }

    @Test fun aMicrophoneLostLaterEndsItToo() {
        live()
        audio.listener!!.onMicrophoneLost()
        assertEquals("麦克风打不开", state.error)
        assertEquals(VoiceState.Phase.Ended, state.phase)
    }

    @Test fun aNormalCloseEndsQuietly() {
        live()
        plugins.plugin.onClosed(1000, "")
        assertEquals(VoiceState.Phase.Ended, state.phase)
        assertNull(state.error)
        assertEquals("对话已结束", statusText(state))
    }

    @Test fun stoppingIsFinal() {
        live()
        session.stop()
        session.stop()
        assertEquals(1000 to "bye", plugins.socket.closed)
        assertEquals(1, audio.stops)
        plugins.plugin.onText("""{"type":"transcript","id":"u9","role":"user","text":"还在吗","final":true}""")
        plugins.plugin.onClosed(1001, "")
        audio.listener!!.onFrame(byteArrayOf(1), 1, 1f)
        assertTrue(state.lines.isEmpty())
        assertNull(state.error)
        assertEquals(VoiceState.Phase.Ended, state.phase)
        session.interrupt()
        session.setMuted(true)
        assertFalse(state.muted)
    }

    @Test fun stoppedBeforeTheSocketOpenedNothingStarts() {
        session.start()
        session.stop()
        plugins.plugin.onOpen()
        plugins.plugin.onText(Ready)
        assertTrue(plugins.socket.texts.isEmpty())
        assertNull(audio.rates)
        assertEquals(0, audio.stops)
    }
}
