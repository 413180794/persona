package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.*
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.domain.tasks.*
import java.io.File

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
class TaskLaunchReconciliationShotsTest {
    @get:Rule val rule = createComposeRule()
    private val key = SessionKey("box", "original")
    private val task = Todo("task", title = "Verify task association", origin = "main",
        agents = listOf(TodoAgent("box", "original", title = "Original agent", cwd = "/code/app", agent = "codex")))
    private class Api(var task: Todo, var agent: Session?) : TasksApi {
        var retries = 0
        override suspend fun todos() = listOf(task)
        override suspend fun taskAgent(key: SessionKey) = agent
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList())
        override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("unused")
        override suspend fun dismissPlan(id: String): TaskPlan = error("unused")
        override suspend fun retryTask(todoId: String): Todo { retries++; return task }
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("unused")
        override suspend fun today(conversation: String): FeedItem = error("unused")
        override suspend fun mainConversation(): String? = "main"
        override suspend fun inboxReply(itemId: String, text: String, card: String?) = Unit
    }
    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }

    private fun verify(style: AppStyle, dark: Boolean) {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val api = Api(task.copy(agents = emptyList()), null)
        val connection = Connection()
        val store = TasksStore(api, connection, scope)
        store.reloadTodos()
        var opened: SessionKey? = null
        lateinit var sheet: MutableState<TaskSheet?>
        rule.mainClock.autoAdvance = false
        try {
            rule.setContent {
                val metadata by store.agentSessions.collectAsState()
                val todos by store.todos.collectAsState()
                LaunchedEffect(todos) { store.watchAgents(todos.flatMap { it.activeAgents }.map { it.key }.toSet()) }
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    sheet = remember { mutableStateOf<TaskSheet?>(TaskSheet.Task("task")) }
                    // The Agents window is filtered to a different computer. No original session is loaded there.
                    val host = AssistantTaskHost(store, "main", metadata.values.toList() + Session("other", "unrelated"), { it },
                        { opened = it }, { throw it }, {}, sheet = sheet)
                    Column { AssistantExtraLine(AssistantExtra.Feed(FeedItem("f", "main", kind = "task", ref = "task")), host) }
                    AssistantTaskSheets(host)
                }
            }
            rule.mainClock.advanceTimeBy(500)
            rule.onNodeWithText("正在开 Agent", useUnmergedTree = true).assertExists()
            // A late successful ACK arrives while the detail remains open.
            rule.runOnIdle { api.task = task; store.reloadTodos() }
            rule.mainClock.advanceTimeBy(500)
            rule.onNodeWithText("打开对话", useUnmergedTree = true).assertExists()
            rule.onAllNodesWithText("正在开 Agent", useUnmergedTree = true).assertCountEquals(0)
            capture("bound-outside-window", style, dark)
            rule.onNodeWithText("打开对话", useUnmergedTree = true).performClick()
            rule.runOnIdle { assertEquals(key, opened); sheet.value = TaskSheet.Task("task") }
            rule.mainClock.advanceTimeBy(500)
            rule.runOnIdle {
                api.agent = Session("box", "original", title = "Original agent", state = "working", activity = "python3 verify.py", rev = 12)
                scope.launch { connection.events.emit(SessionsListEvent("r12")) }
            }
            rule.mainClock.advanceTimeBy(500)
            rule.onAllNodesWithText("在做", useUnmergedTree = true).assertCountEquals(2)
            assertTrue(rule.onAllNodesWithText("python3 verify.py", substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
            capture("working-python", style, dark)
            rule.runOnIdle { api.task = task.copy(doneAt = "2026-01-01T00:00:00Z"); store.reloadTodos() }
            rule.mainClock.advanceTimeBy(500)
            rule.runOnIdle { api.task = task; store.reloadTodos() }
            rule.mainClock.advanceTimeBy(500)
            rule.onAllNodesWithText("在做", useUnmergedTree = true).assertCountEquals(2)
            capture("reopened-working", style, dark)
            rule.runOnIdle {
                api.task = task.copy(supervision = Supervision(supervisor = "main", state = "pending_review"))
                store.reloadTodos()
            }
            rule.mainClock.advanceTimeBy(500)
            rule.onAllNodesWithText("完成", useUnmergedTree = true).assertCountEquals(0)
            capture("pending-review", style, dark)
            rule.runOnIdle {
                api.task = task.copy(doneAt = "2026-01-01T00:00:00Z", doneBy = "review",
                    supervision = Supervision(supervisor = "main", state = "accepted", doneWhen = listOf("Verification passes"),
                        reviews = listOf(TaskReview(verdict = "accepted", checks = listOf(ReviewCheck("Verification passes", true, "Checks passed"))))))
                api.agent = api.agent!!.copy(state = "idle", activity = null, rev = 13)
                store.reloadTodos()
                scope.launch { connection.events.emit(SessionsListEvent("r13")) }
            }
            rule.mainClock.advanceTimeBy(500)
            rule.onAllNodesWithText("完成", useUnmergedTree = true).assertCountEquals(2)
            capture("accepted-done", style, dark)
            assertEquals(0, api.retries)
        } finally { scope.cancel() }
    }

    private fun capture(scene: String, style: AppStyle, dark: Boolean) {
        val directory = File("build/task-launch-shots").also { it.mkdirs() }
        val bitmap = rule.onNode(isDialog()).captureToImage().asAndroidBitmap()
        File(directory, "$scene-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream().use {
            assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
        }
    }

    @Test fun glassLight() = verify(AppStyle.Glass, false)
    @Test fun glassDark() = verify(AppStyle.Glass, true)
    @Test fun briefLight() = verify(AppStyle.Brief, false)
    @Test fun briefDark() = verify(AppStyle.Brief, true)
    @Test fun boardLight() = verify(AppStyle.Board, false)
    @Test fun boardDark() = verify(AppStyle.Board, true)
}
