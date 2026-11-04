package persona.feature.inbox

import android.graphics.Canvas
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.ui.draw.clip
import persona.core.designsystem.PersonaTheme
import androidx.compose.ui.viewinterop.AndroidView
import org.jsoup.Jsoup
import org.jsoup.safety.Safelist
import java.io.ByteArrayInputStream

internal fun safeMailHtml(
    html: String,
    foreground: String,
    background: String,
    link: String,
    allowImages: Boolean = false,
): String {
    val allowed = Safelist.basic().addTags("table", "thead", "tbody", "tfoot", "tr", "th", "td", "hr")
        .removeProtocols("a", "href", "ftp")
    if (allowImages) {
        allowed.addTags("img")
            .addAttributes("img", "src", "alt", "width", "height")
            .addProtocols("img", "src", "http", "https", "data")
    }
    val body = Jsoup.clean(html, allowed)
    val csp = if (allowImages) {
        "default-src 'none'; img-src http: https: data:; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'"
    } else {
        "default-src 'none'; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'"
    }
    return """<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">
        <meta http-equiv="Content-Security-Policy" content="$csp">
        <style>html,body{margin:0;padding:0;background:$background;color:$foreground;font:16px/1.55 sans-serif;overflow-wrap:anywhere}
        body{padding:14px 16px}
        img{max-width:100%;height:auto}
        a{color:$link}pre{white-space:pre-wrap}table{max-width:100%;table-layout:fixed;border-collapse:collapse}
        td,th{overflow-wrap:anywhere;border:1px solid currentColor;padding:4px}blockquote{margin:8px;padding-left:12px;border-left:2px solid currentColor}</style>
        </head><body>$body</body></html>""".trimIndent()
}

internal fun mailPlainText(html: String): String = Jsoup.parse(html).text()

@Composable
internal fun MailHtml(html: String, allowImages: Boolean = false, onLink: (String) -> Unit) {
    val colors = PersonaTheme.palette
    fun css(value: androidx.compose.ui.graphics.Color) = "#%06x".format(value.toArgb() and 0xffffff)
    val document = remember(html, colors.ink, colors.surfaceOpaque, colors.accent, allowImages) {
        safeMailHtml(html, css(colors.ink), css(colors.surfaceOpaque), css(colors.accent), allowImages)
    }
    var height by remember(document) { mutableIntStateOf(1) }
    val openLink by rememberUpdatedState(onLink)
    key(document, allowImages) {
        AndroidView(
            modifier = Modifier.fillMaxWidth().clip(PersonaTheme.shapes.cardShape).height(height.dp),
            factory = { context ->
                object : WebView(context) {
                    override fun onDraw(canvas: Canvas) {
                        super.onDraw(canvas)
                        val measured = contentHeight.coerceAtLeast(1)
                        if (measured != height) post { height = measured }
                    }
                }.apply {
                    setBackgroundColor(colors.surfaceOpaque.toArgb())
                    settings.javaScriptEnabled = false
                    settings.domStorageEnabled = false
                    settings.allowFileAccess = false
                    settings.allowContentAccess = false
                    settings.blockNetworkLoads = !allowImages
                    settings.blockNetworkImage = !allowImages
                    settings.loadsImagesAutomatically = allowImages
                    settings.mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
                    settings.cacheMode = WebSettings.LOAD_NO_CACHE
                    settings.setSupportMultipleWindows(false)
                    isVerticalScrollBarEnabled = false
                    isHorizontalScrollBarEnabled = false
                    webViewClient = object : WebViewClient() {
                        override fun shouldInterceptRequest(view: WebView?, request: WebResourceRequest?): WebResourceResponse? {
                            if (request != null) {
                                val scheme = request.url.scheme.orEmpty().lowercase()
                                if (scheme in setOf("data", "about")) return null
                                if (allowImages && scheme in setOf("http", "https")) {
                                    val accept = request.requestHeaders["Accept"].orEmpty()
                                    val path = request.url.path.orEmpty().lowercase()
                                    val isImage = accept.startsWith("image/") ||
                                        listOf(".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".ico")
                                            .any { path.endsWith(it) }
                                    if (isImage) return null
                                }
                            }
                            return WebResourceResponse("text/plain", "utf-8", ByteArrayInputStream(ByteArray(0)))
                        }

                        override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest): Boolean {
                            val url = request.url.toString()
                            val scheme = request.url.scheme.orEmpty().lowercase()
                            if (scheme in setOf("http", "https", "mailto")) {
                                openLink(url)
                            }
                            return true
                        }

                        @Deprecated("Legacy WebView callback")
                        override fun shouldOverrideUrlLoading(view: WebView?, url: String?): Boolean {
                            if (url != null) openLink(url)
                            return true
                        }
                    }
                    loadDataWithBaseURL(null, document, "text/html", "utf-8", null)
                }
            },
            onRelease = { it.stopLoading(); it.destroy() },
            update = {},
        )
    }
}
