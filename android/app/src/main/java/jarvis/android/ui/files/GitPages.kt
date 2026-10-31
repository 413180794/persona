package jarvis.android.ui.files

import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowDownward
import androidx.compose.material.icons.filled.ArrowUpward
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Cloud
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.Hub
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

/** The Git half (iOS `GitPane`): branch and sync, the working tree's changes, the history (all read-only). */
@Composable
internal fun GitPane(
    hub: Hub,
    machineId: String,
    cwd: String,
    status: GitStatus?,
    error: String?,
    onRefresh: () -> Unit,
    onFile: (GitFile) -> Unit,
    onCommit: (GitCommit) -> Unit,
) {
    val older = remember(status) { mutableStateListOf<GitCommit>() }
    var noMore by remember(status) { mutableStateOf(false) }
    var loadingMore by remember { mutableStateOf(false) }
    var moreError by remember(status) { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val diff = diffColors()
    when {
        status != null && status.isRepo -> LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            item { GitHeader(status, onRefresh) }
            item {
                Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        if (status.files.isEmpty()) "改动" else "改动 ${status.files.size}",
                        style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.weight(1f), maxLines = 1,
                    )
                    if (status.files.isNotEmpty()) {
                        Text("+${status.files.sumOf { it.added }}", fontSize = 13.sp, color = diff.add, maxLines = 1)
                        Text("  −${status.files.sumOf { it.deleted }}", fontSize = 13.sp, color = diff.del, maxLines = 1)
                    }
                }
            }
            if (status.files.isEmpty()) item {
                Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Icon(Icons.Outlined.CheckCircle, null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(18.dp))
                    Text("工作区是干净的", color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            items(status.files.size, key = { "f:" + status.files[it].path }) { i ->
                val f = status.files[i]
                GitFileRow(f, Modifier.clickable { onFile(f) })
            }
            item { SectionTitle("提交") }
            val commits = status.commits + older
            items(commits.size, key = { "c:" + commits[it].hash + ":" + it }) { i ->
                val cm = commits[i]
                GitCommitRow(cm, cm.hash == status.head?.hash, Modifier.clickable { onCommit(cm) })
                if (i < commits.lastIndex) HorizontalDivider(Modifier.padding(start = 16.dp))
            }
            // The status carries the latest 30: page the rest from git/log.
            if (!noMore && status.commits.size >= 30) item {
                Box(Modifier.fillMaxWidth().padding(8.dp), contentAlignment = Alignment.Center) {
                    TextButton(enabled = !loadingMore, onClick = {
                        loadingMore = true
                        moreError = null
                        scope.launch {
                            runCatching { hub.api.gitLog(machineId, cwd, commits.size) }
                                .onSuccess { got -> older += got; noMore = got.size < 50 }
                                .onFailure { moreError = it.shown() }
                            loadingMore = false
                        }
                    }) {
                        if (loadingMore) {
                            CircularProgressIndicator(Modifier.size(14.dp), strokeWidth = 1.5.dp)
                            Spacer(Modifier.width(8.dp))
                        }
                        Text(if (moreError != null) "加载失败，重试" else "加载更早的提交", maxLines = 1)
                    }
                }
            }
        }
        error != null -> ErrorNotice("读不到 Git 状态", error, onRetry = onRefresh)
        status?.isRepo == false -> Notice(Icons.Outlined.AccountTree, "不是 Git 仓库", cwd)
        else -> Loading()
    }
}

@Composable
private fun GitHeader(s: GitStatus, onRefresh: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    OutlinedCard(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Column(Modifier.padding(start = 16.dp, top = 12.dp, bottom = 14.dp, end = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Outlined.AccountTree, null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                Text(
                    s.branch ?: "分离的 HEAD", style = MaterialTheme.typography.titleMedium, maxLines = 1,
                    overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
                )
                if (s.ahead > 0) Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Default.ArrowUpward, "领先", Modifier.size(14.dp), tint = muted)
                    Text("${s.ahead}", fontSize = 13.sp, color = muted, maxLines = 1)
                }
                if (s.behind > 0) Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Default.ArrowDownward, "落后", Modifier.size(14.dp), tint = diffColors().changed)
                    Text("${s.behind}", fontSize = 13.sp, color = diffColors().changed, maxLines = 1)
                }
                IconButton(onClick = onRefresh) { Icon(Icons.Default.Refresh, contentDescription = "刷新") }
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                Icon(if (s.upstream != null) Icons.Outlined.Cloud else Icons.Outlined.CloudOff, null, tint = muted, modifier = Modifier.size(14.dp))
                val words = buildList {
                    add(s.upstream ?: "没有上游分支")
                    if (s.upstream != null && s.ahead == 0 && s.behind == 0) add("已同步")
                    if (s.stashes > 0) add("暂存 ${s.stashes}")
                }
                Text(words.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            s.root?.let {
                Text(FilesPaths.keepEnd(it), fontSize = 11.sp, fontFamily = FontFamily.Monospace, color = muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

@Composable
internal fun GitFileRow(file: GitFile, modifier: Modifier = Modifier, showsStage: Boolean = true) {
    val diff = diffColors()
    Row(
        modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        GitBadge(file.letter)
        Column(Modifier.weight(1f)) {
            Text(file.name, fontSize = 15.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val sub = buildList {
                if (showsStage && file.isStaged) add(if (file.hasUnstaged) "部分已暂存" else "已暂存")
                file.origPath?.let { add("← " + it.substringAfterLast('/')) }
                if (file.folder.isNotEmpty()) add(file.folder)
            }
            if (sub.isNotEmpty()) {
                Text(
                    FilesPaths.keepEnd(sub.joinToString("  "), 40), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1, overflow = TextOverflow.Ellipsis,
                )
            }
        }
        if (file.binary) Text("二进制", fontSize = 11.sp, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        else if (file.added + file.deleted > 0) {
            Text("+${file.added}", fontSize = 12.sp, color = diff.add, maxLines = 1)
            Text("−${file.deleted}", fontSize = 12.sp, color = diff.del, maxLines = 1)
        }
    }
}

@Composable
private fun GitCommitRow(commit: GitCommit, isHead: Boolean, modifier: Modifier = Modifier) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val now = remember { java.time.Instant.now() }
    Column(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(commit.subject, fontSize = 15.sp, fontWeight = FontWeight.Medium, maxLines = 2, overflow = TextOverflow.Ellipsis)
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(commit.short, fontSize = 12.sp, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.primary, maxLines = 1)
            Text(commit.author, fontSize = 12.sp, color = muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
            commit.time?.let { Text("· " + FilesPaths.relative(it, now), fontSize = 12.sp, color = muted, maxLines = 1) }
            if (isHead) Text("· HEAD", fontSize = 12.sp, color = muted, maxLines = 1)
        }
        val refs = commit.refNames
        if (refs.isNotEmpty()) Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            refs.forEach { Tag(it, if ('/' in it) muted else MaterialTheme.colorScheme.primary) }
        }
    }
}

/** One changed file's diff; switches between the unstaged and staged halves when it has both (iOS `GitDiffSheet`). */
@Composable
internal fun GitDiffPage(hub: Hub, machineId: String, cwd: String, file: GitFile, initiallyStaged: Boolean, onBack: () -> Unit) {
    var staged by rememberSaveable { mutableStateOf(initiallyStaged) }
    var diff by remember { mutableStateOf<GitDiff?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    LaunchedEffect(staged, reload) {
        diff = null
        error = null
        runCatching { hub.api.gitDiff(machineId, cwd, file.path, staged) }.onSuccess { diff = it }.onFailure { error = it.shown() }
    }
    DiffScaffold(file.name, file.folder.ifEmpty { "仓库根目录" }, diff?.diff, file.path, onBack) {
        Column(Modifier.fillMaxSize()) {
            if (file.isStaged && file.hasUnstaged) Segments(listOf("未暂存", "已暂存"), if (staged) 1 else 0, { staged = it == 1 })
            val d = diff
            when {
                d != null -> DiffBody(d.diff, d.truncated, Modifier.weight(1f))
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
    DiffScaffold(file.name, file.folder.ifEmpty { "仓库根目录" }, part, file.path, onBack) {
        val s = show
        when {
            s != null -> DiffBody(part.orEmpty(), s.truncated && part == null, Modifier.fillMaxSize())
            error != null -> ErrorNotice("读不到改动", error!!, onRetry = { reload++ })
            else -> Loading()
        }
    }
}

@Composable
private fun DiffScaffold(title: String, subtitle: String, diff: String?, path: String?, onBack: () -> Unit, content: @Composable () -> Unit) {
    val ctx = LocalContext.current
    FilesPage(
        title = title,
        subtitle = subtitle,
        onBack = onBack,
        actions = {
            var open by remember { mutableStateOf(false) }
            Box {
                IconButton(onClick = { open = true }) { Icon(Icons.Default.MoreVert, contentDescription = "更多") }
                DropdownMenu(open, onDismissRequest = { open = false }) {
                    if (!diff.isNullOrEmpty()) DropdownMenuItem(text = { Text("拷贝 diff", maxLines = 1) }, onClick = {
                        MediaShare.copyText(ctx, diff, "diff"); MediaShare.toast(ctx, "已拷贝"); open = false
                    })
                    if (path != null) DropdownMenuItem(text = { Text("拷贝路径", maxLines = 1) }, onClick = {
                        MediaShare.copyText(ctx, path, "路径"); MediaShare.toast(ctx, "已拷贝路径"); open = false
                    })
                }
            }
        },
    ) { pad -> Box(Modifier.fillMaxSize().padding(pad)) { content() } }
}

@Composable
private fun DiffBody(diff: String, truncated: Boolean, modifier: Modifier) {
    val parsed = remember(diff) { DiffText.parse(diff) }
    if (parsed.isEmpty()) {
        Notice(
            Icons.Outlined.Description,
            if (truncated) "改动太大，没有带回来" else "没有文本改动",
            if (truncated) "整个提交的 diff 超过 512 KB，这个文件在截断之后。" else "可能是二进制文件、只改了权限，或者改动已被撤销。",
            modifier = modifier,
        )
        return
    }
    Column(modifier) {
        MonoLines(diffLines(parsed), Modifier.weight(1f).fillMaxWidth(), fontSize = 11)
        if (truncated) {
            Text(
                "改动太大，只显示了前 512 KB", style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(8.dp),
            )
        }
    }
}

/** One commit: message, author, the files it touched; each opens its part of the diff (iOS `GitCommitSheet`). */
@Composable
internal fun GitCommitPage(hub: Hub, machineId: String, cwd: String, commit: GitCommit, onBack: () -> Unit, onFile: (GitFile) -> Unit) {
    val ctx = LocalContext.current
    var show by remember { mutableStateOf<GitShow?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    LaunchedEffect(commit.hash, reload) {
        error = null
        runCatching { GitShowCache.get(hub, machineId, cwd, commit.hash) }.onSuccess { show = it }.onFailure { error = it.shown() }
    }
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    FilesPage(title = commit.short.ifEmpty { commit.hash.take(7) }, subtitle = "提交", onBack = onBack) { pad ->
        LazyColumn(Modifier.fillMaxSize().padding(pad), contentPadding = PaddingValues(bottom = 24.dp)) {
            item {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(commit.subject, style = MaterialTheme.typography.titleMedium)
                    (show?.commit?.body ?: commit.body)?.takeIf { it.isNotBlank() }?.let {
                        SelectionContainer { Text(it.trim(), style = MaterialTheme.typography.bodyMedium, color = muted) }
                    }
                    val date = commit.time?.atZone(ZoneId.systemDefault())?.format(DateTimeFormatter.ofPattern("yyyy年M月d日 HH:mm"))
                    Text(listOfNotNull(commit.author, date).joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = muted)
                    Text(
                        "# " + commit.hash, fontSize = 11.sp, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.primary,
                        maxLines = 1, overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.clickable(onClickLabel = "拷贝提交号") {
                            MediaShare.copyText(ctx, commit.hash, "提交"); MediaShare.toast(ctx, "已拷贝提交号")
                        },
                    )
                }
                HorizontalDivider()
            }
            val s = show
            when {
                s != null -> {
                    item { SectionTitle("${s.files.size} 个文件") }
                    items(s.files.size) { i ->
                        val f = s.files[i]
                        GitFileRow(f, Modifier.clickable { onFile(f) }, showsStage = false)
                    }
                }
                error != null -> item { ErrorNotice("读不到这个提交", error!!, onRetry = { reload++ }, ) }
                else -> item { Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(22.dp), strokeWidth = 2.dp) } }
            }
        }
    }
}
