package persona.feature.chat

import android.content.Context
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Item
import persona.core.contracts.Session
import persona.core.contracts.SessionKey

/** The composer's parity with iOS and the web (#2976): @, quotes, the host's notes, /model and /loop. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class ComposerParityTest {
    private val res = ApplicationProvider.getApplicationContext<Context>().resources

    @Test fun theMentionIsTheAtWordBeforeTheCaret() {
        assertEquals(MentionToken(0, 1, ""), mentionAt("@", 1))
        assertEquals(MentionToken(3, 7, "src"), mentionAt("看下 @src", 7))
        assertNull(mentionAt("me@example.com", 14)) // an address is not one
        assertNull(mentionAt("@a b", 4)) // a space ended it
        assertEquals(MentionToken(0, 2, "a"), mentionAt("@a b", 2))
        assertNull(mentionAt("@a", 5))
    }

    @Test fun pickingTypesThePathAndTheButtonTypesAnAt() {
        val picked = replaceMention(TextFieldValue("看 @ma 好", TextRange(5)), MentionToken(2, 5, "ma"), "@src/main.kt ")
        assertEquals("看 @src/main.kt 好", picked.text) // the space after the token is not doubled
        assertEquals(TextRange(15), picked.selection)
        assertEquals("看 ", replaceMention(TextFieldValue("看 @", TextRange(3)), MentionToken(2, 3, ""), "").text)
        assertEquals(TextFieldValue("看下 @", TextRange(4)), typeMention(TextFieldValue("看下", TextRange(2))))
        assertEquals(TextFieldValue("@", TextRange(1)), typeMention(TextFieldValue("")))
        assertEquals("a @b", typeMention(TextFieldValue("a b", TextRange(2))).text)
        assertEquals("a @b", typeMention(TextFieldValue("ab", TextRange(1))).text)
    }

    @Test fun theMenuListsTheFilesThisChatUsedThenOtherSessions() {
        fun tool(id: String, key: String, path: String) = Item(id, kind = "tool_use", toolInput = buildJsonObject { put(key, path) })
        val items = listOf(tool("1", "file_path", "/w/app/a.kt"), tool("2", "notebook_path", "/w/app/n.ipynb"),
            tool("3", "file_path", "/w/app/a.kt"), Item("4", kind = "text", text = "x"), tool("5", "file_path", "/etc/hosts"))
        assertEquals(listOf("/etc/hosts", "/w/app/a.kt", "/w/app/n.ipynb"), chatFiles(items))
        assertEquals("a.kt", relativeMentionPath("/w/app/a.kt", "/w/app/"))
        assertEquals("/etc/hosts", relativeMentionPath("/etc/hosts", "/w/app"))
        val self = Session(machineId = "m", sessionId = "self", cwd = "/w/app", updatedAt = "2026-11-05T10:00:00Z")
        val older = Session(machineId = "m", sessionId = "old", title = "修登录", cwd = "/w/login", updatedAt = "2026-11-04T10:00:00Z")
        val newer = Session(machineId = "m", sessionId = "new", title = "照片同步", cwd = "/w/photos", agent = "codex", updatedAt = "2026-11-05T09:00:00Z")
        val assistant = Session(machineId = "m", sessionId = "p", title = "Persona", assistant = true)
        val mentions = ComposerMentions(chatFiles(items), "/w/app", listOf(self, older, newer, assistant), self.key)
        val rows = mentionRows(mentions, "")
        assertEquals(listOf("@/etc/hosts ", "@a.kt ", "@n.ipynb "), rows.filterIsInstance<MentionRow.File>().map { it.insert })
        assertEquals(listOf("new", "old"), rows.filterIsInstance<MentionRow.Quote>().map { it.session.sessionId })
        assertEquals(listOf("@a.kt "), mentionRows(mentions, "A.K").map { (it as MentionRow.File).insert })
        assertEquals(listOf("new"), mentionRows(mentions, "codex").map { (it as MentionRow.Quote).session.sessionId })
        assertEquals(emptyList<MentionRow>(), mentionRows(mentions, "nothing"))
    }

    @Test fun aPickedSessionGoesAsContextNotText() {
        val s = Session(machineId = "m", sessionId = "s1", title = "照片同步", cwd = "/w/photos", lastText = " 计划写好了 ")
        val ref = sessionQuote(s, res)
        assertEquals("context", ref.kind)
        assertEquals("persona://session/m/s1", ref.url)
        assertEquals("另一个会话：照片同步（Claude）\n目录：/w/photos\n它最近说：计划写好了", ref.text)
        assertEquals(listOf("context"), ChatLinkContext.parse(ref.xml()).first.map { it.kind })
    }

    @Test fun aQuoteKeepsItsKindAndMessageAndIsNotAddedTwice() {
        val quote = ChatLinkContext.quote(" 第一行\n第二行 ", "Claude", "msg-1")
        assertEquals("<persona-context kind=\"quote\" title=\"Claude\" url=\"\" msg=\"msg-1\">\n第一行\n第二行\n</persona-context>", quote.xml())
        val parsed = ChatLinkContext.parse(quote.xml() + "\n\n好的").first.single()
        assertEquals(listOf("quote", "Claude", "msg-1", "第一行\n第二行"), listOf(parsed.kind, parsed.title, parsed.msg, parsed.text))
        val once = withQuote(emptyList(), quote)
        assertEquals(once, withQuote(once, ChatLinkContext.quote("第一行", "Claude", "msg-1")))
        assertEquals(2, withQuote(once, ChatLinkContext.quote("别的", "我", "msg-2")).size)
        // Contexts other than quotes keep writing kind="context" (a page's text) or "edit".
        assertTrue(ChatLinkContext("other", "t", "", "x").xml().startsWith("<persona-context kind=\"context\""))
    }

    @Test fun theHostsNoteOverTheComposer() {
        val base = Session(machineId = "m", sessionId = "s", status = "idle", state = "unread")
        assertNull(hostNote(base, null))
        val failed = base.copy(failureReason = "⚠️ 模型接口报错，已停下")
        assertEquals(HostNote.Failure("⚠️ 模型接口报错，已停下", "模型接口报错，已停下"), hostNote(failed, null))
        assertNull(hostNote(failed, "⚠️ 模型接口报错，已停下")) // retried this very notice
        assertEquals("模型接口报错，已停下", hostNote(failed, "older notice").let { (it as HostNote.Failure).text })
        assertNull(hostNote(failed.copy(status = "working", state = "working"), null))
        assertNull(hostNote(failed.copy(starting = true), null))
        val waiting = base.copy(state = "needs_you", attentionReason = " 等你确认部署 ", failureReason = "x")
        assertEquals(HostNote.Attention("等你确认部署"), hostNote(waiting, null))
        // A stale reason once it no longer needs you stays out.
        assertNull(hostNote(base.copy(attentionReason = "等你"), null))
        assertEquals("等你确认部署", waiting.reasonPreview)
        assertEquals("模型接口报错，已停下", failed.reasonPreview)
        assertNull(failed.copy(status = "working", state = "working").reasonPreview)
        assertNull(base.copy(failureReason = "⚠️").failureText)
    }

    @Test fun slashListsModelAndLoopWhereTheSessionTakesThem() {
        fun words(draft: String, model: Boolean, loop: Boolean) = SlashCommand.matching(draft, clear = true, res, model, loop).map { it.word }
        assertEquals(listOf("/继续", "/compact", "/context", "/提交", "/汇报", "/model", "/loop", "/clear"), words("/", true, true))
        assertEquals(listOf("/继续", "/compact", "/context", "/提交", "/汇报", "/clear"), words("/", false, false))
        assertEquals(listOf("/model"), words("/m", true, true))
        assertEquals(listOf("/loop"), words("/lo", true, true))
        // /model stays listed when typed out: picking it is what opens the choices.
        assertEquals(listOf("/model"), SlashCommand.suggestions("/model", false, res, model = true).map { it.word })
        assertEquals(emptyList<String>(), SlashCommand.suggestions("/loop", false, res, loop = true).map { it.word })
        assertEquals("/loop ", SlashCommand.loopCommand(res).text)
        assertTrue(SlashCommand.modelCommand(res).model)
        // The "+" sheet's quick messages are the same five: /model and /loop are not sent from there.
        assertFalse(SlashCommand.all(res).any { it.model || it.loop })
    }

    @Test fun aLoopDraftSaysWhatItWillDo() {
        assertNull(loopHint("/looping", res))
        assertNull(loopHint("看下 /loop", res))
        assertEquals(LoopHint("格式：/loop [间隔] 要做的事，如 /loop 10m 看下 CI；不写间隔由 Claude 自己定节奏", true), loopHint("/loop ", res))
        assertEquals(LoopHint("每 10 分钟：还没写要做的事", true), loopHint("/loop 10m", res))
        assertEquals(LoopHint("每 2 小时重复：看下 CI", false), loopHint("/loop 2h 看下 CI", res))
        assertEquals(LoopHint("由 Claude 自己定节奏重复：看下 CI", false), loopHint("/LOOP 看下 CI", res))
    }

    @Test fun otherSessionsLeaveOutTheAssistantAndThisOne() {
        val a = Session(machineId = "m", sessionId = "a", updatedAt = "2026-11-01T00:00:00Z")
        val b = Session(machineId = "m", sessionId = "b", updatedAt = "2026-11-02T00:00:00Z")
        assertEquals(listOf("b"), otherSessions(listOf(a, b), SessionKey("m", "a"), "").map { it.sessionId })
        assertEquals(listOf("b", "a"), otherSessions(listOf(a, b), null, "").map { it.sessionId })
        assertEquals(1, otherSessions(listOf(a, b), null, "", limit = 1).size)
    }

    @Test fun otherSessionsSortByTimeNotByText() {
        // The hub trims RFC3339Nano's trailing zeros: as text "…00.5Z" sorts before "…00Z", as time it is later.
        val whole = Session(machineId = "m", sessionId = "whole", updatedAt = "2026-11-05T10:00:00Z")
        val half = Session(machineId = "m", sessionId = "half", updatedAt = "2026-11-05T10:00:00.5Z")
        val zoned = Session(machineId = "m", sessionId = "zoned", updatedAt = "2026-11-05T17:59:59+08:00")
        assertEquals(listOf("half", "whole", "zoned"), otherSessions(listOf(zoned, whole, half), null, "").map { it.sessionId })
    }

    @Test fun yourQuoteSaysWoOnTheWireInEveryLanguage() {
        val mine = messageQuote("看下这里", user = true, agent = "Codex", msg = "m1")
        assertTrue(mine.xml().startsWith("<persona-context kind=\"quote\" title=\"我\""))
        assertEquals("Codex", messageQuote("x", user = false, agent = "Codex", msg = "m2").title)
    }

    @Test fun onlyAnHttpsTopicIsOfferedToOpen() {
        val base = Session(machineId = "m", sessionId = "s")
        assertEquals("https://applink.example.com/thread?x=1", base.copy(externalUrl = "https://applink.example.com/thread?x=1").externalLink)
        for (url in listOf("http://example.com/t", "javascript:alert(1)", "lark://open", "https://", "not a url", ""))
            assertNull(url, base.copy(externalUrl = url).externalLink)
        assertNull(base.externalLink)
    }
}
