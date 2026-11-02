package jarvis.android.ui.lark

import android.content.ActivityNotFoundException
import android.content.Intent
import android.webkit.MimeTypeMap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.FileProvider
import jarvis.android.JarvisApp
import jarvis.android.data.Hub
import jarvis.android.data.InboxItem
import jarvis.android.lark.LarkMoments
import jarvis.android.lark.LarkMomentsComment
import jarvis.android.lark.LarkMomentsPost
import jarvis.android.ui.InboxActionBar
import jarvis.android.ui.InboxDraftSheet
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.surface
import jarvis.android.ui.media.ImageViewerArgs
import jarvis.android.ui.media.MediaShare
import jarvis.android.ui.media.ViewerImage
import jarvis.android.ui.web.Links
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import java.io.File

/** What an inbox item opens as a page of its own (iOS `InboxDetailView.content`): its Feishu chat, or its removed post. */
sealed interface LarkTarget {
    data class Chat(val info: LarkChatInfo) : LarkTarget
    data class Moment(val postId: String) : LarkTarget

    companion object {
        /** The page for [item], or null when it has none here (mail, a document: the detail shows the hub's text). */
        fun of(item: InboxItem): LarkTarget? {
            if (item.kind == "doc") return null // a document's @我 has its own detail (not here yet)
            item.moment?.postId?.takeIf { it.isNotBlank() }?.let { return Moment(it) }
            val l = item.lark?.takeIf { it.feedId.isNotBlank() } ?: return null
            return Chat(LarkChatInfo(
                feedId = l.feedId, name = item.title, isGroup = l.isGroup, isTopic = l.isTopic,
                unread = if (item.isDone) 0 else item.count, focus = item.larkFocus, mentionPosition = l.mentionPosition,
            ))
        }
    }
}

/**
 * An inbox item opened as a page (iOS `InboxDetailView`): its header with 让 Jarvis 总结 and 已处理 / 移回, the
 * [InboxActionBar] (the AI's gist, AI 草拟回复, 交给 AI), then the Feishu chat or the removed post itself. Links open in
 * the link sheet, pictures in the viewer over the page, files in the app that opens them.
 */
@Composable
fun LarkItemPage(
    hub: Hub, item: InboxItem, target: LarkTarget, onBack: (() -> Unit)?, onDone: (InboxItem, Boolean) -> Unit, onHandOff: () -> Unit,
    onAskJarvis: (String) -> Unit, modifier: Modifier = Modifier,
    /** Loaded ahead (screenshots): drawn at once. */
    chat: LarkChatModel? = null, moment: Pair<LarkMomentsPost, List<LarkMomentsComment>>? = null,
) {
    val ctx = LocalContext.current
    var drafting by remember { mutableStateOf(false) }
    var viewer by remember { mutableStateOf<ImageViewerArgs?>(null) }
    var player by remember { mutableStateOf<Triple<String, Boolean, String>?>(null) }
    var downloading by remember { mutableStateOf(false) }
    var moments by remember { mutableStateOf<Pair<String, String?>?>(null) }
    val pageScope = rememberCoroutineScope()
    val actions = remember(hub) {
        LarkActions(
            // removed share links read here; everything else in the app's link sheet.
            openUrl = { url -> LarkMoments.postId(url)?.let { moments = it to url } ?: Links.open(url, null) },
            openImages = { urls, i -> viewer = ImageViewerArgs(urls.map { ViewerImage(url = it) }, i) },
            openFile = { url, name ->
                downloading = true
                pageScope.launch {
                    runCatching { LarkMedia.file(hub.api, url, name) }
                        .onSuccess { openFile(ctx, it, name) }
                        .onFailure { Toasts.show("没下载下来：${it.message}", ToastStyle.ERROR) }
                    downloading = false
                }
            },
            playMedia = { url, audio, title -> player = Triple(url, audio, title) },
        )
    }
    val c = JTheme.colors
    Box(modifier.fillMaxSize().background(c.page)) {
        Column(Modifier.fillMaxSize()) {
            PageHeader(
                item.title, onBack,
                subtitle = when (target) { is LarkTarget.Moment -> "removed"; is LarkTarget.Chat -> if (target.info.isTopic) "飞书话题群" else "飞书" },
                actions = {
                    if (target is LarkTarget.Chat) CircleButton(JIcons.ai, "让 Jarvis 总结", {
                        onAskJarvis("读取我飞书会话「${item.title}」（lark-connector feedId ${target.info.feedId}）最近的消息，总结要点，列出需要我回复或处理的事，并给出建议的回复。")
                    }, tint = c.accent)
                    if (item.isDone) CircleButton(JIcons.undo, "移回", { onDone(item, false) })
                    else CircleButton(JIcons.done, "已处理", { onDone(item, true) })
                },
            )
            InboxActionBar(item, onDraft = { drafting = true }, onHandOff = onHandOff)
            CompositionLocalProvider(LocalLarkHub provides hub) {
                when (target) {
                    is LarkTarget.Chat -> {
                        val model = chat ?: remember(target.info) {
                            LarkChatModel(hub, target.info, CoroutineScope(SupervisorJob() + Dispatchers.Main), cacheDirOf(ctx), outlive = appScope())
                        }
                        if (chat == null) DisposableEffect(model) { onDispose { model.close() } }
                        LarkChatView(hub, model, actions, Modifier.weight(1f))
                    }
                    is LarkTarget.Moment -> CompositionLocalProvider(LocalLarkActions provides actions) {
                        LarkMomentsPostView(target.postId, Modifier.weight(1f).navigationBarsPadding(), initial = moment)
                    }
                }
            }
        }
        if (downloading) Row(
            Modifier.align(Alignment.Center).surface(RoundedCornerShape(JRadius.card), floating = true).padding(JSpace.l),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
        ) {
            CircularProgressIndicator(Modifier.size(JSize.badge), strokeWidth = JSize.rule, color = c.secondaryText)
            Text("下载中…", style = JTheme.type.secondary, color = c.label)
        }
        viewer?.let { args -> LarkImageViewer(hub, args) { viewer = null } }
    }
    CompositionLocalProvider(LocalLarkHub provides hub, LocalLarkActions provides actions) {
        moments?.let { (id, link) -> LarkMomentsSheet(id, link) { moments = null } }
        player?.let { (url, audio, title) -> LarkPlayer(url, audio, title) { player = null } }
    }
    if (drafting) InboxDraftSheet(hub, item, onDismiss = { drafting = false })
}

/** The image viewer over the page, edge to edge, with light system-bar icons on its black (as the chat page's). */
@Composable
private fun LarkImageViewer(hub: Hub, args: ImageViewerArgs, onClose: () -> Unit) {
    val view = androidx.compose.ui.platform.LocalView.current
    DisposableEffect(view) {
        val window = (view.context as? android.app.Activity)?.window
        val bars = window?.let { androidx.core.view.WindowCompat.getInsetsController(it, view) }
        val status = bars?.isAppearanceLightStatusBars
        val nav = bars?.isAppearanceLightNavigationBars
        bars?.isAppearanceLightStatusBars = false
        bars?.isAppearanceLightNavigationBars = false
        onDispose {
            status?.let { bars.isAppearanceLightStatusBars = it }
            nav?.let { bars.isAppearanceLightNavigationBars = it }
        }
    }
    jarvis.android.ui.media.ImageViewerScreen(hub, args, onBack = onClose)
}

/** Hands a downloaded Feishu file to the app that opens it. */
private fun openFile(ctx: android.content.Context, f: File, name: String) {
    val uri = FileProvider.getUriForFile(ctx, MediaShare.authority(ctx), f)
    val mime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substringAfterLast('.', "").lowercase()) ?: "application/octet-stream"
    val view = Intent(Intent.ACTION_VIEW).setDataAndType(uri, mime).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    try {
        ctx.startActivity(Intent.createChooser(view, name).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    } catch (_: ActivityNotFoundException) {
        Toasts.show("这台手机上没有能打开这个文件的 App")
    }
}

/** The app's cache folder (screenshot tests have none: a temporary one). */
private fun cacheDirOf(ctx: android.content.Context): File =
    runCatching { ctx.cacheDir }.getOrNull() ?: File(System.getProperty("java.io.tmpdir"), "jarvis-cache")

/** The app's own scope (outlives the page), or the main one where there is no app (screenshot tests). */
private fun appScope(): CoroutineScope =
    runCatching { JarvisApp.instance.scope }.getOrNull() ?: CoroutineScope(SupervisorJob() + Dispatchers.Main)
