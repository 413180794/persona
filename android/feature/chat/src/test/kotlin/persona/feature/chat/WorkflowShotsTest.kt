package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.BackgroundTask
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.ServerEvent
import persona.core.contracts.Session
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.TodoAgent
import persona.core.contracts.Workflow
import persona.core.contracts.WorkflowAgent
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.domain.tasks.AssistantExtra
import persona.core.domain.tasks.TasksStore
import java.io.File
import java.time.Instant

/** A Workflow run's card in an agent's details and its line on a task card (#2935), in every style and both appearances. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1100dp-xhdpi")
class WorkflowShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/workflow-shots").also { it.mkdirs() }

    private val now = Instant.now()
    private val audit = Workflow(taskId = "t1", runId = "wf_1", name = "parity-audit", status = "running", running = true,
        phases = listOf("Audit", "Verify"), phase = "Verify", phaseIndex = 2, agentsStarted = 185, agentsDone = 169, agentsRunning = 16,
        startedAt = now.minusSeconds(14 * 60 + 20).toString(), agents = listOf(
            WorkflowAgent("a1", "lead@a1", "audit web/settings", "Audit", running = false, lastText = "No gaps in the settings pages."),
            WorkflowAgent("a2", "lead@a2", "verify android/agents", "Verify", running = true, lastText = "Reading AgentsScreen.kt for the background count"),
            WorkflowAgent("a3", "lead@a3", "verify ios/watch", "Verify", running = true),
        ))
    private val killed = Workflow(taskId = "t0", runId = "wf_0", summary = "Check the parity of the three clients", status = "killed",
        phases = listOf("Audit", "Verify"), phase = "Audit", phaseIndex = 1, agentsStarted = 12, agentsDone = 7,
        startedAt = now.minusSeconds(7200).toString(), endedAt = now.minusSeconds(3300).toString(), error = "Error: Workflow aborted")
    private val stalled = Workflow(taskId = "t9", name = "nightly-triage", status = "stalled", agentsStarted = 4, agentsDone = 2,
        startedAt = now.minusSeconds(5400).toString())
    private val lead = Session(machineId = "mac", sessionId = "lead", title = "Audit the clients", cwd = "/w/persona", status = "idle",
        state = "working", agent = "claude", lastText = "Started parity-audit; I will report when it is done.",
        background = listOf(BackgroundTask("t1", "workflow", runId = "wf_1")), workflows = listOf(audit, killed))
    private val todo = Todo("todo1", "Audit the clients", createdAt = now.minusSeconds(1500).toString(), origin = "main",
        agents = listOf(TodoAgent("mac", "lead", cwd = "/w/persona")))

    private class Api(val todos: List<Todo>) : TasksApi {
        override suspend fun todos() = todos
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList(), emptyMap())
        override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("unused")
        override suspend fun dismissPlan(id: String): TaskPlan = error("unused")
        override suspend fun retryTask(todoId: String): Todo = error("unused")
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("unused")
        override suspend fun today(conversation: String): FeedItem = error("unused")
        override suspend fun mainConversation(): String? = "main"
        override suspend fun inboxReply(itemId: String, text: String, card: String?) = Unit
    }

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject): Boolean = true
    }

    @Test
    fun drawsTheRunCardsAndTheTaskLineInEveryStyleAndAppearance() {
        val store = TasksStore(Api(listOf(todo)), Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.reloadTodos()
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var opened: String? = null
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l),
                    verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                    WorkflowRunCard(audit, expanded = true, onToggle = {}, onOpenAgent = { opened = it })
                    WorkflowRunCard(killed, expanded = false, onToggle = {}, onOpenAgent = { opened = it })
                    WorkflowRunCard(stalled, expanded = false, onToggle = {}, onOpenAgent = null)
                    val host = AssistantTaskHost(store, "main", listOf(lead), { "Work laptop" }, {}, {}, {})
                    AssistantExtraLine(AssistantExtra.Feed(FeedItem("f1", "main", now.toString(), "task", "todo1")), host)
                }
            }
        }
        for (nextStyle in AppStyle.entries) for (nextDark in listOf(false, true)) {
            rule.runOnIdle { style = nextStyle; dark = nextDark }
            rule.onNodeWithText("parity-audit").assertIsDisplayed()
            rule.onNodeWithText("在跑").assertIsDisplayed()
            rule.onNodeWithText("14 分").assertIsDisplayed()
            rule.onNodeWithText("169/185 个 agent 完成 · 16 个在跑").assertIsDisplayed()
            rule.onNodeWithText("收起 agent").assertIsDisplayed()
            rule.onNodeWithText("已停止").assertIsDisplayed()
            rule.onNodeWithText("Error: Workflow aborted").assertIsDisplayed()
            rule.onNodeWithText("没动静了").assertIsDisplayed()
            rule.onNodeWithText("后台 workflow「parity-audit」 · Verify 2/2 · 169/185 · 14 分").assertIsDisplayed()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "workflow-${nextStyle.name.lowercase()}-${if (nextDark) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        // the running agents come first; tapping one opens it as a subagent's transcript
        rule.onNodeWithText("verify ios/watch").performClick()
        assertEquals("lead@a3", opened)
    }
}
