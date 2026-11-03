package persona.feature.files

import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.api.SessionGitCommit
import persona.core.contracts.api.SessionGitFile
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import java.time.Instant

/** The Git half: the branch against its upstream, the working tree's changes, the history (all read-only). */
internal fun LazyListScope.gitBrowser(state: FilesState, controller: FilesController) {
    val git = state.git
    when {
        state.gitError && git == null -> item { RetryState("读不到 Git 状态", "请检查连接后重试。", controller::refreshGit) }
        git == null -> item { Loading() }
        !git.isRepo -> item { JEmptyState("不是 Git 仓库", "这个会话目录没有 Git 仓库。") }
        else -> {
            if (state.gitError) item { RetryState("刷新失败", "下方保留上次读取的状态。", controller::refreshGit) }
            if (state.gitLoading) item { JLinearProgress(null, Modifier.fillMaxWidth()) }
            item("branch") {
                JSectionHeader(git.branch ?: "分离的 HEAD")
                JGroupItem(GroupPosition.Single) {
                    JRow(git.upstream ?: "没有上游分支", subtitle = git.root, leadingIcon = PhIcons.GitBranch, value = gitSync(git))
                }
            }
            item("changes") {
                val (added, deleted) = GitDiffs.totals(git.files)
                // The totals at the heading's far end, on its baseline (the heading's own trailing slot sits mid-row).
                Row {
                    JSectionHeader("改动", Modifier.weight(1f).alignByBaseline(), count = git.files.size)
                    if (added + deleted > 0) {
                        Text("+$added", Modifier.alignByBaseline(), style = PersonaTheme.type.micro, color = PersonaTheme.palette.diffAdd, maxLines = 1)
                        Spacer(Modifier.width(Tokens.Space.s))
                        Text("−$deleted", Modifier.alignByBaseline().padding(end = rowPadding), style = PersonaTheme.type.micro,
                            color = PersonaTheme.palette.diffDel, maxLines = 1)
                    }
                }
            }
            if (git.files.isEmpty()) item { JEmptyState("工作区是干净的", "当前没有未提交的改动。") }
            itemsIndexed(git.files, key = { _, it -> "file:${it.path}" }, contentType = { _, _ -> "change" }) { index, file ->
                JGroupItem(GroupPosition.of(index, git.files.size)) {
                    ChangeRow(file, stage = true) { controller.push(FileSelection.Diff(file, file.staged && !file.unstaged)) }
                }
            }
            item("history") { JSectionHeader("提交历史") }
            val commits = git.commits + state.older
            val remote = remoteOf(git.upstream)
            itemsIndexed(commits, key = { _, it -> "commit:${it.hash}" }, contentType = { _, _ -> "commit" }) { index, commit ->
                JGroupItem(GroupPosition.of(index, commits.size)) {
                    CommitRow(commit, head = commit.hash == git.head?.hash, remote = remote) { controller.push(FileSelection.Commit(commit.hash)) }
                }
            }
            if (git.commits.isEmpty()) item { Notice("还没有提交", padded = false) }
            if (!state.noMore) item {
                JTextButton(when { state.moreLoading -> "正在加载…"; state.moreError -> "加载失败，重试"; else -> "加载更早的提交" },
                    controller::more, Modifier.fillMaxWidth(), enabled = !state.moreLoading && !state.gitLoading)
            }
        }
    }
}

/** A change's letter in its tone: settled when staged, plain otherwise, needs you when it conflicts. */
@Composable
internal fun GitLetter(file: SessionGitFile, stage: Boolean = true) {
    JTag(file.letter, status = if (stage) file.tone else if (file.conflicted) Status.NeedsYou else Status.Unknown)
}

/**
 * A changed file: its name and folder (← the old path of a rename), the letter, where it stands ([stage]: the
 * working tree's changes, not a commit's), ± lines or 二进制.
 */
@Composable
private fun ChangeRow(file: SessionGitFile, stage: Boolean, onClick: () -> Unit) {
    val palette = PersonaTheme.palette
    val where = listOfNotNull(file.originalPath?.let { "← $it" }, file.folder.ifEmpty { null }).joinToString("  ")
    JRow(file.name, subtitle = where.ifEmpty { null }, leadingIcon = fileIcon(file.name), onClick = onClick, subtitleLines = 1, trailing = {
        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                GitLetter(file, stage)
                if (stage) JTag(file.stage, status = file.tone)
            }
            if (file.binary) OneLine("二进制")
            else if (file.added + file.deleted > 0) Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                Text("+${file.added}", style = PersonaTheme.type.micro, color = palette.diffAdd, maxLines = 1)
                Text("−${file.deleted}", style = PersonaTheme.type.micro, color = palette.diffDel, maxLines = 1)
            }
        }
    })
}

/** A commit: the subject (two lines), short hash, author and when, HEAD, its branches and tags. */
@Composable
private fun CommitRow(commit: SessionGitCommit, head: Boolean, remote: String, onClick: () -> Unit) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val now = remember { Instant.now() }
    JRow(onClick = onClick) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
            Text(commit.subject.ifEmpty { commit.shortHash }, style = type.body, color = palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                Text(commit.shortHash, style = type.micro, color = palette.accent, maxLines = 1)
                Text(commit.author, style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false))
                fileDate(commit.date, now)?.let { Text(it, style = type.caption, color = palette.ink2, maxLines = 1) }
                if (head) Text("HEAD", style = type.micro, color = palette.accent, maxLines = 1)
            }
            val refs = commit.refNames
            if (refs.isNotEmpty()) Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                // The local branch stands out; remote branches and tags are facts beside it.
                refs.forEach { JTag(it, status = if (isLocalBranch(it, remote)) Status.Working else null) }
            }
        }
    }
}

/** One commit: message, author and date, the hash (tap to copy), the files it touched; each opens its part of the diff. */
@Composable
internal fun CommitPage(hash: String, state: FilesState, controller: FilesController, back: () -> Unit) {
    val context = LocalContext.current
    val feedback = LocalAppFeedback.current
    val show = state.commit
    val commit = show?.commit ?: (state.git?.commits.orEmpty() + state.older).firstOrNull { it.hash == hash } ?: SessionGitCommit(hash)
    var menu by remember { mutableStateOf(false) }
    val copyHash = { copyText(context, commit.hash, feedback, "已拷贝提交号") }
    JListPage(commit.shortHash, subtitle = "提交", onBack = back, actions = { JIconButton(PhIcons.DotsThree, "提交选项", { menu = true }) }) {
        item("commit") { CommitCard(commit, copyHash) }
        when {
            state.previewError != null -> item { RetryState("读不到这个提交", state.previewError, controller::reload) }
            show == null -> item { Loading() }
            else -> {
                item("files") { JSectionHeader("${show.files.size} 个文件") }
                if (show.files.isEmpty()) item { Notice("这个提交没有改动文件。", padded = false) }
                itemsIndexed(show.files, key = { _, it -> "file:${it.path}" }, contentType = { _, _ -> "change" }) { index, file ->
                    JGroupItem(GroupPosition.of(index, show.files.size)) {
                        ChangeRow(file, stage = false) { controller.push(FileSelection.CommitFile(hash, file)) }
                    }
                }
            }
        }
    }
    if (menu) JActionSheet(buildList {
        add(JAction("拷贝提交号", PhIcons.Hash, onClick = copyHash))
        show?.diff?.takeIf { it.isNotEmpty() }?.let { diff -> add(JAction("拷贝 diff", PhIcons.Copy) { copyText(context, diff, feedback, "已拷贝 diff") }) }
        add(JAction("刷新", PhIcons.ArrowClockwise) { controller.reload() })
    }, { menu = false }, "提交")
}

@Composable
private fun CommitCard(commit: SessionGitCommit, copy: () -> Unit) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    JCard {
        Text(commit.subject.ifEmpty { commit.shortHash }, style = type.headline, color = palette.ink)
        commit.body?.trim()?.takeIf { it.isNotEmpty() }?.let { body ->
            Spacer(Modifier.height(Tokens.Space.s))
            SelectionContainer { Text(body, style = type.callout, color = palette.ink2) }
        }
        Spacer(Modifier.height(Tokens.Space.s))
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            if (commit.author.isNotEmpty()) Text(commit.author, style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f, fill = false))
            fullDate(commit.date)?.let { Text(it, style = type.caption, color = palette.ink2, maxLines = 1) }
        }
        Spacer(Modifier.height(Tokens.Space.xs))
        Row(
            Modifier.clip(PersonaTheme.shapes.controlShape).clickable(role = Role.Button, onClickLabel = "拷贝提交号", onClick = copy)
                .padding(vertical = Tokens.Space.xs),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            JIcon(PhIcons.Hash, null, size = Tokens.Space.l, tint = palette.accent)
            Spacer(Modifier.width(Tokens.Space.xs))
            Text(commit.hash, style = type.codeSmall, color = palette.accent, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** One file of a commit: its part of the commit's diff. */
@Composable
internal fun CommitFilePage(selection: FileSelection.CommitFile, state: FilesState, controller: FilesController, back: () -> Unit) {
    val context = LocalContext.current
    val feedback = LocalAppFeedback.current
    val file = selection.file
    val show = state.commit
    val part = remember(show, file) { show?.let { GitDiffs.part(it.diff, file) } }
    var menu by remember { mutableStateOf(false) }
    val root = state.git?.root ?: controller.cwd
    JPage(file.name, subtitle = file.folder.ifEmpty { "仓库根目录" }, onBack = back,
        actions = { JIconButton(PhIcons.DotsThree, "改动选项", { menu = true }) }) { padding ->
        val diff = part?.diff
        when {
            state.previewError != null -> LazyColumn(contentPadding = padding) { item { RetryState("读不到改动", state.previewError, controller::reload) } }
            show == null -> LazyColumn(contentPadding = padding) { item { Loading() } }
            diff.isNullOrBlank() -> LazyColumn(contentPadding = padding) {
                item {
                    if (show.truncated && diff == null) JEmptyState("改动太大，没有带回来", "整个提交的 diff 超过 512 KB，这个文件在截断之后。")
                    else JEmptyState("没有文本改动", "可能是二进制文件、只改了权限，或者改动已被撤销。")
                }
            }
            else -> FileText(diff, diff = true, contentPadding = padding, header = {
                if (show.truncated && part.last) item("truncated") { Notice("改动太大，只显示了前 512 KB") }
            })
        }
    }
    if (menu) JActionSheet(buildList {
        part?.diff?.takeIf { it.isNotEmpty() }?.let { diff -> add(JAction("拷贝 diff", PhIcons.Copy) { copyText(context, diff, feedback, "已拷贝 diff") }) }
        add(JAction("拷贝路径", PhIcons.Copy) { copyText(context, joinRemotePath(root, file.path), feedback, "已拷贝路径") })
    }, { menu = false }, file.name)
}
