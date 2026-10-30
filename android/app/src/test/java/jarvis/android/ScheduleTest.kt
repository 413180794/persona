package jarvis.android

import jarvis.android.data.ApiClient
import jarvis.android.data.JsonCodec
import jarvis.android.data.parseTime
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The hub's routine list as /api/v1/assistant/routines sends it, and its Go times (offsets, zero time). */
class ScheduleTest {
    private val body = """{"routines":[
        {"id":"morning","title":"早报","prompt":"p","time":"09:00","weekdays":[1,2,3,4,5],"enabled":true,"next_run":"2026-11-02T09:00:00+08:00"},
        {"id":"evening","title":"晚间回顾","prompt":"p","time":"21:30","weekdays":[1,2,3,4,5],"enabled":false,"next_run":"0001-01-01T00:00:00Z"}]}"""

    @Test fun decodesRoutines() {
        @kotlinx.serialization.Serializable data class R(val routines: List<ApiClient.Routine>)
        val r = JsonCodec.decodeFromString<R>(body).routines
        assertEquals(2, r.size)
        assertTrue(r[0].enabled)
        assertEquals(1, r.count { it.enabled })
    }

    @Test fun parsesTimesWithAnOffset() {
        assertNotNull(parseTime("2026-11-02T09:00:00+08:00"))
        assertNull(parseTime("0001-01-01T00:00:00Z"))
    }

    @Test fun memoryPreviewSkipsFrontMatter() {
        assertEquals("测试用的记忆", jarvis.android.ui.memoryPreview("---\nname: x\ndescription: y\n---\n\n测试用的记忆\n更多"))
        assertEquals("没有头", jarvis.android.ui.memoryPreview("没有头\n第二行"))
    }
}
