package persona.feature.chat

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.PlanTask
import persona.core.contracts.ServerEvent
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.designsystem.PersonaTheme
import persona.core.domain.tasks.AssistantExtra
import persona.core.domain.tasks.TasksStore

/** A started plan names each task that did not start with its reason, and says the reason is unknown when it is blank (#4498). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class PlanCardFailureReasonTest {
    @get:Rule val rule = createComposeRule()

    private val plan = TaskPlan("plan1", "main", listOf(
        PlanTask("写发布说明", machine = "mac", cwd = "/w/shop", error = " \n "),
        PlanTask("看崩溃报告", machine = "mac", cwd = "/w/app", error = "  launcher not found\n"),
        PlanTask("整理待办", machine = "mac", cwd = "/w/app"),
    ), state = "started")

    private class Api(val plan: TaskPlan) : TasksApi {
        override suspend fun todos() = emptyList<Todo>()
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList(), mapOf(plan.id to plan))
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
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject): Boolean = true
    }

    @Test fun aBlankReasonReadsUnknownAndARealOneStaysTrimmed() {
        val store = TasksStore(Api(plan), Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.load("main")
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                val host = AssistantTaskHost(store, "main", emptyList(), { it }, {}, {}, {})
                AssistantExtraLine(AssistantExtra.Feed(FeedItem("f1", "main", kind = "plan", ref = "plan1")), host)
            }
        }
        rule.onNodeWithText("「写发布说明」没开起来：原因不明").assertExists()
        rule.onNodeWithText("「看崩溃报告」没开起来：launcher not found").assertExists()
        rule.onNodeWithText("「整理待办」", substring = true).assertDoesNotExist()
    }
}
