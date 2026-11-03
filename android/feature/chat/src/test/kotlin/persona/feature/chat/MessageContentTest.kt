package persona.feature.chat

import androidx.compose.ui.text.input.TextFieldValue
import persona.core.contracts.Item
import persona.core.contracts.api.DraftUploadState
import persona.core.domain.lines.ChatLine
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

/** What a message and a tool step draw (R2, R3): photos, file paths, Codex patches, the running line's time. */
class MessageContentTest {
    @Test fun photosSentFromThePhoneAreThumbnailsNotText() {
        val model = messageModel("看这个 [Image #1]\n\n<attached_images>\n/Users/me/a.jpg\n/Users/me/b.jpg\n</attached_images>")
        assertEquals(listOf("/Users/me/a.jpg", "/Users/me/b.jpg"), model.images)
        assertEquals(listOf<MessagePart>(MessagePart.Text("看这个")), model.parts)
        assertTrue(model.files.isEmpty())
        assertEquals("看这个\n\n<attached_images>\n/Users/me/a.jpg\n/Users/me/b.jpg\n</attached_images>", model.editText())
    }

    @Test fun aPhotoOnlyMessageHasNoText() {
        val model = messageModel("<attached_images>\n/u/p.jpg\n</attached_images>")
        assertTrue(model.parts.isEmpty())
        assertEquals("", model.plain)
        assertEquals(listOf("/u/p.jpg"), model.images)
    }

    @Test fun whatTheComposerSendsComesBackAsPhotos() {
        val composer = ChatComposerState().apply {
            value = TextFieldValue("看图")
            attachments = listOf(ComposerAttachment("a", "a.jpg", "", true, "/u/a.jpg", uploadState = DraftUploadState.Uploaded))
        }
        val model = messageModel(composer.message())
        assertEquals(listOf("/u/a.jpg"), model.images)
        assertEquals("看图", model.plain)
        // 编辑 puts the words and the (uploaded) photo back.
        val editing = ChatComposerState()
        editing.edit(model.editText())
        assertEquals("看图", editing.value.text)
        assertEquals(listOf("/u/a.jpg"), editing.attachments.map { it.path })
    }

    @Test fun filesComeFromTheWordsNotFromPhotosOrContextCards() {
        val model = messageModel("<jarvis-context kind=\"context\" title=\"t\" url=\"\">/x/y/quoted.kt</jarvis-context>" +
            "改了 src/a.kt 和 /w/p/b.md\n<attached_images>\n/w/p/shot.png\n</attached_images>")
        assertEquals(listOf("src/a.kt", "/w/p/b.md"), model.files)
        assertEquals("改了 src/a.kt 和 /w/p/b.md", model.plain)
        assertTrue(model.parts.first() is MessagePart.Reference)
    }

    @Test fun anotherSessionsMessageKeepsItsWords() {
        val model = messageModel("<cross-session-message from-name=\"其他会话\">看一下 /w/p/c.kt</cross-session-message>")
        assertEquals("看一下 /w/p/c.kt", model.plain)
        // Paths inside the other session's words are its, not chips here.
        assertTrue(model.files.isEmpty())
    }

    private fun tool(name: String, vararg fields: Pair<String, String>) = Item("call", kind = "tool_use", toolName = name,
        toolInput = buildJsonObject { fields.forEach { (key, value) -> put(key, value) } })

    @Test fun codexPatchesNumberTheirLines() {
        val patch = "diff --git a/x.kt b/x.kt\nindex 1..2 100644\n--- a/x.kt\n+++ b/x.kt\n" +
            "@@ -10,3 +10,4 @@ fun f()\n keep\n-old\n+new\n+more\n\\ No newline at end of file"
        val lines = unifiedDiffLines(patch)
        assertEquals('@', lines[0].sign)
        assertEquals(DiffLine("keep", ' ', 10, 10), lines[1])
        assertEquals(DiffLine("old", '-', 11, null), lines[2])
        assertEquals(DiffLine("new", '+', null, 11), lines[3])
        assertEquals(DiffLine("more", '+', null, 12), lines[4])
        assertEquals('@', lines[5].sign)
        assertEquals(6, lines.size)
        val edit = toolDiff(tool("Edit", "file_path" to "/w/x.kt", "diff" to patch, "change" to "update"))!!
        assertEquals(2, edit.added)
        assertEquals(1, edit.removed)
        assertEquals("/w/x.kt", toolPath(tool("Edit", "file_path" to "/w/x.kt", "diff" to patch)))
        // One change (old line 11 replaced): 上一处 / 下一处 jump to it.
        assertEquals(listOf(2), diffChangeStarts(edit))
    }

    @Test fun aNewFileFromCodexIsAllAdded() {
        val added = toolDiff(tool("Edit", "file_path" to "/w/new.sql", "diff" to "CREATE INDEX i;\nSELECT 1;\n", "change" to "add"))!!
        assertEquals(listOf(1, 2), added.lines.map { it.new })
        assertEquals(2, added.added)
        assertEquals(0, added.removed)
        val deleted = patchDiff("a\nb\n", "delete")
        assertEquals(listOf(1, 2), deleted.lines.map { it.old })
        assertEquals(2, deleted.removed)
        // Claude's own Edit keeps its old / new strings.
        assertEquals(1, toolDiff(tool("Edit", "file_path" to "/w/a.kt", "old_string" to "a", "new_string" to "b"))!!.added)
        assertNull(toolPath(tool("Bash", "command" to "ls")))
    }

    @Test fun questionsReadTheirChoices() {
        val item = Item("ask", kind = "tool_use", toolName = "AskUserQuestion", toolInput = persona.core.contracts.JsonCodec.parseToJsonElement(
            """{"questions":[{"question":"放在哪个 target？","header":"Target","multiSelect":true,"options":[{"label":"A","description":"一起"},{"label":"B"}]},{"question":""}]}"""))
        val questions = toolQuestions(item)
        assertEquals(1, questions.size)
        assertEquals("放在哪个 target？", questions[0].question)
        assertEquals("Target", questions[0].header)
        assertTrue(questions[0].multiSelect)
        assertEquals(listOf("A" to "一起", "B" to null), questions[0].options)
        assertTrue(toolQuestions(tool("AskUserQuestion")).isEmpty())
    }

    @Test fun theRunningWorkLineCountsItsTime() {
        val start = Instant.parse("2026-11-02T10:00:00Z")
        val line = ChatLine.Work("w-1", emptyList(), live = true, start = start, end = null, open = false)
        assertEquals("正在工作", workSummary(line, null))
        assertEquals("正在工作，1 分 20 秒", workSummary(line, null, start.plusSeconds(80)))
        assertEquals("正在工作，39 秒", workSummary(line, null, start.plusSeconds(39)))
        // A finished line keeps its recorded words.
        assertFalse(workSummary(line.copy(live = false), 30, start.plusSeconds(80)).contains("正在"))
    }

    @Test fun aCutTextLosesTheHubsMark() {
        assertEquals("开头", cutText("开头\n…（已截断，共 26000 字节）"))
        assertEquals("完整", cutText("完整"))
        assertEquals("12 KB", formatBytes(12_345))
        assertEquals("3.4 MB", formatBytes(3_400_000))
        assertEquals("999 字节", formatBytes(999))
    }
}
