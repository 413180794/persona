package persona.feature.chat

import persona.core.domain.ports.PortLinks
import android.content.ComponentCallbacks2
import android.content.Intent
import android.content.res.Configuration
import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.*
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.platform.*
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.currentStateAsState
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.feature.inbox.LarkDocumentLink
import persona.feature.inbox.LarkNativeLink
import persona.feature.inbox.larkNativeLink
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

internal val LocalChatLinkOpening = staticCompositionLocalOf { 0 }

private enum class LinkDetent { Full, Peek, Hidden }

/**
 * Shared URI boundary for Agent, assistant, and other conversation surfaces. http(s) opens in the sheet, `jarvis://`
 * links navigate ([LocalChatAppLinks]), mailto: / tel: / other apps' links go out after asking, the rest is refused.
 *
 * In a conversation ([onContext], [onEdit]) text picked on a page becomes 作为上下文 or AI 修改; with [onHandToPersona]
 * (pages with no conversation behind them: the app shell, the knowledge base) it is 交给 Persona instead.
 */
@Composable
fun ChatLinkHost(
    modifier: Modifier = Modifier,
    preloadUrls: List<String> = emptyList(),
    isScrolling: Boolean = false,
    onContext: ((ChatLinkContext) -> Unit)? = null,
    onEdit: (suspend (ChatLinkContext, String) -> Unit)? = null,
    onVisibilityChanged: (Boolean) -> Unit = {},
    /** Sends a whole message to Persona's main conversation (and shows it); the sheet closes once it went. */
    onHandToPersona: (suspend (String) -> Unit)? = null,
    content: @Composable (bottomInset: Dp) -> Unit,
) {
    val context = LocalContext.current
    val pool = remember(context) { ChatWebViewPool(context) }
    val scope = rememberCoroutineScope()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val lifecycleState by lifecycle.currentStateAsState()
    val feedback = LocalAppFeedback.current
    val appLinks by rememberUpdatedState(LocalChatAppLinks.current)
    val lark = LocalLarkFeatures.current
    var url by remember { mutableStateOf<String?>(null) }
    /** "机器名 :端口" while the sheet shows one of the machine's ports (docs/ports.md「转发」). */
    var forwarded by remember { mutableStateOf<String?>(null) }
    val ports by rememberUpdatedState(LocalChatPorts.current)
    var unsupported by remember { mutableStateOf(false) }
    var external by remember { mutableStateOf<String?>(null) }
    var opening by remember { mutableIntStateOf(0) }
    var closeRequest by remember { mutableIntStateOf(0) }
    val uriHandler = remember { object : UriHandler {
        override fun openUri(uri: String) {
            when (ChatLinks.kind(uri)) {
                ChatLinkKind.Web -> {
                    val target = ChatLinks.webUrl(uri)
                    // A link to this machine's localhost is one of its ports, opened through the hub (docs/ports.md).
                    val machine = ports
                    val port = machine?.let { PortLinks.link(uri, it.hostnames) }
                    when {
                        target == null -> unsupported = true
                        machine != null && port != null -> scope.launch {
                            runCatching { machine.opener.open(machine.machineId, port.port, port.path, port.secure, port.port in machine.listed) }
                                .onSuccess { local -> pool.forget(local); url = local; forwarded = "${machine.name} :${port.port}"; opening++ }
                                .onFailure { error -> if (error !is kotlinx.coroutines.CancellationException) feedback?.notice(error.message ?: "打不开端口 :${port.port}") }
                        }
                        else -> { url = target; forwarded = null; opening++ }
                    }
                }
                ChatLinkKind.App -> {
                    val link = ChatLinks.appLink(uri)
                    val navigate = appLinks
                    if (link == null || navigate == null) unsupported = true
                    else { if (url != null) closeRequest++; navigate(link) }
                }
                ChatLinkKind.External -> { external = uri.trim() }
                ChatLinkKind.Refused -> { unsupported = true }
            }
        }
    } }
    val visibility by rememberUpdatedState(onVisibilityChanged)
    LaunchedEffect(url) { visibility(url != null) }
    DisposableEffect(pool, lifecycle) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_STOP) pool.pause()
            if (event == Lifecycle.Event.ON_START) pool.resume()
        }
        lifecycle.addObserver(observer)
        onDispose { lifecycle.removeObserver(observer); pool.destroy() }
    }
    // Memory pressure (the app in the background, or the system asks): pages not on screen go; each is a renderer.
    DisposableEffect(pool, context) {
        val callbacks = object : ComponentCallbacks2 {
            override fun onTrimMemory(level: Int) { if (level >= ComponentCallbacks2.TRIM_MEMORY_UI_HIDDEN) pool.trimMemory() }
            override fun onConfigurationChanged(newConfig: Configuration) = Unit
            @Deprecated("Superseded by onTrimMemory")
            override fun onLowMemory() = pool.trimMemory()
        }
        context.applicationContext.registerComponentCallbacks(callbacks)
        onDispose { context.applicationContext.unregisterComponentCallbacks(callbacks) }
    }
    LaunchedEffect(preloadUrls, isScrolling, lifecycleState) {
        if (!isScrolling && lifecycleState.isAtLeast(Lifecycle.State.STARTED)) {
            delay(600)
            pool.warmUp()
            // A machine's port is never preloaded: it would load the phone's own localhost (docs/ports.md).
            preloadUrls.mapNotNull { ChatLinks.webUrl(it) }.filter { PortLinks.link(it, ports?.hostnames.orEmpty()) == null }
                .take(3).forEach { pool.preload(it) }
        }
    }
    val picks = if (onHandToPersona != null) listOf(ChatLinkPick.HandToPersona)
        else listOfNotNull(ChatLinkPick.Edit.takeIf { onEdit != null }, ChatLinkPick.Context.takeIf { onContext != null })
    // A web page is over the page: the page's glass goes opaque and the dock makes way (LocalWebSheets).
    WebSheetShown(url != null)
    // A conversation embedded under its host's bar (Persona's page) has the card start below that bar, not under it.
    val hostBar = LocalChromeInsets.current.top
    CompositionLocalProvider(LocalUriHandler provides uriHandler, LocalChatLinkOpening provides opening) {
        BoxWithConstraints(modifier.fillMaxSize()) {
            val height = maxHeight
            val density = LocalDensity.current
            val pixels = with(density) { height.toPx() }
            val above = with(density) { hostBar.toPx() }.coerceIn(0f, pixels)
            val drag = remember { AnchoredDraggableState(LinkDetent.Hidden) }
            SideEffect {
                drag.updateAnchors(DraggableAnchors {
                    LinkDetent.Full at above
                    LinkDetent.Peek at above + (pixels - above) * 0.4f
                    LinkDetent.Hidden at pixels
                })
            }
            LaunchedEffect(opening) { if (url != null) drag.animateTo(LinkDetent.Full) }
            LaunchedEffect(closeRequest) { if (closeRequest > 0 && url != null) drag.animateTo(LinkDetent.Hidden) }
            LaunchedEffect(drag.settledValue) { if (drag.settledValue == LinkDetent.Hidden && !drag.isAnimationRunning) { url = null; forwarded = null } }
            content(if (url != null && drag.currentValue == LinkDetent.Peek) (height - hostBar.coerceAtMost(height)) * 0.6f else 0.dp)
            url?.let { target ->
                // A page whose renderer went away is loaded anew (a new view) when the user asks.
                var generation by remember(target) { mutableIntStateOf(0) }
                val view = remember(pool, target, generation) { pool.acquire(target) }
                var editing by remember(target) { mutableStateOf<ChatLinkContext?>(null) }
                var handing by remember(target) { mutableStateOf<ChatLinkContext?>(null) }
                var menu by remember { mutableStateOf(false) }
                val pick by rememberUpdatedState(onContext)
                DisposableEffect(view, picks) {
                    view.picks = picks
                    view.onPick = { kind, selected ->
                        when (kind) {
                            ChatLinkPick.Edit -> { editing = selected }
                            ChatLinkPick.Context -> { pick?.invoke(selected); scope.launch { drag.animateTo(LinkDetent.Peek) } }
                            ChatLinkPick.HandToPersona -> { handing = selected }
                        }
                    }
                    view.onExternal = { external = it }
                    onDispose { pool.release(view) }
                }
                fun close() { scope.launch { drag.animateTo(LinkDetent.Hidden) } }
                BackHandler {
                    if (!view.gone && view.canGoBack()) view.goBack() else close()
                }
                val offset = drag.offset.takeIf { it.isFinite() } ?: pixels
                val panelHeight = with(density) { (pixels - offset).coerceIn(0f, pixels).toDp() }
                val palette = PersonaTheme.palette
                val shape = PersonaTheme.shapes.sheetShape
                // No glass inside the card either: its page would put the WebView in a glass source (LocalWebSheets).
                // The card sits below the host's bar and over the dock, so its page keeps no room for them.
                CompositionLocalProvider(LocalGlassLevel provides GlassLevel.Solid, LocalChromeInsets provides ChromeInsets()) {
                Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(panelHeight)
                    .shadow(Tokens.Space.s, shape).clip(shape).background(palette.surfaceOpaque)) {
                    Column(Modifier.fillMaxWidth().anchoredDraggable(drag, Orientation.Vertical)) {
                        Box(Modifier.align(Alignment.CenterHorizontally).padding(top = Tokens.Space.s).size(Tokens.Space.xxxl, Tokens.Space.xs)
                            .clip(PersonaTheme.shapes.controlShape).background(palette.ink3))
                        Row(Modifier.fillMaxWidth().padding(end = Tokens.Space.xs), verticalAlignment = Alignment.CenterVertically) {
                            JIconButton(PhIcons.CaretLeft, "网页返回", { if (!view.gone && view.canGoBack()) view.goBack() else close() })
                            Column(Modifier.weight(1f)) {
                                Text(view.pageTitle.ifBlank { "网页" }, style = PersonaTheme.type.bodyStrong, color = palette.ink,
                                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                                Text(forwarded ?: view.currentUrl.ifBlank { target }, style = PersonaTheme.type.micro, color = palette.ink3,
                                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                            }
                            JIconButton(if (drag.currentValue == LinkDetent.Full) PhIcons.CaretDown else PhIcons.CaretUp,
                                if (drag.currentValue == LinkDetent.Full) "收起到六成，继续对话" else "铺满网页",
                                { scope.launch { drag.animateTo(if (drag.currentValue == LinkDetent.Full) LinkDetent.Peek else LinkDetent.Full) } })
                            Box {
                                val clipboard = LocalClipboardManager.current
                                val page = { ChatLinks.webUrl(view.currentUrl) ?: target }
                                JIconButton(PhIcons.DotsThree, "网页操作", { menu = true })
                                // A forwarded port's address is the phone's own loopback: no other app could open it.
                                JMenu(menu, { menu = false }, listOfNotNull(
                                    JAction("复制链接", PhIcons.LinkSimple) {
                                        clipboard.setText(AnnotatedString(page())); feedback?.notice("已复制链接")
                                    }.takeIf { forwarded == null },
                                    JAction("重新加载", PhIcons.ArrowClockwise, enabled = !view.gone) { view.reload() },
                                    if (forwarded != null) null else JAction("在浏览器打开", PhIcons.ArrowSquareOut) {
                                        val opened = runCatching {
                                            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(page())).addCategory(Intent.CATEGORY_BROWSABLE))
                                        }.isSuccess
                                        if (!opened) feedback?.notice("没有可用的浏览器")
                                    },
                                    JAction("分享", PhIcons.ShareNetwork) {
                                        val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, page())
                                        runCatching { context.startActivity(Intent.createChooser(send, null)) }
                                    }.takeIf { forwarded == null },
                                ))
                            }
                            JIconButton(PhIcons.X, "关闭网页", ::close)
                        }
                    }
                    if (!view.gone && view.loadProgress < 100 && view.failure == null)
                        JLinearProgress(view.loadProgress / 100f, Modifier.padding(horizontal = pagePadding))
                    (if (view.gone) "网页意外退出了" else view.failure)?.let { error ->
                        Row(Modifier.padding(horizontal = pagePadding), verticalAlignment = Alignment.CenterVertically) {
                            Text(error, Modifier.weight(1f).padding(vertical = Tokens.Space.s), style = PersonaTheme.type.callout,
                                color = palette.danger.text)
                            JTextButton(if (view.gone) "重新载入" else "重试", { if (view.gone) generation++ else view.reload() })
                        }
                    }
                    if (!view.gone) key(view) {
                        // Feishu documents in the native reader while the hub has it (the lark-connector extension, #1530).
                        if (larkNativeLink(target, lark) is LarkNativeLink.Document) LarkDocumentLink(target, Modifier.fillMaxWidth().weight(1f)) {
                            AndroidView(factory = { view }, modifier = Modifier.fillMaxSize(), onRelease = { pool.release(it) })
                        } else JPage(title = "网页", modifier = Modifier.fillMaxWidth().weight(1f), topBar = {}) { padding ->
                            AndroidView(factory = { view },
                                modifier = Modifier.fillMaxSize().padding(padding).padding(horizontal = pagePadding),
                                onRelease = { pool.release(it) })
                        }
                    } else Spacer(Modifier.fillMaxWidth().weight(1f))
                }
                }
                editing?.let { selection ->
                    LinkPickDialog("让 Agent 改这段", selection, "怎么改？", onDismiss = { editing = null }) { instruction ->
                        onEdit?.invoke(selection, instruction)
                        editing = null
                        scope.launch { drag.animateTo(LinkDetent.Peek) }
                    }
                }
                handing?.let { selection ->
                    HandToPersonaDialog(selection, onDismiss = { handing = null }) { message ->
                        onHandToPersona?.invoke(message)
                        handing = null
                        close()
                    }
                }
            }
        }
        if (unsupported) JDialog(onDismiss = { unsupported = false }, title = "无法打开链接",
            text = "这个链接不能在 Persona 里打开。", confirm = "知道了" to { unsupported = false }, dismiss = null)
        external?.let { link ->
            val prompt = remember(link) { ChatLinks.externalPrompt(link) }
            JDialog(onDismiss = { external = null }, title = prompt.title, text = prompt.text, confirm = "打开" to {
                external = null
                if (!openExternal(context, link)) feedback?.notice("没有能打开这个链接的 App")
            })
        }
    }
}

/**
 * The machine a conversation runs on, for its port links (docs/ports.md「识别回答里的链接」): its id and name, the names
 * it goes by in links, the ports its agents list (the hub allows those without asking), and how to open one.
 */
data class ChatPorts(
    val machineId: String,
    val name: String,
    val hostnames: List<String>,
    val listed: Set<Int>,
    val opener: persona.core.contracts.api.PortOpener,
)

val LocalChatPorts = androidx.compose.runtime.compositionLocalOf<ChatPorts?> { null }
