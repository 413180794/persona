package jarvis.android.ui.lark

import android.media.MediaPlayer
import android.net.Uri
import android.widget.MediaController
import android.widget.VideoView
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import jarvis.android.data.HubException
import jarvis.android.data.JsonCodec
import jarvis.android.lark.LarkChatRules
import jarvis.android.lark.LarkForwardedMessage
import jarvis.android.lark.LarkMessage
import jarvis.android.lark.LarkPalette
import jarvis.android.lark.LarkProfile
import jarvis.android.lark.get
import jarvis.android.lark.larkProfile
import jarvis.android.lark.larkReact
import jarvis.android.lark.larkThread
import jarvis.android.lark.str
import jarvis.android.lark.togglingReaction
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.LarkChrome
import jarvis.android.ui.design.LarkIcons
import jarvis.android.ui.design.LarkSize
import jarvis.android.ui.design.PageLoading
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.media.ImageMath
import jarvis.android.ui.media.MediaShare
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonElement
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

// The chat page's sheets (iOS LarkPeople.swift, LarkReactionsMoments.swift, LarkChatView.swift's thread view): a
// person's card, 表情回复, a thread, a merge-forward's messages, the video / voice player.

private fun Throwable.text(): String = (this as? HubException)?.message ?: message ?: "加载失败"

// --- Profile card ---------------------------------------------------------------------------------------------------

/**
 * A person's card, laid out like lark-connector's: the cover, the avatar, names, status, signature, then the info rows (the
 * first five, 查看更多信息 for the rest). A leader row opens the leader's card in the same sheet; back returns.
 */
@Composable
fun LarkProfileSheet(person: LarkPersonRef, onDismiss: () -> Unit) {
    val stack = remember(person) { mutableStateListOf(person) }
    val outer = LocalLarkActions.current
    JSheet(onDismiss) {
        BackHandler(enabled = stack.size > 1) { stack.removeAt(stack.size - 1) }
        CompositionLocalProvider(LocalLarkActions provides outer.closing(onDismiss)) {
            LarkProfileCard(stack.last(), onOpenPerson = { stack.add(it) })
        }
    }
}

@OptIn(ExperimentalLayoutApi::class, ExperimentalFoundationApi::class)
@Composable
private fun LarkProfileCard(person: LarkPersonRef, onOpenPerson: (LarkPersonRef) -> Unit) {
    val hub = LocalLarkHub.current
    var profile by remember(person) { mutableStateOf<LarkProfile?>(null) }
    var error by remember(person) { mutableStateOf<String?>(null) }
    var showAll by remember(person) { mutableStateOf(false) }
    val actions = LocalLarkActions.current
    val ctx = LocalContext.current
    val c = JTheme.colors
    LaunchedEffect(person, hub) {
        val api = hub?.api ?: return@LaunchedEffect
        runCatching { api.larkProfile(person.id, person.feedId) }.onSuccess { profile = it }.onFailure { error = it.text() }
    }
    val p = profile
    val displayName = p?.alias?.takeIf { it.isNotEmpty() } ?: p?.displayName ?: person.name
    Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState())) {
        Box(Modifier.fillMaxWidth().height(LarkSize.coverWithAvatar)) {
            val cover = p?.coverUrl
            if (!cover.isNullOrEmpty()) LarkRemoteImage(cover, Modifier.fillMaxWidth().height(LarkSize.cover), maxPx = COVER_PX)
            else Box(Modifier.fillMaxWidth().height(LarkSize.cover).background(Brush.linearGradient(listOf(LarkChrome.blue.copy(alpha = 0.35f), LarkChrome.blue.copy(alpha = 0.12f)))))
            Box(Modifier.padding(start = JSpace.l).offset(y = LarkSize.coverWithAvatar - JSize.box80).size(JSize.box80).clip(CircleShape).background(c.page).padding(JSize.bar)) {
                LarkAvatar(displayName, p?.avatarUrl ?: person.avatar, size = LarkSize.profileAvatar)
            }
        }
        Column(Modifier.padding(horizontal = JSpace.page, vertical = JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.l)) {
            Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                    Text(displayName, style = JTheme.type.title, color = c.label, maxLines = 2, modifier = Modifier.weight(1f, fill = false))
                    if (p?.userType == "bot") NameTag("机器人", larkColor(LarkPalette.fixed("orange")))
                    if (p?.relation?.external == true) NameTag("外部", LarkChrome.blue)
                }
                val subtitle = listOfNotNull(p?.enName, if (!p?.alias.isNullOrEmpty()) p?.displayName else null, p?.groupNickname?.let { "群昵称：$it" })
                    .filter { it.isNotEmpty() && it != displayName }
                if (subtitle.isNotEmpty()) Text(subtitle.joinToString(" · "), style = JTheme.type.secondary, color = c.secondaryText)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.sm), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    val ws = p?.workStatus?.status
                    if (ws == 1 || ws == 2) StatusChip(if (ws == 1) "请假中" else "会议中", larkColor(LarkPalette.fixed("orange")))
                    p?.customStatus?.title?.takeIf { it.isNotEmpty() }?.let { StatusChip(it, LarkChrome.blue) }
                    p?.medalName?.takeIf { it.isNotEmpty() }?.let { StatusChip(it, larkColor(LarkPalette.fixed("purple"))) }
                    localTime(p?.timezone)?.let { StatusChip("当地时间 $it", c.secondaryText) }
                }
                if (p?.relation?.external == true) p.relation.tenantName?.let { Text(it, style = JTheme.type.caption, color = c.secondaryText) }
            }
            error?.let { ErrorLine(it) }
            if (p == null && error == null) PageLoading()
            p?.description?.takeIf { it.isNotEmpty() }?.let {
                Text(it, style = JTheme.type.secondary, color = c.secondaryText, maxLines = 6,
                    modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.card)).background(c.label.copy(alpha = 0.04f)).padding(JSpace.m))
            }
            val rows = p?.fields.orEmpty().filter { f -> !f.value.str.isNullOrEmpty() && f.key !in HIDDEN_FIELDS }
            if (rows.isNotEmpty()) Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.card)).background(c.card)) {
                (if (showAll) rows else rows.take(5)).forEachIndexed { i, f ->
                    if (i > 0) RowDivider()
                    val value = f.value.str.orEmpty()
                    val label = when (f.key) { "B-DEPARTMENT" -> "部门"; "B-LEADER" -> "直属上级"; "B-EMPLOYEETYPE" -> "人员类型"; else -> f.label ?: f.key }
                    val leader = f.url?.takeIf { it.startsWith("lark://client/profile") }?.let { Uri.parse(it).getQueryParameter("uid") }
                    val link = f.url?.takeIf { it.startsWith("http") }
                    Row(
                        Modifier.fillMaxWidth().combinedClickable(
                            onClick = { if (leader != null) onOpenPerson(LarkPersonRef(leader, value)) else if (link != null) actions.openUrl(link) },
                            onLongClick = { MediaShare.copyText(ctx, value); Toasts.show("已拷贝", ToastStyle.SUCCESS) },
                        ).padding(horizontal = JSpace.l, vertical = JSpace.m),
                        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
                    ) {
                        Text(label, style = JTheme.type.secondary, color = c.secondaryText, maxLines = 2, modifier = Modifier.width(JSize.box76))
                        Text(value, style = JTheme.type.secondary, color = if (leader != null || link != null) LarkChrome.blue else c.label, modifier = Modifier.weight(1f))
                    }
                }
                if (rows.size > 5) {
                    RowDivider()
                    Box(Modifier.fillMaxWidth().heightIn(min = JSize.tapTarget).clickable { showAll = !showAll }, contentAlignment = Alignment.Center) {
                        Text(if (showAll) "收起" else "查看更多信息", style = JTheme.type.secondary, color = LarkChrome.blue, maxLines = 1)
                    }
                }
            }
        }
    }
}

private const val COVER_PX = 1200
private val HIDDEN_FIELDS = setOf("B-ALIAS", "B-ALIAS-AND-MEMO", "B-CHATGROUPNICKNAME", "B-MORE")

private fun localTime(tz: String?): String? {
    val zone = tz?.let { runCatching { ZoneId.of(it) }.getOrNull() } ?: return null
    val now = Instant.now()
    if (zone.rules.getOffset(now) == ZoneId.systemDefault().rules.getOffset(now)) return null
    return now.atZone(zone).format(DateTimeFormatter.ofPattern("HH:mm"))
}

@Composable
private fun StatusChip(text: String, color: Color) {
    Text(text, style = JTheme.type.labelMedium, color = color, maxLines = 1,
        modifier = Modifier.clip(CircleShape).background(color.copy(alpha = 0.12f)).padding(horizontal = JSpace.s, vertical = JSpace.xxs))
}

/** A small tag after a name: 外部, 机器人. */
@Composable
private fun NameTag(text: String, hue: Color) {
    Text(text, style = JTheme.type.labelMedium, color = hue, maxLines = 1,
        modifier = Modifier.clip(RoundedCornerShape(JSpace.xs)).background(hue.copy(alpha = 0.13f)).padding(horizontal = JSpace.xs, vertical = JSpace.xxs))
}

// --- Reaction picker -------------------------------------------------------------------------------------------------

/** 表情回复: the user's quick reactions on top, then every group of the catalogue (Feishu's picker). */
@Composable
fun LarkReactionPicker(message: LarkMessage, onPick: (String) -> Unit, onDismiss: () -> Unit) {
    val hub = LocalLarkHub.current
    LaunchedEffect(hub) { hub?.let { LarkMedia.loadCatalog(it.api) } }
    val quick by LarkMedia.quickReactions.collectAsState()
    val groups by LarkMedia.groups.collectAsState()
    val mine = remember(message) { message.reactions.orEmpty().filter { it.mine }.map { it.key.lowercase() }.toSet() }
    JSheet(onDismiss, title = "表情回复", onClose = onDismiss, halfFirst = true) {
        LazyVerticalGrid(
            GridCells.Fixed(PICKER_COLUMNS), Modifier.fillMaxWidth().heightIn(max = LarkSize.pickerMax),
            contentPadding = PaddingValues(horizontal = JSpace.page), verticalArrangement = Arrangement.spacedBy(JSpace.s),
            horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
        ) {
            items(quick.take(PICKER_COLUMNS), key = { "q:$it" }) { key -> EmojiCell(key, key.lowercase() in mine) { onPick(key); onDismiss() } }
            for (g in groups) {
                item(span = { GridItemSpan(maxLineSpan) }, key = "h:${g.title}") {
                    Text(g.title, style = JTheme.type.sectionHeader, color = JTheme.colors.secondaryText, modifier = Modifier.padding(top = JSpace.m))
                }
                items(g.keys, key = { "${g.title}:$it" }) { key -> EmojiCell(key, key.lowercase() in mine) { onPick(key); onDismiss() } }
            }
            if (groups.isEmpty()) item(span = { GridItemSpan(maxLineSpan) }) { PageLoading() }
        }
    }
}

private const val PICKER_COLUMNS = 7

@Composable
private fun EmojiCell(key: String, selected: Boolean, onClick: () -> Unit) {
    Box(
        Modifier.fillMaxWidth().heightIn(min = JSize.tapTarget).clip(RoundedCornerShape(JRadius.control))
            .background(if (selected) LarkChrome.blue.copy(alpha = 0.15f) else Color.Transparent)
            .clickable(onClick = onClick).semantics { contentDescription = LarkMedia.emoji(key)?.label ?: key },
        contentAlignment = Alignment.Center,
    ) { LarkEmojiImage(key, JSize.wellS) }
}

// --- Thread ------------------------------------------------------------------------------------------------------------

/** A thread: its first message, then every reply (reactions work here too). */
@Composable
fun LarkThreadSheet(root: LarkMessage, isGroup: Boolean, feedId: String?, onDismiss: () -> Unit) {
    val hub = LocalLarkHub.current
    var replies by remember(root.id) { mutableStateOf<List<LarkMessage>>(emptyList()) }
    var loading by remember(root.id) { mutableStateOf(true) }
    var error by remember(root.id) { mutableStateOf<String?>(null) }
    var picking by remember { mutableStateOf<LarkMessage?>(null) }
    val outer = LocalLarkActions.current
    val scope = rememberCoroutineScope()
    val c = JTheme.colors
    LaunchedEffect(root.id, hub) {
        val api = hub?.api ?: return@LaunchedEffect
        runCatching { api.larkThread(root.id) }.onSuccess { replies = it.messages.filter { m -> m.id != root.id } }.onFailure { error = it.text() }
        loading = false
    }
    val react: (LarkMessage, String) -> Unit = { m, key ->
        val i = replies.indexOfFirst { it.id == m.id }
        val api = hub?.api
        if (i >= 0 && api != null) {
            val (next, remove) = m.togglingReaction(key, "我")
            replies = replies.toMutableList().also { it[i] = next }
            scope.launch { runCatching { api.larkReact(m.id, key, remove) }.onFailure { Toasts.show("没回应上：${it.text()}", ToastStyle.ERROR) } }
        }
    }
    JSheet(onDismiss, title = "话题", onClose = onDismiss) {
        val base = outer.closing(onDismiss)
        val actions = LarkActions(
            openUrl = base.openUrl, openImages = base.openImages, openFile = base.openFile, playMedia = base.playMedia,
            openProfile = base.openProfile, react = react, pickReaction = { picking = it }, openForwarded = base.openForwarded, openMoments = base.openMoments,
        )
        CompositionLocalProvider(LocalLarkActions provides actions) {
            LazyColumn(Modifier.fillMaxWidth(), contentPadding = PaddingValues(horizontal = JSpace.m)) {
                item(key = root.id) { LarkMessageRow(root, grouped = false, isGroup = isGroup, feedId = feedId, inThread = true) }
                item(key = "count") {
                    Row(Modifier.padding(vertical = JSpace.m), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                        Text(if (loading) "加载回复…" else "${replies.size} 条回复", style = JTheme.type.labelMedium, color = c.secondaryText)
                        HorizontalDivider(Modifier.weight(1f), color = c.label.copy(alpha = 0.08f))
                    }
                }
                error?.let { item(key = "err") { ErrorLine(it) } }
                itemsIndexed(replies, key = { _, m -> m.id }) { i, m ->
                    val cont = LarkChatRules.continues(m, replies.getOrNull(i - 1))
                    LarkMessageRow(m, grouped = cont, isGroup = isGroup, feedId = feedId, inThread = true,
                        modifier = Modifier.padding(top = if (cont) JSize.bar else JSpace.m))
                }
            }
        }
    }
    picking?.let { m -> LarkReactionPicker(m, onPick = { react(m, it) }, onDismiss = { picking = null }) }
}

// --- Merge-forward -----------------------------------------------------------------------------------------------------

/** Every message of a merge-forward, drawn like the chat. */
@Composable
fun LarkForwardedSheet(title: String, meta: JsonElement, onDismiss: () -> Unit) {
    val messages = remember(meta) {
        runCatching { JsonCodec.decodeFromJsonElement(ListSerializer(LarkForwardedMessage.serializer()), meta["messages"] ?: return@remember emptyList()) }.getOrDefault(emptyList())
    }
    val outer = LocalLarkActions.current
    val c = JTheme.colors
    JSheet(onDismiss, title = title.ifEmpty { "聊天记录" }, onClose = onDismiss) {
        val base = outer.closing(onDismiss)
        val actions = LarkActions(openUrl = base.openUrl, openImages = base.openImages, openFile = base.openFile, playMedia = base.playMedia,
            openForwarded = base.openForwarded, openMoments = base.openMoments)
        CompositionLocalProvider(LocalLarkActions provides actions) {
            LazyColumn(Modifier.fillMaxWidth(), contentPadding = PaddingValues(horizontal = JSpace.page), verticalArrangement = Arrangement.spacedBy(JSpace.l)) {
                itemsIndexed(messages, key = { i, m -> m.id.ifEmpty { "$i" } }) { _, m ->
                    Row(horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
                        LarkAvatar(m.senderName ?: "?", m.senderAvatarUrl?.let(::hubMediaUrl), size = JSize.well)
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                            Row(horizontalArrangement = Arrangement.spacedBy(JSpace.sm), verticalAlignment = Alignment.CenterVertically) {
                                Text(m.senderName ?: "", style = JTheme.type.labelStrong, color = c.secondaryText, maxLines = 1)
                                m.createdAt?.let { Text(LarkChatRules.dayLabel(it), style = JTheme.type.label, color = c.tertiaryText, maxLines = 1) }
                            }
                            val parts = m.parts.orEmpty()
                            if (parts.isNotEmpty()) WithLarkMessage(m.id, parts) { LarkPartsView(parts) }
                            else Text(m.textPreview ?: "", style = JTheme.type.body, color = c.label)
                        }
                    }
                }
            }
        }
    }
}

/** Avatars inside forwarded messages are lark-connector paths; the hub serves them under /api/v1/lark. */
fun hubMediaUrl(s: String): String = if (s.startsWith("/api/media/")) "/api/v1/lark/media/" + s.removePrefix("/api/media/") else s

// --- Player ------------------------------------------------------------------------------------------------------------

/**
 * Plays a Feishu video (full screen) or voice message through the hub: the platform player sends the token header
 * and asks with Range itself; the hub forwards Range / Content-Range (CLAUDE.md).
 */
@Composable
fun LarkPlayer(url: String, audio: Boolean, title: String, onDismiss: () -> Unit) {
    val hub = LocalLarkHub.current ?: return
    val target = remember(url) { ImageMath.resolve(hub.api.baseUrl, url) }
    if (target == null) {
        LaunchedEffect(Unit) { onDismiss() }
        return
    }
    val headers = if (target.hub) mapOf("Authorization" to "Bearer ${hub.api.token}") else emptyMap()
    if (audio) AudioPlayer(target.url.toString(), headers, title, onDismiss) else VideoPlayer(target.url.toString(), headers, onDismiss)
}

@Composable
private fun VideoPlayer(url: String, headers: Map<String, String>, onDismiss: () -> Unit) {
    var loading by remember { mutableStateOf(true) }
    var failed by remember { mutableStateOf(false) }
    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        Box(Modifier.fillMaxSize().background(Color.Black)) {
            AndroidView(
                factory = { ctx ->
                    VideoView(ctx).apply {
                        val mc = MediaController(ctx)
                        mc.setAnchorView(this)
                        setMediaController(mc)
                        setOnPreparedListener { loading = false; start() }
                        setOnErrorListener { _, _, _ -> loading = false; failed = true; true }
                        setVideoURI(Uri.parse(url), headers)
                    }
                },
                modifier = Modifier.align(Alignment.Center).fillMaxWidth(),
                onRelease = { it.stopPlayback() },
            )
            if (loading) CircularProgressIndicator(Modifier.align(Alignment.Center).size(JSize.well), color = Color.White, strokeWidth = JSize.rule)
            if (failed) Text("播放失败", style = JTheme.type.secondary, color = Color.White, modifier = Modifier.align(Alignment.Center))
            CircleButton(JIcons.close, "关闭", onDismiss, Modifier.statusBarsPadding().padding(JSpace.m))
        }
    }
}

@Composable
private fun AudioPlayer(url: String, headers: Map<String, String>, title: String, onDismiss: () -> Unit) {
    val ctx = LocalContext.current
    var playing by remember { mutableStateOf(false) }
    var ready by remember { mutableStateOf(false) }
    var failed by remember { mutableStateOf(false) }
    var progress by remember { mutableFloatStateOf(0f) }
    val player = remember(url) { MediaPlayer() }
    DisposableEffect(url) {
        try {
            player.setDataSource(ctx, Uri.parse(url), headers)
            player.setOnPreparedListener { ready = true; it.start(); playing = true }
            player.setOnCompletionListener { playing = false; progress = 1f }
            player.setOnErrorListener { _, _, _ -> failed = true; true }
            player.prepareAsync()
        } catch (e: Exception) {
            failed = true
        }
        onDispose { runCatching { player.release() } }
    }
    LaunchedEffect(playing) {
        while (playing) {
            runCatching { if (player.duration > 0) progress = player.currentPosition.toFloat() / player.duration }
            delay(PROGRESS_MS)
        }
    }
    val c = JTheme.colors
    JSheet(onDismiss, title = title, onClose = onDismiss) {
        Column(Modifier.fillMaxWidth().padding(JSpace.xl), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(JSpace.l)) {
            if (failed) Text("播放失败", style = JTheme.type.secondary, color = c.danger)
            Box(
                Modifier.size(JSize.box64).clip(CircleShape).background(LarkChrome.blue).clickable(enabled = ready) {
                    if (player.isPlaying) { player.pause(); playing = false } else { player.start(); playing = true }
                }.semantics { contentDescription = if (playing) "暂停" else "播放" },
                contentAlignment = Alignment.Center,
            ) {
                if (!ready && !failed) CircularProgressIndicator(Modifier.size(JSize.icon), color = Color.White, strokeWidth = JSize.rule)
                else Icon(if (playing) LarkIcons.pause else LarkIcons.play, null, Modifier.size(JSize.well), tint = Color.White)
            }
            LinearProgressIndicator(progress = { progress }, Modifier.fillMaxWidth(), color = LarkChrome.blue, trackColor = c.fillChip)
        }
    }
}

private const val PROGRESS_MS = 200L
