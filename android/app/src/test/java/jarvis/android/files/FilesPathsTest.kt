package jarvis.android.files

import jarvis.android.ui.files.FilesPaths
import jarvis.android.ui.files.FilesTarget
import jarvis.android.ui.files.GitFile
import jarvis.android.ui.files.GitStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant
import java.time.ZoneOffset

class FilesPathsTest {
    @Test fun joinNeverDoublesOrDropsTheSlash() {
        assertEquals("/a", FilesPaths.join("/", "a"))
        assertEquals("/x/a", FilesPaths.join("/x", "a"))
        assertEquals("/x/a", FilesPaths.join("/x/", "a/"))
        assertEquals("~/a", FilesPaths.join("~", "a"))
        assertEquals("/x", FilesPaths.join("/x", ""))
    }

    @Test fun parentStopsAtTheTop() {
        assertEquals("/x", FilesPaths.parent("/x/y"))
        assertEquals("/", FilesPaths.parent("/x"))
        assertEquals("/x", FilesPaths.parent("/x/y/"))
        assertEquals("~", FilesPaths.parent("~/a"))
        assertNull(FilesPaths.parent("/"))
        assertNull(FilesPaths.parent("~"))
    }

    @Test fun crumbsFromRootToHere() {
        assertEquals(listOf("code" to "/home/me/code"), FilesPaths.crumbs("/home/me/code", "/home/me/code/"))
        assertEquals(
            listOf("code" to "/home/me/code", "app" to "/home/me/code/app", "src" to "/home/me/code/app/src"),
            FilesPaths.crumbs("/home/me/code", "/home/me/code/app/src"),
        )
        assertEquals(listOf("/" to "/", "etc" to "/etc"), FilesPaths.crumbs("/", "/etc"))
        // Outside the root (a prefix that is not a folder boundary): only where it is.
        assertEquals(listOf("code2" to "/home/me/code2"), FilesPaths.crumbs("/home/me/code", "/home/me/code2"))
    }

    @Test fun shortUsesTilde() {
        assertEquals("~/code", FilesPaths.short("/home/me/code", "/home/me"))
        assertEquals("~", FilesPaths.short("/home/me", "/home/me/"))
        assertEquals("/home/user", FilesPaths.short("/home/user", "/home/me"))
        assertEquals("/etc", FilesPaths.short("/etc", null))
    }

    @Test fun modeGuessesFromThePathUnlessTold() {
        fun mode(path: String, m: FilesTarget.Mode = FilesTarget.Mode.AUTO) = FilesTarget("m", path, mode = m).resolvedMode
        assertEquals(FilesTarget.Mode.DIR, mode("~"))
        assertEquals(FilesTarget.Mode.DIR, mode("/x/src/"))
        assertEquals(FilesTarget.Mode.DIR, mode("/x/Makefile")) // listing a file falls back fine
        assertEquals(FilesTarget.Mode.FILE, mode("/x/README.md"))
        assertEquals(FilesTarget.Mode.FILE, mode("/x.y/a.kt"))
        assertEquals(FilesTarget.Mode.GIT, mode("/x", FilesTarget.Mode.GIT))
        assertTrue(FilesPaths.looksLikeDir(""))
        assertFalse(FilesPaths.looksLikeDir("a/.env"))
    }

    @Test fun gitMarkForFilesAndFolders() {
        val git = GitStatus(
            isRepo = true, root = "/r",
            files = listOf(GitFile("src/a.kt", index = ".", worktree = "M"), GitFile("new.txt", index = "?", worktree = "?")),
        )
        assertEquals("M", FilesPaths.gitMark(git, "/r/src/a.kt", dir = false))
        assertEquals("M", FilesPaths.gitMark(git, "/r/src", dir = true))
        assertEquals("U", FilesPaths.gitMark(git, "/r/new.txt", dir = false))
        assertNull(FilesPaths.gitMark(git, "/r/src/b.kt", dir = false))
        assertNull(FilesPaths.gitMark(git, "/rx/src/a.kt", dir = false))
        assertNull(FilesPaths.gitMark(git, "/r/sr", dir = true))
        assertNull(FilesPaths.gitMark(null, "/r/src/a.kt", dir = false))
    }

    @Test fun formatBytes() {
        assertEquals("0 B", FilesPaths.formatBytes(0))
        assertEquals("1023 B", FilesPaths.formatBytes(1023))
        assertEquals("1 KB", FilesPaths.formatBytes(1024))
        assertEquals("1.5 KB", FilesPaths.formatBytes(1536))
        assertEquals("12 MB", FilesPaths.formatBytes(12L * 1024 * 1024 + 5))
        assertEquals("2 GB", FilesPaths.formatBytes(2L * 1024 * 1024 * 1024))
    }

    @Test fun relativeTimes() {
        val now = Instant.parse("2026-06-15T12:00:00Z")
        assertEquals("刚刚", FilesPaths.relative(now.minusSeconds(30), now, ZoneOffset.UTC))
        assertEquals("5 分钟前", FilesPaths.relative(now.minusSeconds(300), now, ZoneOffset.UTC))
        assertEquals("3 小时前", FilesPaths.relative(now.minusSeconds(3 * 3600), now, ZoneOffset.UTC))
        assertEquals("2 天前", FilesPaths.relative(now.minusSeconds(2 * 86400), now, ZoneOffset.UTC))
        assertEquals("5月1日", FilesPaths.relative(Instant.parse("2026-05-01T08:00:00Z"), now, ZoneOffset.UTC))
        assertEquals("2025年5月1日", FilesPaths.relative(Instant.parse("2025-05-01T08:00:00Z"), now, ZoneOffset.UTC))
    }

    @Test fun keepEndCutsTheStart() {
        assertEquals("short", FilesPaths.keepEnd("short", 10))
        assertEquals("…/b/c.kt", FilesPaths.keepEnd("/aaaa/b/c.kt", 8))
        assertTrue(FilesPaths.isMarkdown("README.MD"))
        assertFalse(FilesPaths.isMarkdown("notes.txt"))
    }
}
