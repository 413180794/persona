package jarvis.android.ui.web

import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.net.Uri
import android.util.Log
import android.view.ViewGroup
import android.widget.Toast
import androidx.activity.compose.BackHandler
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.Orientation
import androidx.compose.foundation.gestures.draggable
import androidx.compose.foundation.gestures.rememberDraggableState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Link
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.InputChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import jarvis.android.JarvisApp
import jarvis.android.data.SessionKey
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.UUID
import kotlin.math.roundToInt

/** A link to show in the sheet; [from] is the chat it was tapped in (null: the inbox, which has no agent to send to). */
data class LinkRequest(val url: String, val from: SessionKey?)

/** The chat whose text is being drawn, so links tapped in it can hand page text back to it. */
val LocalLinkSource = staticCompositionLocalOf<SessionKey?> { null }

/**
 * Where links go (CLAUDE.md「链接在 App 内打开」): http(s) into the sheet, `jarvis://` links navigate ([AppLink]),
 * mailto: / tel: / intent: to the system after [externalIntent] strips them; content:, file:, javascript: and the
 * like are dropped. Main thread.
 */
object Links {
    /** The page in the sheet, or null. */
    val current = mutableStateOf<LinkRequest?>(null)

    /** 「作为上下文」 picks waiting in each chat's composer (by [SessionKey.path]); sent with its next message. */
    val contexts = mutableStateMapOf<String, List<ContextRef>>()

    /** Set by the root: opens a chat or a tab. */
    var navigate: ((AppLink) -> Unit)? = null

    /** Set by the root: starts a Jarvis conversation with this first message (the inbox's 交给 Jarvis). */
    var handToJarvis: ((String) -> Unit)? = null

    fun open(url: String, from: SessionKey?) {
        when (LinkSafety.kind(url)) {
            LinkSafety.Kind.WEB -> current.value = LinkRequest(url.trim(), from)
            LinkSafety.Kind.APP_LINK -> AppLink.parse(url)?.let { link -> current.value = null; navigate?.invoke(link) }
            LinkSafety.Kind.EXTERNAL -> {
                val app = JarvisApp.instance
                val intent = runCatching { externalIntent(url, app.packageName) }.getOrNull() ?: return
                runCatching { app.startActivity(intent) }.onFailure { toast("没有能打开这个链接的 App") }
            }
            LinkSafety.Kind.REFUSED -> Log.i("Links", "refused a ${url.substringBefore(':')} link")
        }
    }

    fun close() {
        current.value = null
    }

    fun addContext(key: SessionKey, ref: ContextRef) {
        contexts[key.path] = contexts[key.path].orEmpty() + ref
    }

    fun removeContext(key: SessionKey, ref: ContextRef) {
        contexts[key.path] = contexts[key.path].orEmpty().filterNot { it.id == ref.id }
    }

    internal fun toast(text: String) = Toast.makeText(JarvisApp.instance, text, Toast.LENGTH_SHORT).show()
}

/**
 * Hosts the link sheet over [content] (the whole app below pairing). While the sheet rests at 60 % the screen behind
 * is laid out in the 40 % above it, so the chat there scrolls and its composer can be typed in (Telegram style); at
 * full screen it is simply covered.
 */
@Composable
fun LinkHost(content: @Composable () -> Unit) {
    val request = Links.current.value
    // The sheet's top as a fraction of the height: 0 = full screen, 1 = gone.
    val offset = remember { Animatable(1f) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(request) {
        if (request != null) offset.animateTo(0f, spring(dampingRatio = 0.9f, stiffness = Spring.StiffnessMediumLow))
        else offset.snapTo(1f)
    }
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val density = LocalDensity.current
        val heightPx = constraints.maxHeight.toFloat()
        val inset = with(density) { (SheetStops.behindInset(offset.value) * heightPx).toDp() }
        // The inset is consumed so the keyboard padding inside (imePadding) only adds what reaches past the sheet.
        Box(Modifier.fillMaxSize().padding(bottom = inset).consumeWindowInsets(PaddingValues(bottom = inset))) { content() }
        if (request != null) {
            LinkSheet(
                request, offset.value, heightPx,
                onDrag = { delta -> scope.launch { offset.snapTo((offset.value + delta / heightPx).coerceIn(0f, 1f)) } },
                onSettle = { target ->
                    scope.launch {
                        if (target >= 1f) {
                            offset.animateTo(1f, spring(stiffness = Spring.StiffnessMedium))
                            Links.close()
                        } else {
                            offset.animateTo(target, spring(dampingRatio = 0.9f, stiffness = Spring.StiffnessMediumLow))
                        }
                    }
                },
            )
        }
    }
}

/**
 * The sheet itself (iOS `LinkSheet`): a handle and header that drag it between full screen, 60 % and closed, over a
 * page from the warm [WebViewPool]. Text selected on the page goes to the chat it came from as an edit request
 * (「AI 修改」) or rides along with its next message (「作为上下文」), both as `<jarvis-context>`; from the inbox,
 * 「交给 Jarvis」 starts an assistant conversation with it.
 */
@Composable
private fun LinkSheet(request: LinkRequest, offset: Float, heightPx: Float, onDrag: (Float) -> Unit, onSettle: (Float) -> Unit) {
    val ctx = LocalContext.current
    val web = remember(request.url) { WebViewPool.view(ctx, request.url) }
    var editing by remember(request.url) { mutableStateOf<ContextRef?>(null) }
    BackHandler { onSettle(1f) }

    val host = remember(request.url) { runCatching { Uri.parse(request.url).host }.getOrNull().orEmpty() }
    val title = web.page.title.ifEmpty { host.ifEmpty { "网页" } }

    fun pick(text: String, kind: String) {
        val ref = ContextRef(kind = kind, title = web.page.title.ifEmpty { host }, url = request.url, text = text.ifEmpty { "（整篇）" })
        val from = request.from
        if (kind == "edit" || from == null) {
            editing = ref
        } else {
            Links.addContext(from, ref)
            Links.toast("已作为上下文，会随下一条消息发送")
        }
    }

    // A light dim near full screen only; it has no pointer input, so at 60 % touches reach the screen above.
    val dim = ((SheetStops.MID - offset) / SheetStops.MID).coerceIn(0f, 1f) * 0.25f
    if (dim > 0.01f) Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = dim)))

    val drag = rememberDraggableState(onDrag)
    val sheetShape = RoundedCornerShape(topStart = JRadius.panel, topEnd = JRadius.panel)
    Column(
        Modifier
            .offset { IntOffset(0, (offset * heightPx).roundToInt()) }
            .fillMaxSize()
            // A shadow in light mode, a lighter container colour in dark: the sheet stays apart from the chat at 60 %.
            .shadow(16.dp, sheetShape)
            .clip(sheetShape)
            .background(JTheme.colors.card),
    ) {
        // Full screen runs under the status bar: the header moves below it as the sheet reaches the top.
        val statusTop = WindowInsets.statusBars.getTop(LocalDensity.current)
        val clear = ((SheetStops.MID - offset) / SheetStops.MID).coerceIn(0f, 1f)
        Spacer(Modifier.height(with(LocalDensity.current) { (statusTop * clear).toDp() }))
        LinkSheetTop(
            title = title, host = host.ifEmpty { request.url },
            canGoBack = web.page.canGoBack,
            progress = if (web.page.loading && web.page.progress < 1f) web.page.progress else null,
            drag = Modifier.draggable(
                drag, Orientation.Vertical,
                onDragStopped = { v -> onSettle(SheetStops.settle(offset, v / heightPx)) },
            ),
            onBack = { web.goBack() },
            onCopy = {
                ctx.getSystemService(ClipboardManager::class.java)?.setPrimaryClip(ClipData.newRawUri("链接", Uri.parse(request.url)))
                Links.toast("已拷贝链接")
            },
            onBrowser = {
                runCatching { ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(request.url)).addCategory(Intent.CATEGORY_BROWSABLE).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
                    .onFailure { Links.toast("没有能打开这个链接的浏览器") }
            },
            onShare = {
                val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, request.url)
                runCatching { ctx.startActivity(Intent.createChooser(send, null).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
            },
            onClose = { onSettle(1f) },
        )
        // Typing into the page at full screen: keep the field above the keyboard. At 60 % the keyboard belongs to the chat.
        Box(Modifier.fillMaxWidth().weight(1f).then(if (offset < 0.05f) Modifier.imePadding() else Modifier)) {
            PooledWebView(web, hasChat = request.from != null, background = JTheme.colors.page.toArgb(), onPick = ::pick)
        }
    }

    editing?.let { ref ->
        EditDialog(ref, toAssistant = request.from == null, onDismiss = { editing = null }) { instruction ->
            editing = null
            val from = request.from
            if (from == null) {
                Links.handToJarvis?.invoke(ref.copy(kind = "context").xml + "\n\n" + instruction.trim().ifEmpty { "看看上面这段。" })
                onSettle(1f)
            } else {
                sendEdit(from, ContextRef.editMessage(ref, instruction))
            }
        }
    }
}

/** 「AI 修改」: one message to the chat's agent (or the assistant), at once. */
private fun sendEdit(key: SessionKey, text: String) {
    val app = JarvisApp.instance
    val hub = app.hub.value ?: return
    app.scope.launch {
        val r = runCatching { hub.api.send(key, text, UUID.randomUUID().toString()) }
        withContext(Dispatchers.Main) { Links.toast(if (r.isSuccess) "已发给 Agent" else "没有发出去：${r.exceptionOrNull()?.message}") }
    }
}

/**
 * The sheet's top (iOS `LinkSheet.header`): the grabber and the header, which drag the sheet ([drag]); the page's
 * loading line; a hairline over the page.
 */
@Composable
internal fun LinkSheetTop(
    title: String,
    host: String,
    canGoBack: Boolean,
    progress: Float?,
    drag: Modifier,
    onBack: () -> Unit,
    onCopy: () -> Unit,
    onBrowser: () -> Unit,
    onShare: () -> Unit,
    onClose: () -> Unit,
) {
    Column(Modifier.fillMaxWidth().then(drag)) {
        Box(Modifier.fillMaxWidth().padding(top = JSpace.s, bottom = JSpace.xs), contentAlignment = Alignment.Center) {
            Box(Modifier.size(width = JSize.wellL, height = JSize.grabber).background(JTheme.colors.fillPressed, CircleShape))
        }
        LinkSheetHeader(title, host, canGoBack, onBack, onCopy, onBrowser, onShare, onClose)
    }
    Box(Modifier.fillMaxWidth().height(JSize.rule)) {
        if (progress != null) {
            LinearProgressIndicator(
                progress = { progress.coerceAtLeast(0.05f) },
                modifier = Modifier.fillMaxSize(), color = JTheme.colors.accent, trackColor = Color.Transparent,
            )
        }
    }
    HorizontalDivider(thickness = JSize.hairline, color = JTheme.colors.separator)
}

/** The page's title and host, then ⋯ (拷贝链接 · 在浏览器打开 · 分享) and ✕; ‹ first once the page has been followed. */
@Composable
private fun LinkSheetHeader(
    title: String,
    host: String,
    canGoBack: Boolean,
    onBack: () -> Unit,
    onCopy: () -> Unit,
    onBrowser: () -> Unit,
    onShare: () -> Unit,
    onClose: () -> Unit,
) {
    val c = JTheme.colors
    var menu by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().padding(start = if (canGoBack) JSpace.m else JSpace.l, end = JSpace.m, bottom = JSpace.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        if (canGoBack) CircleButton(JIcons.back, "返回上一页", onBack, size = JSize.wellL)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(title, style = JTheme.type.secondaryStrong, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(host, style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Box {
            CircleButton(JIcons.more, "更多", { menu = true }, size = JSize.wellL)
            DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                DropdownMenuItem(text = { Text("拷贝链接", maxLines = 1) }, leadingIcon = { Icon(JIcons.link, null) }, onClick = { menu = false; onCopy() })
                DropdownMenuItem(
                    text = { Text("在浏览器打开", maxLines = 1) }, leadingIcon = { Icon(JIcons.openExternal, null) },
                    onClick = { menu = false; onBrowser() },
                )
                DropdownMenuItem(text = { Text("分享", maxLines = 1) }, leadingIcon = { Icon(JIcons.share, null) }, onClick = { menu = false; onShare() })
            }
        }
        CircleButton(JIcons.close, "关闭", onClose, size = JSize.wellL)
    }
}

/** Shows a pooled WebView: under the activity while on screen and running; back to the application and paused after. */
@Composable
private fun PooledWebView(web: SelectableWebView, hasChat: Boolean, background: Int, onPick: (String, String) -> Unit) {
    val ctx = LocalContext.current
    val lifecycle = androidx.lifecycle.compose.LocalLifecycleOwner.current.lifecycle
    DisposableEffect(web, lifecycle) {
        ctx.findActivity()?.let { web.wrapper.baseContext = it }
        web.onPick = onPick
        web.setBackgroundColor(background) // until the page paints (no white flash in dark mode)
        web.pickLabels = if (hasChat) "AI 修改" to "作为上下文" else "交给 Jarvis" to null
        web.onResume()
        // Follow the activity: a page left playing must stop when the app goes to the background.
        val obs = LifecycleEventObserver { _, e ->
            when (e) {
                Lifecycle.Event.ON_RESUME -> web.onResume()
                Lifecycle.Event.ON_PAUSE -> web.onPause()
                else -> Unit
            }
        }
        lifecycle.addObserver(obs)
        onDispose {
            lifecycle.removeObserver(obs)
            web.onPick = null
            web.onPause() // closed sheet: no audio / video / timers from the pool
            (web.parent as? ViewGroup)?.removeView(web)
            web.wrapper.baseContext = web.home
        }
    }
    AndroidView(
        factory = {
            (web.parent as? ViewGroup)?.removeView(web)
            web.layoutParams = ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
            web
        },
        modifier = Modifier.fillMaxSize(),
    )
}

@Composable
private fun EditDialog(ref: ContextRef, toAssistant: Boolean, onDismiss: () -> Unit, onSend: (String) -> Unit) {
    var instruction by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (toAssistant) "交给 Jarvis" else "让 Agent 改这段", maxLines = 1) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(ref.text.take(120), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 4, overflow = TextOverflow.Ellipsis)
                OutlinedTextField(
                    value = instruction,
                    onValueChange = { instruction = it },
                    placeholder = { Text(if (toAssistant) "要 Jarvis 做什么？" else "怎么改？") },
                    modifier = Modifier.fillMaxWidth(),
                    maxLines = 5,
                )
            }
        },
        confirmButton = { TextButton(onClick = { onSend(instruction) }) { Text("发送", maxLines = 1) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消", maxLines = 1) } },
    )
}

/** Pending 「作为上下文」 picks above a chat's composer; each can be removed before sending. */
@Composable
fun ContextChips(key: SessionKey) {
    val refs = Links.contexts[key.path].orEmpty()
    if (refs.isEmpty()) return
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        refs.forEach { ref ->
            InputChip(
                selected = false,
                onClick = { Links.removeContext(key, ref) },
                label = { Text("「${ref.text}」", maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = 200.dp)) },
                leadingIcon = { Icon(Icons.Default.Link, null, Modifier.size(16.dp)) },
                trailingIcon = { Icon(Icons.Default.Close, "移除这段上下文", Modifier.size(16.dp)) },
            )
        }
    }
}

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
