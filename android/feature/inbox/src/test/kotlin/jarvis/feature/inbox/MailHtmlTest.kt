package jarvis.feature.inbox

import org.jsoup.Jsoup
import org.junit.Assert.*
import org.junit.Test

class MailHtmlTest {
    @Test fun stripsActiveContentTrackingAndStylesButKeepsReadableBodyAndSafeLinks() {
        val html = safeMailHtml("""
            <script>alert(1)</script><iframe src="https://example.com"></iframe>
            <meta http-equiv="refresh" content="0;url=https://example.com">
            <img src="https://example.com/pixel"><img src="file:///private/file">
            <form action="https://example.com"><input value="secret"></form>
            <p style="color:white;background:white" onclick="alert(1)">正文<strong>重点</strong></p>
            <a href="javascript:alert(1)">危险链接</a><a href="https://example.com/help">查看</a>
            <table><tr><td>附件说明</td></tr></table>
        """.trimIndent(), "#eeeeee", "#111111", "#aabbff")
        val doc = Jsoup.parse(html)
        assertTrue(doc.select("script,iframe,img,form,input,[onclick],[style]").isEmpty())
        assertFalse(html.contains("javascript:"))
        assertFalse(html.contains("file:///"))
        assertEquals("正文重点", doc.selectFirst("p")?.text())
        assertEquals("https://example.com/help", doc.select("a[href]").single().attr("href"))
        assertEquals("附件说明", doc.selectFirst("td")?.text())
        assertTrue(html.contains("default-src 'none'"))
        assertTrue(html.contains("background:#111111;color:#eeeeee"))
    }

    @Test fun mailDetailsShowReadableSizesAndLocalTimes() {
        assertEquals("812 字节", readableSize(812))
        assertEquals("178 KB", readableSize(182400))
        assertEquals("2.5 MB", readableSize(2_621_440))
        val zone = java.time.ZoneId.of("Asia/Shanghai")
        val today = java.time.LocalDate.of(2026, 10, 31)
        assertEquals("今天 03:42", mailTime("2026-10-30T19:42:55.439273Z", zone, today))
        assertEquals("昨天 09:00", mailTime("Fri, 30 Oct 2026 09:00:00 +0800", zone, today))
        assertEquals("1月2日 08:00", mailTime("2026-01-02T08:00:00+08:00", zone, today))
        assertEquals("not a date", mailTime("not a date", zone, today))
    }
}
