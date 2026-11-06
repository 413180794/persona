package persona.core.domain

import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.Injected
import persona.core.contracts.Item

class OwnMessagesTest {
    private fun u(text: String, from: String? = null, clientMsgId: String? = null, injected: Injected? = null) =
        Item(id = "u#0", role = "user", kind = "text", text = text, from = from, clientMsgId = clientMsgId, injected = injected)

    private val notice = "🔔 任务「跑测试」做完了\n（这是 Persona 的任务通知，不是用户发的消息。）\n\nAgent 最后说的：\n好了"
    private val relay = "<persona-context kind=\"context\" title=\"来自对话「写周报」\" url=\"\">\n这条消息不是用户本人发的。\n</persona-context>\n周报写好了"

    @Test fun ownMessagesAreBlue() {
        assertEquals(true, isOwnMessage(u("看一下 CI", clientMsgId = "c1"))) // the composer
        assertEquals(true, isOwnMessage(u("帮我看看明天的日程", clientMsgId = "c2"))) // dictated
        assertEquals(true, isOwnMessage(u("提交并推送当前改动"))) // a quick command expanded, or typed in the terminal
        assertEquals(true, isOwnMessage(u("<persona-context kind=\"context\" title=\"选中的文字\">一段</persona-context>\n改一下")))
        assertEquals(true, isOwnMessage(Item(id = "i#1", role = "user", kind = "image")))
    }

    @Test fun relayedAndInjectedAreNot() {
        assertEquals(false, isOwnMessage(u("build passed", from = "build-bot")))
        assertEquals(false, isOwnMessage(u("<cross-session-message from-name=\"bot\">hi</cross-session-message>")))
        assertEquals(false, isOwnMessage(u(relay)))
        assertEquals(false, isOwnMessage(u(notice)))
        assertEquals(false, isOwnMessage(u("⏰ 早报（定时任务）\n\n看看今天的日程。")))
        assertEquals(false, isOwnMessage(u("x", injected = Injected(kind = "system", state = "lost_tasks", line = "node 重启过"))))
    }
}
