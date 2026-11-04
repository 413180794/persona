package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.*
import org.junit.Test

class HubSessionFilesTest {
    @Test fun fileAndGitRequestsEncodePathsAndAcceptEmptyAndPartialResponses() = runBlocking {
        val requests = mutableListOf<String>()
        var body = """{"path":"/workspace/a b#%.kt","name":"a b#%.kt","text":"val a = 1","future":true}"""
        var status = 200
        val api = HubSessionFiles({ path ->
            requests += path
            Response.Builder().request(Request.Builder().url("https://example.com$path").build())
                .protocol(Protocol.HTTP_1_1).code(status).message("response").body(body.toResponseBody()).build()
        })
        assertEquals("val a = 1", api.file("machine/segment", "/workspace/中文 &", "/workspace/a b#%.kt").text)
        val url = ("https://example.com" + requests.last()).toHttpUrl()
        assertEquals("/api/v1/machines/machine%2Fsegment/file", url.encodedPath)
        assertEquals("/workspace/中文 &", url.queryParameter("cwd"))
        assertEquals("/workspace/a b#%.kt", url.queryParameter("path"))

        body = """{"is_repo":true,"files":null,"commits":null}"""
        assertTrue(api.status("machine", "/workspace").files.isEmpty())
        body = """{"path":"/workspace","entries":null,"truncated":true}"""
        assertTrue(api.directory("machine", "/workspace", "/workspace").truncated)
        body = """{"path":"a.kt","diff":"+new","truncated":true}"""
        assertTrue(api.diff("machine", "/workspace", "a.kt", true).truncated)
        assertEquals("1", ("https://example.com" + requests.last()).toHttpUrl().queryParameter("staged"))
        body = """{"commits":null}"""
        assertTrue(api.log("machine", "/workspace", 30).commits.isEmpty())
        assertEquals("30", ("https://example.com" + requests.last()).toHttpUrl().queryParameter("skip"))
        body = """{"commit":{"hash":"abc"},"files":null,"diff":""}"""
        assertEquals("abc", api.show("machine", "/workspace", "abc").commit.hash)
        body = """{"stats":[{"path":"src/a.kt","kind":"file","size":12},{"path":"~/x y/","kind":"dir"},{"path":"/gone/a.md"}]}"""
        val stats = api.stat("machine", "/workspace", listOf("src/a.kt", "~/x y/", "/gone/a.md")).stats
        assertEquals(listOf("file", "dir", "missing"), stats.map { it.kind })
        assertEquals(12L, stats.first().size)
        val statUrl = ("https://example.com" + requests.last()).toHttpUrl()
        assertEquals("/api/v1/machines/machine/stat", statUrl.encodedPath)
        assertEquals("src/a.kt\n~/x y/\n/gone/a.md", statUrl.queryParameter("paths"))
        assertEquals("/workspace", statUrl.queryParameter("cwd"))
        status = 404
        body = """{"error":"not found","code":"not_found"}"""
        try { api.file("machine", "/workspace", "missing"); fail("Expected HTTP failure") }
        catch (failure: HubFailure) { assertEquals(404, failure.status) }
    }

    @Test fun downloadStreamsTheWholeFileIntoPlace(): Unit = runBlocking {
        val requests = mutableListOf<String>()
        var status = 200
        val bytes = ByteArray(300_000) { (it % 251).toByte() }
        val respond: suspend (String) -> Response = { path ->
            requests += path
            Response.Builder().request(Request.Builder().url("https://example.com$path").build()).protocol(Protocol.HTTP_1_1)
                .code(status).message("response").body(if (status == 200) bytes.toResponseBody() else """{"code":"not_found"}""".toResponseBody()).build()
        }
        val api = HubSessionFiles({ error("buffered request used for a download") }, stream = respond)
        val dir = kotlin.io.path.createTempDirectory("dl").toFile()
        val into = java.io.File(dir, "a/song.mp3")
        assertEquals(300_000L, api.download("m", "/w", "music/song a.mp3", into))
        assertArrayEquals(bytes, into.readBytes())
        val url = ("https://example.com" + requests.last()).toHttpUrl()
        assertEquals("/api/v1/machines/m/file/download", url.encodedPath)
        assertEquals("music/song a.mp3", url.queryParameter("path"))
        assertEquals("/w", url.queryParameter("cwd"))

        status = 404
        val missing = java.io.File(dir, "missing.mp3")
        try { api.download("m", "/w", "missing.mp3", missing); fail("Expected HTTP failure") }
        catch (failure: HubFailure) { assertEquals(404, failure.status) }
        assertFalse(missing.exists())
        assertEquals(listOf("song.mp3"), java.io.File(dir, "a").list()!!.toList())
        dir.deleteRecursively()
    }
}
