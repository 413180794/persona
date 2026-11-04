package persona.feature.voice

import android.media.AudioDeviceInfo
import persona.core.contracts.api.VoiceModelChoice
import persona.core.contracts.api.VoiceModelOption
import persona.core.contracts.api.VoiceModelOptions
import persona.core.contracts.api.VoiceOption
import persona.core.designsystem.VoiceOrbMode
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import java.io.IOException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** What the page says, which model a conversation gets, and where its sound goes. */
class VoiceTextTest {
    private val live = VoiceState(phase = VoiceState.Phase.Live)

    @Test fun theStatusLineSaysWhatIsHappening() {
        assertEquals("正在连接…", statusText(VoiceState()))
        assertEquals("在听", statusText(live))
        assertEquals("正在说，直接开口就能打断", statusText(live.copy(speaking = true)))
        assertEquals("已静音", statusText(live.copy(muted = true)))
        assertEquals("A tool is said over speaking and muting", "查日程…", statusText(live.copy(tool = "查日程", speaking = true, muted = true)))
        assertEquals("Speaking is said over muted", "正在说，直接开口就能打断", statusText(live.copy(speaking = true, muted = true)))
        assertEquals("对话已结束", statusText(live.copy(phase = VoiceState.Phase.Ended, tool = "查日程", speaking = true)))
    }

    @Test fun theOrbFollowsTheSameState() {
        assertEquals(VoiceOrbMode.Connecting, orbMode(VoiceState()))
        assertEquals(VoiceOrbMode.Listening, orbMode(live))
        assertEquals(VoiceOrbMode.Speaking, orbMode(live.copy(speaking = true, muted = true)))
        assertEquals(VoiceOrbMode.Muted, orbMode(live.copy(muted = true)))
        assertEquals(VoiceOrbMode.Ended, orbMode(live.copy(phase = VoiceState.Phase.Ended, speaking = true)))
    }

    @Test fun aVoiceReadsAsGenderLanguagesAndDefault() {
        assertEquals("女声 · 中文、英文 · 默认", voiceDetail(VoiceOption("a", "温柔", "female", listOf("zh", "en")), isDefault = true))
        assertEquals("男声", voiceDetail(VoiceOption("b", "沉稳", "male", listOf("xx")), isDefault = false))
        assertEquals("日语", voiceDetail(VoiceOption("c", "元气", null, listOf("ja")), isDefault = false))
        assertNull(voiceDetail(VoiceOption("d", "无名"), isDefault = false))
    }

    @Test fun aSocketThatFailsSaysWhy() {
        assertEquals("hub 上没有语音扩展", connectFailure(IOException("Expected HTTP 101"), 404, live = false))
        assertEquals("Mid-conversation it is a restart or the network", VoiceText.Restarted, connectFailure(IOException("Broken pipe"), null, live = true))
        assertEquals("连不上语音服务：连不上 hub", connectFailure(HubFailure(HubError.ConnectionRefused), null, live = false))
        assertEquals("连不上语音服务：连不上 hub", connectFailure(IOException("unexpected end of stream"), null, live = false))
        assertEquals("连不上语音服务：语音服务没有响应", connectFailure(HubFailure(HubError.Server, "plugin voice is not answering", 502), 502, live = false))
        assertEquals("连不上语音服务：这台设备不能用语音对话", connectFailure(HubFailure(HubError.PairingExpired, "forbidden", 403), 403, live = false))
        assertEquals("连不上语音服务：配对已失效，请重新扫码", connectFailure(HubFailure(HubError.PairingExpired, "unauthorized", 401), 401, live = false))
        assertEquals("连不上语音服务：HTTP 418", connectFailure(HubFailure(HubError.Server, "teapot", 418), 418, live = false))
        assertEquals("连不上语音服务：连接失败", connectFailure(HubFailure(HubError.Protocol), null, live = false))
    }

    @Test fun aCloseSaysWhyOnlyWhenThereIsSomethingToSay() {
        assertEquals(VoiceText.Restarted, closeMessage(1001, "", live = false))
        assertNull("The plugin ended it", closeMessage(1000, "bye", live = true))
        assertEquals("会话超时", closeMessage(4000, " 会话超时 ", live = true))
        assertEquals(VoiceText.Restarted, closeMessage(1011, "", live = true))
        assertNull(closeMessage(1011, "", live = false))
    }

    @Test fun readFailuresAreToldApartFromNoConfiguration() {
        assertEquals("连不上 hub，连上后点重试。", loadFailure(HubFailure(HubError.Timeout)))
        assertEquals("hub 没有给出结果：upstream", loadFailure(HubFailure(HubError.Server, "upstream", 502)))
        assertEquals("hub 没有给出结果。", loadFailure(HubFailure(HubError.Protocol)))
        assertEquals("配对已失效，请重新扫码", loadFailure(HubFailure(HubError.PairingExpired, "unauthorized", 401)))
        assertEquals("连不上 hub", voicesFailure(HubFailure(HubError.Unreachable)))
        assertEquals("no such plugin", voicesFailure(HubFailure(HubError.Server, "no such plugin", 404)))
        assertEquals("音色列表没拿到：连不上 hub", VoiceText.voicesFailed(voicesFailure(IOException("reset"))))
    }

    @Test fun theModelComesFromTheModelConfigurationAndIsNeverQuietlySwapped() {
        val models = VoiceModelOptions(listOf(VoiceModelOption("fast", "快"), VoiceModelOption("deep")), default = "deep")
        assertEquals("An older hub: the plugin's default", VoiceSetup.Ready("deep"), resolveVoiceModel(null, models))
        assertEquals("No models and no default: none named", VoiceSetup.Ready(null), resolveVoiceModel(null, VoiceModelOptions()))
        assertEquals(VoiceSetup.Ready("fast"), resolveVoiceModel(VoiceModelChoice("fast", null), models))
        assertEquals("未验证 is usable", VoiceSetup.Ready("fast"), resolveVoiceModel(VoiceModelChoice("fast", "未验证"), models))
        assertEquals("Nothing chosen: the plugin's default", VoiceSetup.Ready("deep"), resolveVoiceModel(VoiceModelChoice(null, " "), models))
        assertEquals(VoiceSetup.Unavailable("读不到扩展模型列表"), resolveVoiceModel(VoiceModelChoice("fast", "读不到扩展模型列表"), models))
        assertEquals(VoiceSetup.Unavailable(VoiceText.ModelMissing), resolveVoiceModel(VoiceModelChoice("retired", null), models))
        assertEquals("A plugin without a catalog has no room for a configured model",
            VoiceSetup.Unavailable(VoiceText.ModelMissing), resolveVoiceModel(VoiceModelChoice("fast", null), VoiceModelOptions()))
    }

    @Test fun soundGoesToAHeadsetElseTheLoudspeakerNeverTheEarpiece() {
        val earpiece = AudioDeviceInfo.TYPE_BUILTIN_EARPIECE
        val speaker = AudioDeviceInfo.TYPE_BUILTIN_SPEAKER
        assertEquals(1, preferredRoute(listOf(earpiece, speaker)))
        assertEquals(2, preferredRoute(listOf(earpiece, speaker, AudioDeviceInfo.TYPE_BLUETOOTH_SCO)))
        assertEquals(0, preferredRoute(listOf(AudioDeviceInfo.TYPE_WIRED_HEADSET, AudioDeviceInfo.TYPE_BLE_HEADSET, speaker)))
        assertEquals(1, preferredRoute(listOf(speaker, AudioDeviceInfo.TYPE_USB_HEADSET)))
        assertNull(preferredRoute(listOf(earpiece)))
        assertNull(preferredRoute(emptyList()))
    }
}
