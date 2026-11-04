package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

/** 重试 and 移除 on a task (#1396): the hub's routes, the todo it answers, and its refusal kept for the user. */
class HttpTasksApiTest {
    private fun api(server: MockWebServer): HttpTasksApi {
        val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
        return HttpTasksApi { path, method, body ->
            val rb = body?.toString()?.toRequestBody("application/json".toMediaType())
            http.newCall(Request.Builder().url(server.url(path)).method(method, rb).build()).execute()
        }
    }

    @Test fun retryPostsAndAnswersTheReopenedTodo() = runBlocking {
        MockWebServer().use { server ->
            server.enqueue(MockResponse().setBody("""{"todo":{"id":"todo_1","title":"设置页按钮对齐","origin":"c","plan_id":"p","agents":[]}}"""))
            val todo = api(server).retryTask("todo_1")
            val req = server.takeRequest()
            assertEquals("POST", req.method)
            assertEquals("/api/v1/todos/todo_1/retry", req.path)
            assertEquals("todo_1", todo.id)
            assertNull(todo.doneAt)
            assertNull(todo.startError)
        }
    }

    @Test fun anOfflineMachineIsSaidInTheHubsWords() = runBlocking {
        MockWebServer().use { server ->
            server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"「公司 Mac」现在离线，等它连上再重试"}"""))
            try {
                api(server).retryTask("todo_1")
                fail("a refused retry answered")
            } catch (e: HubFailure) {
                assertEquals(409, e.status)
                assertTrue(e.message.orEmpty(), e.message.orEmpty().contains("现在离线"))
            }
        }
    }

    @Test fun aCardsReplyNamesTheCard() = runBlocking {
        MockWebServer().use { server ->
            server.enqueue(MockResponse().setBody("""{"id":"example:1","done_by":"replied"}"""))
            api(server).inboxReply("example:1", "修好了", "feed_1")
            val req = server.takeRequest()
            assertEquals("POST", req.method)
            assertTrue(req.path.orEmpty(), req.path.orEmpty().endsWith("/reply"))
            assertEquals("""{"text":"修好了","card":"feed_1"}""", req.body.readUtf8())
        }
    }

    @Test fun removeDeletesTheTodo() = runBlocking {
        MockWebServer().use { server ->
            server.enqueue(MockResponse().setBody("""{"ok":true}"""))
            api(server).removeTask("todo_1")
            val req = server.takeRequest()
            assertEquals("DELETE", req.method)
            assertEquals("/api/v1/todos/todo_1", req.path)
        }
    }
}
