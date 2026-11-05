package persona.feature.files

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import persona.feature.files.HtmlPreview.Policy

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class HtmlPreviewTest {
    private val res get() = RuntimeEnvironment.getApplication().resources

    @Test fun policyGoesAfterTheDoctype() {
        val doc = HtmlPreview.document("<!DOCTYPE html><html><body>x</body></html>", Policy())
        assertTrue(doc, doc.startsWith("<!DOCTYPE html><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none';"))
        assertTrue(doc.endsWith("<html><body>x</body></html>"))
        val xml = HtmlPreview.document("<?xml version=\"1.0\"?>\n<!doctype html><p>", Policy())
        assertTrue(xml, xml.startsWith("<?xml version=\"1.0\"?>\n<!doctype html><meta "))
        assertTrue(HtmlPreview.document("<p>hi", Policy()).startsWith("<meta "))
    }

    @Test fun scriptsAndNetworkOffByDefault() {
        assertTrue(HtmlPreview.csp(Policy()).contains("script-src 'none'"))
        assertTrue(HtmlPreview.csp(Policy(scripts = true)).contains("script-src 'unsafe-inline' 'unsafe-eval'"))
        assertEquals("", HtmlPreview.csp(Policy(network = true)))
        assertEquals("<p>", HtmlPreview.document("<p>", Policy(network = true)))
    }

    @Test fun requestsLoadOnlyAsAllowed() {
        assertTrue(HtmlPreview.allows("data", Policy()))
        assertFalse(HtmlPreview.allows("https", Policy()))
        assertFalse(HtmlPreview.allows("file", Policy(network = true)))
        assertFalse(HtmlPreview.allows("content", Policy(network = true)))
        assertTrue(HtmlPreview.allows("HTTPS", Policy(network = true)))
        assertTrue(HtmlPreview.opens("https"))
        assertFalse(HtmlPreview.opens("javascript"))
        assertFalse(HtmlPreview.opens("file"))
    }

    @Test fun countsReferencesBesideThePage() {
        val html = """<img src="a.png"><link href='style.css'><a href="#top"><img src="data:image/png;base64,x">
            <script src="https://cdn.example.com/x.js"></script><a href="//example.com/x"><a href="other.html">"""
        assertEquals(2, HtmlPreview.relativeRefs(html))
    }

    @Test fun notice() {
        assertEquals("预览：脚本和联网已关闭，可在 ⋯ 里打开 · 页面引用的 2 个相邻文件不会加载", HtmlPreview.notice(res, Policy(), 2))
        assertEquals("已允许脚本和联网", HtmlPreview.notice(res, Policy(scripts = true, network = true), 0))
    }

    @Test fun htmlFiles() {
        assertTrue(HtmlPreview.isHtml("index.HTML"))
        assertTrue(HtmlPreview.isHtml("a.htm"))
        assertFalse(HtmlPreview.isHtml("a.md"))
    }
}
