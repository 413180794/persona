package jarvis.android.shots

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import jarvis.android.data.ApiClient
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.mail.MailAccountForm
import jarvis.android.mail.MailCompose
import jarvis.android.mail.MailStore
import jarvis.android.mail.larkStatus
import jarvis.android.mail.mailAccounts
import jarvis.android.mail.mailMessage
import jarvis.android.ui.InboxDetailPane
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.mail.AddMailAccountScreen
import jarvis.android.ui.mail.ComposeMailPage
import jarvis.android.ui.mail.ConnectorsGroup
import jarvis.android.ui.mail.FeishuConnectorScreen
import jarvis.android.ui.mail.MailAccountsScreen
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Rule
import org.junit.Test
import java.io.File
import java.net.ServerSocket

/** scripts/mock_hub.py with MOCK_MAIL=1: whole mails for the reader (an HTML one, a thread) and three kinds of account. */
object MailMockHub {
    private val url: String by lazy {
        val port = ServerSocket(0).use { it.localPort }
        val repo = generateSequence(File("").absoluteFile) { it.parentFile }.first { File(it, "scripts/mock_hub.py").exists() }
        val pb = ProcessBuilder("python3", "scripts/mock_hub.py").directory(repo).redirectErrorStream(true)
        pb.environment()["PORT"] = port.toString()
        pb.environment()["MOCK_MAIL"] = "1"
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
            h.refresh()
            h.loadInbox()
            // A snapshot is one frame: what the pages load themselves is fetched here first.
            MailStore.setAccounts(h.api.mailAccounts())
            MailStore.setLark(h.api.larkStatus())
            for (uid in listOf(502L, 505L, 506L)) MailStore.put("acc1", "INBOX", h.api.mailMessage("acc1", uid, "INBOX"))
        }
        h
    }
}

/**
 * Mail and 设置 › 连接器 (docs/android.md「邮件」), light and dark: an inbox mail opened (a thread; an HTML mail with remote
 * images held back and an attachment), 回复 being written, the 连接器 group, 邮箱, 添加邮箱 (Gmail, other IMAP) and 飞书.
 */
class MailShots {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6, theme = "android:Theme.Material.Light.NoActionBar", maxPercentDifference = 100.0)

    private val hub get() = MailMockHub.hub

    private fun both(name: String, content: @Composable () -> Unit) {
        hub // started (and its data loaded) before the first frame
        for (dark in listOf(false, true)) paparazzi.snapshot(name + if (dark) "-dark" else "") { ShotHost(dark) { content() } }
    }

    @Test fun mailThread() = both("mail-thread") { InboxDetailPane(hub, "mail:acc1:t5:505", onClose = {}, onHandOff = {}) }

    @Test fun mailHtml() = both("mail-html") { InboxDetailPane(hub, "mail:acc1:t2:502", onClose = {}, onHandOff = {}) }

    @Test fun compose() {
        hub
        val reply =MailCompose.reply(MailStore.message("acc1", "INBOX", 506)!!, "acc1", "me@example.com", all = true)
        both("mail-compose") { ComposeMailPage(hub.api, reply, onDone = {}) }
    }

    @Test fun connectors() = both("settings-connectors") {
        Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
            PageHeader("设置", onBack = {})
            ConnectorsGroup(hub.api, onPhone = {}, onMail = {}, onFeishu = {})
        }
    }

    @Test fun accounts() = both("mail-accounts") { MailAccountsScreen(hub.api, onBack = {}, onAdd = {}, preloaded = true) }

    @Test fun addGmail() = both("mail-add") { AddMailAccountScreen(hub.api, onBack = {}, onAdded = {}) }

    @Test fun addImap() = both("mail-add-imap") {
        AddMailAccountScreen(hub.api, onBack = {}, onAdded = {}, start = MailAccountForm(MailAccountForm.Kind.IMAP, email = "me@example.com"))
    }

    @Test fun feishu() = both("feishu-connector") { FeishuConnectorScreen(hub.api, onBack = {}, onAgentModel = {}, preloaded = true) }
}
