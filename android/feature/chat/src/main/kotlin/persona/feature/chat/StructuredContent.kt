package persona.feature.chat

import androidx.compose.foundation.background
import androidx.compose.animation.core.*
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.dp
import persona.core.contracts.Item
import persona.core.contracts.api.ChannelSources
import persona.core.designsystem.*
import persona.core.designsystem.WorkStep as TimelineStep
import persona.core.designsystem.icons.PhIcons


/**
 * An agent's words (a reply, or narration inside a work line): Markdown, its file paths as chips, a long press for the
 * message menu. A text the hub cut (`full_size`) shows the part the page carries and 「看全文（N KB）」, which loads the
 * rest in place (R3).
 */
@Composable
internal fun MessageContent(item: Item, user: Boolean, update: ChatTextUpdate? = null) {
    var whole by remember(item.id) { mutableStateOf<String?>(null) }
    val cut = item.fullSize != null && whole == null && update?.streaming != true
    val text = whole ?: update?.text ?: item.text.orEmpty().let { if (item.fullSize != null) cutText(it) else it }
    val model = remember(text, item.from) { messageModel(text, item.from) }
    val uri = LocalUriHandler.current
    Column(Modifier.fillMaxWidth().messageMenu(MessageTarget(item.id, model.plain, user, model.editText()))) {
        MessageContextGroup(model.contexts, "${item.id}/contexts", uri::openUri, alignEnd = user)
        MessageParts(model.parts, user, item.id)
        if (cut) FullTextButton(item) { whole = it } else ReaderEntry(text)
        if (update?.streaming == true) StreamingCursor()
        FileChips(model.files, alignEnd = user)
    }
}

@Composable
private fun StreamingCursor() {
    val transition = rememberInfiniteTransition(label = "streaming-cursor")
    val alpha by transition.animateFloat(1f, 0f,
        infiniteRepeatable(tween(500), RepeatMode.Reverse), label = "cursor-alpha")
    Text("▍", Modifier.alpha(alpha), color = PersonaTheme.palette.accent, style = PersonaTheme.type.body)
}

@Composable
private fun MessageParts(parts: List<MessagePart>, user: Boolean, id: String, onLastLine: ((Int) -> Unit)? = null) {
    // Bubble padding below the last text line, so the reported extent reaches the bottom of the row.
    val bottomPadding = with(LocalDensity.current) { Tokens.Space.m.roundToPx() }
    parts.forEachIndexed { index, part ->
        when (part) {
            is MessagePart.Text -> if (user) Text(messageLinkAnnotations(part.text), style = PersonaTheme.type.body, color = PersonaTheme.palette.ink,
                onTextLayout = { layout ->
                    if (index == parts.lastIndex && layout.lineCount > 0)
                        onLastLine?.invoke((layout.size.height - layout.getLineTop(layout.lineCount - 1)).toInt() + bottomPadding)
                })
                else ChatMarkdown(part.text, "$id/$index", selectable = false)
            is MessagePart.Source -> JGroupItem(GroupPosition.Single) {
                Column(Modifier.fillMaxWidth().padding(10.dp)) {
                    Text("来自 ${part.name}", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                    // Markdown like an answer: agents write Markdown, and the hub turns a teammate's JSON notes into it.
                    if (part.text.isNotBlank()) ChatMarkdown(keepLineBreaks(part.text), "$id/$index", selectable = false)
                }
            }
        }
    }
}

/**
 * Your message (several queued ones read as one): the photos it carries above its text (`<attached_images>`, read from
 * the session's machine, R3), its file paths as chips under the bubble, a long press for the message menu. Its
 * `<jarvis-context>` blocks fold into one row above the bubble; a message of nothing but blocks has no bubble (#876).
 */
@Composable
internal fun MergedUserBubble(items: List<Item>, updates: Map<String, ChatTextUpdate> = emptyMap(),
    onLastLine: ((Int) -> Unit)? = null, images: (@Composable () -> Unit)? = null, menu: Boolean = true) {
    val models = remember(items, updates) { items.map { messageModel(updates[it.id]?.text ?: it.text.orEmpty(), it.from) } }
    val parts = remember(models) { models.flatMap { it.parts } }
    val contexts = remember(models) { models.flatMap { it.contexts } }
    val photos = remember(models) { models.flatMap { it.images }.distinct() }
    val files = remember(models) { models.flatMap { it.files }.distinct().take(6) }
    val target = remember(models, items) {
        MessageTarget(items.firstOrNull()?.id.orEmpty(), models.joinToString("\n\n") { it.plain }.trim(), true,
            models.joinToString("\n\n") { it.editText() }.trim())
    }
    val timestamp = items.lastOrNull()?.ts
    val time = remember(timestamp) { timestamp?.let {
        runCatching { java.time.format.DateTimeFormatter.ofPattern("HH:mm")
            .withZone(java.time.ZoneId.systemDefault()).format(requireNotNull(persona.core.contracts.parseTime(it))) }.getOrNull()
    } }
    // A message that came in from a channel (WeChat, the Feishu bot…) says so under it; the app's own say nothing.
    val channels = LocalChannels.current
    val source = remember(items, channels) { items.firstNotNullOfOrNull { ChannelSources.of(it.clientMsgId, channels) } }
    val footer: (@Composable () -> Unit)? = if (time == null && files.isEmpty() && source == null) null else ({
        Column(horizontalAlignment = Alignment.End) {
            FileChips(files, alignEnd = true)
            if (source == null) time?.let { Text(it, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3) }
            else Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
                JChannelSource(source)
                time?.let { Text(it, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3) }
            }
        }
    })
    val uri = LocalUriHandler.current
    val streaming = items.any { updates[it.id]?.streaming == true }
    // Only blocks, no words, photos or cursor: the folded row alone, no empty bubble.
    val bubble = contexts.isEmpty() || parts.isNotEmpty() || photos.isNotEmpty() || images != null || streaming
    Column(Modifier.fillMaxWidth()) {
        MessageContextGroup(contexts, "${items.firstOrNull()?.id.orEmpty()}/contexts", uri::openUri)
        if (bubble) UserBubbleFrame(Modifier.messageMenu(target.takeIf { menu }), footer = footer) {
            images?.invoke()
            if (photos.isNotEmpty()) {
                AttachedPhotoRow(photos)
                if (parts.isNotEmpty()) Spacer(Modifier.height(Tokens.Space.s))
            }
            Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                MessageParts(parts, true, items.firstOrNull()?.id.orEmpty(), onLastLine)
                if (streaming) StreamingCursor()
            }
        }
    }
}

@Composable
internal fun ThoughtContent(text: String, id: String, collapsed: Boolean, last: Boolean = true) {
    var open by rememberSaveable(id) { mutableStateOf(!collapsed) }
    Column {
        TimelineStep(PhIcons.Lightbulb, "思考", verb = "思考", last = last, onClick = { open = !open })
        if (open) {
            Column(Modifier.padding(start = Tokens.Space.m)) {
                ChatMarkdown(text, id, PersonaTheme.type.callout, PersonaTheme.palette.ink2)
            }
            ReaderEntry(text, "思考")
        }
    }
}

internal fun toolCategory(name: String) = when (name) {
    "Read", "LS" -> PhIcons.Eye
    "Edit", "MultiEdit", "NotebookEdit" -> PhIcons.PencilSimple
    "Write" -> PhIcons.File
    "Bash" -> PhIcons.TerminalWindow
    "Grep", "Glob", "ToolSearch" -> PhIcons.MagnifyingGlass
    "WebFetch", "WebSearch" -> PhIcons.GlobeSimple
    "Task", "Agent" -> PhIcons.Robot
    "TodoWrite", "TaskCreate", "TaskUpdate", "TaskList" -> PhIcons.ListChecks
    "EnterPlanMode", "ExitPlanMode" -> PhIcons.Lightbulb
    else -> PhIcons.Wrench
}

@Composable
internal fun ToolCategoryIcon(name: String) {
    JIcon(toolCategory(name), null, size = 18.dp, tint = PersonaTheme.palette.ink2)
}

@Composable
internal fun DiffStats(diff: ToolDiff) {
    val palette = PersonaTheme.palette
    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Text("+${diff.added}", color = palette.diffAdd, style = PersonaTheme.type.caption, maxLines = 1)
        Text("−${diff.removed}", color = palette.diffDel, style = PersonaTheme.type.caption, maxLines = 1)
    }
}

@Composable
internal fun DiffView(diff: ToolDiff, id: String) {
    var expanded by rememberSaveable(id) { mutableStateOf(false) }
    val list = rememberLazyListState()
    val lines = remember(diff, expanded) { if (expanded) diff.lines else diff.lines.take(40) }
    val palette = PersonaTheme.palette
    val horizontal = rememberScrollState()
    val measurer = rememberTextMeasurer()
    val style = PersonaTheme.type.code
    val density = LocalDensity.current
    val longest = remember(lines) { lines.maxByOrNull { it.text.length }?.text.orEmpty().replace("\t", "    ") }
    val measured = remember(longest, style, density) { measurer.measure(longest, style, softWrap = false).size.width }
    Column(Modifier.clip(PersonaTheme.shapes.controlShape).background(palette.sunken)) {
        Column {
            DiffNavigation(diff, list) { expanded = true }
            BoxWithConstraints(Modifier.fillMaxWidth()) {
                val codeWidth = maxOf(maxWidth - 84.dp, with(density) { measured.toDp() } + Tokens.Space.s)
                LazyColumn(Modifier.fillMaxWidth().heightIn(max = 360.dp), state = list) {
                    items(lines) { line ->
                        Row(Modifier.fillMaxWidth().background(when (line.sign) {
                            '+' -> palette.diffAdd.copy(alpha = 0.12f)
                            '-' -> palette.diffDel.copy(alpha = 0.12f)
                            else -> palette.sunken
                        }), verticalAlignment = Alignment.CenterVertically) {
                            Text("${line.old ?: ""}  ${line.new ?: ""}", Modifier.width(64.dp),
                                style = style, color = PersonaTheme.palette.ink2, maxLines = 1)
                            // A hunk header or a note of a unified diff ('@') reads quietly, with no sign.
                            Text(if (line.sign == '@') " " else line.sign.toString(), Modifier.width(Tokens.Space.xl), style = style,
                                color = when (line.sign) { '+' -> palette.diffAdd; '-' -> palette.diffDel; else -> PersonaTheme.palette.ink2 })
                            Box(Modifier.weight(1f).horizontalScroll(horizontal)) {
                                Text(line.text.replace("\t", "    "), Modifier.width(codeWidth), style = style, softWrap = false,
                                    color = if (line.sign == '@') palette.ink3 else palette.ink)
                            }
                        }
                    }
                }
            }
            if (diff.lines.size > 40) JTextButton(if (expanded) "收起" else "展开全部（${diff.lines.size} 行）", { expanded = !expanded })
        }
    }
}

@Composable
internal fun TodoList(todos: List<TodoEntry>, failed: Boolean) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text("${todos.count { it.status == "completed" }}/${todos.size} 项待办已完成" + if (failed) "，更新失败" else "",
            style = PersonaTheme.type.caption, color = if (failed) PersonaTheme.palette.danger.text else PersonaTheme.palette.ink2)
        todos.forEach { todo ->
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                val label = when (todo.status) {
                    "completed" -> "已完成"
                    "in_progress" -> "进行中"
                    "pending" -> "待办"
                    else -> "状态未知"
                }
                JCheckbox(todo.status == "completed", onCheckedChange = null)
                Column(Modifier.weight(1f)) {
                    Text(todo.content, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink)
                    Text(label, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                    if (todo.status == "in_progress" && !todo.activeForm.isNullOrBlank() && todo.activeForm != todo.content)
                        Text(todo.activeForm, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
                }
            }
        }
    }
}
