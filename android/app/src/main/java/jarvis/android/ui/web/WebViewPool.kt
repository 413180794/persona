package jarvis.android.ui.web

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.content.MutableContextWrapper
import android.graphics.Bitmap
import android.graphics.Rect
import android.net.Uri
import android.view.ActionMode
import android.view.Menu
import android.view.MenuItem
import android.view.View
import android.view.ViewGroup
import android.webkit.CookieManager
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.webkit.WebSettingsCompat
import androidx.webkit.WebViewFeature
import jarvis.android.data.JsonCodec
import kotlinx.serialization.json.JsonPrimitive

/** What the sheet shows about a pooled page (observable; written on the main thread by the WebView's callbacks). */
class PageState {
    var title by mutableStateOf("")
    var progress by mutableFloatStateOf(0f)
    var loading by mutableStateOf(true)
    var canGoBack by mutableStateOf(false)
}

/**
 * A WebView whose text-selection menu starts with 「AI 修改」 and 「作为上下文」 (iOS `SelectableWebView`). Chromium
 * asks its container view for the floating action mode, so wrapping the callback here is enough.
 */
@SuppressLint("ViewConstructor")
class SelectableWebView(val wrapper: MutableContextWrapper) : WebView(wrapper) {
    /** Where the view lives while pooled (the application, themed: see [WebViewPool.create]). */
    val home: Context = wrapper.baseContext

    val page = PageState()

    /** (selected text, "edit" | "context"); null = no extra menu items. */
    var onPick: ((String, String) -> Unit)? = null

    /** Labels of the two items (the inbox, with no chat to send to, has only 交给 Jarvis). */
    var pickLabels: Pair<String, String?> = "AI 修改" to "作为上下文"

    override fun startActionMode(callback: ActionMode.Callback?, type: Int): ActionMode? = super.startActionMode(wrap(callback), type)

    override fun startActionMode(callback: ActionMode.Callback?): ActionMode? = super.startActionMode(wrap(callback))

    private fun wrap(cb: ActionMode.Callback?): ActionMode.Callback? {
        if (cb == null || onPick == null) return cb
        return object : ActionMode.Callback2() {
            override fun onCreateActionMode(mode: ActionMode, menu: Menu): Boolean = cb.onCreateActionMode(mode, menu)

            // Chromium fills the menu here, each time the selection changes, with items that ask for an action button
            // (the floating toolbar puts those first). Ours do the same with lower orders, so they come first as on iOS.
            override fun onPrepareActionMode(mode: ActionMode, menu: Menu): Boolean {
                cb.onPrepareActionMode(mode, menu)
                menu.removeItem(ID_EDIT)
                menu.removeItem(ID_CONTEXT)
                val group = (0 until menu.size()).map { menu.getItem(it) }.minByOrNull { it.order }?.groupId ?: Menu.NONE
                menu.add(group, ID_EDIT, 0, pickLabels.first).setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS)
                pickLabels.second?.let { menu.add(group, ID_CONTEXT, 0, it).setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS) }
                return true
            }

            override fun onActionItemClicked(mode: ActionMode, item: MenuItem): Boolean {
                val kind = when (item.itemId) {
                    ID_EDIT -> "edit"
                    ID_CONTEXT -> "context"
                    else -> return cb.onActionItemClicked(mode, item)
                }
                // Read the selection before closing the menu: finishing first clears it, and the script runs later.
                pickSelection(kind) { mode.finish() }
                return true
            }

            override fun onDestroyActionMode(mode: ActionMode) = cb.onDestroyActionMode(mode)

            override fun onGetContentRect(mode: ActionMode, view: View, outRect: Rect) {
                if (cb is ActionMode.Callback2) cb.onGetContentRect(mode, view, outRect) else super.onGetContentRect(mode, view, outRect)
            }
        }
    }

    private fun pickSelection(kind: String, then: () -> Unit) {
        evaluateJavascript("window.getSelection().toString()") { raw ->
            // The result is a JSON string literal.
            val text = runCatching { (JsonCodec.parseToJsonElement(raw) as? JsonPrimitive)?.content }.getOrNull().orEmpty().trim()
            then()
            if (text.isNotEmpty()) onPick?.invoke(text, kind)
        }
    }

    companion object {
        private const val ID_EDIT = 0x4a01
        private const val ID_CONTEXT = 0x4a02
    }
}

/**
 * WebViews kept warm (iOS `WebViewPool`): links of fresh answers start loading in the background, so opening one shows
 * a finished page; at most [LIMIT] are kept, least recently used dropped. All share the default (on-disk) cookie store
 * and HTTP cache, so logins and assets carry over. Pages never get the hub token: they are other sites, `loadUrl` is
 * called without extra headers, and the hub's token lives only in OkHttp's requests. Main thread only. Each view lives
 * in a [MutableContextWrapper] over the application while pooled and over the activity while on screen.
 */
object WebViewPool {
    private const val LIMIT = 6
    private val views = LinkedHashMap<String, SelectableWebView>(8, 0.75f, true)

    /** A view for [url], already loading (or loaded) if it was preloaded. */
    fun view(ctx: Context, url: String): SelectableWebView {
        views[url]?.let { return it }
        val v = create(ctx.applicationContext, url)
        views[url] = v
        trim()
        return v
    }

    /** Starts loading links that may be opened soon. */
    fun preload(ctx: Context, urls: List<String>) {
        for (u in urls.take(3)) if (!views.containsKey(u) && LinkSafety.kind(u) == LinkSafety.Kind.WEB) view(ctx, u)
    }

    /** Drops least recently used views beyond [LIMIT]; views on screen are skipped. */
    private fun trim() {
        val it = views.entries.iterator()
        while (views.size > LIMIT && it.hasNext()) {
            val e = it.next()
            if (e.value.parent != null) continue
            it.remove()
            e.value.destroy()
        }
    }

    /** Memory pressure: destroy every pooled view that is not on screen (each one is a live renderer). */
    fun trimMemory() {
        val it = views.entries.iterator()
        while (it.hasNext()) {
            val e = it.next()
            if (e.value.parent != null) continue
            it.remove()
            e.value.destroy()
        }
    }

    private fun drop(v: WebView) {
        val it = views.entries.iterator()
        while (it.hasNext()) if (it.next().value === v) it.remove()
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun create(app: Context, url: String): SelectableWebView {
        // The application context carries no theme of its own (it would read as light), and WebView darkens pages only
        // when its context's theme is a dark one: give it the app theme, which follows the system's dark mode.
        val v = SelectableWebView(MutableContextWrapper(android.view.ContextThemeWrapper(app, jarvis.android.R.style.Theme_Jarvis)))
        v.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            loadWithOverviewMode = true
            useWideViewPort = true
            builtInZoomControls = true
            displayZoomControls = false
            mediaPlaybackRequiresUserGesture = true
            allowFileAccess = false
            allowContentAccess = false
            javaScriptCanOpenWindowsAutomatically = false
            setSupportMultipleWindows(false)
            mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
            // A page seen before paints from disk at once (iOS returnCacheDataElseLoad); later navigations are normal.
            cacheMode = WebSettings.LOAD_CACHE_ELSE_NETWORK
        }
        if (WebViewFeature.isFeatureSupported(WebViewFeature.ALGORITHMIC_DARKENING)) {
            WebSettingsCompat.setAlgorithmicDarkeningAllowed(v.settings, true)
        }
        CookieManager.getInstance().setAcceptThirdPartyCookies(v, true)
        v.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                val u = request.url
                if (u.scheme == "http" || u.scheme == "https") return false
                // mailto:, tel:, intent:, other apps' links: to the system, but only when the user tapped something on a
                // page that is on screen (preloaded pages and scripts can't launch apps), and never our own jarvis: links.
                if (!request.hasGesture() || view.parent == null) return true
                val intent = runCatching { externalIntent(u.toString(), app.packageName) }.getOrNull() ?: return true
                runCatching { view.context.startActivity(intent) }
                return true
            }

            override fun onPageStarted(view: WebView, url: String?, favicon: Bitmap?) {
                v.page.loading = true
            }

            override fun onPageFinished(view: WebView, url: String?) {
                v.page.loading = false
                v.page.canGoBack = view.canGoBack()
                view.settings.cacheMode = WebSettings.LOAD_DEFAULT
                // A preloaded page that is not shown: stop its media / timers until it is opened.
                if (view.parent == null) view.onPause()
            }

            override fun doUpdateVisitedHistory(view: WebView, url: String?, isReload: Boolean) {
                v.page.canGoBack = view.canGoBack()
            }

            // A crashed or killed renderer must not take the app with it: forget the view, the next open makes a new one.
            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail?): Boolean {
                drop(view)
                (view.parent as? ViewGroup)?.removeView(view)
                view.destroy()
                return true
            }
        }
        v.webChromeClient = object : WebChromeClient() {
            override fun onReceivedTitle(view: WebView, title: String?) {
                if (!title.isNullOrBlank()) v.page.title = title
            }

            override fun onProgressChanged(view: WebView, newProgress: Int) {
                v.page.progress = newProgress / 100f
            }
        }
        v.loadUrl(url)
        return v
    }
}

/**
 * The intent a non-http link may start, or null to drop it: `intent:` URIs lose their component, selector, clip data
 * and every URI-grant flag (a page could otherwise make us grant another app our files, or start one of our own
 * unexported components); `content:` / `file:` data, our `jarvis` scheme and our own package are refused.
 */
fun externalIntent(url: String, ownPackage: String): Intent? {
    if (LinkSafety.kind(url) != LinkSafety.Kind.EXTERNAL) return null
    val scheme = url.trim().substringBefore(':').lowercase()
    val intent = if (scheme == "intent") Intent.parseUri(url, Intent.URI_INTENT_SCHEME) else Intent(Intent.ACTION_VIEW, Uri.parse(url))
    intent.component = null
    intent.selector = null
    intent.clipData = null
    intent.flags = intent.flags and GRANT_FLAGS.inv()
    val ds = intent.data?.scheme?.lowercase()
    if (ds != null && (ds == "jarvis" || ds == "intent" || LinkSafety.isRefusedScheme(ds))) return null
    if (intent.`package` == ownPackage) return null
    intent.addCategory(Intent.CATEGORY_BROWSABLE).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    return intent
}

private const val GRANT_FLAGS = Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION or
    Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION or Intent.FLAG_GRANT_PREFIX_URI_PERMISSION
