package jarvis.android.shots

import jarvis.android.settings.modelConfiguration
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import jarvis.android.data.ApiClient
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.ui.JTheme
import jarvis.android.ui.JarvisTheme
import jarvis.android.ui.PairScreen
import jarvis.android.ui.StylePickerScreen
import jarvis.android.ui.VoicePreview
import jarvis.android.ui.VoiceScreen
import jarvis.android.ui.chat.FullScreenPageBody
import jarvis.android.ui.design.IconAction
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.files.FilesPreload
import jarvis.android.ui.files.FsListing
import jarvis.android.ui.files.GitStatus
import jarvis.android.ui.files.FilesScreen
import jarvis.android.ui.files.FilesTarget
import jarvis.android.ui.files.GitCommitPage
import jarvis.android.ui.files.GitDiffPage
import jarvis.android.ui.files.MonoLines
import jarvis.android.ui.files.gitDiff
import jarvis.android.ui.files.gitShow
import jarvis.android.ui.files.gitStatus
import jarvis.android.ui.files.listDir
import jarvis.android.ui.files.numberedLines
import jarvis.android.ui.files.readFile
import jarvis.android.ui.media.ViewerChrome
import jarvis.android.ui.web.LinkSheetTop
import jarvis.android.voice.Line
import jarvis.android.voice.Plugin
import jarvis.android.voice.VoiceState
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

/**
 * scripts/mock/mock_hub.py with MOCK_FILES=1 (a made-up checkout of a weather app under /Users/me/code/weather, with files
 * per language for the viewer) for the files and Git pages; the rest of the mock's data as usual.
 */
object FilesMockHub {
    private val url: String by lazy {
        val port = ServerSocket(0).use { it.localPort }
        val repo = generateSequence(File("").absoluteFile) { it.parentFile }.first { File(it, "scripts/mock/mock_hub.py").exists() }
        val pb = ProcessBuilder("python3", "scripts/mock/mock_hub.py").directory(repo).redirectErrorStream(true)
        pb.environment()["PORT"] = port.toString()
        pb.environment()["MOCK_FILES"] = "1"
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
        }
        h
    }
}

/**
 * The pages converted to the design system after the main screens (pairing, 界面风格, voice, files and Git, the
 * full-screen page, the image viewer's and the link sheet's controls), light and dark. Data a page loads itself is
 * fetched from the mock first and handed in, since a snapshot is one frame.
 */
class PagesShots {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6, theme = "android:Theme.Material.Light.NoActionBar", maxPercentDifference = 100.0)

    private val hub get() = FilesMockHub.hub
    private val mac = "macbook"
    private val repo = "/Users/me/code/weather"

    private fun both(name: String, content: @Composable () -> Unit) {
        for (dark in listOf(false, true)) paparazzi.snapshot(name + if (dark) "-dark" else "") { ShotHost(dark) { content() } }
    }

    @Test fun pair() = both("pair") { PairScreen() }

    @Test fun pairReason() = both("pair-reason") {
        PairScreen(reason = "hub 不认这台手机的 token 了（在别的设备上撤销了，或者换了 hub），重新配对一下")
    }

    @Test fun style() = both("style") { StylePickerScreen(onBack = {}) }

    private val plugin = Plugin(id = "voice", name = "语音", kind = "voice", status = "running")

    @Test fun voiceNoMic() = both("voice-nomic") { VoiceScreen(hub, plugin, onBack = {}, preview = VoicePreview.NoMic(denied = false)) }

    @Test fun voiceLive() {
        val state = VoiceState(
            phase = VoiceState.Phase.LIVE, speaking = true,
            lines = listOf(
                Line("1", user = true, text = "明天上海会下雨吗？", final = true),
                Line("2", user = false, text = "明天上海白天多云，傍晚以后有阵雨，降雨概率七成。出门记得带伞。", final = true),
                Line("3", user = true, text = "那后天呢", final = false),
            ),
        )
        both("voice-live") { VoiceScreen(hub, plugin, onBack = {}, preview = VoicePreview.Live(state)) }
    }

    @Test fun voiceEnded() {
        val state = VoiceState(phase = VoiceState.Phase.ENDED, error = "语音插件断开了：连接被对方关闭",
            lines = listOf(Line("1", user = true, text = "好的，谢谢", final = true)))
        both("voice-ended") { VoiceScreen(hub, plugin, onBack = {}, preview = VoicePreview.Live(state)) }
    }

    // The browser's own calls may answer before the snapshot: what is handed in is what they answer (the repository
    // is the target), so the shot is the same either way.
    @Test fun filesBrowser() {
        val listing = runBlocking { hub.api.listDir(mac, repo) }
        val git = runBlocking { hub.api.gitStatus(mac, repo) }
        both("files-browser") {
            FilesScreen(
                hub, FilesTarget(mac, repo, mode = FilesTarget.Mode.DIR), onBack = {}, onOpenImage = {},
                preload = FilesPreload(listings = mapOf(repo to listing), git = git),
            )
        }
    }

    @Test fun filesBrowserDeep() {
        val here = "$repo/Sources/App"
        val listing = runBlocking { hub.api.listDir(mac, here) }
        val git = runBlocking { hub.api.gitStatus(mac, repo) }
        both("files-browser-deep") {
            FilesScreen(
                hub, FilesTarget(mac, repo, mode = FilesTarget.Mode.DIR), onBack = {}, onOpenImage = {},
                preload = FilesPreload(listings = mapOf(here to listing), git = git, here = here),
            )
        }
    }

    @Test fun filesEmpty() {
        val here = "$repo/empty"
        val git = runBlocking { hub.api.gitStatus(mac, repo) }
        both("files-empty") {
            FilesScreen(
                hub, FilesTarget(mac, here, mode = FilesTarget.Mode.DIR), onBack = {}, onOpenImage = {},
                preload = FilesPreload(listings = mapOf(here to FsListing(path = here)), git = git),
            )
        }
    }

    @Test fun filesNotRepo() {
        val path = "/Users/me/notes"
        both("files-git-none") {
            FilesScreen(hub, FilesTarget(mac, path, mode = FilesTarget.Mode.GIT), onBack = {}, onOpenImage = {}, preload = FilesPreload(git = GitStatus(isRepo = false)))
        }
    }

    @Test fun filesGit() {
        val git = runBlocking { hub.api.gitStatus(mac, repo) }
        both("files-git") {
            FilesScreen(hub, FilesTarget(mac, repo, mode = FilesTarget.Mode.GIT), onBack = {}, onOpenImage = {}, preload = FilesPreload(git = git))
        }
    }

    @Test fun fileCode() {
        val file = runBlocking { hub.api.readFile(mac, "$repo/Sources/App.swift") }
        both("file-code") {
            FilesScreen(hub, FilesTarget(mac, file.path, mode = FilesTarget.Mode.FILE), onBack = {}, onOpenImage = {}, preload = FilesPreload(file = file))
        }
    }

    @Test fun fileMarkdown() {
        val file = runBlocking { hub.api.readFile(mac, "$repo/README.md") }
        both("file-markdown") {
            FilesScreen(hub, FilesTarget(mac, file.path, mode = FilesTarget.Mode.FILE), onBack = {}, onOpenImage = {}, preload = FilesPreload(file = file))
        }
    }

    @Test fun gitDiff() {
        val git = runBlocking { hub.api.gitStatus(mac, repo) }
        val f = git.files.first { it.isStaged && it.hasUnstaged }
        val diff = runBlocking { hub.api.gitDiff(mac, repo, f.path, false) }
        both("git-diff") { GitDiffPage(hub, mac, repo, f, initiallyStaged = false, onBack = {}, initial = diff) }
    }

    @Test fun gitCommit() {
        val git = runBlocking { hub.api.gitStatus(mac, repo) }
        val c = git.commits.first()
        val show = runBlocking { hub.api.gitShow(mac, repo, c.hash) }
        both("git-commit") { GitCommitPage(hub, mac, repo, c, onBack = {}, onFile = {}, initial = show) }
    }

    @Test fun fullScreenPage() {
        val text = (1..60).joinToString("\n") { "2026-10-31T09:%02d:00Z INFO  request id=%04d path=/v1/forecast status=200".format(it % 60, it) }
        both("fullscreen-page") {
            FullScreenPageBody("工具输出", onBack = {}, actions = { IconAction(JIcons.copy, "拷贝全文", {}) }) { pad ->
                Box(Modifier.fillMaxSize().padding(pad).background(JTheme.colors.codeBackground)) {
                    MonoLines(numberedLines(text), Modifier.fillMaxSize())
                }
            }
        }
    }

    @Test fun imageViewer() = both("image-viewer") {
        // The viewer is always dark (ImageViewerScreen sets it); a gradient stands in for the picture.
        JarvisTheme(dark = true) {
            Box(Modifier.fillMaxSize().background(Color.Black), contentAlignment = Alignment.Center) {
                Box(Modifier.fillMaxWidth().height(280.dp).background(Brush.linearGradient(listOf(Color(0xFFFFC371), Color(0xFFFF5F6D), Color(0xFF5B4FD6)))))
                ViewerChrome(index = 1, count = 3, saving = false, ready = true, canEdit = true, onClose = {}, onSave = {}, onEdit = {}, onShare = {}, onCopy = {})
            }
        }
    }

    @Test fun linkSheet() = both("link-sheet") {
        Column(Modifier.fillMaxSize().padding(top = 24.dp).clip(RoundedCornerShape(topStart = 22.dp, topEnd = 22.dp)).background(JTheme.colors.card)) {
            LinkSheetTop(
                title = "天气预报 · 未来 15 天", host = "weather.example.com", canGoBack = true, progress = 0.45f, drag = Modifier,
                onBack = {}, onCopy = {}, onBrowser = {}, onShare = {}, onClose = {},
            )
            Box(Modifier.fillMaxSize().background(JTheme.colors.page), contentAlignment = Alignment.Center) {
                Text("（网页）", color = JTheme.colors.tertiaryText)
            }
        }
    }

    @Test fun linkSheetFirstPage() = both("link-sheet-first") {
        Column(Modifier.fillMaxSize().padding(top = 24.dp).clip(RoundedCornerShape(topStart = 22.dp, topEnd = 22.dp)).background(JTheme.colors.card)) {
            LinkSheetTop(
                title = "weather.example.com", host = "weather.example.com", canGoBack = false, progress = null, drag = Modifier,
                onBack = {}, onCopy = {}, onBrowser = {}, onShare = {}, onClose = {},
            )
            Box(Modifier.fillMaxSize().background(JTheme.colors.page))
        }
    }

    /** Right after pairing: which engine the hub's machine uses (the hub's engines as listed, one must be chosen). */
    @Test fun engine() {
        val main = MockHub.hub
        val m = main.machines.value.first()
        both("engine-pairing") { jarvis.android.ui.machines.EngineScreen(main, m.id, onDone = {}, pairing = true) }
    }

    /** 设置 › AI › 模型配置: the purposes by group, what each uses. */
    @Test fun modelConfig() {
        val main = MockHub.hub
        val body = kotlinx.coroutines.runBlocking { main.api.modelConfiguration() }
        both("model-config") { jarvis.android.ui.settings.ModelConfigurationScreen(main, onBack = {}, onPurpose = {}, preloaded = body) }
    }
}
