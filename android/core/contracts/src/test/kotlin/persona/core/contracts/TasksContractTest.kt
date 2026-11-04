package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The unified entry's wire shapes (docs/protocol.md「任务和对话动态」) decode, and its events enter the stream. */
class TasksContractTest {
    @Test fun eventsDecode() {
        assertTrue(ServerEvent.decode("""{"type":"todos"}""") is TodosEvent)
        val feed = ServerEvent.decode("""{"type":"assistant_feed","conversation":"c1","item":{"id":"f1","conversation":"c1",
            "at":"2026-11-03T10:00:00Z","kind":"receipt","ref":"t1","body":{"title":"修登录崩溃","state":"done","pr":"https://github.com/o/r/pull/9"}}}""")
        val item = (feed as AssistantFeedEvent).item
        assertEquals("receipt", item.kind); assertEquals("修登录崩溃", item.field("title")); assertEquals("t1", item.ref)
        val plan = ServerEvent.decode("""{"type":"task_plan","plan":{"id":"p1","conversation":"c1","state":"open",
            "tasks":[{"title":"t","prompt":"p","machine":"box","cwd":"/code/app","readonly":true}]}}""")
        assertEquals("app", (plan as TaskPlanEvent).plan.tasks.single().project)
    }

    @Test fun unknownKindsAndTodosDecode() {
        val res = JsonCodec.decodeFromString<FeedResponse>("""{"items":[{"id":"f","conversation":"c","at":"2026-11-03T10:00:00Z","kind":"new_kind"}],
            "plans":{"p":{"id":"p","state":"started","tasks":[]}}}""")
        assertEquals("unknown", res.items.single().kind)
        assertEquals("started", res.plans.getValue("p").state)
        val todos = JsonCodec.decodeFromString<TodosResponse>("""{"todos":[{"id":"t","title":"x","origin":"c","plan_id":"p",
            "agents":[{"machine_id":"box","session_id":"s1"},{"machine_id":"box","session_id":"s0","released_at":"2026-11-03T09:00:00Z"}]}]}""")
        assertEquals(listOf("s1"), todos.todos.single().activeAgents.map { it.sessionId })
        val ix = JsonCodec.decodeFromString<Interaction>("""{"id":"ix","machine_id":"box","session_id":"s1","kind":"permission","task_title":"修登录崩溃"}""")
        assertEquals("修登录崩溃", ix.taskTitle)
    }
}
