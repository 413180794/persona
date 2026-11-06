package persona.core.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MessageTextTest {
    @Test fun attachedImagesBecomePathsNotText() {
        val text = messageText("看这个 [Image #1]\n<attached_images>\n/tmp/persona/a.jpg\n/tmp/persona/b.jpg\n</attached_images>")
        assertEquals("看这个", (text.text).zhAny())
        assertEquals(listOf("/tmp/persona/a.jpg", "/tmp/persona/b.jpg"), (text.images).zhAny())
        assertTrue(text.files.isEmpty())
    }

    @Test fun onlyPhotosLeaveNoText() {
        val text = messageText("<attached_images>\n/Users/me/.persona/uploads/photo.jpg\n</attached_images>")
        assertEquals("", (text.text).zhAny())
        assertEquals(listOf("/Users/me/.persona/uploads/photo.jpg"), (text.images).zhAny())
    }

    @Test fun olderImageNotesAreReadToo() {
        assertEquals(listOf("/a/b.png"), (imagePaths("说明\n相关图片：/a/b.png")).zhAny())
        assertEquals(listOf("/a/b.png", "~/c.jpg"), (imagePaths("看图（我从手机发来了图片，请用 Read 查看：/a/b.png、~/c.jpg）")).zhAny())
        assertEquals("看图", (messageText("看图（我从手机发来了图片，请用 Read 查看：/a/b.png）").text).zhAny())
        assertEquals("说明", (messageText("说明\n相关图片：/a/b.png").text).zhAny())
    }

    @Test fun pastedContentIsUnwrapped() {
        assertEquals("abc", (messageText("<pasted_content id=\"1\">abc</pasted_content>").text).zhAny())
    }

    @Test fun findsFilePathsButNotUrlsOrModelNames() {
        val t = "改了 ~/.config/x.json、/home/me/code/app/Main.kt 和 ios/Persona/Foo.swift，见 https://example.com/a/b.html，模型 deepseek/v4.1-flash。"
        assertEquals(listOf("~/.config/x.json", "/home/me/code/app/Main.kt", "ios/Persona/Foo.swift"), (FilePaths.find(t)).zhAny())
        assertEquals(emptyList<String>(), (FilePaths.find("/tmp")).zhAny())
        assertEquals(listOf("src/a.kt"), (FilePaths.find("看 src/a.kt:42 这一行")).zhAny())
        assertEquals(listOf("/w/proj/"), (FilePaths.find("目录 /w/proj/ 下面")).zhAny())
    }

    @Test fun filePathsStopAtSixAndSkipRepeats() {
        val many = (1..9).joinToString(" ") { "/w/p/f$it.kt" } + " /w/p/f1.kt"
        assertEquals((1..6).map { "/w/p/f$it.kt" }, FilePaths.find(many))
        assertEquals(listOf("/w/p/a.kt"), (messageText("改 /w/p/a.kt 和 /w/p/a.kt").files).zhAny())
    }

    @Test fun photoPathsAreNotFileChips() {
        val text = messageText("见 /w/p/notes.md\n<attached_images>\n/w/p/shot.png\n</attached_images>")
        assertEquals(listOf("/w/p/shot.png"), (text.images).zhAny())
        assertEquals(listOf("/w/p/notes.md"), (text.files).zhAny())
    }

    @Test fun previewsSayPicture() {
        assertEquals("[图片]\nlook", (cleanText("<attached_images>\n/tmp/a.png\n</attached_images>\nlook", "[图片]")).zhAny())
        assertEquals("[图片]", (cleanText("<attached_images>\n/tmp/a.png\n</attached_images>", "[图片]")).zhAny())
        // A context block is not what the user wrote: previews leave it out (#876).
        assertEquals("看看", (cleanText("<persona-context kind=\"context\" title=\"t\" url=\"\">引用</persona-context>看看", "[图片]")).zhAny())
        assertEquals("", (cleanText("<persona-context kind=\"doc\" title=\"t\">正文</persona-context>", "[图片]")).zhAny())
        assertEquals("hey", (cleanText("<cross-session-message from-name=\"other\">hey</cross-session-message>", "[图片]")).zhAny())
    }
}
