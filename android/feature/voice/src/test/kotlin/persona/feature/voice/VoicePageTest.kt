package persona.feature.voice

import persona.core.contracts.api.VoiceModelChoice
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import java.io.File
import java.nio.file.Files
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Setting up a conversation: the model from the hub's configuration, the voice kept with the pairing. */
@OptIn(ExperimentalCoroutinesApi::class)
class VoicePageTest {
    private val directory: File = Files.createTempDirectory("voice-page").toFile()
    private val prefs = VoicePrefs(File(directory, "voice.json"))
    private val plugins = FakePlugins()
    private val audios = mutableListOf<FakeAudio>()

    @After fun clean() { directory.deleteRecursively() }

    private fun TestScope.page() = VoicePage(plugins, "voice", prefs, this) { FakeAudio().also { audios += it } }

    @Test fun anOlderHubTalksWithThePluginsOwnDefaultModel() = runTest {
        plugins.choice = null
        val page = page()
        page.begin()
        assertEquals(VoiceSetup.Loading, page.state.value.setup)
        advanceUntilIdle()
        assertEquals(VoiceSetup.Ready("deep"), page.state.value.setup)
        assertEquals(listOf<String?>("deep"), plugins.voiceRequests)
        plugins.plugin.onOpen()
        assertEquals("""{"type":"start","model":"deep"}""", plugins.socket.texts.single())
        assertEquals("深 · 沉稳", page.state.value.subtitle)
        assertEquals("calm", page.state.value.checkedVoice)
        page.close()
    }

    @Test fun aModelThatCannotBeUsedIsSaidNotSwapped() = runTest {
        plugins.choice = VoiceModelChoice("fast", "读不到插件模型列表")
        val page = page()
        page.begin()
        advanceUntilIdle()
        assertEquals(VoiceSetup.Unavailable("读不到插件模型列表"), page.state.value.setup)
        assertTrue("No conversation with another model", plugins.sockets.isEmpty())
        assertNull(page.session.value)
        assertNull(page.state.value.subtitle)

        plugins.choice = VoiceModelChoice("retired", null)
        page.begin()
        advanceUntilIdle()
        assertEquals(VoiceSetup.Unavailable(VoiceText.ModelMissing), page.state.value.setup)

        plugins.choice = VoiceModelChoice("fast", "未验证")
        page.begin()
        advanceUntilIdle()
        assertEquals(VoiceSetup.Ready("fast"), page.state.value.setup)
        plugins.plugin.onOpen()
        assertEquals("""{"type":"start","model":"fast"}""", plugins.socket.texts.single())
        page.close()
    }

    @Test fun aConfigurationThatCannotBeReadIsNotTakenForNone() = runTest {
        plugins.choiceError = HubFailure(HubError.Server, "upstream", 502)
        val page = page()
        page.begin()
        advanceUntilIdle()
        assertEquals(VoiceSetup.Failed("hub 没有给出结果：upstream"), page.state.value.setup)
        assertTrue(plugins.sockets.isEmpty())

        plugins.choiceError = null
        plugins.modelsError = HubFailure(HubError.Timeout)
        page.begin()
        advanceUntilIdle()
        assertEquals(VoiceSetup.Failed("连不上 hub，连上后点重试。"), page.state.value.setup)

        plugins.modelsError = null
        page.begin()
        advanceUntilIdle()
        assertEquals("重试 reads it again", VoiceSetup.Ready("deep"), page.state.value.setup)
        assertNotNull(page.session.value)
        page.close()
    }

    @Test fun thePickedVoiceIsKeptAndANewOneStartsAgain() = runTest {
        val page = page()
        page.begin()
        advanceUntilIdle()
        val first = plugins.socket
        page.pickVoice("calm")
        assertEquals("The voice it already speaks with: no new conversation", 1, plugins.sockets.size)
        assertEquals("calm", prefs.voice("voice"))
        page.pickVoice("soft")
        assertEquals(1000 to "bye", first.closed)
        assertEquals(2, plugins.sockets.size)
        plugins.plugin.onOpen()
        assertEquals("""{"type":"start","model":"deep","voice":"soft"}""", plugins.socket.texts.single())
        assertEquals("soft", prefs.voice("voice"))
        assertEquals("深 · 温柔", page.state.value.subtitle)
        assertEquals("Kept on this phone for the next time", "soft", VoicePage(plugins, "voice", prefs, this) { FakeAudio() }.state.value.voice)
        page.close()
    }

    @Test fun aVoiceThePluginNoLongerHasIsNotSent() = runTest {
        prefs.setVoice("voice", "retired")
        val page = page()
        page.begin()
        advanceUntilIdle()
        plugins.plugin.onOpen()
        assertEquals("""{"type":"start","model":"deep"}""", plugins.socket.texts.single())
        assertEquals("calm", page.state.value.checkedVoice)
        assertEquals("Another model may still have it", "retired", prefs.voice("voice"))
        page.close()
    }

    @Test fun voicesThatCannotBeReadDoNotStopTheConversation() = runTest {
        prefs.setVoice("voice", "soft")
        plugins.voicesError = HubFailure(HubError.Unreachable)
        val page = page()
        page.begin()
        advanceUntilIdle()
        assertEquals("连不上 hub", page.state.value.voicesError)
        plugins.plugin.onOpen()
        assertEquals("""{"type":"start","model":"deep","voice":"soft"}""", plugins.socket.texts.single())
        assertEquals("深", page.state.value.subtitle)
        page.close()
    }

    @Test fun startingAgainReadsTheModelConfigurationAgain() = runTest {
        plugins.choice = VoiceModelChoice("fast", null)
        val page = page()
        page.begin()
        advanceUntilIdle()
        val first = plugins.socket
        plugins.choice = VoiceModelChoice("deep", null)
        page.begin()
        assertEquals(1000 to "bye", first.closed)
        advanceUntilIdle()
        plugins.plugin.onOpen()
        assertEquals("""{"type":"start","model":"deep"}""", plugins.socket.texts.single())
        page.close()
    }

    @Test fun leavingTheAppEndsTheConversation() = runTest {
        val page = page()
        page.begin()
        page.pause()
        advanceUntilIdle()
        assertEquals("Stopped while setting up", VoiceSetup.Stopped, page.state.value.setup)
        assertTrue(plugins.sockets.isEmpty())

        page.begin()
        advanceUntilIdle()
        plugins.plugin.onOpen()
        plugins.plugin.onText(Ready)
        assertEquals(VoiceState.Phase.Live, page.session.value!!.state.value.phase)
        page.pause()
        assertEquals(VoiceState.Phase.Ended, page.session.value!!.state.value.phase)
        assertEquals(1, audios.last().stops)
        assertEquals("The setup stays: 重新开始 starts again", VoiceSetup.Ready("deep"), page.state.value.setup)
        page.close()
    }
}
