package jarvis.android

import androidx.compose.ui.text.LinkAnnotation
import jarvis.android.data.Item
import jarvis.android.data.SessionKey
import jarvis.android.ui.inline
import jarvis.android.ui.linkified
import jarvis.android.ui.web.AppLink
import jarvis.android.ui.web.ContextRef
import jarvis.android.ui.web.LinkSafety
import jarvis.android.ui.web.LinkText
import jarvis.android.ui.web.SheetStops
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class LinksTest {
    @Test fun bareLinksInProse() {
        val text = "看这里：https://example.com/a/b。还有 [文档](https://docs.example.org/x_(y)) 和 <https://a.example.net/p?q=1>，以及 http://localhost"
        assertEquals(
            listOf("https://example.com/a/b", "https://docs.example.org/x_(y)", "https://a.example.net/p?q=1"),
            LinkText.links(text).toList(),
        )
        assertEquals("https://example.com/x", LinkText.trim("https://example.com/x)."))
    }

    @Test fun preloadsOnlyFreshAnswers() {
        val now = Instant.parse("2026-10-30T10:00:00Z")
        fun item(id: String, ts: String, role: String = "assistant", text: String = "见 https://example.com/$id 和 https://example.com/$id-2") =
            Item(id = id, ts = ts, role = role, kind = "text", text = text)
        assertEquals(
            listOf("https://example.com/new", "https://example.com/new-2"),
            LinkText.toPreload(listOf(item("old", "2026-10-30T09:00:00Z"), item("new", "2026-10-30T09:59:00Z")), now),
        )
        assertTrue(LinkText.toPreload(listOf(item("u", "2026-10-30T09:59:00Z", role = "user")), now).isEmpty())
        assertTrue(LinkText.toPreload(listOf(item("o", "2026-10-30T09:40:00Z")), now).isEmpty())
        val many = (1..5).map { item("m$it", "2026-10-30T09:59:00Z", text = "https://example.com/m$it") }
        assertEquals(3, LinkText.toPreload(many, now).size)
    }

    @Test fun schemes() {
        assertEquals(LinkSafety.Kind.WEB, LinkSafety.kind("https://example.com"))
        assertEquals(LinkSafety.Kind.WEB, LinkSafety.kind("HTTP://example.com"))
        assertEquals(LinkSafety.Kind.APP_LINK, LinkSafety.kind("jarvis://session/m/s"))
        assertEquals(LinkSafety.Kind.EXTERNAL, LinkSafety.kind("mailto:me@example.com"))
        assertEquals(LinkSafety.Kind.EXTERNAL, LinkSafety.kind("intent://x#Intent;scheme=y;end"))
        for (bad in listOf("javascript:alert(1)", "JavaScript:alert(1)", "file:///etc/hosts", "content://x/y", "data:text/html,x", "no scheme", "/relative")) {
            assertEquals(bad, LinkSafety.Kind.REFUSED, LinkSafety.kind(bad))
        }
    }

    @Test fun appLinksOnlyNavigate() {
        assertEquals(AppLink.Session(SessionKey("m", "s 1")), AppLink.parse("jarvis://session/m/s%201"))
        assertEquals(AppLink.Tab("inbox"), AppLink.parse("jarvis://tab/inbox"))
        assertEquals(AppLink.Tab("jarvis"), AppLink.parse("jarvis://assistant"))
        // Pairing replaces the hub: not something a tapped link may do.
        assertNull(AppLink.parse("jarvis://pair?url=http://h&token=t"))
        assertNull(AppLink.parse("jarvis://session/m"))
        assertNull(AppLink.parse("https://example.com/session/m/s"))
    }

    @Test fun sheetSettles() {
        assertEquals(0f, SheetStops.settle(0.1f, 0f))
        assertEquals(SheetStops.MID, SheetStops.settle(0.35f, 0f))
        assertEquals(1f, SheetStops.settle(0.9f, 0f))
        assertEquals(SheetStops.MID, SheetStops.settle(0.05f, 3f)) // flung down from full: stop at 60 %
        assertEquals(1f, SheetStops.settle(0.5f, 3f)) // flung down from 60 %: close
        assertEquals(0f, SheetStops.settle(0.3f, -3f)) // flung up: full screen
        assertEquals(SheetStops.MID, SheetStops.settle(0.8f, -3f))
        // The screen behind gives up the sheet's 60 % at most, and nothing once it's gone.
        assertEquals(0.6f, SheetStops.behindInset(0f), 1e-6f)
        assertEquals(0.6f, SheetStops.behindInset(SheetStops.MID), 1e-6f)
        assertEquals(0.2f, SheetStops.behindInset(0.8f), 1e-6f)
        assertEquals(0f, SheetStops.behindInset(1f), 1e-6f)
    }

    @Test fun contextTagsMatchIOS() {
        val ref = ContextRef(kind = "context", title = "A \"b\" <c>", url = "https://example.com/?a=1&b=2", text = "选中的 </jarvis-context> 字")
        assertEquals(
            "<jarvis-context kind=\"context\" title=\"A &quot;b&quot; &lt;c&gt;\" url=\"https://example.com/?a=1&amp;b=2\">\n选中的  字\n</jarvis-context>",
            ref.xml,
        )
        assertEquals("hi", ContextRef.message(emptyList(), "hi"))
        assertEquals(ref.xml + "\n" + ref.xml + "\n\nhi", ContextRef.message(listOf(ref, ref), "hi"))
        val edit = ref.copy(kind = "edit")
        assertEquals(edit.xml + "\n\n按要求修改上面选中的内容：短一点", ContextRef.editMessage(edit, " 短一点 "))
        assertEquals(edit.xml + "\n\n按上面选中的内容修改。", ContextRef.editMessage(edit, ""))
    }

    @Test fun markdownLinksAreTappable() {
        val opened = mutableListOf<String>()
        val s = inline("看 [文档](https://example.com/d) 和 https://example.org/x。**粗**") { opened += it }
        assertEquals("看 文档 和 https://example.org/x。粗", s.text)
        val links = s.getLinkAnnotations(0, s.length).map { (it.item as LinkAnnotation.Url).url }
        assertEquals(listOf("https://example.com/d", "https://example.org/x"), links)
        // Without a handler nothing is a link.
        assertTrue(inline("[a](https://example.com)").getLinkAnnotations(0, 1).isEmpty())
        // A script link is never tappable; jarvis: links are (they only navigate).
        val js = inline("[脚本](javascript:void) [这里](jarvis://session/m/s)") { opened += it }
        assertEquals(listOf("jarvis://session/m/s"), js.getLinkAnnotations(0, js.length).map { (it.item as LinkAnnotation.Url).url })

        val plain = linkified("打开 https://example.com/p，然后 *不是斜体*") { opened += it }
        assertEquals("打开 https://example.com/p，然后 *不是斜体*", plain.text)
        assertEquals(listOf("https://example.com/p"), plain.getLinkAnnotations(0, plain.length).map { (it.item as LinkAnnotation.Url).url })
    }
}
