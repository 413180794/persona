package persona.feature.chat

import android.content.Context
import android.graphics.Rect
import android.os.Message
import android.view.ActionMode
import android.view.Menu
import android.view.MenuItem
import android.view.View
import android.view.ViewGroup
import android.webkit.*
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.webkit.*
import java.util.concurrent.Executors
import org.json.JSONTokener
import kotlinx.coroutines.CompletableDeferred

/** Owned by one chat host; never retains a departed activity or a different account. */
internal class ChatWebViewPool(private val context: Context) {
    private val pages = LinkedHashMap<String, ChatSelectionWebView>(3, 0.75f, true)
    private var active: ChatSelectionWebView? = null
    private val speculative = mutableSetOf<String>()
    private val popups = mutableSetOf<WebView>()
    private val startupExecutor = Executors.newSingleThreadExecutor()
    private var started = false
    private val startup = CompletableDeferred<Unit>()

    suspend fun warmUp() {
        if (!started) {
            started = true
            WebViewCompat.startUpWebView(context.applicationContext, WebViewStartUpConfig.Builder(startupExecutor).build(),
                object : WebViewOutcomeReceiver<WebViewStartUpResult, WebViewStartupException> {
                    override fun onResult(result: WebViewStartUpResult) { startup.complete(Unit) }
                    override fun onError(error: WebViewStartupException) { startup.complete(Unit) }
                })
        }
        startup.await()
    }

    fun acquire(url: String): ChatSelectionWebView {
        val view = pages[url] ?: create().also { pages[url] = it; it.loadUrl(url) }
        active?.takeIf { it !== view }?.let { mute(it, true); it.onPause() }
        active = view
        if (speculative.remove(url)) view.loadUrl(url)
        mute(view, false)
        view.onResume()
        trim()
        return view
    }

    @androidx.annotation.OptIn(markerClass = [Profile.ExperimentalUrlPrefetch::class])
    fun preload(url: String) {
        if (pages.containsKey(url)) return
        val view = create()
        pages[url] = view
        if (WebViewFeature.isFeatureSupported(WebViewFeature.PRERENDER_WITH_URL)) {
            speculative.add(url)
            WebViewCompat.prerenderUrlAsync(view, url, null, context.mainExecutor, object : PrerenderOperationCallback {
                override fun onPrerenderActivated() = Unit
                override fun onError(error: PrerenderException) {
                    if (pages[url] === view && speculative.remove(url)) view.loadUrl(url)
                }
            })
        } else view.loadUrl(url)
        mute(view, true)
        view.onPause()
        trim()
    }

    fun release(view: ChatSelectionWebView) {
        if (view !in pages.values) return
        view.onPick = null
        mute(view, true)
        (view.parent as? ViewGroup)?.removeView(view)
        view.onPause()
        if (active === view) active = null
    }

    private fun trim() {
        while (pages.size > 3) {
            val entry = pages.entries.firstOrNull { it.value !== active } ?: return
            pages.remove(entry.key)
            speculative.remove(entry.key)
            entry.value.destroy()
        }
    }

    @Suppress("SetJavaScriptEnabled")
    private fun create() = ChatSelectionWebView(context).apply {
        settings.javaScriptEnabled = true
        settings.domStorageEnabled = true
        settings.allowFileAccess = false
        settings.allowContentAccess = false
        settings.mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
        settings.cacheMode = WebSettings.LOAD_DEFAULT
        settings.setSupportMultipleWindows(true)
        settings.javaScriptCanOpenWindowsAutomatically = false
        if (WebViewFeature.isFeatureSupported(WebViewFeature.ALGORITHMIC_DARKENING)) {
            WebSettingsCompat.setAlgorithmicDarkeningAllowed(settings, true)
        }
        webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                if (ChatLinks.webUrl(request.url.toString()) != null) return false
                if (request.isForMainFrame) failure = "此链接无法在网页中打开"
                return true
            }
            override fun onPageStarted(view: WebView, url: String?, favicon: android.graphics.Bitmap?) {
                failure = null; currentUrl = url.orEmpty(); loadProgress = 0
            }
            override fun onPageFinished(view: WebView, url: String?) {
                currentUrl = url.orEmpty(); pageTitle = view.title.orEmpty(); loadProgress = 100
            }
            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                if (request.isForMainFrame) failure = "网页未能载入，请重试"
            }
            override fun onReceivedHttpError(view: WebView, request: WebResourceRequest, response: WebResourceResponse) {
                if (request.isForMainFrame) failure = "网页暂时无法访问，请重试"
            }
        }
        webChromeClient = object : WebChromeClient() {
            override fun onProgressChanged(view: WebView, value: Int) { loadProgress = value }
            override fun onReceivedTitle(view: WebView, title: String?) { pageTitle = title.orEmpty() }
            override fun onCreateWindow(view: WebView, isDialog: Boolean, isUserGesture: Boolean, resultMsg: Message): Boolean {
                if (!isUserGesture) return false
                // Route target=_blank back to this view without handing off to another app.
                val popup = WebView(context)
                popups.add(popup)
                popup.webViewClient = object : WebViewClient() {
                    override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                        ChatLinks.webUrl(request.url.toString())?.let { this@apply.loadUrl(it) }
                        popup.post { if (popups.remove(popup)) popup.destroy() }
                        return true
                    }
                }
                (resultMsg.obj as WebView.WebViewTransport).webView = popup
                resultMsg.sendToTarget()
                return true
            }
        }
    }

    private fun mute(view: WebView, muted: Boolean) {
        if (WebViewFeature.isFeatureSupported(WebViewFeature.MUTE_AUDIO)) WebViewCompat.setAudioMuted(view, muted)
    }

    fun pause() { pages.values.forEach { mute(it, true); it.onPause() } }
    fun resume() { active?.let { mute(it, false); it.onResume() } }
    fun destroy() { popups.forEach { it.destroy() }; popups.clear(); pages.values.forEach { (it.parent as? ViewGroup)?.removeView(it); it.destroy() }; pages.clear(); speculative.clear(); active = null; startupExecutor.shutdown() }
}

internal class ChatSelectionWebView(context: Context) : WebView(context) {
    var onPick: ((ChatLinkContext) -> Unit)? = null
    var currentUrl by mutableStateOf("")
    var pageTitle by mutableStateOf("")
    var failure by mutableStateOf<String?>(null)
    var loadProgress by mutableIntStateOf(0)

    override fun startActionMode(callback: ActionMode.Callback): ActionMode? = super.startActionMode(wrap(callback))
    override fun startActionMode(callback: ActionMode.Callback, type: Int): ActionMode? = super.startActionMode(wrap(callback), type)

    private fun wrap(delegate: ActionMode.Callback) = object : ActionMode.Callback2() {
        override fun onCreateActionMode(mode: ActionMode, menu: Menu): Boolean {
            if (!delegate.onCreateActionMode(mode, menu)) return false
            if (menu.findItem(CONTEXT) == null) {
                menu.add(Menu.NONE, EDIT, Menu.NONE, "AI 修改")
                menu.add(Menu.NONE, CONTEXT, Menu.NONE, "作为上下文")
            }
            return true
        }
        override fun onPrepareActionMode(mode: ActionMode, menu: Menu) = delegate.onPrepareActionMode(mode, menu)
        override fun onActionItemClicked(mode: ActionMode, item: MenuItem): Boolean {
            if (item.itemId != EDIT && item.itemId != CONTEXT) return delegate.onActionItemClicked(mode, item)
            val sourceUrl = url.orEmpty()
            val sourceTitle = title.orEmpty()
            evaluateJavascript("(function(){var e=document.activeElement;return e&&(e.tagName==='TEXTAREA'||e.tagName==='INPUT')&&e.type!=='password'&&typeof e.selectionStart==='number'?e.value.substring(e.selectionStart,e.selectionEnd):window.getSelection().toString();})()") { result ->
                val selected = runCatching { JSONTokener(result).nextValue() as? String }.getOrNull().orEmpty()
                if (selected.isNotBlank() && url == sourceUrl) onPick?.invoke(ChatLinkContext(
                    if (item.itemId == EDIT) "edit" else "context", sourceTitle, sourceUrl, selected))
                mode.finish()
            }
            return true
        }
        override fun onDestroyActionMode(mode: ActionMode) = delegate.onDestroyActionMode(mode)
        override fun onGetContentRect(mode: ActionMode, view: View, outRect: Rect) {
            if (delegate is ActionMode.Callback2) delegate.onGetContentRect(mode, view, outRect)
            else super.onGetContentRect(mode, view, outRect)
        }
    }

    companion object {
        private const val EDIT = 0x6a0101
        private const val CONTEXT = 0x6a0102
    }
}
