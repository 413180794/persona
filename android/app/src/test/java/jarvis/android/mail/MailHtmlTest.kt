package jarvis.android.mail

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MailHtmlTest {
    @Test fun removesWhatCouldRun() {
        val dirty = """
            <html><head><meta http-equiv="refresh" content="0;url=https://evil.example.com"><base href="https://evil.example.com/">
            <link rel=stylesheet href="https://evil.example.com/x.css"><script>alert(1)</script><SCRIPT src=x></SCRIPT></head>
            <body onload="steal()"><iframe src="https://evil.example.com"></iframe>
            <a href="javascript:alert(1)" onclick='x()'>link</a> <a href = " JavaScript:alert(2)">two</a>
            <img src=x onerror=alert(3)><form action="https://evil.example.com"><input name=a value=b><button>go</button></form>
            <div style="width:expression(alert(4))">ok</div><object data=x></object><embed src=y>
            <svg><script>alert(5)</script></svg><p>keep me</p></body></html>
        """.trimIndent()
        val clean = MailHtml.sanitize(dirty).lowercase()
        listOf("<script", "alert(5)", "alert(3)", "onload", "onclick", "onerror", "<iframe", "<form", "<input", "<button",
            "<object", "<embed", "<meta", "<base", "<link", "javascript:", "expression(").forEach {
            assertFalse("still has $it: $clean", clean.contains(it))
        }
        assertTrue(clean.contains("<p>keep me</p>"))
        assertTrue(clean.contains(">link</a>"))
        assertTrue(clean.contains("href=\"about:blank#alert(1)\""))
    }

    @Test fun findsRemoteImages() {
        assertTrue(MailHtml.hasRemoteImages("<img alt=x src=\"https://t.example.com/p.gif\">"))
        assertTrue(MailHtml.hasRemoteImages("<img src='//cdn.example.com/a.png'>"))
        assertTrue(MailHtml.hasRemoteImages("<td style=\"background:url('https://x.example.com/bg.jpg')\">"))
        assertTrue(MailHtml.hasRemoteImages("<table background=\"http://x.example.com/bg.jpg\">"))
        assertFalse(MailHtml.hasRemoteImages("<img src=\"cid:logo@1\"><img src=\"data:image/png;base64,AAAA\">"))
        assertFalse(MailHtml.hasRemoteImages("<a href=\"https://example.com\">x</a>"))
    }

    @Test fun reducesToText() {
        val text = MailHtml.toText("<html><head><style>p{}</style><title>t</title></head><body><p>A&amp;B&nbsp;&lt;c&gt;</p>" +
            "<div>line<br>break</div><ul><li>one</li><li>two</li></ul>&#20320;&#x597D;&bogus;</body></html>")
        assertEquals("A&B <c>\nline\nbreak\none\ntwo\n你好&bogus;", text)
        assertEquals("", MailHtml.toText("   "))
        assertEquals("x", MailHtml.toText("x&#99999999;".replace("&#99999999;", "")))
    }

    @Test fun pageWrapsTheBody() {
        val page = MailHtml.page("<p>hi</p>", dark = true, textColor = "#FFFFFF", linkColor = "#0A84FF")
        assertTrue(page.contains("<body><p>hi</p></body>"))
        assertTrue(page.contains("color-scheme\" content=\"dark\""))
        assertTrue(page.contains("color:#FFFFFF"))
    }
}
