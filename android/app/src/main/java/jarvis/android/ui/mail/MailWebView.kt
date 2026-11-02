package jarvis.android.ui.mail

import android.annotation.SuppressLint
import android.graphics.Color as AColor
import android.net.Uri
import android.view.ViewGroup
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.viewinterop.AndroidView
import jarvis.android.mail.MailHtml
import jarvis.android.ui.JTheme
import java.io.ByteArrayInputStream

private fun Color.css(): String = String.format("#%06X", toArgb() and 0xFFFFFF)

/** Layoutlib (Paparazzi) has a WebView class without its settings, and sets neither inspection nor edit mode. */
private val webViewWorks: Boolean by lazy { runCatching { WebView::class.java.getMethod("getSettings") }.isSuccess }

/**
 * A mail's HTML (iOS `HTMLView`): JavaScript off, no file / content access, every network load refused until the
 * user taps 显示图片 ([allowImages]), and even then only images (no scripts, frames or beacons via fetch). The page
 * is [MailHtml.sanitize]d and wrapped in a phone-sized shell; the view is as tall as its content (the page scrolls).
 * Links go to [onLink]: nothing navigates the reader itself.
 */
@SuppressLint("SetJavaScriptEnabled")
@Composable
fun MailWebView(html: String, allowImages: Boolean, onLink: (String) -> Unit, modifier: Modifier = Modifier) {
    val c = JTheme.colors
    // Screenshot tests (layoutlib) cannot draw a WebView: the words of the mail stand in.
    if (LocalInspectionMode.current || !webViewWorks) {
        Text(MailHtml.toText(html), modifier, style = JTheme.type.body, color = c.label)
        return
    }
    val page = remember(html, c.dark, c.label, c.accent) {
        MailHtml.page(MailHtml.sanitize(html), c.dark, c.label.css(), c.accent.css())
    }
    val state = remember { WebState() }
    state.allowImages = allowImages
    state.onLink = onLink
    AndroidView(
        modifier = modifier,
        factory = { ctx ->
            WebView(ctx).apply {
                layoutParams = ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
                setBackgroundColor(AColor.TRANSPARENT)
                isVerticalScrollBarEnabled = false
                isHorizontalScrollBarEnabled = false
                overScrollMode = WebView.OVER_SCROLL_NEVER
                settings.apply {
                    javaScriptEnabled = false
                    allowFileAccess = false
                    allowContentAccess = false
                    domStorageEnabled = false
                    javaScriptCanOpenWindowsAutomatically = false
                    setSupportMultipleWindows(false)
                    loadWithOverviewMode = true
                    useWideViewPort = false
                    builtInZoomControls = false
                    blockNetworkImage = true
                    blockNetworkLoads = true
                }
                webViewClient = object : WebViewClient() {
                    override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                        val url = request.url.toString()
                        if (!url.startsWith("about:")) state.onLink?.invoke(url)
                        return true
                    }

                    override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                        val scheme = request.url.scheme.orEmpty()
                        if (scheme == "data" || scheme == "about") return null
                        if (state.allowImages && (scheme == "http" || scheme == "https") && looksLikeImage(request)) return null
                        return WebResourceResponse("text/plain", "utf-8", ByteArrayInputStream(ByteArray(0)))
                    }
                }
            }
        },
        update = { v ->
            v.settings.blockNetworkLoads = !state.allowImages
            v.settings.blockNetworkImage = !state.allowImages
            val key = page.hashCode() * 31 + if (state.allowImages) 1 else 0
            if (v.tag != key) {
                v.tag = key
                v.loadDataWithBaseURL(null, page, "text/html", "utf-8", null)
            }
        },
        onRelease = { it.destroy() },
    )
    DisposableEffect(Unit) { onDispose { state.onLink = null } }
}

private class WebState {
    @Volatile var allowImages = false
    @Volatile var onLink: ((String) -> Unit)? = null
}

/** An image fetch (the request says so, or the path ends like one); trackers that pretend to be images still are. */
private fun looksLikeImage(r: WebResourceRequest): Boolean {
    val accept = r.requestHeaders["Accept"].orEmpty()
    if (accept.startsWith("image/")) return true
    val path = Uri.decode(r.url.path.orEmpty()).lowercase()
    return listOf(".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".ico").any { path.endsWith(it) }
}
