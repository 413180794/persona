package jarvis.android.lark

import jarvis.android.data.ApiClient
import jarvis.android.data.JsonCodec
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.AfterClass
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.BeforeClass
import org.junit.Test
import java.io.File
import java.net.ServerSocket

/**
 * The Feishu chat page's endpoints against scripts/mock/mock_hub.py with MOCK_LARK=1, through the app's own [ApiClient]
 * extensions and models: paging (around / before / after), the thread, writes and what the fake hub recorded, people,
 * removed, emoji, media with Range. A real hub is never written to (docs/android.md「契约测试」).
 */
class LarkContractTest {
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

    private val api get() = ApiClient(url, "mock")

    @Test fun chatPage() = runBlocking {
        // An unread chat opens around its first new message, never at the latest page.
        val around = api.larkMessages("f2", around = "200")
        assertTrue(around.messages.any { it.position == "200" })
        assertTrue(around.messages.zipWithNext().all { (a, b) -> LarkPosition.less(a.position, b.position) })
        val kinds = around.messages.flatMap { it.parts.orEmpty() }.map { it.kind }.toSet()
        assertTrue(kinds.containsAll(listOf("text", "at", "link", "emotion", "image", "code", "card", "media")))
        assertTrue(around.messages.any { it.isSystem })
        assertTrue(around.messages.any { it.reactions?.isNotEmpty() == true })
        assertTrue(around.messages.any { it.replyTo != null })
        around.messages.flatMap { it.parts.orEmpty() }.let { parts ->
            assertTrue(LarkRichNode.tree(parts).isNotEmpty())
            val card = parts.first { it.kind == "card" && it.metadata["header"] != null }
            assertTrue(LarkCardNode.of(card.metadata["body"])!!.children.any { it.tag == "column_set" })
        }

        val latest = api.larkMessages("f3")
        assertEquals("300", latest.messages.last().position)
        assertEquals(false, latest.hasNewer)
        val older = api.larkMessages("f3", before = "295", limit = 3)
        assertEquals(listOf("292", "293", "294"), older.messages.map { it.position })
        assertTrue(older.hasOlder)
        assertEquals(listOf("299", "300"), api.larkMessages("f3", after = "298").messages.map { it.position })
        assertEquals(3, api.larkThread("lm_f2_187").messages.size)
    }

    @Test fun writesAreRecorded() = runBlocking {
        api.larkSend("f1", "收到，周五上午发")
        api.larkRead("f2", LarkReadReport("203", messageIds = listOf("lm_f2_200", "lm_f2_203")))
        api.larkReact("lm_f2_201", "THUMBSUP", remove = false)
        api.larkCardAction("lm_f2_185", "approve")
        val log = JsonCodec.parseToJsonElement(get("$url/mock/lark")).jsonObject
        assertTrue(log["sent"]!!.jsonArray.any { it["text"].str == "收到，周五上午发" && it["feed"].str == "f1" })
        assertTrue(log["reads"]!!.jsonArray.any { it["position"].str == "203" && it["message_ids"].arr?.size == 2 })
        assertTrue(log["reactions"]!!.jsonArray.any { it["key"].str == "THUMBSUP" && it["remove"].bool == false })
        assertTrue(log["actions"]!!.jsonArray.any { it["action"].str == "approve" })
        assertTrue(api.larkMessages("f1").messages.last().let { it.isMine && it.text == "收到，周五上午发" })
        assertTrue(api.larkMessages("f2", around = "201").messages.first { it.id == "lm_f2_201" }.reactions!!.any { it.key == "THUMBSUP" && it.mine })
    }

    @Test fun peopleMomentsEmojiMedia() = runBlocking {
        val profile = api.larkProfile("u_hmm", "f2")
        assertEquals("韩梅梅", profile.displayName)
        assertTrue(profile.fields!!.any { it.key == "B-LEADER" && it.url?.startsWith("lark://client/profile") == true })
        assertTrue(api.larkUrlPreview("lm_f1_95", "https://applink.example.com/client/moments/detail?postId=1").card?.author != null)
        assertEquals(3, api.larkMomentsPost("7000000000000000001").images!!.size)
        assertEquals(1, api.larkMomentsComments("7000000000000000001").size)
        // The inbox's own removed item (mp1): its post and its comments.
        assertEquals(3, api.larkMomentsComments("mp1").size)
        val emojis = api.larkEmojis()
        assertTrue(emojis.emojis.containsKey("thumbsup"))
        assertTrue(emojis.groups!!.first().keys.isNotEmpty())
        assertTrue(api.larkQuickReactions().isNotEmpty())
        val head = api.http.newCall(api.larkMediaRequest("/api/v1/lark/media/emoji-ok", range = "bytes=0-1")!!).execute().use { it.code to it.body!!.bytes().size }
        assertEquals(206 to 2, head)
        assertEquals(0x89.toByte(), api.larkMediaBytes("/api/v1/lark/media/emoji-ok")[0])
        // A thumbnail is asked for with size=, replacing one already in the URL.
        assertEquals("48", api.larkMediaRequest("/api/v1/lark/media/k?size=24", size = 48)!!.url.queryParameter("size"))
    }

    private fun get(u: String): String = OkHttpClient().newCall(Request.Builder().url(u).header("Authorization", "Bearer mock").build()).execute().use { it.body!!.string() }

}
