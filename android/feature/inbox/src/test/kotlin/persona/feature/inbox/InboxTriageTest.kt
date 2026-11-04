package persona.feature.inbox

import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import persona.core.contracts.JsonCodec
import org.junit.Assert.*
import org.junit.Test

// New items waiting for triage (#1773): the banner's words and its 去配置, the same as on the web and iOS.
class InboxTriageTest {
    private fun triage(state: String, waiting: Int, reason: String? = null, vararg sources: InboxTriageSource) =
        InboxTriage(state, reason, waiting, sources.toList())

    private val lark = InboxTriageSource("lark", "飞书", "unconfigured", waiting = 3)
    private val wechat = InboxTriageSource("wechat-connector", "微信", "behind", waiting = 2)

    @Test fun readsTheHubsTriageAndNothingFromAnOlderHub() {
        val body = JsonCodec.parseToJsonElement("""{"items":[],"triage":{"state":"unconfigured","waiting":3,
            "sources":[{"source":"lark","name":"飞书","state":"unconfigured","waiting":3,"later":1}]}}""").jsonObject
        assertEquals(triage("unconfigured", 3, null, lark), InboxTriage.decode(body["triage"]))
        // An older hub sends none; anything that is not an object is none too.
        assertNull(InboxTriage.decode(null))
        assertNull(InboxTriage.decode(JsonPrimitive("ok")))
        assertNull(InboxTriageText.banner(null))
    }

    @Test fun oneSourceIsNamed() {
        assertEquals(InboxTriageBanner("有 3 条飞书消息在等分拣：还没配分拣模型", configure = true),
            InboxTriageText.banner(triage("unconfigured", 3, null, lark)))
        // A source without a name says only how many.
        assertEquals("有 3 条消息在等分拣：还没配分拣模型", InboxTriageText.banner(triage("unconfigured", 3, null, lark.copy(name = " ")))?.text)
    }

    @Test fun severalSourcesAreNot() {
        assertEquals(InboxTriageBanner("有 5 条消息在等分拣：新消息太多，正在排队", configure = false),
            InboxTriageText.banner(triage("behind", 5, null, wechat, lark.copy(state = "behind"))))
        // A source with nothing waiting does not count: the one that waits is named.
        assertEquals("有 2 条微信消息在等分拣：新消息太多，正在排队",
            InboxTriageText.banner(triage("behind", 2, null, wechat, lark.copy(waiting = 0)))?.text)
        // No sources listed: how many, without a name.
        assertEquals("有 4 条消息在等分拣：分拣模型出错了", InboxTriageText.banner(triage("error", 4))?.text)
    }

    @Test fun theHubsReasonWinsElseTheStatesWords() {
        assertEquals("有 3 条飞书消息在等分拣：模型没有额度",
            InboxTriageText.banner(triage("error", 3, "  模型没有额度 ", lark.copy(state = "error")))?.text)
        mapOf("unconfigured" to "还没配分拣模型", "paused" to "分拣模型暂停了", "error" to "分拣模型出错了",
            "timeout" to "分拣模型太慢了", "behind" to "新消息太多，正在排队").forEach { (state, words) ->
            assertEquals(state, "有 1 条消息在等分拣：$words", InboxTriageText.banner(triage(state, 1, ""))?.text)
        }
        // A state this app does not know, without a reason: only how many wait.
        assertEquals("有 1 条消息在等分拣", InboxTriageText.banner(triage("throttled", 1))?.text)
    }

    @Test fun nothingWhenTriageIsOkOrNothingWaits() {
        assertNull(InboxTriageText.banner(triage("ok", 0)))
        assertNull(InboxTriageText.banner(triage("ok", 3)))
        assertNull(InboxTriageText.banner(triage("unconfigured", 0, null, lark.copy(waiting = 0))))
        assertNull(InboxTriageText.banner(triage("", 3)))
    }

    @Test fun onlyAModelNotSetUpOffersToConfigureIt() {
        assertTrue(InboxTriageText.banner(triage("unconfigured", 1))!!.configure)
        listOf("paused", "error", "timeout", "behind").forEach { assertFalse(it, InboxTriageText.banner(triage(it, 1))!!.configure) }
        assertEquals("inbox.triage", InboxTriage.Purpose)
    }
}
