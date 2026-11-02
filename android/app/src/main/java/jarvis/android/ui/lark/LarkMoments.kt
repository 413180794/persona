package jarvis.android.ui.lark

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.data.HubException
import jarvis.android.lark.LarkFormat
import jarvis.android.lark.LarkMoments
import jarvis.android.lark.LarkMomentsComment
import jarvis.android.lark.LarkMomentsPost
import jarvis.android.lark.LarkPart
import jarvis.android.lark.LarkParts
import jarvis.android.lark.LarkTextBase
import jarvis.android.lark.LarkUrlPreview
import jarvis.android.lark.larkMomentsComments
import jarvis.android.lark.larkMomentsPost
import jarvis.android.lark.larkUrlPreview
import jarvis.android.lark.runs
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.IconAction
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.LarkChrome
import jarvis.android.ui.design.LarkIcons
import jarvis.android.ui.design.PageLoading
import java.util.concurrent.ConcurrentHashMap

// removed (iOS LarkReactionsMoments.swift): a shared post in a chat as Feishu's link preview, and the post itself with
// its comments — opened from that card, or as the inbox's removed item (docs/inbox.md「removed」).

/** Link previews seen in this process (a card scrolled past does not fetch again). */
private val previews = ConcurrentHashMap<String, LarkUrlPreview>()

/** A shared removed post in a chat: Feishu's preview (title, author, excerpt, counts); a tap reads the post. */
@Composable
fun LarkMomentsCard(part: LarkPart) {
    val ctx = LocalLarkContext.current
    val actions = LocalLarkActions.current
    val hub = LocalLarkHub.current
    val c = JTheme.colors
    val url = part.url
    val key = "${ctx.messageId}|$url"
    var preview by remember(key) { mutableStateOf(previews[key]) }
    LaunchedEffect(key, hub) {
        val mid = ctx.messageId ?: return@LaunchedEffect
        val api = hub?.api ?: return@LaunchedEffect
        if (preview != null || url == null) return@LaunchedEffect
        preview = runCatching { api.larkUrlPreview(mid, url) }.getOrNull()?.also { previews[key] = it }
    }
    val card = preview?.card
    val shape = RoundedCornerShape(JRadius.card)
    val postId = LarkMoments.postId(url)
    Column(
        Modifier.fillMaxWidth().clip(shape).background(LarkChrome.cardBackground).border(JSize.hairline, LarkChrome.cardStroke, shape)
            .clickable { if (postId != null && actions.openMoments != null) actions.openMoments.invoke(postId, url) else url?.let(actions.openUrl) }
            .padding(JSpace.m),
        verticalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            Icon(LarkIcons.moments, null, Modifier.size(JSize.markS), tint = LarkChrome.blue)
            Text(preview?.title ?: "removed动态", style = JTheme.type.secondaryStrong, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        card?.author?.let { author ->
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                LarkAvatar(author, card.avatarUrl, size = JSize.wellXS)
                Column {
                    Text(author, style = JTheme.type.captionMedium, color = c.label, maxLines = 1)
                    card.subtitle?.let { Text(it, style = JTheme.type.label, color = c.secondaryText, maxLines = 1) }
                }
            }
        }
        val body = card?.body
        if (!body.isNullOrEmpty()) Text(body, style = JTheme.type.secondary, color = c.label.copy(alpha = 0.85f), maxLines = 5, overflow = TextOverflow.Ellipsis)
        else if (preview == null) Text(part.text ?: "", style = JTheme.type.label, color = c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis)
        card?.footer?.let { Text(it, style = JTheme.type.label, color = c.secondaryText) }
    }
}

/**
 * One removed post: the author, the text with Feishu emoji, the pictures, reactions, then the comments. As a page's
 * content (the inbox item) or inside [LarkMomentsSheet]. People open their card, pictures the viewer.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LarkMomentsPostView(
    postId: String, modifier: Modifier = Modifier, contentPadding: PaddingValues = PaddingValues(JSpace.page),
    /** Already read (screenshots): drawn at once, read again from the hub. */
    initial: Pair<LarkMomentsPost, List<LarkMomentsComment>>? = null,
) {
    val hub = LocalLarkHub.current
    var post by remember(postId) { mutableStateOf(initial?.first) }
    var comments by remember(postId) { mutableStateOf(initial?.second.orEmpty()) }
    var error by remember(postId) { mutableStateOf<String?>(null) }
    var person by remember { mutableStateOf<LarkPersonRef?>(null) }
    val outer = LocalLarkActions.current
    val c = JTheme.colors
    LaunchedEffect(postId, hub) {
        val api = hub?.api ?: return@LaunchedEffect
        LarkMedia.loadCatalog(api)
        runCatching { api.larkMomentsPost(postId) }.onSuccess { post = it }.onFailure { error = (it as? HubException)?.message ?: it.message ?: "加载失败" }
        comments = runCatching { api.larkMomentsComments(postId) }.getOrDefault(emptyList())
    }
    val actions = remember(outer) { LarkActions(openUrl = outer.openUrl, openImages = outer.openImages, openProfile = { person = it }) }
    CompositionLocalProvider(LocalLarkActions provides actions) {
        LazyColumn(modifier.fillMaxWidth(), contentPadding = contentPadding, verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
            error?.let { e -> item(key = "error") { ErrorLine(e) } }
            val p = post
            if (p == null && error == null) item(key = "loading") { PageLoading() }
            if (p != null) {
                item(key = "author") { MomentsAuthor(p.userName ?: "", p.userAvatarUrl, p.userId, p.createTime, small = false) { person = it } }
                item(key = "text") { LarkInlineText(remember(p) { p.content.runs() }, base = LarkTextBase(size = 17f)) }
                if (!p.images.isNullOrEmpty()) item(key = "images") { MomentsImages(p.images) }
                val groups = p.reactionGroups.orEmpty()
                if (groups.isNotEmpty()) item(key = "reactions") {
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.sm), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                        for (g in groups) Row(
                            Modifier.clip(CircleShape).background(c.label.copy(alpha = 0.05f)).padding(horizontal = JSpace.s, vertical = JSpace.xs),
                            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
                        ) {
                            LarkEmojiImage(g.type, JSize.markL)
                            Text("${g.count}", style = JTheme.type.label, color = if (g.selfInvolved) LarkChrome.blue else c.secondaryText)
                        }
                    }
                }
                item(key = "count") {
                    Column(verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
                        HorizontalDivider(color = c.separator.copy(alpha = 0.4f))
                        Text("${p.commentCount ?: comments.size} 条评论", style = JTheme.type.secondaryStrong, color = c.label)
                    }
                }
                items(comments, key = { it.id }) { cm ->
                    Column(verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                        MomentsAuthor(cm.userName ?: "", cm.userAvatarUrl, cm.userId, cm.createTime, small = true) { person = it }
                        LarkInlineText(remember(cm) { cm.content.runs() }, Modifier.padding(start = JSize.avatar), base = LarkTextBase(size = 15f))
                    }
                }
            }
        }
    }
    person?.let { LarkProfileSheet(it, onDismiss = { person = null }) }
}

/** A removed post over the page (opened from a share card in a chat), with 在飞书中打开. */
@Composable
fun LarkMomentsSheet(postId: String, link: String?, onDismiss: () -> Unit) {
    val actions = LocalLarkActions.current.closing(onDismiss)
    JSheet(onDismiss, title = "removed", onClose = onDismiss) {
        if (link != null) Row(Modifier.fillMaxWidth().padding(horizontal = JSpace.s), horizontalArrangement = Arrangement.End) {
            IconAction(JIcons.openExternal, "在飞书中打开", { actions.openUrl(link) }, tint = LarkChrome.blue)
        }
        CompositionLocalProvider(LocalLarkActions provides actions) { LarkMomentsPostView(postId, Modifier.fillMaxSize()) }
    }
}

@Composable
private fun MomentsAuthor(name: String, avatar: String?, id: String?, time: Double?, small: Boolean, open: (LarkPersonRef) -> Unit) {
    val c = JTheme.colors
    Row(
        Modifier.clip(RoundedCornerShape(JRadius.control)).clickable(enabled = id != null) { id?.let { open(LarkPersonRef(it, name, avatar)) } },
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        LarkAvatar(name, avatar, size = if (small) JSize.wellS else JSize.avatar)
        Column(verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(name, style = if (small) JTheme.type.captionStrong else JTheme.type.secondaryStrong, color = c.label, maxLines = 1)
            if (time != null) Text(LarkFormat.momentsTime(time), style = JTheme.type.label, color = c.secondaryText)
        }
    }
}

@Composable
private fun MomentsImages(imgs: List<LarkMomentsPost.Image>) {
    val actions = LocalLarkActions.current
    val urls = imgs.mapNotNull { it.url }
    if (urls.isEmpty()) return
    if (urls.size == 1) {
        val img = imgs.first()
        val (w, h) = LarkParts.displaySize(img.width ?: 0.0, img.height ?: 0.0, ONE_W, ONE_H)
        LarkRemoteImage(urls[0], Modifier.size(w.dp, h.dp).clip(RoundedCornerShape(JRadius.control)).clickable { actions.openImages(urls, 0) }, maxPx = ONE_PX)
        return
    }
    Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
        urls.withIndex().chunked(3).forEach { row ->
            Row(horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                for ((i, u) in row) LarkRemoteImage(u, Modifier.weight(1f).aspectRatio(1f).clip(RoundedCornerShape(JRadius.badge)).clickable { actions.openImages(urls, i) }, maxPx = GRID_PX)
                repeat(3 - row.size) { Spacer(Modifier.weight(1f)) }
            }
        }
    }
}

/** A single picture of a post fits 300 × 360; several make a grid of three. */
private const val ONE_W = 300.0
private const val ONE_H = 360.0
private const val ONE_PX = 1200
private const val GRID_PX = 600
