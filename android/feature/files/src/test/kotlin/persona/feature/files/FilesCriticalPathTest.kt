package persona.feature.files

import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import persona.core.contracts.api.*
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withContext
import kotlinx.coroutines.ExperimentalCoroutinesApi
import org.junit.Assert.*
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class FilesCriticalPathTest {
    @Test fun switchingDirectoriesAndDiffsRejectsOldResultsAndInsertsIntoDraft() = runTest {
        val oldDirectory = CompletableDeferred<SessionDirectory>()
        val oldDiff = CompletableDeferred<SessionGitDiff>()
        val requests = mutableListOf<Pair<String, Boolean>>()
        val changed = SessionGitFile("src/a.kt", index = "M", worktree = "M")
        val api = object : SessionFiles {
            override suspend fun directory(machineId: String, cwd: String, path: String): SessionDirectory =
                if (path == cwd) withContext(NonCancellable) { oldDirectory.await() }
                else SessionDirectory(path, listOf(SessionFileEntry("a.kt")))
            override suspend fun file(machineId: String, cwd: String, path: String) = SessionFileContent(path, "a.kt", text = "val a = 1")
            override suspend fun status(machineId: String, cwd: String) = SessionGitStatus(true, root = "/workspace", files = listOf(changed))
            override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean): SessionGitDiff {
                requests += cwd to staged
                return if (!staged) withContext(NonCancellable) { oldDiff.await() }
                else SessionGitDiff(path, "@@ -1 +1 @@\n-old\n+new")
            }
            override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int) = SessionGitLog()
            override suspend fun show(machineId: String, cwd: String, hash: String) = SessionGitShow(SessionGitCommit(hash))
        }
        val controller = FilesController(api, "machine", "/workspace/sub", this)
        controller.directory()
        runCurrent()
        controller.directory("/workspace/sub/src")
        controller.refreshGit()
        runCurrent()
        oldDirectory.complete(SessionDirectory("/workspace/sub"))
        runCurrent()
        assertEquals("/workspace/sub/src", controller.state.value.directory?.path)
        assertEquals("/workspace/sub/src", controller.state.value.here)
        assertNull(controller.state.value.directoryError)
        assertEquals("M", gitMark(controller.state.value.git, "/workspace/src", true))
        assertNull(gitMark(controller.state.value.git, "/workspace/src-other", true))

        controller.preview(FileSelection.Diff(changed, false))
        runCurrent()
        controller.preview(FileSelection.Diff(changed, true))
        runCurrent()
        oldDiff.complete(SessionGitDiff(changed.path, "stale"))
        runCurrent()
        assertEquals(listOf("/workspace" to false, "/workspace" to true), requests)
        assertTrue(controller.state.value.diff!!.diff.contains("+new"))
        val lines = fileLines(controller.state.value.diff!!.diff + "\n\\ No newline at end of file", diff = true)
        assertEquals(1, lines[1].old)
        assertNull(lines[1].new)
        assertEquals(1, lines[2].new)
        assertNull(lines[2].old)
        assertEquals(FileLineKind.Info, lines[3].kind)
        controller.preview(null)
        assertNull(controller.state.value.diff)

        val inserted = insertFilePath(TextFieldValue("查看旧路径继续", TextRange(2, 5)), "/workspace/a b'c.kt")
        assertEquals("查看 '/workspace/a b'\\''c.kt' 继续", inserted.text)
        assertEquals(inserted.text.indexOf("继续"), inserted.selection.start)
        val appended = insertFilePath(TextFieldValue("草稿", TextRange(0)), "/workspace/a.kt", focused = false)
        assertEquals("草稿 /workspace/a.kt ", appended.text)
        assertTrue(SessionGitFile("new.kt", index = "?", worktree = "?").unstaged)
        assertTrue(SessionGitFile("conflict.kt", index = "A", worktree = "A").conflicted)
    }
}
