package jarvis.android

import jarvis.android.data.HubException
import jarvis.android.data.Interaction
import jarvis.android.data.Resolution
import jarvis.android.shortcuts.ShortcutText
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.io.IOException
import java.time.Instant

class ShortcutTextTest {
    private val t = Instant.parse("2026-10-30T08:00:00Z")

    @Test fun picksTheOldestAndSettlesOnlyPermissionsAndPlans() {
        val a = Interaction("a", kind = "plan", createdAt = t.plusSeconds(10).toString(), plan = "# 计划\n先改   模型")
        val b = Interaction("b", kind = "permission", createdAt = t.toString(), toolName = "Bash", summary = "Bash: swift test")
        assertEquals("b", ShortcutText.pending(listOf(a, b))!!.id)
        assertNull(ShortcutText.pending(emptyList()))
        assertEquals(Resolution(allow = true), ShortcutText.resolution(b))
        // As the iOS intent: 「批准并自动执行」 (docs/intents.md).
        assertEquals(Resolution(approve = true, mode = "bypassPermissions"), ShortcutText.resolution(a))
        assertNull(ShortcutText.resolution(Interaction("q", kind = "question")))
        assertNull(ShortcutText.resolution(Interaction("s", kind = "screen")))
        assertEquals("允许 Bash: swift test？", ShortcutText.ask(b))
        assertEquals("批准计划并自动执行？\n「计划 先改 模型」", ShortcutText.ask(a))
        assertEquals("已允许 Bash", ShortcutText.done(b))
        assertEquals("已批准计划，开始执行", ShortcutText.done(a))
        assertEquals("这是个提问，需要打开 Jarvis 回答。", ShortcutText.needsApp("question"))
        assertEquals("终端在等你输入，需要打开 Jarvis。", ShortcutText.needsApp("screen"))
    }

    @Test fun failures() {
        assertEquals("配对已失效，打开 Jarvis 重新配对。", ShortcutText.failure(HubException(401, "unauthorized")))
        assertEquals("这个已经处理过了。", ShortcutText.failure(HubException(410, "gone")))
        assertEquals("连不上 hub，确认手机能访问 hub（同一个网络或 VPN）。", ShortcutText.failure(IOException("timeout")))
        assertEquals("abc…", ShortcutText.excerpt("abcdef", 3))
    }
}
