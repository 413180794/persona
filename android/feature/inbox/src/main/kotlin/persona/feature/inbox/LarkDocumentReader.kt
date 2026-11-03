package persona.feature.inbox

import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.*
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.contracts.api.ConnectionState
import persona.core.navigation.ConnectionSettingsRoute
import androidx.compose.ui.draw.clip
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

internal val LocalLarkLinkOpener = staticCompositionLocalOf<((String, LarkContentSource?) -> Unit)?> { null }
private data class LarkLink(val url: String, val source: LarkContentSource?)
private enum class ReaderDetent { Full, Peek, Hidden }

/** In-app links open full height; the drag handle leaves the conversation interactive at the peek detent. */
@Composable
internal fun LarkLinkHost(repository: InboxRepository, content: @Composable () -> Unit) {
    var target by remember { mutableStateOf<LarkLink?>(null) }
    val parentOccluded = LocalLarkReadOccluded.current
    val scope = rememberCoroutineScope()
    // A web page over the page: no glass may sample its WebView, here or in the page around it (LocalWebSheets).
    WebSheetShown(target != null)
    CompositionLocalProvider(LocalLarkLinkOpener provides { url, source ->
        larkWebUrl(url)?.let { target = LarkLink(it, source) }
    }, LocalLarkReadOccluded provides (parentOccluded || target != null)) {
        BoxWithConstraints(Modifier.fillMaxSize()) {
            content()
            val pixels = with(LocalDensity.current) { maxHeight.toPx() }
            val drag = remember { AnchoredDraggableState(ReaderDetent.Hidden) }
            SideEffect { drag.updateAnchors(DraggableAnchors {
                ReaderDetent.Full at 0f; ReaderDetent.Peek at pixels * .4f; ReaderDetent.Hidden at pixels
            }) }
            LaunchedEffect(target) { if (target != null) drag.animateTo(ReaderDetent.Full) }
            LaunchedEffect(drag.settledValue) {
                if (drag.settledValue == ReaderDetent.Hidden && !drag.isAnimationRunning) target = null
            }
            target?.let { link ->
                BackHandler { scope.launch { drag.animateTo(ReaderDetent.Hidden); target = null } }
                val height = with(LocalDensity.current) {
                    (pixels - (drag.offset.takeIf { it.isFinite() } ?: pixels)).coerceIn(0f, pixels).toDp()
                }
                CompositionLocalProvider(LocalGlassLevel provides GlassLevel.Solid) {
                Box(Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(height)
                    .clip(PersonaTheme.shapes.sheetShape).background(PersonaTheme.palette.surfaceOpaque)) {
                    Column {
                        Column(Modifier.fillMaxWidth().anchoredDraggable(drag, Orientation.Vertical)) {
                            Box(Modifier.align(Alignment.CenterHorizontally).padding(Tokens.Space.s).size(40.dp, 4.dp)
                                .background(PersonaTheme.palette.ink3, PersonaTheme.shapes.controlShape))
                            Row(Modifier.fillMaxWidth().padding(end = Tokens.Space.m), verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                                InboxConnectionTitle(repository, if (isLarkDocumentUrl(link.url)) "飞书文档" else "网页",
                                    Modifier.weight(1f).padding(start = Tokens.Space.m))
                                JIconButton(if (drag.currentValue == ReaderDetent.Full) PhIcons.CaretDown else PhIcons.CaretUp,
                                    if (drag.currentValue == ReaderDetent.Full) "收起" else "展开", onClick = {
                                    scope.launch { drag.animateTo(if (drag.currentValue == ReaderDetent.Full) ReaderDetent.Peek else ReaderDetent.Full) }
                                })
                                JIconButton(PhIcons.X, "关闭", onClick = { scope.launch { drag.animateTo(ReaderDetent.Hidden); target = null } })
                            }
                        }
                        LarkDocumentReader(link.url, link.source, repository, Modifier.weight(1f), showConnectionTitle = false)
                    }
                }
                }
            }
        }
    }
}

/** The shared conversation link host can use the current inbox pairing without changing the app shell. */
@Composable
fun LarkDocumentLink(url: String, modifier: Modifier = Modifier, webpageContent: @Composable () -> Unit) {
    val repository = injectedInboxRepository()
    val snapshot by repository.snapshot.collectAsStateWithLifecycle()
    LarkDocumentReader(url, snapshot.contentSource, repository, modifier, webpageContent)
}

/** Reusable native reader for a pairing-bound source; every read failure falls back to the original webpage. */
@Composable
fun LarkDocumentReader(url: String, source: LarkContentSource?, repository: InboxRepository, modifier: Modifier = Modifier,
    webpageContent: (@Composable () -> Unit)? = null,
    showConnectionTitle: Boolean = true,
) {
    var document by remember(url, source?.scope) { mutableStateOf<LarkDocument?>(null) }
    var webpage by remember(url, source?.scope) { mutableStateOf(!isLarkDocumentUrl(url) || source == null) }
    LaunchedEffect(url, source?.scope) {
        if (!webpage && source != null) {
            try { document = withContext(Dispatchers.Default) { source.document(url) } }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { webpage = true }
        }
    }
    val navigator = LocalInboxNavigator.current
    val connection by repository.connectionState.collectAsStateWithLifecycle(ConnectionState.Connecting)
    JPage(title = if (connection == ConnectionState.Online) document?.title ?: "飞书文档" else inboxTitle(connection), modifier = modifier,
        titleBusy = connection in setOf(ConnectionState.Connecting, ConnectionState.Updating),
        onTitleClick = if (connection == ConnectionState.Online || navigator == null) null else ({ navigator.navigate(ConnectionSettingsRoute) }),
        onBack = navigator?.let { { it.back() } },
        topBar = if (showConnectionTitle) null else ({}),
        actions = { JTextButton("切换网页", { webpage = true }) }) { padding ->
        if (webpage) {
            Box(Modifier.fillMaxSize().padding(padding).padding(horizontal = pagePadding)) {
                if (webpageContent != null) webpageContent() else LarkDocumentWebView(url, Modifier.fillMaxSize())
            }
        } else if (document == null) {
            LazyColumn(contentPadding = PaddingValues(start = pagePadding, end = pagePadding,
                top = padding.calculateTopPadding(), bottom = padding.calculateBottomPadding())) {
                item { JLoading(text = "正在读取文档") }
                item { JButton("切换网页", onClick = { webpage = true }) }
            }
        } else {
            val loaded = requireNotNull(document)
            val body: @Composable () -> Unit = {
                LazyColumn(contentPadding = PaddingValues(start = pagePadding, end = pagePadding,
                    top = padding.calculateTopPadding() + Tokens.Space.m, bottom = padding.calculateBottomPadding() + Tokens.Space.m),
                    verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                    item {
                        Text(loaded.title, style = PersonaTheme.type.title, color = PersonaTheme.palette.ink)
                        if (!showConnectionTitle) JTextButton("切换网页", { webpage = true })
                    }
                    items(loaded.blocks) { block ->
                        when (block.kind) {
                            "heading" -> Text(block.text, style = if (block.level <= 2) PersonaTheme.type.title else PersonaTheme.type.headline,
                                color = PersonaTheme.palette.ink)
                            "codeBlock" -> JCard(padding = Tokens.Space.m) {
                                if (block.language.isNotBlank()) Text(block.language, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
                                Text(block.text, Modifier.horizontalScroll(rememberScrollState()), style = PersonaTheme.type.code, color = PersonaTheme.palette.ink)
                            }
                            "image" -> if (block.image.isBlank()) Text(block.text.ifBlank { "图片暂不可用，可切换网页查看" },
                                style = PersonaTheme.type.body, color = PersonaTheme.palette.ink2)
                                else LarkPartContent(requireNotNull(source).image(block.image, document = true), repository) { }
                            "divider" -> JDivider()
                            else -> Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                                if (block.marker.isNotEmpty()) Text(block.marker, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink3)
                                LarkLinkedText(block.text, source, Modifier.weight(1f))
                            }
                        }
                    }
                    if (loaded.partial) item { JNotice("文档较长，只显示了部分内容，可切换网页继续阅读") }
                }
            }
            if (loaded.copyAllowed) SelectionContainer { body() } else body()
        }
    }
}

@Composable
private fun LarkDocumentWebView(url: String, modifier: Modifier) {
    val context = LocalContext.current
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    var failed by remember(url) { mutableStateOf(false) }
    val pick = LocalInboxWebSelection.current
    val web = remember(url) {
        InboxSelectionWebView(context).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccess = false
            settings.allowContentAccess = false
            settings.mixedContentMode = android.webkit.WebSettings.MIXED_CONTENT_NEVER_ALLOW
            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                    if (larkWebUrl(request.url.toString()) == null) return true
                    return false
                }
                override fun onReceivedError(view: WebView, request: WebResourceRequest, error: android.webkit.WebResourceError) {
                    if (request.isForMainFrame) failed = true
                }
            }
            larkWebUrl(url)?.let { loadUrl(it) }
        }
    }
    DisposableEffect(web, lifecycle) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_STOP) web.onPause()
            if (event == Lifecycle.Event.ON_START) web.onResume()
        }
        lifecycle.addObserver(observer)
        onDispose { lifecycle.removeObserver(observer); web.stopLoading(); web.destroy() }
    }
    // Text picked on the page can go to Persona (the app shell supplies it; LocalInboxWebSelection).
    SideEffect { web.onPick = pick }
    BackHandler(enabled = web.canGoBack()) { web.goBack() }
    Column(modifier) {
        if (failed) Row(verticalAlignment = Alignment.CenterVertically) {
            Text("网页加载失败", Modifier.weight(1f).padding(Tokens.Space.m), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
            JButton("重试", onClick = { failed = false; web.reload() })
        }
        AndroidView(factory = { web }, modifier = Modifier.fillMaxWidth().weight(1f))
    }
}
