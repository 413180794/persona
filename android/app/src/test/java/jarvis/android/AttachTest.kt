package jarvis.android

import jarvis.android.data.ApiClient
import jarvis.android.data.ImagePayload
import jarvis.android.data.MAX_SIDE
import jarvis.android.data.SessionKey
import jarvis.android.data.quotedPath
import jarvis.android.data.scaleFor
import jarvis.android.data.withPaths
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.BufferedInputStream
import java.net.ServerSocket
import kotlin.concurrent.thread

class AttachTest {
    @Test fun scaling() {
        assertEquals(1f, scaleFor(1200, 800))
        assertEquals(MAX_SIDE.toFloat() / 4000, scaleFor(3000, 4000))
        assertEquals(1f, scaleFor(0, 0))
    }

    @Test fun pathQuotingMatchesIos() {
        assertEquals("/tmp/a.log", quotedPath("/tmp/a.log"))
        assertEquals("'/tmp/my file.txt'", quotedPath("/tmp/my file.txt"))
        assertEquals("'/tmp/it'\\''s.txt'", quotedPath("/tmp/it's.txt")) // shell-safe single quote
    }

    @Test fun pathsGoAtTheEndSpaced() {
        assertEquals("/tmp/a ", withPaths("", listOf("/tmp/a")))
        assertEquals("看下 /tmp/a '/tmp/b c' ", withPaths("看下", listOf("/tmp/a", "/tmp/b c")))
        assertEquals("看下 /tmp/a ", withPaths("看下 ", listOf("/tmp/a")))
    }

    /** One request against a throwaway local server: its request line, headers and body. */
    private fun capture(respond: String, call: (ApiClient) -> Unit): Triple<String, Map<String, String>, ByteArray> {
        val server = ServerSocket(0)
        var line = ""; var headers = mapOf<String, String>(); var body = ByteArray(0)
        val t = thread {
            server.accept().use { c ->
                val inp = BufferedInputStream(c.getInputStream())
                fun readLine(): String { val sb = StringBuilder(); while (true) { val b = inp.read(); if (b < 0 || b == '\n'.code) break; if (b != '\r'.code) sb.append(b.toChar()) }; return sb.toString() }
                line = readLine()
                val h = mutableMapOf<String, String>()
                while (true) { val l = readLine(); if (l.isEmpty()) break; h[l.substringBefore(":").lowercase()] = l.substringAfter(":").trim() }
                headers = h
                body = inp.readNBytes(h["content-length"]?.toInt() ?: 0)
                c.getOutputStream().write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${respond.toByteArray().size}\r\n\r\n$respond".toByteArray())
            }
        }
        call(ApiClient("http://127.0.0.1:${server.localPort}", "tok"))
        t.join(5000); server.close()
        return Triple(line, headers, body)
    }

    @Test fun messageCarriesImages() {
        val (line, _, body) = capture("{}") { api ->
            runBlocking { api.send(SessionKey("mac", "s1"), "看图", "m1", images = listOf(ImagePayload(mime = "image/jpeg", data = "QUJD"))) }
        }
        val b = body.decodeToString()
        assertTrue(line, line.startsWith("POST /api/v1/sessions/mac/s1/messages "))
        assertTrue(b, "\"images\":[{\"data\":\"QUJD\"}]" in b || ("\"data\":\"QUJD\"" in b && "image/jpeg" in b))
        // No images: no images key (an older node would choke on an empty list for nothing).
        val (_, _, plain) = capture("{}") { api -> runBlocking { api.send(SessionKey("mac", "s1"), "hi", "m2") } }
        assertTrue("images" !in plain.decodeToString())
    }

    @Test fun uploadStreamsTheFileWithItsName() {
        val bytes = ByteArray(300_000) { (it % 251).toByte() }
        var got: ApiClient.Uploaded? = null
        val (line, headers, body) = capture("""{"path":"/tmp/report-20261031-090000.pdf","name":"report.pdf","size":300000}""") { api ->
            got = runBlocking { api.upload("mac", "report 1.pdf", bytes.size.toLong()) { bytes.inputStream() } }
        }
        assertTrue(line, line.startsWith("POST /api/v1/machines/mac/files?name=report%201.pdf "))
        assertEquals("300000", headers["content-length"])
        assertEquals("Bearer tok", headers["authorization"])
        assertTrue(body.contentEquals(bytes))
        assertEquals("/tmp/report-20261031-090000.pdf", got?.path)
    }
}
