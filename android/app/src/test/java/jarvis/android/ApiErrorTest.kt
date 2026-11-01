package jarvis.android

import jarvis.android.data.Item
import jarvis.android.ui.ChatRow
import jarvis.android.ui.FoldedRow
import jarvis.android.ui.chat.ApiErrorInfo
import jarvis.android.ui.chat.ChatItem
import jarvis.android.ui.chat.foldApiErrors
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** API error cards (iOS APIErrorInfo / ChatLines.foldAPIErrors): the same wording and folding rules. */
class ApiErrorTest {
    @Test fun readsClaudeCodesAndTheHubsErrors() {
        val a = ApiErrorInfo.parse("API Error: 529 {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\"}} request_id: req_011CUxyzABCD1234")!!
        assertEquals(529, a.status); assertEquals("模型服务太忙，过载了", a.reason); assertEquals(true, a.transient)
        assertEquals("req_011CUxyzABCD1234", a.requestId)
        val b = ApiErrorInfo.parse("API 错误：429 The Tokens Per Minute (TPM) limit was reached（正在重试）")!!
        assertEquals("请求太多，被限流了", b.reason); assertEquals(true, b.transient)
        val c = ApiErrorInfo.parse("API Error: 401 authentication_error · Please run /login")!!
        assertEquals("登录过期了，要在电脑上重新 /login", c.reason); assertEquals(false, c.transient)
        assertEquals("请求被拒绝：参数或上下文有问题", ApiErrorInfo.parse("API Error: 400 bad request")!!.reason)
        assertEquals("模型服务暂时不可用", ApiErrorInfo.parse("API Error: 502 Bad Gateway")!!.reason)
        // Claude Code's auth failure leads with a sentence.
        val d = ApiErrorInfo.parse("Failed to authenticate. API Error: 401 Authentication credentials are required; sign in or provide a valid API key")!!
        assertEquals(401, d.status); assertEquals(false, d.transient)
        assertNull(ApiErrorInfo.parse("The API Error: field is documented here"))
        assertNull(ApiErrorInfo.parse("I looked at it. The log says API Error: 500 somewhere in the middle of a long answer"))
        assertNull(ApiErrorInfo.parse(null))
    }

    private var n = 0
    private fun item(role: String, text: String) = Item(id = "i${n++}", kind = "text", role = role, text = text)
    private fun answer(t: String) = FoldedRow.Plain(ChatRow.Assistant(item("assistant", t)))
    private fun user(t: String) = FoldedRow.Plain(ChatRow.User(item("user", t)))

    @Test fun theSameErrorAfterARetryFoldsWithACount() {
        val e = "API Error: 529 overloaded"
        val out = foldApiErrors(listOf(user("做点事"), answer(e), user("继续"), answer(e), user("继续"), answer(e)))
        assertEquals(2, out.size)
        val card = (out[1] as ChatItem.Error).error
        assertEquals(3, card.count)
    }

    @Test fun somethingElseInBetweenKeepsThemApart() {
        val e = "API Error: 529 overloaded"
        val out = foldApiErrors(listOf(answer(e), user("换个问题"), answer(e)))
        assertEquals(listOf(1, 1), out.filterIsInstance<ChatItem.Error>().map { it.error.count })
        // A different error after a retry is a new card.
        val other = foldApiErrors(listOf(answer(e), user("继续"), answer("API Error: 401 /login")))
        assertEquals(2, other.filterIsInstance<ChatItem.Error>().size)
        assertTrue(other[1] is ChatItem.Row)
    }
}
