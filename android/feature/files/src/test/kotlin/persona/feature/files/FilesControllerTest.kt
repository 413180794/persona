package persona.feature.files

import persona.core.contracts.api.*
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

/** A machine with a few folders under /Users/me; records what the pane asked for. */
private class FakeMachine : SessionFiles {
    val asked = mutableListOf<String>()
    val folders = setOf("/", "/Users", "/Users/me", "/Users/me/code", "/Users/me/code/weather", "/Users/me/code/weather/Sources",
        "/Users/me/code/weather/Sources/App")
    var failing: Exception? = null

    private fun expand(path: String) = if (path == "~" || path.startsWith("~/")) "/Users/me" + path.removePrefix("~") else path

    override suspend fun directory(machineId: String, cwd: String, path: String): SessionDirectory {
        asked += "fs $path"
        failing?.let { throw it }
        val real = expand(path)
        require(real in folders) { "not a folder: $real" }
        return SessionDirectory(real, folders.filter { it != real && FilesPaths.parent(it) == real }.map { SessionFileEntry(FilesPaths.name(it), dir = true) })
    }

    override suspend fun file(machineId: String, cwd: String, path: String): SessionFileContent {
        asked += "file $path"
        failing?.let { throw it }
        val real = expand(path)
        return if (real in folders) SessionFileContent(real, FilesPaths.name(real), isDir = true, entries = listOf("App/"))
        else SessionFileContent(real, FilesPaths.name(real), size = 5, text = "hello")
    }

    override suspend fun status(machineId: String, cwd: String) = SessionGitStatus(true, root = "/Users/me/code/weather")
    override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean) = SessionGitDiff(path, "")
    override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int) = SessionGitLog()
    override suspend fun show(machineId: String, cwd: String, hash: String): SessionGitShow {
        asked += "show $hash"
        return SessionGitShow(SessionGitCommit(hash, subject = "Add README"), files = listOf(SessionGitFile("README.md", index = "M")),
            diff = "diff --git a/README.md b/README.md\n@@ -1 +1 @@\n-a\n+b")
    }
}

@OptIn(ExperimentalCoroutinesApi::class)
class FilesControllerTest {
    private val cwd = "/Users/me/code/weather"

    private fun TestScope.controller(machine: FakeMachine) =
        FilesController(machine, "macbook", cwd, this, describe = { it.message ?: "?" })

    @Test fun theBrowserStartsInTheSessionFolderAndGoesUpToTheRoot() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        assertEquals(cwd, files.state.value.here)
        assertEquals(listOf("weather" to cwd), files.state.value.crumbs)
        assertEquals(listOf("Sources"), files.state.value.directory!!.entries.map { it.name })

        // Above the session folder: the breadcrumb's top moves up with it, one folder at a time, to the root.
        files.up()
        runCurrent()
        assertEquals("/Users/me/code", files.state.value.root)
        assertEquals(listOf("weather"), files.state.value.directory!!.entries.map { it.name })
        repeat(3) { files.up(); runCurrent() }
        assertEquals("/", files.state.value.here)
        assertEquals(listOf("/" to "/"), files.state.value.crumbs)
        assertNull(files.state.value.parent)
        files.up() // nothing above the root
        runCurrent()
        assertEquals("/", files.state.value.here)

        // Back into the tree: the crumbs grow from the new top, back walks up to it and then leaves.
        files.directory("/Users")
        runCurrent()
        files.directory("/Users/me")
        runCurrent()
        assertEquals(listOf("/" to "/", "Users" to "/Users", "me" to "/Users/me"), files.state.value.crumbs)
        assertTrue(files.back())
        runCurrent()
        assertEquals("/Users", files.state.value.here)
        assertTrue(files.back())
        runCurrent()
        assertFalse(files.back())
        assertFalse(files.back(FilesPane.Git))
    }

    @Test fun goingUpInsideTheTreeKeepsTheTop() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        files.directory("$cwd/Sources/App")
        runCurrent()
        assertTrue(files.state.value.belowRoot)
        files.up()
        runCurrent()
        assertEquals("$cwd/Sources", files.state.value.here)
        assertEquals(cwd, files.state.value.root)
        // Git's pane does not walk folders: back leaves from there.
        assertFalse(files.back(FilesPane.Git))
    }

    @Test fun aFileFromTheConversationOpensAloneAndBackLeaves() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start("Sources/App/main.swift")
        runCurrent()
        assertEquals(listOf("file $cwd/Sources/App/main.swift"), machine.asked.filter { it.startsWith("file") })
        assertTrue(machine.asked.none { it.startsWith("fs") }) // no browser under it
        assertEquals("hello", files.state.value.content?.text)
        assertTrue(files.state.value.direct)
        assertFalse(files.back())
    }

    @Test fun aFileFromTheConversationKeepsItsLine() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start("Sources/App/main.swift", initialLine = 12)
        runCurrent()
        assertEquals(FileSelection.File("$cwd/Sources/App/main.swift", 12), files.state.value.selection)
        assertEquals("hello", files.state.value.content?.text)

        // No line, or one that is not a line: the file opens at its top.
        val other = controller(FakeMachine())
        other.start("Sources/App/main.swift", initialLine = 0)
        runCurrent()
        assertNull((other.state.value.selection as FileSelection.File).line)
    }

    @Test fun homePathsTravelAsWritten() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start("~/notes.txt")
        runCurrent()
        assertTrue("file ~/notes.txt" in machine.asked)
    }

    @Test fun aFolderFromTheConversationOpensInTheBrowserAsItsTop() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start("Sources/")
        runCurrent()
        assertEquals("$cwd/Sources", files.state.value.root)
        assertEquals("$cwd/Sources", files.state.value.here)
        assertNull(files.state.value.selection)
        assertFalse(files.back())

        // Without a slash the hub says it is a folder: the same, after one read.
        val other = FakeMachine()
        val again = controller(other)
        again.start("Sources")
        runCurrent()
        assertEquals(listOf("file $cwd/Sources", "fs $cwd/Sources"), other.asked.filter { !it.startsWith("show") })
        assertEquals("$cwd/Sources", again.state.value.root)
        assertNull(again.state.value.selection)
        assertFalse(again.state.value.direct)
        assertEquals(listOf("App"), again.state.value.directory!!.entries.map { it.name })
        again.directory("$cwd/Sources/App")
        runCurrent()
        assertTrue(again.back())
        runCurrent()
        assertFalse(again.back())
    }

    @Test fun theHomeOpensResolved() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start("~")
        runCurrent()
        // The hub expands "~": the breadcrumb shows where that is.
        assertEquals("/Users/me", files.state.value.root)
        assertEquals("/Users/me", files.state.value.here)
    }

    @Test fun aCommitsFilesOpenOverItWithoutAskingAgain() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        files.push(FileSelection.Commit("abc"))
        runCurrent()
        assertEquals("Add README", files.state.value.commit?.commit?.subject)
        files.push(FileSelection.CommitFile("abc", SessionGitFile("README.md", index = "M")))
        runCurrent()
        assertEquals(2, files.state.value.stack.size)
        assertNotNull(files.state.value.commit)
        assertEquals(1, machine.asked.count { it == "show abc" })
        assertTrue(files.back())
        runCurrent()
        assertTrue(files.state.value.selection is FileSelection.Commit)
        assertNotNull(files.state.value.commit)
        assertEquals(1, machine.asked.count { it == "show abc" })
        assertTrue(files.back())
        assertNull(files.state.value.selection)
        assertFalse(files.back())
    }

    @Test fun backToAFileUnderAnotherShowsItAtOnceAndReloadAsksAgain() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        files.push(FileSelection.File("$cwd/README.md"))
        runCurrent()
        files.push(FileSelection.File("$cwd/docs/guide.md")) // a link in the README
        runCurrent()
        assertTrue(files.back())
        // No request, no loading frame: the README as it was.
        assertEquals("$cwd/README.md", files.state.value.content?.path)
        assertEquals(1, machine.asked.count { it == "file $cwd/README.md" })
        files.reload()
        runCurrent()
        assertEquals(2, machine.asked.count { it == "file $cwd/README.md" })
    }

    @Test fun aFolderOpenedFromAPageOpensInTheBrowser() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        files.push(FileSelection.File("$cwd/README.md"))
        runCurrent()
        // A README's link to a folder below the session folder: the browser goes there and back walks up.
        files.push(FileSelection.File("$cwd/Sources"))
        runCurrent()
        assertNull(files.state.value.selection)
        assertEquals("$cwd/Sources", files.state.value.here)
        assertEquals(cwd, files.state.value.root)
        assertTrue(files.back())
    }

    @Test fun failuresSayWhyAndKeepWhatWasShown() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        machine.failing = IllegalStateException("macOS 不让 Persona 读「桌面」文件夹里的文件")
        files.directory()
        runCurrent()
        assertEquals("macOS 不让 Persona 读「桌面」文件夹里的文件", files.state.value.directoryError)
        assertNotNull(files.state.value.directory) // the listing from before stays under the error
        files.preview(FileSelection.File("$cwd/a.txt"))
        runCurrent()
        assertEquals("macOS 不让 Persona 读「桌面」文件夹里的文件", files.state.value.previewError)
        machine.failing = null
        files.reload()
        runCurrent()
        assertNull(files.state.value.previewError)
        assertEquals("hello", files.state.value.content?.text)
    }
}
