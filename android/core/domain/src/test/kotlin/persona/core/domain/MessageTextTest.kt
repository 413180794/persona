package persona.core.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MessageTextTest {
    @Test fun attachedImagesBecomePathsNotText() {
        val text = messageText("看这个 [Image #1]\n<attached_images>\n/tmp/jarvis/a.jpg\n/tmp/jarvis/b.jpg\n</attached_images>")
        assertEquals("看这个", text.text)
        assertEquals(listOf("/tmp/jarvis/a.jpg", "/tmp/jarvis/b.jpg"), text.images)
        assertTrue(text.files.isEmpty())
    }

    @Test fun onlyPhotosLeaveNoText() {
        val text = messageText("<attached_images>\n/Users/me/.jarvis/uploads/photo.jpg\n</attached_images>")
        assertEquals("", text.text)
        assertEquals(listOf("/Users/me/.jarvis/uploads/photo.jpg"), text.images)
    }

    @Test fun olderImageNotesAreReadToo() {
        assertEquals(listOf("/a/b.png"), imagePaths("说明\n相关图片：/a/b.png"))
        assertEquals(listOf("/a/b.png", "~/c.jpg"), imagePaths("看图（我从手机发来了图片，请用 Read 查看：/a/b.png、~/c.jpg）"))
        assertEquals("看图", messageText("看图（我从手机发来了图片，请用 Read 查看：/a/b.png）").text)
        assertEquals("说明", messageText("说明\n相关图片：/a/b.png").text)
    }

    @Test fun pastedContentIsUnwrapped() {
        assertEquals("abc", messageText("<pasted_content id=\"1\">abc</pasted_content>").text)
    }

    @Test fun findsFilePathsButNotUrlsOrModelNames() {
        val t = "改了 ~/.config/x.json、/home/me/code/app/Main.kt 和 ios/Jarvis/Foo.swift，见 https://example.com/a/b.html，模型 deepseek/v4.1-flash。"
        assertEquals(listOf("~/.config/x.json", "/home/me/code/app/Main.kt", "ios/Jarvis/Foo.swift"), FilePaths.find(t))
        assertEquals(emptyList<String>(), FilePaths.find("/tmp"))
        assertEquals(listOf("src/a.kt"), FilePaths.find("看 src/a.kt:42 这一行"))
        assertEquals(listOf("/w/proj/"), FilePaths.find("目录 /w/proj/ 下面"))
    }

    @Test fun filePathsStopAtSixAndSkipRepeats() {
        val many = (1..9).joinToString(" ") { "/w/p/f$it.kt" } + " /w/p/f1.kt"
        assertEquals((1..6).map { "/w/p/f$it.kt" }, FilePaths.find(many))
        assertEquals(listOf("/w/p/a.kt"), messageText("改 /w/p/a.kt 和 /w/p/a.kt").files)
    }

    @Test fun photoPathsAreNotFileChips() {
        val text = messageText("见 /w/p/notes.md\n<attached_images>\n/w/p/shot.png\n</attached_images>")
        assertEquals(listOf("/w/p/shot.png"), text.images)
        assertEquals(listOf("/w/p/notes.md"), text.files)
    }

    @Test fun previewsSayPicture() {
        assertEquals("[图片]\nlook", cleanText("<attached_images>\n/tmp/a.png\n</attached_images>\nlook"))
        assertEquals("[图片]", cleanText("<attached_images>\n/tmp/a.png\n</attached_images>"))
        // A context block is not what the user wrote: previews leave it out (#876).
        assertEquals("看看", cleanText("<jarvis-context kind=\"context\" title=\"t\" url=\"\">引用</jarvis-context>看看"))
        assertEquals("", cleanText("<jarvis-context kind=\"doc\" title=\"t\">正文</jarvis-context>"))
        assertEquals("hey", cleanText("<cross-session-message from-name=\"other\">hey</cross-session-message>"))
    }
}
