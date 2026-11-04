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

    /** An answered record has every question with its answer in full (#1231); one from before it shows its line. */
    @Test fun answeredItemsInFullOrTheLine() {
        val res = JsonCodec.decodeFromString<FeedResponse>("""{"items":[
            {"id":"f1","conversation":"c","at":"2026-11-03T10:00:00Z","kind":"answered","ref":"t",
             "body":{"question":"先补哪一块？","answer":"重试；假服务器","items":[{"question":"先补哪一块？","answer":"重试\n和超时"},
               {"question":"请求怎么处理？","answer":"假服务器"},{"question":"","answer":""},"x"]}},
            {"id":"f2","conversation":"c","at":"2026-11-03T10:00:00Z","kind":"answered","body":{"question":"使用 Bash","answer":"批准了"}},
            {"id":"f3","conversation":"c","at":"2026-11-03T10:00:00Z","kind":"answered","body":{"items":[]}}]}""")
        assertEquals(listOf(AnsweredItem("先补哪一块？", "重试\n和超时"), AnsweredItem("请求怎么处理？", "假服务器")), res.items[0].answeredItems())
        assertEquals("重试；假服务器", res.items[0].field("answer"))
        assertEquals(listOf(AnsweredItem("使用 Bash", "批准了")), res.items[1].answeredItems())
        assertTrue(res.items[2].answeredItems().isEmpty())
    }
}
