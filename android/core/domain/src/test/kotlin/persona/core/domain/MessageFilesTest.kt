package persona.core.domain

import org.junit.Assert.*
import org.junit.Test

class MessageFilesTest {
    @Test fun tagsBecomeFilesAndLeaveTheText() {
        val text = "看看这个\n\n<persona-file kind=\"video\" path=\"/up/a &amp; b.mov\" name=\"clip.mov\" size=\"2400000\" duration=\"12.6\"/>\n" +
            "<persona-file kind=\"audio\" path=\"/up/rec.m4a\"></persona-file>"
        val (files, rest) = parseAttachedFiles(text)
        assertEquals("看看这个", rest)
        assertEquals(2, files.size)
        assertEquals(AttachedFile("video", "/up/a & b.mov", "clip.mov", 2_400_000, 12.6), files[0])
        assertEquals("rec.m4a", files[1].name)
        assertEquals(0L, files[1].size)
        assertNull(files[1].duration)
    }

    @Test fun aTagWithoutAPathIsDroppedAndOtherTextIsUntouched() {
        assertEquals(emptyList<AttachedFile>() to "", parseAttachedFiles("<persona-file kind=\"video\" name=\"x\"/>"))
        assertEquals(emptyList<AttachedFile>() to " plain ", parseAttachedFiles(" plain "))
        val (files, rest) = parseAttachedFiles("<persona-file path=\"/a.mp4\" size=\"-3\" duration=\"NaN\"/>")
        assertEquals("video", files.single().kind)
        assertEquals(0L, files.single().size)
        assertNull(files.single().duration)
        assertEquals("", rest)
    }

    @Test fun aFileWritesItsTagBackAsIOSDoes() {
        val file = AttachedFile("video", "/up/a & \"b\" <c>.mov", "clip.mov", 2_400_000, 12.64)
        assertEquals("<persona-file kind=\"video\" path=\"/up/a &amp; &quot;b&quot; &lt;c&gt;.mov\" name=\"clip.mov\" size=\"2400000\" duration=\"12.6\"/>", file.toXml())
        assertEquals(listOf(file.copy(duration = 12.6)), parseAttachedFiles(file.toXml()).first)
        assertEquals("<persona-file kind=\"audio\" path=\"/r.m4a\" name=\"r.m4a\" size=\"0\"/>", AttachedFile("audio", "/r.m4a", "r.m4a").toXml())
    }

    @Test fun clocksRoundToTheNearestSecondAsIOS() {
        assertEquals("0:03", mediaClock(2.9))
        assertEquals("0:13", mediaClock(12.6))
        assertEquals("0:02", mediaClock(2.4))
        assertEquals("1:05", mediaClock(65.0))
        assertEquals("1:00:03", mediaClock(3603.0))
    }
}
