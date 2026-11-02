package jarvis.android.shots

import jarvis.android.data.ApiClient
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import java.io.File
import java.net.ServerSocket

/**
 * scripts/mock/mock_hub.py on a free port for the screenshot tests (docs/testing.md「用 mock 数据截图」): made-up machines,
 * agents, conversations, interactions and an inbox. One per test JVM, stopped with it.
 */
object MockHub {
    val url: String by lazy { start() }

    private fun start(): String {
        val port = ServerSocket(0).use { it.localPort }
        val repo = generateSequence(File("").absoluteFile) { it.parentFile }.first { File(it, "scripts/mock/mock_hub.py").exists() }
        val pb = ProcessBuilder("python3", "scripts/mock/mock_hub.py").directory(repo).redirectErrorStream(true)
        pb.environment()["PORT"] = port.toString()
        val p = pb.start()
        Runtime.getRuntime().addShutdownHook(Thread { p.destroy() })
        val out = p.inputStream.bufferedReader()
        val deadline = System.currentTimeMillis() + 20_000
        while (System.currentTimeMillis() < deadline) {
            val line = out.readLine() ?: error("mock hub exited")
            if (line.startsWith("mock hub on")) break
        }
        Thread { runCatching { out.forEachLine { } } }.apply { isDaemon = true }.start()
        return "http://127.0.0.1:$port"
    }

    /** A hub connected to the mock with everything loaded (machines, agents, interactions, pins, inbox). */
    val hub: Hub by lazy {
        val h = Hub(ApiClient(url, "mock"), CoroutineScope(SupervisorJob() + Dispatchers.IO), "shots")
        h.start()
        runBlocking {
            withTimeout(20_000) { h.conn.first { it == ConnState.Online } }
            h.refresh()
            h.loadInbox()
        }
        h
    }
}
