package jarvis.feature.chat

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.animation.core.*
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.core.contracts.Item
import jarvis.core.designsystem.*
import jarvis.core.designsystem.WorkStep as TimelineStep
import jarvis.core.designsystem.icons.PhIcons


@Composable
internal fun MessageContent(item: Item, user: Boolean, update: ChatTextUpdate? = null) {
    val text = update?.text ?: item.text.orEmpty()
    val parts = remember(text, item.from) { messageParts(text, item.from) }
    MessageParts(parts, user, item.id)
    ReaderEntry(text)
    if (update?.streaming == true) StreamingCursor()
}

@Composable
private fun StreamingCursor() {
    val transition = rememberInfiniteTransition(label = "streaming-cursor")
    val alpha by transition.animateFloat(1f, 0f,
        infiniteRepeatable(tween(500), RepeatMode.Reverse), label = "cursor-alpha")
    Text("▍", Modifier.alpha(alpha), color = JarvisTheme.palette.accent, style = JarvisTheme.type.body)
}

@Composable
private fun MessageParts(parts: List<MessagePart>, user: Boolean, id: String, onLastLine: ((Int) -> Unit)? = null) {
    // Bubble padding below the last text line, so the reported extent reaches the bottom of the row.
    val bottomPadding = with(LocalDensity.current) { Tokens.Space.m.roundToPx() }
    parts.forEachIndexed { index, part ->
        when (part) {
            is MessagePart.Text -> if (user) Text(chatLinkAnnotations(part.text), style = JarvisTheme.type.body, color = JarvisTheme.palette.ink,
                onTextLayout = { layout ->
                    if (index == parts.lastIndex && layout.lineCount > 0)
                        onLastLine?.invoke((layout.size.height - layout.getLineTop(layout.lineCount - 1)).toInt() + bottomPadding)
                })
                else ChatMarkdown(part.text, "$id/$index")
            is MessagePart.Reference -> ReferenceCard(part)
            is MessagePart.Source -> JGroupItem(GroupPosition.Single) {
                Column(Modifier.fillMaxWidth().padding(10.dp)) {
                    Text("来自 ${part.name}", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
                    if (part.text.isNotBlank()) ChatLinkText(part.text, style = JarvisTheme.type.body.copy(color = JarvisTheme.palette.ink))
                }
            }
        }
    }
}

@Composable
private fun ReferenceCard(reference: MessagePart.Reference) {
    val uri = LocalUriHandler.current
    val url = reference.url
    val canOpen = url?.let { runCatching { java.net.URI(it).scheme?.lowercase() in setOf("http", "https") }.getOrDefault(false) } == true
    JGroupItem(GroupPosition.Single, Modifier.clickable(enabled = canOpen) { if (canOpen) uri.openUri(url!!) }) {
        Column(Modifier.fillMaxWidth().padding(10.dp), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            Text(if (reference.kind == "edit") "待修改内容" else "关联上下文", style = JarvisTheme.type.micro,
                color = JarvisTheme.palette.ink2)
            Text(reference.title, style = JarvisTheme.type.callout, color = JarvisTheme.palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (reference.quote.isNotBlank()) Text(reference.quote, maxLines = 4, overflow = TextOverflow.Ellipsis,
                style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
            if (canOpen) JTextButton("查看原文", { uri.openUri(url!!) }, icon = PhIcons.ArrowSquareOut)
            else if (url != null) Text(url, color = JarvisTheme.palette.ink2,
                style = JarvisTheme.type.micro, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
internal fun MergedUserBubble(items: List<Item>, updates: Map<String, ChatTextUpdate> = emptyMap(),
    onLastLine: ((Int) -> Unit)? = null, images: (@Composable () -> Unit)? = null) {
    val parts = remember(items, updates) { items.flatMap { messageParts(updates[it.id]?.text ?: it.text.orEmpty(), it.from) } }
    val timestamp = items.lastOrNull()?.ts
    val time = remember(timestamp) { timestamp?.let {
        runCatching { java.time.format.DateTimeFormatter.ofPattern("HH:mm")
            .withZone(java.time.ZoneId.systemDefault()).format(java.time.Instant.parse(it)) }.getOrNull()
    } }
    UserBubbleFrame(footer = time?.let { { Text(it, style = JarvisTheme.type.micro, color = JarvisTheme.palette.ink3) } }) {
        images?.invoke()
        SelectionContainer {
            Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                MessageParts(parts, true, items.firstOrNull()?.id.orEmpty(), onLastLine)
                if (items.any { updates[it.id]?.streaming == true }) StreamingCursor()
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
                ChatMarkdown(text, id, JarvisTheme.type.callout, JarvisTheme.palette.ink2)
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
    JIcon(toolCategory(name), null, size = 18.dp, tint = JarvisTheme.palette.ink2)
}

@Composable
internal fun DiffStats(diff: ToolDiff) {
    val palette = JarvisTheme.palette
    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Text("+${diff.added}", color = palette.diffAdd, style = JarvisTheme.type.caption, maxLines = 1)
        Text("−${diff.removed}", color = palette.diffDel, style = JarvisTheme.type.caption, maxLines = 1)
    }
}

@Composable
internal fun DiffView(diff: ToolDiff, id: String) {
    var expanded by rememberSaveable(id) { mutableStateOf(false) }
    val list = rememberLazyListState()
    val lines = remember(diff, expanded) { if (expanded) diff.lines else diff.lines.take(40) }
    val palette = JarvisTheme.palette
    val horizontal = rememberScrollState()
    val measurer = rememberTextMeasurer()
    val style = JarvisTheme.type.code
    val density = LocalDensity.current
    val longest = remember(lines) { lines.maxByOrNull { it.text.length }?.text.orEmpty().replace("\t", "    ") }
    val measured = remember(longest, style, density) { measurer.measure(longest, style, softWrap = false).size.width }
    Column(Modifier.clip(JarvisTheme.shapes.controlShape).background(palette.sunken)) {
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
                                style = style, color = JarvisTheme.palette.ink2, maxLines = 1)
                            Text(line.sign.toString(), Modifier.width(Tokens.Space.xl), style = style,
                                color = when (line.sign) { '+' -> palette.diffAdd; '-' -> palette.diffDel; else -> JarvisTheme.palette.ink2 })
                            Box(Modifier.weight(1f).horizontalScroll(horizontal)) {
                                Text(line.text.replace("\t", "    "), Modifier.width(codeWidth), style = style, softWrap = false)
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
            style = JarvisTheme.type.caption, color = if (failed) JarvisTheme.palette.danger.text else JarvisTheme.palette.ink2)
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
                    Text(todo.content, style = JarvisTheme.type.callout, color = JarvisTheme.palette.ink)
                    Text(label, style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
                    if (todo.status == "in_progress" && !todo.activeForm.isNullOrBlank() && todo.activeForm != todo.content)
                        Text(todo.activeForm, style = JarvisTheme.type.callout, color = JarvisTheme.palette.ink2)
                }
            }
        }
    }
}
