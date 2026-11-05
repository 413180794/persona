package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Item
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.ToolCall
import persona.core.domain.lines.WorkItem

/** A step of Persona's own, the computer's, the phone's and mail tools reads as iOS names it (#2753). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class ToolPresentationTest {
    private val res get() = ApplicationProvider.getApplicationContext<Context>().resources

    private fun tool(name: String, vararg fields: Pair<String, String>) = Item("call", kind = "tool_use", toolName = name,
        toolInput = buildJsonObject { fields.forEach { (key, value) -> put(key, value) } })

    // The whole screen follows the app's language, not iOS's hard-coded Chinese.
    @Test @Config(qualifiers = "ja") fun wholeScreenIsInTheAppsLanguage() {
        assertEquals("macbook · 画面全体", toolPresentation(res, tool("mcp__jarvis__computer_state", "machine" to "macbook")).title)
    }

    @Test fun namedToolsShowTheirWordsNotTheirRawName() {
        toolPresentation(res, tool("mcp__jarvis__computer_run", "machine" to "macbook")).let {
            assertEquals("操控电脑", it.verb)
            assertEquals("macbook", it.title)
        }
        toolPresentation(res, tool("mcp__jarvis__conversation_rename", "kind" to "功能", "topic" to "登录页")).let {
            assertEquals("重命名对话", it.verb)
            assertEquals("功能 | 登录页", it.title)
        }
        // Nothing to say about it: the words are the title too.
        assertEquals("归档对话", toolPresentation(res, tool("mcp__jarvis__conversation_archive")).title)
        assertEquals("用浏览器", toolPresentation(res, tool("mcp__jarvis__browser_run")).verb)
        assertEquals("看电脑", toolPresentation(res, tool("computer_state")).verb)
        assertEquals("macbook · 整个屏幕", toolPresentation(res, tool("mcp__jarvis__computer_state", "machine" to "macbook")).title)
        assertEquals("手机", toolPresentation(res, tool("mcp__jarvis__phone_device_info")).verb)
        assertEquals("device info", toolPresentation(res, tool("mcp__jarvis__phone_device_info")).title)
        assertEquals("邮件", toolPresentation(res, tool("mcp__jarvis__mail_send")).verb)
        assertEquals("读 Agent 对话", toolPresentation(res, tool("mcp__jarvis__agent_read")).verb)
        assertEquals("调用 Persona · github read", toolPresentation(res, tool("mcp__jarvis__github_read")).verb)
    }

    @Test fun claudeCodeAndUnknownToolsKeepTodaysWords() {
        assertEquals("读取", toolPresentation(res, tool("Read", "file_path" to "/w/a.kt")).verb)
        toolPresentation(res, tool("mcp__github__create_issue")).let {
            assertEquals("执行", it.verb)
            assertEquals("执行", it.title)
        }
    }

    @Test fun theRunningLineSaysTheNamedToolsWords() {
        fun running(item: Item) = workSummary(res, ChatLine.Work("w", listOf(WorkItem.Tool(ToolCall(item.asLineItem(), null, emptyList()))),
            live = true, start = null, end = null, open = false), null)
        assertEquals("操控电脑 macbook，第 1 步", running(tool("mcp__jarvis__computer_run", "machine" to "macbook")))
        assertEquals("归档对话，第 1 步", running(tool("mcp__jarvis__conversation_archive")))
        assertEquals("正在读取 a.kt，第 1 步", running(tool("Read", "file_path" to "/w/a.kt")))
        assertEquals("正在执行 执行，第 1 步", running(tool("mcp__github__create_issue")))
    }

    @Test fun namedToolsHaveTheirIcons() {
        assertEquals(PhIcons.Desktop, toolCategory("mcp__jarvis__computer_run"))
        assertEquals(PhIcons.GlobeSimple, toolCategory("mcp__jarvis__browser_look"))
        assertEquals(PhIcons.Archive, toolCategory("mcp__jarvis__conversation_unarchive"))
        assertEquals(PhIcons.ChatCircle, toolCategory("mcp__jarvis__conversation_send"))
        assertEquals(PhIcons.DeviceMobile, toolCategory("mcp__jarvis__phone_photos_recent"))
        assertEquals(PhIcons.EnvelopeSimple, toolCategory("mcp__jarvis__mail_send"))
        assertEquals(PhIcons.Eye, toolCategory("Read"))
        assertEquals(PhIcons.Wrench, toolCategory("mcp__github__create_issue"))
    }
}
