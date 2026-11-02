package jarvis.android.ui.files

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import jarvis.android.data.Hub
import jarvis.android.ui.CardSlice
import jarvis.android.ui.JTheme
import jarvis.android.ui.tailOf
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChipLook
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.DOT_SEP
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.IconAction
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.media.MediaShare
import kotlinx.coroutines.launch
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Collections

/** `git show` answers by machine + repository + hash: a commit's files open their part of the diff without a refetch. */
internal object GitShowCache {
    private val map = Collections.synchronizedMap(object : LinkedHashMap<String, GitShow>(8, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, GitShow>?) = size > 8
    })

    suspend fun get(hub: Hub, machineId: String, cwd: String, hash: String): GitShow {
        val k = "$machineId\u0000$cwd\u0000$hash"
        map[k]?.let { return it }
        return hub.api.gitShow(machineId, cwd, hash).also { map[k] = it }
    }
}

/** Where a Git file row's words start (after its letter). */
private val GitTextColumn: Dp = JSpace.l + JSize.iconXS + JSpace.m

/**
 * The Git half (iOS `GitPane`): branch and sync in a card, the working tree's changes and the history as grouped
 * cards (all read-only). [insets]: the gesture bar's, kept clear at the end of the list.
 */
@Composable
internal fun GitPane(
    hub: Hub,
    machineId: String,
    cwd: String,
    status: GitStatus?,
    error: String?,
    insets: PaddingValues,
    onRefresh: () -> Unit,
    onFile: (GitFile) -> Unit,
    onCommit: (GitCommit) -> Unit,
) {
    val older = remember(status) { mutableStateListOf<GitCommit>() }
    var noMore by remember(status) { mutableStateOf(false) }
    var loadingMore by remember { mutableStateOf(false) }
    var moreError by remember(status) { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val diff = JTheme.diff
    val c = JTheme.colors
    when {
        status != null && status.isRepo -> LazyColumn(
            Modifier.fillMaxSize(), contentPadding = PaddingValues(top = JSpace.xs, bottom = JSpace.xxl + insets.calculateBottomPadding()),
        ) {
            item(key = "head") { GitHeader(status, onRefresh) }
            item(key = "changes") {
                SectionHeader("改动", count = status.files.size.takeIf { it > 0 }) {
                    if (status.files.isNotEmpty()) {
                        Text("+${status.files.sumOf { it.added }}", style = JTheme.type.sectionHeader, color = diff.add, maxLines = 1)
                        Text("−${status.files.sumOf { it.deleted }}", style = JTheme.type.sectionHeader, color = diff.del, maxLines = 1)
                    }
                }
            }
            if (status.files.isEmpty()) item(key = "clean") {
                GroupCard {
                    Row(
                        Modifier.fillMaxWidth().heightIn(min = JSize.box50).padding(horizontal = JSpace.l),
                        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
                    ) {
                        Icon(JIcons.success, null, Modifier.size(JSize.iconXS), tint = c.positive)
                        Text("工作区是干净的", style = JTheme.type.secondary, color = c.secondaryText, maxLines = 1)
                    }
                }
            }
            itemsIndexed(status.files, key = { _, f -> "f:" + f.path }) { i, f ->
                CardSlice(first = i == 0, last = i == status.files.lastIndex, dividerStart = GitTextColumn) {
                    GitFileRow(f, Modifier.clickable { onFile(f) })
                }
            }
            item(key = "commits") { SectionHeader("提交") }
            val commits = status.commits + older
            // The status carries the latest 30: page the rest from git/log.
            val pages = !noMore && status.commits.size >= 30
            itemsIndexed(commits, key = { i, cm -> "c:" + cm.hash + ":" + i }) { i, cm ->
                CardSlice(first = i == 0, last = i == commits.lastIndex && !pages, dividerStart = JSpace.l) {
                    GitCommitRow(cm, cm.hash == status.head?.hash, Modifier.clickable { onCommit(cm) })
                }
            }
            if (pages) item(key = "more") {
                CardSlice(first = commits.isEmpty(), last = true, dividerStart = JSpace.l) {
                    Row(
                        Modifier.fillMaxWidth().heightIn(min = JSize.box50).clickable(enabled = !loadingMore) {
                            loadingMore = true
                            moreError = null
                            scope.launch {
                                runCatching { hub.api.gitLog(machineId, cwd, commits.size) }
                                    .onSuccess { got -> older += got; noMore = got.size < 50 }
                                    .onFailure { moreError = it.shown() }
                                loadingMore = false
                            }
                        }.padding(horizontal = JSpace.l),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(JSpace.s, Alignment.CenterHorizontally),
                    ) {
                        if (loadingMore) CircularProgressIndicator(Modifier.size(JSize.markS), strokeWidth = JSize.lineMedium, color = c.accent)
                        Text(if (moreError != null) "加载失败，重试" else "加载更早的提交", style = JTheme.type.body, color = c.accent, maxLines = 1)
                    }
                }
            }
        }
        error != null -> ErrorNotice("读不到 Git 状态", error, onRetry = onRefresh)
        status?.isRepo == false -> Notice(JIcons.branch, "不是 Git 仓库", cwd)
        else -> Loading()
    }
}

/** The branch and how it stands against its upstream (iOS `GitPane.header`): ↑ ahead, ↓ behind, the stash, the root. */
@Composable
private fun GitHeader(s: GitStatus, onRefresh: () -> Unit) {
    val c = JTheme.colors
    GroupCard(Modifier.fillMaxWidth().padding(top = JSpace.s)) {
        Column(Modifier.fillMaxWidth().padding(start = JSpace.l, end = JSpace.xs, top = JSpace.xs, bottom = JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                Icon(JIcons.branch, null, Modifier.size(JSize.markL), tint = c.accent)
                Text(
                    s.branch ?: "分离的 HEAD", style = JTheme.type.heading, color = c.label, maxLines = 1,
                    overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
                )
                if (s.ahead > 0) Chip("${s.ahead}", Modifier.clearAndSetSemantics { contentDescription = "领先 ${s.ahead}" }, icon = JIcons.ahead)
                if (s.behind > 0) Chip(
                    "${s.behind}", Modifier.clearAndSetSemantics { contentDescription = "落后 ${s.behind}" },
                    icon = JIcons.behind, look = ChipLook.Tint(c.cautionText),
                )
                IconAction(JIcons.retry, "刷新", onRefresh, tint = c.accent, size = JSize.iconXS)
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                Icon(if (s.upstream != null) JIcons.upstream else JIcons.noUpstream, null, Modifier.size(JSize.markS), tint = c.secondaryText)
                val words = buildList {
                    add(s.upstream ?: "没有上游分支")
                    if (s.upstream != null && s.ahead == 0 && s.behind == 0) add("已同步")
                    if (s.stashes > 0) add("暂存 ${s.stashes}")
                }
                Text(words.joinToString(DOT_SEP), style = JTheme.type.caption, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            s.root?.let {
                Text(FilesPaths.keepEnd(it), style = JTheme.type.labelMono, color = c.tertiaryText, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(top = JSpace.xxs))
            }
        }
    }
}

/** A changed file (iOS `GitFileRow`): its letter, the name, staged or not and its folder, +N −N. */
@Composable
internal fun GitFileRow(file: GitFile, modifier: Modifier = Modifier, showsStage: Boolean = true) {
    val c = JTheme.colors
    val diff = JTheme.diff
    Row(
        modifier.fillMaxWidth().heightIn(min = JSize.box50).padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        GitBadge(file.letter)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(file.name, style = JTheme.type.secondaryMedium, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val stage = if (showsStage && file.isStaged) (if (file.hasUnstaged) "部分已暂存" else "已暂存") else null
            val where = buildList {
                file.origPath?.let { add("← " + it.substringAfterLast('/')) }
                if (file.folder.isNotEmpty()) add(file.folder)
            }
            if (stage != null || where.isNotEmpty()) {
                Text(
                    buildAnnotatedString {
                        // The stage in the label's color, as iOS: it is what tells two halves of a change apart.
                        stage?.let { withStyle(SpanStyle(color = c.label)) { append(it) }; if (where.isNotEmpty()) append("  ") }
                        append(FilesPaths.keepEnd(where.joinToString("  "), 40))
                    },
                    style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis,
                )
            }
        }
        if (file.binary) Text("二进制", style = JTheme.type.label, color = c.secondaryText, maxLines = 1)
        else if (file.added + file.deleted > 0) Row(horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            Text("+${file.added}", style = JTheme.type.labelMedium, color = diff.add, maxLines = 1)
            Text("−${file.deleted}", style = JTheme.type.labelMedium, color = diff.del, maxLines = 1)
        }
    }
}

/** A commit (iOS `GitCommitRow`): the subject, its short hash, who and when, and its branches and tags as chips. */
@Composable
private fun GitCommitRow(commit: GitCommit, isHead: Boolean, modifier: Modifier = Modifier) {
    val c = JTheme.colors
    val now = remember { java.time.Instant.now() }
    Column(modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
        Text(commit.subject, style = JTheme.type.secondaryMedium, color = c.label, maxLines = 2, overflow = TextOverflow.Ellipsis)
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            Text(commit.short, style = JTheme.type.code, color = c.accent, maxLines = 1)
            Text(commit.author, style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
            commit.time?.let { Text("· " + FilesPaths.relative(it, now), style = JTheme.type.label, color = c.secondaryText, maxLines = 1) }
            if (isHead) Text("· HEAD", style = JTheme.type.label, color = c.secondaryText, maxLines = 1)
        }
        val refs = commit.refNames
        if (refs.isNotEmpty()) Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            // A local branch outlined, a remote one (origin/…) plain: refs are facts, not buttons.
            refs.forEach { Chip(it, look = if ('/' in it) ChipLook.Neutral else ChipLook.Outline) }
        }
    }
}

/**
 * One changed file's diff; switches between the unstaged and staged halves when it has both (iOS `GitDiffSheet`).
 * [initial]: the first half's diff already fetched (screenshots).
 */
@Composable
internal fun GitDiffPage(hub: Hub, machineId: String, cwd: String, file: GitFile, initiallyStaged: Boolean, onBack: () -> Unit, initial: GitDiff? = null) {
    var staged by rememberSaveable { mutableStateOf(initiallyStaged) }
    var diff by remember { mutableStateOf(initial) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    LaunchedEffect(staged, reload) {
        if (diff != null && diff === initial && staged == initiallyStaged) return@LaunchedEffect
        diff = null
        error = null
        runCatching { hub.api.gitDiff(machineId, cwd, file.path, staged) }.onSuccess { diff = it }.onFailure { error = it.shown() }
    }
    DiffScaffold(file.name, tailOf(file.folder, 2).ifEmpty { "仓库根目录" }, diff?.diff, file.path, onBack) { insets ->
        Column(Modifier.fillMaxSize()) {
            if (file.isStaged && file.hasUnstaged) Segments(listOf("未暂存", "已暂存"), if (staged) 1 else 0, { staged = it == 1 })
            val d = diff
            when {
                d != null -> DiffBody(d.diff, d.truncated, insets, Modifier.weight(1f))
                error != null -> ErrorNotice("读不到改动", error!!, onRetry = { reload++ })
                else -> Loading()
            }
        }
    }
}

/** One file of a commit: its part of the commit's diff. */
@Composable
internal fun CommitFilePage(hub: Hub, machineId: String, cwd: String, hash: String, file: GitFile, onBack: () -> Unit) {
    var show by remember { mutableStateOf<GitShow?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    LaunchedEffect(hash, reload) {
        error = null
        runCatching { GitShowCache.get(hub, machineId, cwd, hash) }.onSuccess { show = it }.onFailure { error = it.shown() }
    }
    val part = remember(show) { show?.let { DiffText.split(it.diff)[file.path] } }
    DiffScaffold(file.name, tailOf(file.folder, 2).ifEmpty { "仓库根目录" }, part, file.path, onBack) { insets ->
        val s = show
        when {
            s != null -> DiffBody(part.orEmpty(), s.truncated && part == null, insets, Modifier.fillMaxSize())
            error != null -> ErrorNotice("读不到改动", error!!, onRetry = { reload++ })
            else -> Loading()
        }
    }
}

@Composable
private fun DiffScaffold(title: String, subtitle: String, diff: String?, path: String?, onBack: () -> Unit, content: @Composable (PaddingValues) -> Unit) {
    val ctx = LocalContext.current
    FilesPage(
        title = title,
        subtitle = subtitle,
        onBack = onBack,
        actions = {
            var open by remember { mutableStateOf(false) }
            Box {
                CircleButton(JIcons.more, "更多", { open = true })
                DropdownMenu(open, onDismissRequest = { open = false }) {
                    if (!diff.isNullOrEmpty()) DropdownMenuItem(text = { Text("拷贝 diff", maxLines = 1) }, onClick = {
                        MediaShare.copyText(ctx, diff, "diff"); MediaShare.toast(ctx, "已拷贝"); open = false
                    }, leadingIcon = { Icon(JIcons.copy, null) })
                    if (path != null) DropdownMenuItem(text = { Text("拷贝路径", maxLines = 1) }, onClick = {
                        MediaShare.copyText(ctx, path, "路径"); MediaShare.toast(ctx, "已拷贝路径"); open = false
                    }, leadingIcon = { Icon(JIcons.copy, null) })
                }
            }
        },
    ) { insets -> Box(Modifier.fillMaxSize()) { content(insets) } }
}

/** A unified diff edge to edge on the code background (iOS `DiffText`), or why there is none. */
@Composable
private fun DiffBody(diff: String, truncated: Boolean, insets: PaddingValues, modifier: Modifier) {
    val parsed = remember(diff) { DiffText.parse(diff) }
    if (parsed.isEmpty()) {
        Notice(
            JIcons.document,
            if (truncated) "改动太大，没有带回来" else "没有文本改动",
            if (truncated) "整个提交的 diff 超过 512 KB，这个文件在截断之后。" else "可能是二进制文件、只改了权限，或者改动已被撤销。",
            modifier = modifier,
        )
        return
    }
    val bottom = insets.calculateBottomPadding()
    Column(modifier.background(JTheme.colors.codeBackground)) {
        MonoLines(
            diffLines(parsed), Modifier.weight(1f).fillMaxWidth(), fontSize = 11,
            // The last lines scroll clear of the gesture bar, unless the note below already keeps them there.
            contentPadding = PaddingValues(top = JSpace.sm, bottom = if (truncated) JSpace.sm else JSpace.sm + bottom),
        )
        if (truncated) {
            Text(
                "改动太大，只显示了前 512 KB", style = JTheme.type.caption, color = JTheme.colors.secondaryText,
                modifier = Modifier.padding(horizontal = JSpace.page, vertical = JSpace.s).padding(bottom = bottom),
            )
        }
    }
}

/** One commit: message, author, the files it touched; each opens its part of the diff (iOS `GitCommitSheet`). [initial]: its `git show` (screenshots). */
@Composable
internal fun GitCommitPage(hub: Hub, machineId: String, cwd: String, commit: GitCommit, onBack: () -> Unit, onFile: (GitFile) -> Unit, initial: GitShow? = null) {
    val ctx = LocalContext.current
    var show by remember { mutableStateOf(initial) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    LaunchedEffect(commit.hash, reload) {
        error = null
        runCatching { GitShowCache.get(hub, machineId, cwd, commit.hash) }.onSuccess { show = it }.onFailure { error = it.shown() }
    }
    val c = JTheme.colors
    FilesPage(title = commit.short.ifEmpty { commit.hash.take(7) }, subtitle = "提交", onBack = onBack) { insets ->
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(top = JSpace.xs, bottom = JSpace.xxl + insets.calculateBottomPadding())) {
            item {
                GroupCard(Modifier.fillMaxWidth().padding(top = JSpace.s)) {
                    Column(Modifier.fillMaxWidth().padding(JSpace.l), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
                        Text(commit.subject, style = JTheme.type.heading, color = c.label)
                        (show?.commit?.body ?: commit.body)?.takeIf { it.isNotBlank() }?.let {
                            SelectionContainer { Text(it.trim(), style = JTheme.type.body, color = c.secondaryText) }
                        }
                        val date = commit.time?.atZone(ZoneId.systemDefault())?.format(DateTimeFormatter.ofPattern("yyyy年M月d日 HH:mm"))
                        Text(listOfNotNull(commit.author, date).joinToString(DOT_SEP), style = JTheme.type.caption, color = c.secondaryText)
                        Row(
                            Modifier.clip(RoundedCornerShape(JRadius.badge)).clickable(onClickLabel = "拷贝提交号") {
                                MediaShare.copyText(ctx, commit.hash, "提交"); MediaShare.toast(ctx, "已拷贝提交号")
                            }.padding(vertical = JSpace.xxs),
                            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
                        ) {
                            Icon(JIcons.id, null, Modifier.size(JSize.markS), tint = c.accent)
                            Text(commit.hash, style = JTheme.type.labelMono, color = c.accent, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                }
            }
            val s = show
            when {
                s != null -> {
                    item { SectionHeader("${s.files.size} 个文件") }
                    itemsIndexed(s.files) { i, f ->
                        CardSlice(first = i == 0, last = i == s.files.lastIndex, dividerStart = GitTextColumn) {
                            GitFileRow(f, Modifier.clickable { onFile(f) }, showsStage = false)
                        }
                    }
                }
                error != null -> item { ErrorNotice("读不到这个提交", error!!, onRetry = { reload++ }) }
                else -> item {
                    Box(Modifier.fillMaxWidth().padding(JSpace.xl), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator(Modifier.size(JSize.iconS), strokeWidth = JSize.rule, color = c.secondaryText)
                    }
                }
            }
        }
    }
}
