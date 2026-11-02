package jarvis.android.shots

import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import jarvis.android.data.ApiClient
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.lark.LarkPart
import jarvis.android.lark.larkMomentsComments
import jarvis.android.lark.larkMomentsPost
import jarvis.android.ui.lark.LarkChatModel
import jarvis.android.ui.lark.LarkItemPage
import jarvis.android.ui.lark.LarkMedia
import jarvis.android.ui.lark.LarkTarget
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Rule
import org.junit.Test
import java.io.File
import java.net.ServerSocket
import java.nio.file.Files

/** scripts/mock_hub.py with MOCK_LARK=1: the Feishu chats behind the inbox's items, with every kind of message. */
object LarkMockHub {
    private val url: String by lazy {
        val port = ServerSocket(0).use { it.localPort }
        val repo = generateSequence(File("").absoluteFile) { it.parentFile }.first { File(it, "scripts/mock_hub.py").exists() }
        val pb = ProcessBuilder("python3", "scripts/mock_hub.py").directory(repo).redirectErrorStream(true)
        pb.environment()["PORT"] = port.toString()
        pb.environment()["MOCK_LARK"] = "1"
        val p = pb.start()
        Runtime.getRuntime().addShutdownHook(Thread { p.destroy() })
        val out = p.inputStream.bufferedReader()
        val deadline = System.currentTimeMillis() + 20_000
        while (System.currentTimeMillis() < deadline) {
            val line = out.readLine() ?: error("mock hub exited")
            if (line.startsWith("mock hub on")) break
        }
        Thread { runCatching { out.forEachLine { } } }.apply { isDaemon = true }.start()
        "http://127.0.0.1:$port"
    }

    val hub: Hub by lazy {
        val h = Hub(ApiClient(url, "mock"), CoroutineScope(SupervisorJob() + Dispatchers.IO), "shots")
        h.start()
        runBlocking {
            withTimeout(20_000) { h.conn.first { it == ConnState.Online } }
            h.loadInbox()
        }
        LarkMedia.diskDir = Files.createTempDirectory("lark-media").toFile()
        h
    }
}

/**
 * The Feishu pages an inbox item opens (iOS `InboxDetailView`): a chat at its first new message, a chat with cards,
 * tables and media scrolled to, a direct chat with a file, voice and a removed share, and the inbox's removed post.
 * Light and dark; everything a frame shows is read from the mock first (a snapshot is one frame).
 */
class LarkShots {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6, theme = "android:Theme.Material.Light.NoActionBar", maxPercentDifference = 100.0)

    private val hub get() = LarkMockHub.hub

    private fun item(id: String) = hub.inbox.value.first { it.id == id }

    /** The emoji catalogue and every picture of [parts] decoded, at the sizes the rows ask for. */
    private fun warm(parts: List<LarkPart>) = runBlocking {
        LarkMedia.loadCatalog(hub.api)
        withTimeout(10_000) { while (LarkMedia.emojis.value.isEmpty()) delay(50) }
        val urls = parts.mapNotNull { it.url } + parts.mapNotNull { it.metaString("posterUrl") } + LarkMedia.emojis.value.values.map { it.url }
        for (u in urls) runCatching { LarkMedia.bytes(hub.api, u) }
    }

    /** A chat's model, loaded: the page draws where it opens in the first frame. */
    private fun chat(id: String, around: String? = null, seen: Boolean = true): LarkChatModel = runBlocking {
        val target = LarkTarget.of(item(id)) as LarkTarget.Chat
        val info = if (around != null) target.info.copy(focus = around) else target.info
        val m = LarkChatModel(hub, info, CoroutineScope(SupervisorJob() + Dispatchers.IO), Files.createTempDirectory("lark").toFile())
        m.start()
        withTimeout(10_000) { m.scrollRequests.first() }
        warm(m.state.value.messages.flatMap { it.parts.orEmpty() } + m.state.value.messages.mapNotNull { it.senderAvatar?.let { a -> LarkPart(kind = "image", url = a) } })
        if (seen) m.markSeen(m.state.value.messages.map { it.position })
        m
    }

    /**
     * Pictures decode off the main thread, after the first frame: a first render (under the same name, replaced by the
     * real one) asks for every picture at the size it is drawn, and the real frames find them in memory.
     */
    private fun shots(name: String, content: @androidx.compose.runtime.Composable () -> Unit) {
        // Three times: what one load brings (a link's preview) asks for pictures of its own (the author's face).
        repeat(3) {
            paparazzi.snapshot(name) { ShotHost(false) { content() } }
            Thread.sleep(WARM_MS)
            runBlocking { withTimeout(10_000) { LarkMedia.settle() } }
        }
        for (dark in listOf(false, true)) paparazzi.snapshot(name + if (dark) "-dark" else "") { ShotHost(dark) { content() } }
    }

    private fun page(name: String, id: String, model: LarkChatModel) {
        val it = item(id)
        shots(name) { LarkItemPage(hub, it, LarkTarget.Chat(model.info), onBack = {}, onDone = { _, _ -> }, onHandOff = {}, onAskJarvis = {}, chat = model) }
    }

    /** 设计评审 opened from its @我 item: 以下为新消息 above the first new message, 有人@你 while the mention is unseen. */
    @Test fun chatUnread() = page("lark-chat", "lark:f2:200", chat("lark:f2:200", seen = false))

    /** The same chat further up: the bot's card with its columns, a code block, the duty table and its chart. */
    @Test fun chatCards() = page("lark-chat-cards", "lark:f2:200", chat("lark:f2:200", around = "185"))

    /** Further up still: the rich post, pictures, a merge-forward, a sticker, a video and the meeting card. */
    @Test fun chatMedia() = page("lark-chat-media", "lark:f2:200", chat("lark:f2:200", around = "189"))

    /** 韩梅梅 (a direct chat): a file, a voice message, a removed share, a reply and links. */
    @Test fun chatDirect() = page("lark-chat-direct", "lark:f1:100", chat("lark:f1:100", around = "93"))

    /** The inbox's removed item: the user's post and its comments. */
    @Test fun moment() {
        val it = item("moment:mp1:mn1")
        val pre = runBlocking { hub.api.larkMomentsPost("mp1") to hub.api.larkMomentsComments("mp1") }
        warm(emptyList())
        shots("lark-moment") { LarkItemPage(hub, it, LarkTarget.Moment("mp1"), onBack = {}, onDone = { _, _ -> }, onHandOff = {}, onAskJarvis = {}, moment = pre) }
    }
}

private const val WARM_MS = 400L
