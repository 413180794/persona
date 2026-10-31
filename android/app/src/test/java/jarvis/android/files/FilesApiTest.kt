package jarvis.android.files

import jarvis.android.data.ApiClient
import jarvis.android.data.HubException
import jarvis.android.ui.files.gitDiff
import jarvis.android.ui.files.gitLog
import jarvis.android.ui.files.gitShow
import jarvis.android.ui.files.gitStatus
import jarvis.android.ui.files.listDir
import jarvis.android.ui.files.readFile
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

class FilesApiTest {
    private lateinit var server: MockWebServer
    private lateinit var api: ApiClient

    @Before fun setUp() {
        server = MockWebServer().apply { start() }
        api = ApiClient(server.url("/").toString(), "tok")
    }

    @After fun tearDown() { runCatching { server.shutdown() } }

    private fun json(body: String, code: Int = 200) =
        MockResponse().setResponseCode(code).setHeader("Content-Type", "application/json").setBody(body)

    @Test fun readFileSendsPathAndCwd() = runBlocking {
        server.enqueue(json("""{"path":"/r/a b.md","name":"a b.md","size":5,"mime":"text/markdown","text":"# hi","truncated":false,"is_dir":false}"""))
        val f = api.readFile("mac", "a b.md", cwd = "/r")
        assertEquals("# hi", f.text)
        val r = server.takeRequest()
        assertEquals("/api/v1/machines/mac/file", r.requestUrl!!.encodedPath)
        assertEquals("a b.md", r.requestUrl!!.queryParameter("path"))
        assertEquals("/r", r.requestUrl!!.queryParameter("cwd"))
        assertEquals("Bearer tok", r.getHeader("Authorization"))
    }

    @Test fun listDirLeavesOutAnEmptyCwd() = runBlocking {
        server.enqueue(json("""{"path":"/home/me","entries":[{"name":"code","dir":true,"size":96,"mtime":"2026-06-01T10:00:00Z"},{"name":".env","dir":false,"symlink":true,"size":3}],"truncated":true}"""))
        val l = api.listDir("mac", "~")
        assertEquals("/home/me", l.path)
        assertTrue(l.truncated)
        assertTrue(l.entries[0].dir)
        assertEquals(java.time.Instant.parse("2026-06-01T10:00:00Z"), l.entries[0].modified)
        assertTrue(l.entries[1].symlink)
        assertNull(l.entries[1].modified)
        val r = server.takeRequest()
        assertEquals("/api/v1/machines/mac/fs", r.requestUrl!!.encodedPath)
        assertEquals("~", r.requestUrl!!.queryParameter("path"))
        assertNull(r.requestUrl!!.queryParameter("cwd"))
    }

    @Test fun gitCallsUseTheirQueries() = runBlocking {
        server.enqueue(json("""{"is_repo":false,"files":[],"commits":[]}"""))
        server.enqueue(json("""{"path":"a.kt","diff":"@@ -1 +1 @@\n-a\n+b","truncated":false}"""))
        server.enqueue(json("""{"commit":{"hash":"h","short":"h","subject":"s","author":"a"},"files":[{"path":"a.kt","index":"M"}],"diff":"","truncated":true}"""))
        server.enqueue(json("""{"commits":[{"hash":"1"},{"hash":"2"}]}"""))
        assertEquals(false, api.gitStatus("mac", "~/code").isRepo)
        assertEquals("a.kt", api.gitDiff("mac", "/r", "a.kt", staged = true).path)
        assertTrue(api.gitShow("mac", "/r", "h").truncated)
        assertEquals(listOf("1", "2"), api.gitLog("mac", "/r", skip = 30).map { it.hash })

        val st = server.takeRequest().requestUrl!!
        assertEquals("/api/v1/machines/mac/git/status", st.encodedPath)
        assertEquals("~/code", st.queryParameter("cwd"))
        val d = server.takeRequest().requestUrl!!
        assertEquals("/api/v1/machines/mac/git/diff", d.encodedPath)
        assertEquals("1", d.queryParameter("staged"))
        assertEquals("a.kt", d.queryParameter("path"))
        val sh = server.takeRequest().requestUrl!!
        assertEquals("h", sh.queryParameter("hash"))
        val lg = server.takeRequest().requestUrl!!
        assertEquals("/api/v1/machines/mac/git/log", lg.encodedPath)
        assertEquals("30", lg.queryParameter("skip"))
        assertEquals("50", lg.queryParameter("limit"))
    }

    @Test fun nodeErrorsBecomeHubExceptions() = runBlocking {
        server.enqueue(json("""{"error":"open /x: no such file or directory"}""", 502))
        try {
            api.readFile("mac", "/x")
            fail("expected an error")
        } catch (e: HubException) {
            assertEquals(502, e.status)
            assertTrue(e.message.contains("no such file"))
        }
    }
}
