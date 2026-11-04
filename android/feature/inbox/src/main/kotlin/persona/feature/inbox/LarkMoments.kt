package persona.feature.inbox

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil3.compose.AsyncImage
import coil3.imageLoader
import coil3.request.ImageRequest
import coil3.request.SuccessResult
import coil3.toBitmap
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import persona.core.contracts.LarkMomentsComment
import persona.core.contracts.LarkMomentsImage
import persona.core.contracts.LarkMomentsPost
import persona.core.contracts.LarkUrlPreview
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.Navigator
import java.io.ByteArrayOutputStream
import java.net.URI
import java.net.URLDecoder
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** 公司圈 share links (`…/client/moments/detail?postId=…`). */
object LarkMoments {
    fun postId(link: String?): String? {
        link ?: return null
        val uri = runCatching { URI(link.trim()) }.getOrNull() ?: return null
        val path = uri.path ?: return null
        if (!path.endsWith("/client/moments/detail") && !path.endsWith("/moments/detail")) return null
        val id = (uri.rawQuery ?: return null).split('&').firstNotNullOfOrNull { kv ->
            val i = kv.indexOf('=')
            if (i > 0 && kv.substring(0, i) == "postId") runCatching { URLDecoder.decode(kv.substring(i + 1), "UTF-8") }.getOrNull() else null
        } ?: return null
        return id.takeIf { it.isNotEmpty() && it.all { ch -> ch.isLetterOrDigit() || ch == '_' || ch == '-' } }
    }
}

internal fun formatMomentTime(ms: Double?): String = runCatching {
    if (ms == null) return ""
    val instant = Instant.ofEpochMilli(ms.toLong())
    val zone = ZoneId.systemDefault()
    DateTimeFormatter.ofPattern("MM-dd HH:mm").format(instant.atZone(zone))
}.getOrDefault("")

/** A shared 公司圈 post in a chat. */
@Composable
internal fun LarkMomentsCard(
    postId: String,
    url: String,
    repository: InboxRepository,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var preview by remember(url) { mutableStateOf<LarkUrlPreview?>(null) }
    LaunchedEffect(url) {
        preview = repository.urlPreview("", url)
    }
    val card = preview?.card
    JCard(modifier.fillMaxWidth().clickable(onClick = onClick), padding = Tokens.Space.m) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            Icon(PhIcons.ChatsCircle.regular, contentDescription = "公司圈", tint = PersonaTheme.palette.accent, modifier = Modifier.size(20.dp))
            Text(preview?.title ?: "公司圈动态", style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
        }
        card?.author?.let { author ->
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JAvatar(author, size = 28.dp)
                Column {
                    Text(author, style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                    card.subtitle?.let { Text(it, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3) }
                }
            }
        }
        val body = card?.body
        if (!body.isNullOrEmpty()) {
            Text(body, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2, maxLines = 4)
        }
        card?.footer?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3) }
    }
}

/** One 公司圈 post page opened from an inbox item. */
@Composable
fun LarkMomentsScreen(
    repository: InboxRepository,
    route: LarkMomentRoute,
    navigator: Navigator,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val inbox by repository.snapshot.collectAsStateWithLifecycle()
    val record = inbox.records.firstOrNull { it.item.id == route.itemId || it.moment?.postId == route.postId }
    val composers = rememberReplyComposers(repository)
    val composer = record?.let { composers.get(ReplyTarget.Inbox(it.item.id)) }
    val reply by composer?.state?.collectAsStateWithLifecycle() ?: remember { mutableStateOf(ReplyComposerState()) }

    var post by remember(route.postId) { mutableStateOf<LarkMomentsPost?>(null) }
    var comments by remember(route.postId) { mutableStateOf<List<LarkMomentsComment>>(emptyList()) }
    var loading by remember(route.postId) { mutableStateOf(true) }
    var error by remember(route.postId) { mutableStateOf<String?>(null) }
    var markingDone by remember { mutableStateOf(false) }
    var drafting by remember { mutableStateOf(false) }
    var handingOff by remember { mutableStateOf(false) }
    var confirmSend by remember { mutableStateOf(false) }
    var viewerImage by remember { mutableStateOf<Pair<List<ViewerImage>, Int>?>(null) }

    fun reload() {
        scope.launch {
            loading = true
            error = null
            try {
                post = repository.momentsPost(route.postId)
                comments = repository.momentsComments(route.postId)
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { error = e.message ?: "加载失败，请重试" }
            finally { loading = false }
        }
    }

    LaunchedEffect(route.postId) { reload() }

    LaunchedEffect(reply.sent) {
        if (reply.sent) {
            drafting = false
            reload()
        }
    }

    val feishuUrl = "https://applink.feishu.cn/client/moments/detail?postId=${route.postId}"

    JListPage(
        title = "公司圈",
        subtitle = post?.userName?.takeIf { it.isNotBlank() }?.let { "$it 的动态" },
        onBack = navigator::back,
        refreshing = loading,
        onRefresh = ::reload,
        actions = {
            if (record != null) {
                JIconButton(
                    PhIcons.CheckCircle,
                    if (record.item.isDone) "移回收件箱" else "已处理",
                    enabled = !markingDone,
                    onClick = {
                        markingDone = true
                        scope.launch {
                            try {
                                repository.setDone(record.item.id, !record.item.isDone)
                                navigator.back()
                            } catch (e: CancellationException) { throw e }
                            catch (e: Exception) { error = "操作失败，请重试" }
                            finally { markingDone = false }
                        }
                    }
                )
            }
            JIconButton(PhIcons.ArrowSquareOut, "在飞书中打开", onClick = {
                try {
                    val intent = Intent(Intent.ACTION_VIEW, Uri.parse(feishuUrl)).apply {
                        addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    }
                    context.startActivity(intent)
                } catch (_: Exception) {}
            })
        },
        bottomBar = {
            if (record != null) {
                InboxAiActions(
                    onDraft = { drafting = true },
                    onHandOff = { handingOff = true },
                    modifier = Modifier.imePadding().padding(Tokens.Space.m),
                    primaryAction = JAction("评论", PhIcons.ArrowBendUpLeft) { drafting = true },
                    draftEnabled = true,
                )
            }
        },
        modifier = modifier,
    ) {
        item("plugin-notice") { InboxExtensionNotice(route.itemId, route) }
        if (error != null) {
            item("error") {
                JNotice(error ?: "加载失败", status = Status.NeedsYou, actionLabel = "重试", onAction = ::reload)
            }
        }
        if (loading && post == null) {
            item("loading") { JLoading(text = "正在读取帖子与评论") }
        }

        record?.item?.summary?.takeIf { it.isNotBlank() }?.let { summary ->
            item("summary") {
                JNotice(summary, Modifier.padding(bottom = Tokens.Space.m), title = "AI 摘要", icon = PhIcons.Sparkle)
            }
        }

        if (record != null && !record.item.isDone) {
            item("agent-draft") {
                AgentDraftCard(
                    record, composers, navigator, repository = repository, keepComposer = true,
                    onEdit = { drafting = true },
                    onSend = { confirmSend = true }
                )
            }
        }

        val p = post
        if (p != null) {
            item("post-card") {
                MomentsPostCard(
                    post = p,
                    onOpenImage = { images, index -> viewerImage = images to index }
                )
            }

            item("comments-header") {
                JSectionHeader(title = "${p.commentCount ?: comments.size} 条评论")
            }

            if (comments.isEmpty() && !loading) {
                item("empty-comments") {
                    JEmptyState("暂无评论", "这篇帖子还没有人评论。")
                }
            }

            items(comments.size, key = { comments[it].id }) { index ->
                val comment = comments[index]
                MomentsCommentRow(comment)
            }
        }
    }

    if (drafting && record != null && composer != null) {
        InboxDraftSheet(repository, record, composer, navigator, onDismiss = { drafting = false })
    }
    if (handingOff && record != null) {
        InboxHandOffSheet(repository, record, navigator, onDismiss = { handingOff = false })
    }
    if (confirmSend && record != null && composer != null) {
        SendAsYouDialog(
            sendConfirmation(record, reply.draft.text),
            onSend = {
                confirmSend = false
                composer.send()
            },
            onDismiss = { confirmSend = false }
        )
    }
    viewerImage?.let { (images, index) ->
        ImageViewer(images = images, initialIndex = index, onDismiss = { viewerImage = null })
    }
}

/** Bottom sheet for a 公司圈 post opened from a share link inside a conversation. */
@Composable
internal fun LarkMomentsSheet(
    postId: String,
    link: String?,
    repository: InboxRepository,
    onDismiss: () -> Unit,
) {
    val context = LocalContext.current
    var post by remember(postId) { mutableStateOf<LarkMomentsPost?>(null) }
    var comments by remember(postId) { mutableStateOf<List<LarkMomentsComment>>(emptyList()) }
    var loading by remember(postId) { mutableStateOf(true) }
    var error by remember(postId) { mutableStateOf<String?>(null) }
    var viewerImage by remember { mutableStateOf<Pair<List<ViewerImage>, Int>?>(null) }
    val scope = rememberCoroutineScope()

    fun reload() {
        scope.launch {
            loading = true
            error = null
            try {
                post = repository.momentsPost(postId)
                comments = repository.momentsComments(postId)
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { error = e.message ?: "加载失败" }
            finally { loading = false }
        }
    }

    LaunchedEffect(postId) { reload() }

    val feishuUrl = link ?: "https://applink.feishu.cn/client/moments/detail?postId=$postId"

    JSheet(onDismiss = onDismiss, title = "公司圈", scrollable = false) {
        Row(Modifier.fillMaxWidth().padding(bottom = Tokens.Space.s), horizontalArrangement = Arrangement.End) {
            JButton(
                "在飞书中打开",
                onClick = {
                    try {
                        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(feishuUrl)).apply {
                            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                        }
                        context.startActivity(intent)
                    } catch (_: Exception) {}
                },
                kind = JButtonKind.Secondary,
                size = JButtonSize.Small,
                icon = PhIcons.ArrowSquareOut
            )
        }

        if (error != null) {
            JNotice(error ?: "加载失败", status = Status.NeedsYou, actionLabel = "重试", onAction = ::reload)
        }
        if (loading && post == null) {
            JLoading(text = "正在读取帖子与评论")
        }

        val p = post
        if (p != null) {
            androidx.compose.foundation.lazy.LazyColumn(
                Modifier.fillMaxWidth().weight(1f, fill = false),
                verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)
            ) {
                item("post") {
                    MomentsPostCard(post = p, onOpenImage = { images, idx -> viewerImage = images to idx })
                }
                item("comments-header") {
                    JSectionHeader(title = "${p.commentCount ?: comments.size} 条评论")
                }
                if (comments.isEmpty() && !loading) {
                    item("empty-comments") {
                        JEmptyState("暂无评论", "这篇帖子还没有人评论。")
                    }
                }
                items(comments.size, key = { comments[it].id }) { index ->
                    MomentsCommentRow(comments[index])
                }
            }
        }
    }

    viewerImage?.let { (images, index) ->
        ImageViewer(images = images, initialIndex = index, onDismiss = { viewerImage = null })
    }
}

@Composable
private fun MomentsPostCard(
    post: LarkMomentsPost,
    onOpenImage: (List<ViewerImage>, Int) -> Unit,
    modifier: Modifier = Modifier,
) {
    JCard(modifier.fillMaxWidth(), padding = Tokens.Space.m) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)
        ) {
            JAvatar(post.userName.orEmpty(), size = 40.dp)
            Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
                Text(post.userName.orEmpty(), style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                post.createTime?.let {
                    Text(formatMomentTime(it), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
                }
            }
        }

        val postText = post.content?.text ?: post.content?.segments?.joinToString("") { it.text ?: it.key ?: "" }.orEmpty()
        if (postText.isNotBlank()) {
            LarkLinkedText(postText)
        }

        val images = post.images.orEmpty().filter { !it.url.isNullOrBlank() }
        if (images.isNotEmpty()) {
            MomentsImagesLayout(images, onOpenImage)
        }

        val groups = post.reactionGroups.orEmpty()
        if (groups.isNotEmpty()) {
            Row(
                modifier = Modifier.horizontalScroll(rememberScrollState()),
                horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)
            ) {
                for (g in groups) {
                    JChip(
                        text = "${reactionLabel(g.type)} ${g.count}",
                        selected = g.selfInvolved,
                    )
                }
            }
        }
    }
}

@Composable
private fun MomentsCommentRow(
    comment: LarkMomentsComment,
    modifier: Modifier = Modifier,
) {
    JCard(modifier.fillMaxWidth().padding(bottom = Tokens.Space.s), padding = Tokens.Space.m) {
        Row(
            verticalAlignment = Alignment.Top,
            horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)
        ) {
            JAvatar(comment.userName.orEmpty(), size = 32.dp)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(comment.userName.orEmpty(), style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                    Spacer(Modifier.weight(1f))
                    comment.createTime?.let {
                        Text(formatMomentTime(it), style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
                    }
                }
                if (!comment.replyUserName.isNullOrBlank()) {
                    Text("回复 @${comment.replyUserName}：", style = PersonaTheme.type.caption, color = PersonaTheme.palette.accent)
                }
                val commentText = comment.content?.text
                    ?: comment.content?.segments?.joinToString("") { it.text ?: it.key ?: "" }.orEmpty()
                LarkLinkedText(commentText)
            }
        }
    }
}

@Composable
private fun MomentsImagesLayout(
    images: List<LarkMomentsImage>,
    onOpenImage: (List<ViewerImage>, Int) -> Unit,
) {
    val context = LocalContext.current
    val viewerImages = images.mapIndexed { idx, img ->
        ViewerImage(
            key = img.url ?: "$idx",
            width = img.width?.toInt(),
            height = img.height?.toInt(),
            load = {
                val req = ImageRequest.Builder(context).data(img.url).build()
                val result = context.imageLoader.execute(req)
                if (result is SuccessResult) {
                    val bitmap = result.image.toBitmap()
                    val stream = ByteArrayOutputStream()
                    bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, stream)
                    stream.toByteArray()
                } else {
                    byteArrayOf()
                }
            }
        )
    }

    if (images.size == 1) {
        val img = images.first()
        val shape = RoundedCornerShape(8.dp)
        AsyncImage(
            model = img.url,
            contentDescription = "图片",
            modifier = Modifier
                .fillMaxWidth(0.7f)
                .aspectRatio(1.2f)
                .clip(shape)
                .clickable { onOpenImage(viewerImages, 0) }
        )
    } else {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            images.chunked(3).forEachIndexed { rowIndex, row ->
                Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                    row.forEachIndexed { colIndex, img ->
                        val index = rowIndex * 3 + colIndex
                        val shape = RoundedCornerShape(6.dp)
                        AsyncImage(
                            model = img.url,
                            contentDescription = "图片",
                            modifier = Modifier
                                .weight(1f)
                                .aspectRatio(1f)
                                .clip(shape)
                                .clickable { onOpenImage(viewerImages, index) }
                        )
                    }
                    repeat(3 - row.size) {
                        Spacer(Modifier.weight(1f))
                    }
                }
            }
        }
    }
}

private fun reactionLabel(type: String): String = when (type.uppercase()) {
    "THUMBSUP" -> "👍"
    "HEART" -> "❤️"
    "APPLAUD" -> "👏"
    "SMILE" -> "😄"
    "OK" -> "👌"
    else -> type
}
