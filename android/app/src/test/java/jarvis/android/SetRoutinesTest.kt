package jarvis.android

import jarvis.android.data.ApiClient
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.ServerSocket
import kotlin.concurrent.thread

/** What setRoutines actually puts on the wire (method, path, body), against a one-shot local server. */
class SetRoutinesTest {
    @Test fun putsTheRoutineList() {
        val server = ServerSocket(0)
        var request = ""
        val t = thread {
            server.accept().use { c ->
                val r = BufferedReader(InputStreamReader(c.getInputStream()))
                val head = generateSequence { r.readLine() }.takeWhile { it.isNotEmpty() }.toList()
                val len = head.firstOrNull { it.startsWith("Content-Length:", true) }?.substringAfter(":")?.trim()?.toInt() ?: 0
                val body = CharArray(len).also { r.read(it) }.concatToString()
                request = head.first() + "\n" + body
                val out = """{"routines":[{"id":"a","title":"t","prompt":"p","time":"21:45","enabled":true}]}"""
                c.getOutputStream().write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${out.toByteArray().size}\r\n\r\n$out".toByteArray())
            }
        }
        val api = ApiClient("http://127.0.0.1:${server.localPort}", "tok")
        val got = runBlocking {
            api.setRoutines(listOf(
                ApiClient.Routine(id = "a", title = "t", prompt = "p", time = "21:45", enabled = true, nextRun = "x"),
                // Every edited field equal to its default: none may be dropped (09:00 is the time's default).
                ApiClient.Routine(id = "b", title = "早报", prompt = "p", time = "09:00", weekdays = listOf(1, 2, 3, 4, 5)),
            ))
        }
        t.join(5000)
        server.close()
        println(request)
        assertTrue(request, request.startsWith("PUT /api/v1/assistant/routines "))
        assertTrue(request, "\"time\":\"21:45\"" in request)
        // Default-valued fields still go out: the hub reads a missing enabled as off, missing weekdays as every day.
        assertTrue(request, "\"enabled\":true" in request)
        assertTrue(request, "\"weekdays\":[]" in request)
        assertTrue(request, "next_run" !in request) // the hub's own fields aren't sent back
        assertTrue(request, "\"time\":\"09:00\"" in request)
        assertEquals("21:45", got.single().time)
    }
}
