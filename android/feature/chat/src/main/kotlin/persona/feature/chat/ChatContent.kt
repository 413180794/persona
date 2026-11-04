package persona.feature.chat

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.FirstBaseline
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil3.ImageLoader
import coil3.compose.AsyncImage
import coil3.decode.DataSource
import coil3.decode.ImageSource
import coil3.fetch.Fetcher
import coil3.fetch.SourceFetchResult
import coil3.key.Keyer
import coil3.request.Options
import com.mikepenz.markdown.compose.components.markdownComponents
import com.mikepenz.markdown.compose.elements.MarkdownBulletList
import com.mikepenz.markdown.compose.elements.MarkdownOrderedList
import com.mikepenz.markdown.compose.elements.listDepth
import com.mikepenz.markdown.m3.markdownColor
import com.mikepenz.markdown.m3.markdownTypography
import com.mikepenz.markdown.m3.Markdown
import com.mikepenz.markdown.model.State as MarkdownResult
import com.mikepenz.markdown.model.parseMarkdown
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatRepository
import persona.core.designsystem.*
import persona.core.designsystem.WorkStep as TimelineStep
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.lines.*
import androidx.compose.runtime.key
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okio.FileSystem
import okio.Path.Companion.toOkioPath

internal class ChatMarkdownCache {
    private val entries = object : LinkedHashMap<String, Pair<String, MarkdownResult>>(80, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, Pair<String, MarkdownResult>>) = size > 80
    }
    @Synchronized fun cached(id: String, text: String) = entries[id]?.takeIf { it.first == text }?.second
    suspend fun parse(id: String, text: String): MarkdownResult {
        cached(id, text)?.let { return it }
        val parsed = withContext(Dispatchers.Default) { parseMarkdown(ChatLinks.markdown(text)) }
        synchronized(this) { entries[id] = text to parsed }
        return parsed
    }
}

internal val LocalChatMarkdownCache = staticCompositionLocalOf<ChatMarkdownCache?> { null }

/**
 * Markdown in a conversation. A message's words pass [selectable] false: a long press on a message opens its menu (whose
 * 选择 shows the text to select from), and a SelectionContainer would take the press (docs/android.md, the old App's
 * lessons). Text without that menu (thinking, a plan, a recap) stays selectable.
 */
@Composable
internal fun ChatMarkdown(text: String, id: String = text,
    bodyStyle: TextStyle = PersonaTheme.type.body, color: Color = PersonaTheme.palette.ink, selectable: Boolean = true) {
    if (selectable) SelectionContainer { ChatMarkdownBody(text, id, bodyStyle, color) }
    else ChatMarkdownBody(text, id, bodyStyle, color)
}

@Composable
private fun ChatMarkdownBody(text: String, id: String, bodyStyle: TextStyle, color: Color) {
    val cache = LocalChatMarkdownCache.current ?: remember { ChatMarkdownCache() }
    // The paths there on the session's machine become links that open the files pane (PathLinks.kt).
    val actions = LocalChatRowActions.current
    val presence = actions?.paths
    val open = actions?.openPath
    val asked = remember(text, presence) { if (presence == null) emptyList() else PathLinks.asked(text) }
    if (presence != null && asked.isNotEmpty()) LaunchedEffect(presence, asked) { presence.ask(asked) }
    val linkable = if (presence == null || open == null) emptySet() else asked.filterTo(LinkedHashSet(), presence::linkable)
    val source = remember(text, linkable) { PathLinks.markdown(text, linkable) }
    val parsed by produceState(cache.cached(id, source), id, source, cache) { value = cache.parse(id, source) }
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val outer = LocalUriHandler.current
    val uriHandler = remember(outer, open) { if (open == null) outer else PathLinks.handler(outer, open) }
    CompositionLocalProvider(LocalUriHandler provides uriHandler) {
        if (parsed == null) ChatLinkText(text, Modifier.fillMaxWidth(), bodyStyle.copy(color = color))
        else Markdown(parsed!!, modifier = Modifier.fillMaxWidth(),
            colors = markdownColor(text = color, codeBackground = palette.sunken,
                inlineCodeBackground = palette.sunken, dividerColor = palette.line,
                tableBackground = palette.surface, darkTheme = palette.dark,
                highlightText = palette.ink, highlightBackground = palette.marker),
            typography = markdownTypography(h1 = type.largeTitle, h2 = type.title, h3 = type.headline,
                h4 = type.bodyStrong, h5 = type.callout, h6 = type.caption,
                text = bodyStyle, paragraph = bodyStyle, code = type.code, inlineCode = type.code,
                quote = type.callout.copy(color = palette.ink2), ordered = bodyStyle,
                bullet = bodyStyle, list = bodyStyle, table = bodyStyle, alertTitle = type.bodyStrong,
                textLink = TextLinkStyles(SpanStyle(color = palette.accent, textDecoration = TextDecoration.Underline))),
            components = markdownComponents(
                orderedList = { model -> MarkdownOrderedList(model.content, model.node, depth = model.listDepth,
                    markerModifier = { Modifier.alignBy(FirstBaseline).padding(end = 8.dp) },
                    listModifier = { Modifier.weight(1f).alignBy(FirstBaseline) }) },
                unorderedList = { model -> MarkdownBulletList(model.content, model.node, depth = model.listDepth,
                    markerModifier = { Modifier.alignBy(FirstBaseline).padding(end = 8.dp) },
                    listModifier = { Modifier.weight(1f).alignBy(FirstBaseline) }) },
                codeFence = { model ->
                    val raw = model.content.substring(model.node.startOffset, model.node.endOffset)
                    val lines = raw.lines()
                    val code = lines.drop(1).let { if (it.lastOrNull()?.trim()?.startsWith("```") == true || it.lastOrNull()?.trim()?.startsWith("~~~") == true) it.dropLast(1) else it }.joinToString("\n")
                    CodeText(code, lines.firstOrNull()?.trim()?.trimStart('`', '~').orEmpty())
                },
                codeBlock = { model -> CodeText(model.content.substring(model.node.startOffset, model.node.endOffset).trimIndent()) },
            ))
    }
}

@Composable
internal fun CodeText(text: String, language: String = "") {
    var expanded by remember(text) { mutableStateOf(false) }
    val preview = remember(text, expanded) { if (expanded) text else text.lineSequence().take(40).joinToString("\n") }
    val clipboard = LocalClipboardManager.current
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(language, Modifier.weight(1f), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            JTextButton("复制", { clipboard.setText(AnnotatedString(text)) }, icon = PhIcons.Copy)
        }
        if (remember(preview) { ChatLinks.matches(preview).isNotEmpty() }) {
            SelectionContainer {
                Text(chatLinkAnnotations(preview), style = PersonaTheme.type.code, color = PersonaTheme.palette.ink,
                    softWrap = false, modifier = Modifier.fillMaxWidth().clip(PersonaTheme.shapes.controlShape)
                        .background(PersonaTheme.palette.sunken).horizontalScroll(rememberScrollState()).padding(Tokens.Space.m))
            }
        } else JCodeBlock(preview)
        Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            if (preview.length < text.length) JTextButton("展开全部", { expanded = true })
            ReaderEntry(text, language.ifBlank { "全文" })
        }
    }
}

internal data class ChatImage(val session: SessionKey, val item: Item)

internal fun chatImageLoader(context: android.content.Context, repository: ChatRepository): ImageLoader =
    ImageLoader.Builder(context).components {
        add(Keyer<ChatImage> { data, _ -> "${(repository as? ChatLocalStorage)?.localStorage?.value?.scope}/${data.session.path}/${data.item.id}" })
        add(object : Fetcher.Factory<ChatImage> {
            override fun create(data: ChatImage, options: Options, imageLoader: ImageLoader): Fetcher = object : Fetcher {
                override suspend fun fetch() = withContext(Dispatchers.IO) {
                    val file = checkNotNull(repository as? ChatLocalStorage) { "Scoped image storage is unavailable" }
                        .imageFile(data.session, data.item,
                            (options.size.width as? coil3.size.Dimension.Pixels)?.px ?: context.resources.displayMetrics.widthPixels,
                            (options.size.height as? coil3.size.Dimension.Pixels)?.px ?: context.resources.displayMetrics.heightPixels)
                    SourceFetchResult(ImageSource(file.toOkioPath(), FileSystem.SYSTEM), "image/png",
                        DataSource.DISK)
                }
            }
        })
    }.build()

/**
 * A picture of the conversation (R5): it keeps its shape while it loads, at most [ChatPictureMaxHeight] tall; a tap opens
 * the image viewer on all the conversation's pictures, a failed one retries.
 */
@Composable
internal fun ChatPicture(session: SessionKey, item: Item, loader: ImageLoader, alignEnd: Boolean = false) {
    val image = item.image ?: return
    val actions = LocalChatRowActions.current
    val ratio = if ((image.width ?: 0) > 0 && (image.height ?: 0) > 0) image.width!!.toFloat() / image.height!! else null
    var failed by remember(item.id) { mutableStateOf(false) }
    var attempt by remember(item.id) { mutableIntStateOf(0) }
    ChatPictureFrame(ratio, failed, onClick = actions?.let { rowActions -> { rowActions.openPicture(item) } },
        onRetry = { failed = false; attempt++ }, description = "对话图片", alignEnd = alignEnd) {
        key(attempt) {
            AsyncImage(ChatImage(session, item), "对话图片", imageLoader = loader,
                modifier = Modifier.fillMaxSize(), onError = { failed = true }, onSuccess = { failed = false })
        }
    }
}

/** [userImages] renders neighboring user-image rows in the same bubble; hide those rows with [imagesInUserBubble].
 * [workRunning] comes from the parent work group's live flag; [lastWorkStep] ends its timeline connector.
 */
@Composable
internal fun ChatLineContent(
    line: ChatLine, session: SessionKey, loader: ImageLoader, recordedDuration: Long?,
    onToggle: (String) -> Unit, onFold: (Item) -> Unit, folding: Set<String>, foldErrors: Map<String, String>,
    lastUserId: String? = null, onLastLine: (Int) -> Unit = {},
    onFullText: (Item) -> Unit,
    style: ChatRenderStyle = ChatRenderStyle.Agent,
    textUpdates: Map<String, ChatTextUpdate> = emptyMap(),
    userImages: List<Item> = emptyList(),
    imagesInUserBubble: Boolean = false,
    workRunning: Boolean = false,
    lastWorkStep: Boolean = false,
) {
    when (line) {
        is ChatLine.Row -> when (line.row.type) {
            "user" -> line.row.item?.item?.let {
                MergedUserBubble(listOf(it), textUpdates, onLastLine.takeIf { line.row.id == lastUserId },
                    images = userPictures(userImages, session, loader))
            }
            "reply" -> line.row.item?.item?.let { MessageContent(it, false, textUpdates[it.id]) }
            "thinking" -> if (!style.hideThoughts) ThoughtContent(line.row.item?.text.orEmpty(), line.id, style.collapseThoughts)
            "image" -> line.row.item?.item?.let { ChatPicture(session, it, loader) }
            "recap" -> line.row.item?.item?.let { RecapLine(listOf(it)) }
            else -> ChatSystemLine(line.row.item?.text.orEmpty(), line.row.item?.task?.failed == true)
        }
        is ChatLine.Recaps -> RecapLine(line.items.map { it.item })
        is ChatLine.Extra -> AssistantExtraLine(line.payload, LocalAssistantTaskHost.current)
        is ChatLine.ApiError -> ApiErrorLine(line)
        is ChatLine.UserImages -> if (!imagesInUserBubble) UserBubbleFrame {
            line.images.forEach { ChatPicture(session, it.item, loader, alignEnd = true) }
        }
        is ChatLine.MergedUsers -> MergedUserBubble(line.users.mapNotNull { it.item?.item }, textUpdates,
            onLastLine.takeIf { line.users.any { it.id == lastUserId } },
            images = userPictures(userImages, session, loader))
        is ChatLine.SplitHeader -> JTextButton("合并 ${line.count} 条输入", { onToggle(line.groupId) })
        is ChatLine.Work -> {
            // The running line counts its time up every second (R1); a finished one is still.
            val now by rememberNow(1_000, enabled = line.live)
            WorkGroup(workSummary(line, recordedDuration, now.takeIf { line.live }), line.open,
                { onToggle(line.id) }, running = line.live) {}
        }
        is ChatLine.Step -> WorkStep(line.step, session, loader, onFold, folding, foldErrors, onFullText, style, textUpdates, workRunning, lastWorkStep)
        is ChatLine.End -> TimelineMark("收起 ${line.count} 步",
            Modifier.heightIn(min = 44.dp).clickable(role = Role.Button) { onToggle(line.groupId) })
        is ChatLine.Wakes -> ChatSystemLine("后台唤醒 ${line.turn.wakes.size} 次")
    }
}

@Composable
private fun WorkStep(step: WorkItem, session: SessionKey, loader: ImageLoader, onFold: (Item) -> Unit,
    folding: Set<String>, errors: Map<String, String>, onFullText: (Item) -> Unit,
    style: ChatRenderStyle, textUpdates: Map<String, ChatTextUpdate>, running: Boolean, last: Boolean) {
    when (step) {
        is WorkItem.Folded -> {
            val item = step.item.item
            val loading = item.id in folding
            WorkGroup(
                summary = if (loading) "正在载入 ${step.stepCount()} 步…"
                    else if (item.id in errors) "载入失败，点此重试" else "做了 ${step.stepCount()} 步",
                expanded = false, onToggle = { if (!loading) onFold(item) }, running = loading,
            ) {}

        }
        is WorkItem.Tool -> {
            val call = step.call
            var open by remember(call.id) { mutableStateOf(false) }
            val diff = remember(call.use.item) { toolDiff(call.use.item) }
            val todos = remember(call.use.item) { todoEntries(call.use.item) }
            val searchItem = LocalChatSearchItem.current
            LaunchedEffect(searchItem) {
                if (searchItem != null && (searchItem == call.use.id || searchItem == call.result?.id)) {
                    open = true
                }
            }
            val presentation = toolPresentation(call.use.item)
            Column(Modifier.fillMaxWidth()) {
                TimelineStep(toolCategory(call.name), presentation.title, verb = presentation.verb,
                    detail = listOfNotNull(presentation.detail, if (call.running) (if (running) "正在执行" else "无结果记录") else null).joinToString("，").ifBlank { null },
                    state = when {
                        call.failed -> WorkState.Failed
                        running && call.running -> WorkState.Running
                        call.running -> WorkState.Waiting
                        else -> WorkState.Done
                    },
                    last = last, mono = presentation.mono, added = diff?.added ?: 0, removed = diff?.removed ?: 0,
                    onClick = { open = !open })
                if (todos != null) TodoList(todos, call.failed)
                if (call.failed && !open) call.result?.text?.takeIf { it.isNotBlank() }?.let {
                    Text(it, maxLines = 3, overflow = TextOverflow.Ellipsis, color = PersonaTheme.palette.danger.text,
                        style = PersonaTheme.type.caption)
                }
                if (open) Column(Modifier.fillMaxWidth().padding(start = Tokens.Space.m),
                    verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    if (diff != null) {
                        toolPath(call.use.item)?.let { DiffHeader(it, diff.added, diff.removed) }
                        DiffView(diff, call.id)
                    } else if (todos == null) ToolInputDetail(call.use.item)
                    // A truncated output opens in full from 原始输出 below. An edit's own result only says it worked.
                    call.result?.item?.let { result -> if (todos == null && (diff == null || call.failed)) CodeText(result.text.orEmpty(), "输出") }
                    call.images.forEach { ChatPicture(session, it.item, loader) }
                    ToolReaderActions(call.use.item.toolInput?.toString(), call.result?.text,
                        call.result?.item?.takeIf { it.fullSize != null }?.let { result -> { onFullText(result) } })
                }
            }
        }

        is WorkItem.Narration -> MessageContent(step.item.item, false, textUpdates[step.item.id])
        is WorkItem.Thought -> if (step.item.kind == "thinking") {
            if (!style.hideThoughts) ThoughtContent(textUpdates[step.item.id]?.text ?: step.item.text.orEmpty(), step.item.id, style.collapseThoughts, last)
        }
        else ChatSystemLine(step.item.task?.detail ?: step.item.text.orEmpty(), step.item.task?.failed == true)
    }
}

/** The pictures shown inside a user bubble, or null when there are none (a message of only contexts has no bubble). */
private fun userPictures(images: List<Item>, session: SessionKey, loader: ImageLoader): (@Composable () -> Unit)? =
    if (images.isEmpty()) null else ({ images.forEach { image -> ChatPicture(session, image, loader, alignEnd = true) } })

/** A message not in the transcript yet (the outbox): no long-press menu, its own status line has the actions. */
@Composable
internal fun UserBubble(text: String) {
    MergedUserBubble(listOf(Item("pending", text = text)), menu = false)
}

@Composable
internal fun ChatSystemLine(text: String, failed: Boolean = false) {
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.Top) {
        JIcon(if (failed) PhIcons.WarningCircle else PhIcons.Info, null, size = 16.dp,
            tint = if (failed) PersonaTheme.palette.danger.text else PersonaTheme.palette.ink2)
        ChatLinkText(text, style = PersonaTheme.type.caption.copy(color = if (failed) PersonaTheme.palette.danger.text else PersonaTheme.palette.ink2))
    }
}
