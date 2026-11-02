package jarvis.android.lark

import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.InboxItem
import jarvis.android.data.InboxLark
import jarvis.android.data.InboxMoment
import jarvis.android.data.JsonCodec
import jarvis.android.ui.lark.LarkChatInfo
import jarvis.android.ui.lark.LarkChatModel
import jarvis.android.ui.lark.LarkScroll
import jarvis.android.ui.lark.LarkTarget
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.AfterClass
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.BeforeClass
import org.junit.Test
import java.io.File
import java.net.ServerSocket
import java.nio.file.Files

/**
 * How an inbox item opens its Feishu page (iOS `InboxDetailView`, `LarkChatView`), against scripts/mock/mock_hub.py with
 * MOCK_LARK=1: at the first new message (never the latest page for an unread chat), 以下为新消息 above it, 有人@你 for
 * the unread mention, nothing reported read until rows were on screen, then exactly what was seen.
 */
class LarkChatModelTest {
    companion object {
        private var proc: Process? = null
        private lateinit var url: String

        @BeforeClass @JvmStatic fun start() {
            val port = ServerSocket(0).use { it.localPort }
            val repo = generateSequence(File("").absoluteFile) { it.parentFile }.first { File(it, "scripts/mock/mock_hub.py").exists() }
            val pb = ProcessBuilder("python3", "scripts/mock/mock_hub.py").directory(repo).redirectErrorStream(true)
            pb.environment()["PORT"] = port.toString()
            pb.environment()["MOCK_LARK"] = "1"
            val p = pb.start()
            proc = p
            val out = p.inputStream.bufferedReader()
            val deadline = System.currentTimeMillis() + 20_000
            while (System.currentTimeMillis() < deadline) {
                val line = out.readLine() ?: error("mock hub exited")
                if (line.startsWith("mock hub on")) break
            }
            Thread { runCatching { out.forEachLine { } } }.apply { isDaemon = true }.start()
            url = "http://127.0.0.1:$port"
        }

        @AfterClass @JvmStatic fun stop() { proc?.destroy() }
    }

    private fun reads(feed: String) = JsonCodec.parseToJsonElement(
        OkHttpClient().newCall(Request.Builder().url("$url/mock/lark").build()).execute().use { it.body!!.string() },
    ).jsonObject["reads"]!!.jsonArray.filter { it["feed"].str == feed }

    @Test fun inboxItemsOpenTheirPage() {
        val chat = InboxItem(id = "lark:f2:200", source = "lark", kind = "mention", title = "设计评审", count = 3,
            lark = InboxLark(feedId = "f2", isGroup = true, startPosition = "200", mentionPosition = "202"))
        val t = LarkTarget.of(chat) as LarkTarget.Chat
        assertEquals(LarkChatInfo("f2", "设计评审", isGroup = true, unread = 3, focus = "200", mentionPosition = "202"), t.info)
        // Done: it opens at the latest, nothing counted as new.
        val done = LarkTarget.of(chat.copy(doneAt = "2026-11-01T10:00:00Z")) as LarkTarget.Chat
        assertNull(done.info.focus)
        assertEquals(0, done.info.unread)
        assertEquals(LarkTarget.Moment("mp1"), LarkTarget.of(InboxItem(id = "moment:mp1:mn1", source = "lark", kind = "moment", moment = InboxMoment(postId = "mp1"))))
        assertNull(LarkTarget.of(InboxItem(id = "mail:1", source = "mail", kind = "mail")))
    }

    @Test fun opensAtTheFirstNewMessageAndReportsOnlyWhatWasSeen() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val hub = Hub(ApiClient(url, "mock"), scope, "test")
        val dir = Files.createTempDirectory("lark").toFile()
        val model = LarkChatModel(hub, LarkChatInfo("f2", "设计评审", unread = 3, focus = "200", mentionPosition = "202"), scope, dir)
        try {
            model.start()
            val first = withTimeout(10_000) { model.scrollRequests.first() }
            val s = model.state.value
            assertEquals("199", s.anchor)
            assertEquals("202", s.mention)
            assertEquals(LarkScroll.ToMessage("lm_f2_200"), first)
            assertTrue(s.messages.any { it.position == "200" })
            // Opening alone reports nothing.
            delay(600)
            assertTrue(reads("f2").isEmpty())
            // Two rows reached the screen: those are reported, with the unread ids from others up to the newest.
            model.markSeen(listOf("200", "201"))
            withTimeout(5_000) { while (reads("f2").isEmpty()) delay(100) }
            val r = reads("f2").last()
            assertEquals("201", r["position"].str)
            assertEquals(listOf("lm_f2_200", "lm_f2_201"), r["message_ids"].arr!!.map { it.str })
            // Nothing new on screen: no second report.
            model.flushRead()
            delay(300)
            assertEquals(1, reads("f2").size)
            // The cache now paints the chat at once next time.
            assertTrue(File(dir, "lark-chats").listFiles()!!.isNotEmpty())
        } finally {
            scope.cancel()
        }
    }

    @Test fun sendsAndFollows() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val hub = Hub(ApiClient(url, "mock"), scope, "test")
        val model = LarkChatModel(hub, LarkChatInfo("f3", "周末徒步群", unread = 0), scope, Files.createTempDirectory("lark").toFile())
        try {
            model.start()
            withTimeout(10_000) { model.scrollRequests.first() }
            assertEquals(LarkScroll.Bottom(true), run { assertTrue(model.send("我也去")); withTimeout(10_000) { model.scrollRequests.first() } })
            assertTrue(model.state.value.messages.last().let { it.isMine && it.text == "我也去" })
        } finally {
            scope.cancel()
        }
    }
}
