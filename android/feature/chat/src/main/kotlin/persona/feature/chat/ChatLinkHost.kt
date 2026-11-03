package persona.feature.chat

import android.content.Intent
import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.PersonaTypography
import persona.core.designsystem.Tokens
import persona.core.designsystem.JPage
import persona.core.designsystem.pagePadding
import persona.feature.inbox.LarkDocumentLink
import persona.feature.inbox.isLarkDocumentUrl
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

internal val LocalChatLinkOpening = staticCompositionLocalOf { 0 }

private enum class LinkDetent { Full, Peek, Hidden }

/** Shared URI boundary for Agent, assistant, and other conversation surfaces. */
@Composable
fun ChatLinkHost(
    modifier: Modifier = Modifier,
    preloadUrls: List<String> = emptyList(),
    isScrolling: Boolean = false,
    onContext: (ChatLinkContext) -> Unit,
    onEdit: suspend (ChatLinkContext, String) -> Unit,
    onVisibilityChanged: (Boolean) -> Unit = {},
    content: @Composable (bottomInset: Dp) -> Unit,
) {
    val context = LocalContext.current
    val pool = remember(context) { ChatWebViewPool(context) }
    val scope = rememberCoroutineScope()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val lifecycleState by lifecycle.currentStateAsState()
    var url by remember { mutableStateOf<String?>(null) }
    var unsupported by remember { mutableStateOf(false) }
    var opening by remember { mutableIntStateOf(0) }
    val uriHandler = remember { object : UriHandler {
        override fun openUri(uri: String) {
            val target = ChatLinks.webUrl(uri)
            if (target == null) unsupported = true else { url = target; opening++ }
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
    LaunchedEffect(preloadUrls, isScrolling, lifecycleState) {
        if (!isScrolling && lifecycleState.isAtLeast(Lifecycle.State.STARTED)) {
            delay(600)
            pool.warmUp()
            preloadUrls.firstNotNullOfOrNull { ChatLinks.webUrl(it) }?.let { pool.preload(it) }
        }
    }
    CompositionLocalProvider(LocalUriHandler provides uriHandler, LocalChatLinkOpening provides opening) {
        BoxWithConstraints(modifier.fillMaxSize()) {
            val height = maxHeight
            val density = LocalDensity.current
            val pixels = with(density) { height.toPx() }
            val drag = remember { AnchoredDraggableState(LinkDetent.Hidden) }
            SideEffect {
                drag.updateAnchors(DraggableAnchors {
                    LinkDetent.Full at 0f
                    LinkDetent.Peek at pixels * 0.4f
                    LinkDetent.Hidden at pixels
                })
            }
            LaunchedEffect(opening) { if (url != null) drag.animateTo(LinkDetent.Full) }
            LaunchedEffect(drag.settledValue) { if (drag.settledValue == LinkDetent.Hidden && !drag.isAnimationRunning) url = null }
            content(if (url != null && drag.currentValue == LinkDetent.Peek) height * 0.6f else 0.dp)
            url?.let { target ->
                val view = remember(pool, target) { pool.acquire(target) }
                var editing by remember(target) { mutableStateOf<ChatLinkContext?>(null) }
                var instruction by remember(target) { mutableStateOf("") }
                var sending by remember(target) { mutableStateOf(false) }
                var editError by remember(target) { mutableStateOf(false) }
                var menu by remember { mutableStateOf(false) }
                var browserError by remember { mutableStateOf(false) }
                val pick by rememberUpdatedState(onContext)
                DisposableEffect(view) {
                    view.onPick = { selected ->
                        if (selected.kind == "edit") { editing = selected; instruction = ""; editError = false }
                        else { pick(selected); scope.launch { drag.animateTo(LinkDetent.Peek) } }
                    }
                    onDispose { pool.release(view) }
                }
                fun close() { scope.launch { drag.animateTo(LinkDetent.Hidden) } }
                BackHandler {
                    if (view.canGoBack()) view.goBack() else close()
                }
                val offset = drag.offset.takeIf { it.isFinite() } ?: pixels
                val panelHeight = with(density) { (pixels - offset).coerceIn(0f, pixels).toDp() }
                Surface(Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(panelHeight),
                    shape = RoundedCornerShape(topStart = Tokens.Space.xl, topEnd = Tokens.Space.xl), shadowElevation = Tokens.Space.s,
                    color = MaterialTheme.colorScheme.surface, contentColor = MaterialTheme.colorScheme.onSurface) {
                    Column {
                        Column(Modifier.fillMaxWidth().anchoredDraggable(drag, Orientation.Vertical)) {
                            Box(Modifier.align(Alignment.CenterHorizontally).padding(top = Tokens.Space.s).size(Tokens.Space.xxxl, Tokens.Space.xs)
                                .background(MaterialTheme.colorScheme.onSurfaceVariant, RoundedCornerShape(Tokens.Space.xs)))
                            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                IconButton(onClick = { if (view.canGoBack()) view.goBack() else close() }) {
                                    Icon(Icons.AutoMirrored.Filled.ArrowBack, "网页返回")
                                }
                                Column(Modifier.weight(1f)) {
                                    Text(view.pageTitle.ifBlank { "网页" }, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    Text(view.currentUrl.ifBlank { target }, maxLines = 1, overflow = TextOverflow.Ellipsis,
                                        style = PersonaTypography.micro, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                }
                                IconButton(onClick = { scope.launch { drag.animateTo(if (drag.currentValue == LinkDetent.Full) LinkDetent.Peek else LinkDetent.Full) } }) {
                                    Icon(if (drag.currentValue == LinkDetent.Full) Icons.Default.ExpandMore else Icons.Default.ExpandLess,
                                        if (drag.currentValue == LinkDetent.Full) "收起到六成，继续对话" else "铺满网页")
                                }
                                Box {
                                    val clipboard = LocalClipboardManager.current
                                    IconButton(onClick = { menu = true }) { Icon(Icons.Default.MoreVert, "网页操作") }
                                    DropdownMenu(menu, onDismissRequest = { menu = false }) {
                                        DropdownMenuItem(text = { Text("复制链接", maxLines = 1) }, onClick = {
                                            clipboard.setText(AnnotatedString(view.currentUrl.ifBlank { target })); menu = false
                                        })
                                        DropdownMenuItem(text = { Text("重新加载", maxLines = 1) }, onClick = { view.reload(); menu = false })
                                        DropdownMenuItem(text = { Text("在浏览器打开", maxLines = 1) }, onClick = {
                                            menu = false
                                            val external = ChatLinks.webUrl(view.currentUrl) ?: target
                                            browserError = runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(external))) }.isFailure
                                        })
                                    }
                                }
                                IconButton(onClick = ::close) { Icon(Icons.Default.Close, "关闭网页") }
                            }
                        }
                        if (view.loadProgress < 100 && view.failure == null) LinearProgressIndicator(progress = { view.loadProgress / 100f }, modifier = Modifier.fillMaxWidth())
                        view.failure?.let { error ->
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(error, Modifier.weight(1f).padding(Tokens.Space.s), color = PersonaTheme.colors.danger)
                                TextButton(onClick = { view.reload() }) { Text("重试", maxLines = 1) }
                            }
                        }
                        if (browserError) Text("没有可用的浏览器", Modifier.padding(Tokens.Space.s), color = PersonaTheme.colors.danger)
                        if (isLarkDocumentUrl(target)) LarkDocumentLink(target, Modifier.fillMaxWidth().weight(1f)) {
                            AndroidView(factory = { view }, modifier = Modifier.fillMaxSize(), onRelease = { pool.release(it) })
                        } else JPage(title = "网页", modifier = Modifier.fillMaxWidth().weight(1f), topBar = {}) { padding ->
                            AndroidView(factory = { view },
                                modifier = Modifier.fillMaxSize().padding(padding).padding(horizontal = pagePadding),
                                onRelease = { pool.release(it) })
                        }
                    }
                }
                editing?.let { selection ->
                    AlertDialog(onDismissRequest = { if (!sending) editing = null }, title = { Text("AI 修改") },
                        text = { Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                            Text(selection.text, maxLines = 4, overflow = TextOverflow.Ellipsis)
                            OutlinedTextField(instruction, { instruction = it }, label = { Text("怎么修改？") }, enabled = !sending)
                            if (editError) Text("未能提交，请重试", color = PersonaTheme.colors.danger)
                        } }, confirmButton = {
                            TextButton(enabled = !sending && instruction.isNotBlank(), onClick = {
                                sending = true
                                scope.launch {
                                    try { onEdit(selection, instruction.trim()); editing = null; drag.animateTo(LinkDetent.Peek) }
                                    catch (e: CancellationException) { throw e }
                                    catch (_: Exception) { editError = true }
                                    finally { sending = false }
                                }
                            }) { Text(if (sending) "提交中" else "发送", maxLines = 1) }
                        }, dismissButton = { TextButton(enabled = !sending, onClick = { editing = null }) { Text("取消", maxLines = 1) } })
                }
            }
        }
        if (unsupported) AlertDialog(onDismissRequest = { unsupported = false }, title = { Text("无法打开链接") },
            text = { Text("当前支持在 App 内打开 HTTP 和 HTTPS 网页。") },
            confirmButton = { TextButton(onClick = { unsupported = false }) { Text("知道了", maxLines = 1) } })
    }
}
