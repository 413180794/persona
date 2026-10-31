package jarvis.android

import jarvis.android.voice.Line
import jarvis.android.voice.VoiceEvent
import jarvis.android.voice.startFrame
import jarvis.android.voice.with
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class VoiceTest {
    @Test fun parsesThePluginsFrames() {
        assertEquals(VoiceEvent.Ready(16000, 24000), VoiceEvent.parse("""{"type":"ready","input_rate":16000,"output_rate":24000,"voice":null}"""))
        assertEquals(VoiceEvent.Clear, VoiceEvent.parse("""{"type":"clear"}"""))
        assertEquals(VoiceEvent.Transcript("u1", true, "你好", false), VoiceEvent.parse("""{"type":"transcript","id":"u1","role":"user","text":"你好","final":false}"""))
        assertEquals(VoiceEvent.Tool("t1", "查知识库", false), VoiceEvent.parse("""{"type":"tool","id":"t1","label":"查知识库","done":false}"""))
        assertEquals(VoiceEvent.Tool("t1", null, true), VoiceEvent.parse("""{"type":"tool","id":"t1","done":true}"""))
        assertEquals(VoiceEvent.Error("boom", true), VoiceEvent.parse("""{"type":"error","message":"boom","fatal":true}"""))
        assertNull(VoiceEvent.parse("""{"type":"something_new"}"""))
        assertNull(VoiceEvent.parse("not json"))
    }

    @Test fun subtitlesRestateByIdAndAppendNewOnes() {
        var lines = emptyList<Line>()
        lines = lines.with(VoiceEvent.Transcript("u1", true, "今天", false))
        lines = lines.with(VoiceEvent.Transcript("u1", true, "今天有什么会", true))
        lines = lines.with(VoiceEvent.Transcript("a1", false, "我查", false))
        lines = lines.with(VoiceEvent.Transcript("a2", false, "", true)) // an answer cut off before a word: no line
        assertEquals(listOf(Line("u1", true, "今天有什么会", true), Line("a1", false, "我查", false)), lines)
    }

    @Test fun theStartFrameNamesTheVoiceOnlyWhenChosen() {
        assertEquals("""{"type":"start"}""", startFrame(null))
        assertEquals("""{"type":"start","voice":"zh_x"}""", startFrame("zh_x"))
        assertEquals("""{"type":"start","model":"m1","voice":"zh_x"}""", startFrame("zh_x", "m1"))
    }
}
