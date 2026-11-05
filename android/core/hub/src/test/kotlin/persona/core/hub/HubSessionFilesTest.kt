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
        assertEquals("val a = 1", (api.file("machine/segment", "/workspace/中文 &", "/workspace/a b#%.kt").text).zhAny())
        val url = ("https://example.com" + requests.last()).toHttpUrl()
        assertEquals("/api/v1/machines/machine%2Fsegment/file", (url.encodedPath).zhAny())
        assertEquals("/workspace/中文 &", (url.queryParameter("cwd")).zhAny())
        assertEquals("/workspace/a b#%.kt", (url.queryParameter("path")).zhAny())

        body = """{"is_repo":true,"files":null,"commits":null}"""
        assertTrue(api.status("machine", "/workspace").files.isEmpty())
        body = """{"path":"/workspace","entries":null,"truncated":true}"""
        assertTrue(api.directory("machine", "/workspace", "/workspace").truncated)
        body = """{"path":"a.kt","diff":"+new","truncated":true}"""
        assertTrue(api.diff("machine", "/workspace", "a.kt", true).truncated)
        assertEquals("1", (("https://example.com" + requests.last()).toHttpUrl().queryParameter("staged")).zhAny())
        body = """{"commits":null}"""
        assertTrue(api.log("machine", "/workspace", 30).commits.isEmpty())
        assertEquals("30", (("https://example.com" + requests.last()).toHttpUrl().queryParameter("skip")).zhAny())
        body = """{"commit":{"hash":"abc"},"files":null,"diff":""}"""
        assertEquals("abc", (api.show("machine", "/workspace", "abc").commit.hash).zhAny())
        body = """{"stats":[{"path":"src/a.kt","kind":"file","size":12},{"path":"~/x y/","kind":"dir"},{"path":"/gone/a.md"}]}"""
        val stats = api.stat("machine", "/workspace", listOf("src/a.kt", "~/x y/", "/gone/a.md")).stats
        assertEquals(listOf("file", "dir", "missing"), (stats.map { it.kind }).zhAny())
        assertEquals(12L, stats.first().size)
        val statUrl = ("https://example.com" + requests.last()).toHttpUrl()
        assertEquals("/api/v1/machines/machine/stat", (statUrl.encodedPath).zhAny())
        assertEquals("src/a.kt\n~/x y/\n/gone/a.md", (statUrl.queryParameter("paths")).zhAny())
        assertEquals("/workspace", (statUrl.queryParameter("cwd")).zhAny())
        status = 404
        body = """{"error":"not found","code":"not_found"}"""
        try { api.file("machine", "/workspace", "missing"); fail("Expected HTTP failure") }
        catch (failure: HubFailure) { assertEquals(404, failure.status) }
    }

    @Test fun pullPostsTheFolderAndDecodesTheNewStatus(): Unit = runBlocking {
        val posts = mutableListOf<Pair<String, String>>()
        var status = 200
        var answer = """{"status":{"is_repo":true,"branch":"main","upstream":"origin/main","behind":0,"commits":[{"hash":"fff","subject":"更新示例代码"}],"files":null},"updated":true,"future":1}"""
        val api = HubSessionFiles({ error("GET used for a pull") }, post = { path, body ->
            posts += path to body
            Response.Builder().request(Request.Builder().url("https://example.com$path").build()).protocol(Protocol.HTTP_1_1)
                .code(status).message("response").body(answer.toResponseBody()).build()
        })
        val pulled = api.pull("machine/segment", "/workspace/中文 &")
        assertTrue(pulled.updated)
        assertEquals("main", pulled.status.branch)
        assertEquals(listOf("fff"), pulled.status.commits.map { it.hash })
        assertEquals("/api/v1/machines/machine%2Fsegment/git/pull", posts.last().first)
        assertEquals("""{"cwd":"/workspace/中文 &"}""", posts.last().second)

        answer = """{"status":{"is_repo":true}}"""
        assertFalse(api.pull("machine", "/workspace").updated)

        status = 502
        answer = """{"error":"工作区有未提交的改动，请先在电脑上提交或 stash 后再拉取"}"""
        try { api.pull("machine", "/workspace"); fail("Expected HTTP failure") }
        catch (failure: HubFailure) {
            assertEquals(502, failure.status)
            assertEquals("工作区有未提交的改动，请先在电脑上提交或 stash 后再拉取", failure.message)
        }
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
        assertEquals("/api/v1/machines/m/file/download", (url.encodedPath).zhAny())
        assertEquals("music/song a.mp3", (url.queryParameter("path")).zhAny())
        assertEquals("/w", (url.queryParameter("cwd")).zhAny())

        status = 404
        val missing = java.io.File(dir, "missing.mp3")
        try { api.download("m", "/w", "missing.mp3", missing); fail("Expected HTTP failure") }
        catch (failure: HubFailure) { assertEquals(404, failure.status) }
        assertFalse(missing.exists())
        assertEquals(listOf("song.mp3"), (java.io.File(dir, "a").list()!!.toList()).zhAny())
        dir.deleteRecursively()
    }
}
