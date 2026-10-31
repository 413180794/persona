package jarvis.android.files

import jarvis.android.data.JsonCodec
import jarvis.android.ui.files.DiffLine
import jarvis.android.ui.files.DiffText
import jarvis.android.ui.files.FilesRoute
import jarvis.android.ui.files.GitCommit
import jarvis.android.ui.files.GitFile
import jarvis.android.ui.files.GitStatus
import jarvis.android.ui.files.monoColumns
import jarvis.android.ui.files.simplifyHtml
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class GitModelsTest {
    @Test fun decodesStatus() {
        val s = JsonCodec.decodeFromString<GitStatus>(
            """{"is_repo":true,"root":"/r","branch":"main","upstream":"origin/main","ahead":2,"behind":0,"stashes":1,
               "head":{"hash":"abc123","short":"abc","subject":"x","author":"a","date":"2026-06-01T10:00:00+08:00","refs":"HEAD -> main, origin/main, tag: v1"},
               "files":[{"path":"a/b.kt","index":"M","worktree":"M","added":3,"deleted":1},
                        {"path":"n.txt","index":"?","worktree":"?"},
                        {"path":"c.go","orig_path":"old.go","index":"R","worktree":"."},
                        {"path":"m.go","index":"U","worktree":"U"}],
               "commits":[{"hash":"abc123","short":"abc","subject":"x","author":"a","date":"0001-01-01T00:00:00Z"}],"extra":1}""",
        )
        assertTrue(s.isRepo)
        assertEquals(listOf("main", "origin/main", "tag: v1"), s.head!!.refNames)
        assertEquals(Instant.parse("2026-06-01T02:00:00Z"), s.head!!.time)
        assertNull(s.commits[0].time) // Go's zero time
        val (partly, untracked, renamed, conflict) = s.files
        assertEquals("M", partly.letter)
        assertTrue(partly.isStaged && partly.hasUnstaged)
        assertEquals("b.kt", partly.name)
        assertEquals("a", partly.folder)
        assertEquals("U", untracked.letter)
        assertFalse(untracked.isStaged || untracked.hasUnstaged)
        assertEquals("R", renamed.letter)
        assertTrue(renamed.isStaged && !renamed.hasUnstaged)
        assertEquals("old.go", renamed.origPath)
        assertEquals("", renamed.folder)
        assertEquals("!", conflict.letter)
    }

    @Test fun refNamesDropABareHead() {
        assertEquals(emptyList<String>(), GitCommit(refs = "HEAD").refNames)
        assertEquals(emptyList<String>(), GitCommit().refNames)
    }

    @Test fun diffLinesAreClassifiedAndNumbered() {
        val diff = """
            diff --git a/x.kt b/x.kt
            index 1..2 100644
            --- a/x.kt
            +++ b/x.kt
            @@ -10,3 +10,4 @@ fun a()
             keep
            -old
            +new
            +more
            \ No newline at end of file
        """.trimIndent()
        val lines = DiffText.parse(diff)
        assertEquals(
            listOf(DiffLine.Kind.HUNK, DiffLine.Kind.CONTEXT, DiffLine.Kind.DEL, DiffLine.Kind.ADD, DiffLine.Kind.ADD, DiffLine.Kind.INFO),
            lines.map { it.kind },
        )
        assertEquals(DiffLine(DiffLine.Kind.CONTEXT, "keep", 10, 10), lines[1])
        assertEquals(DiffLine(DiffLine.Kind.DEL, "old", old = 11), lines[2])
        assertEquals(DiffLine(DiffLine.Kind.ADD, "new", new = 11), lines[3])
        assertEquals(DiffLine(DiffLine.Kind.ADD, "more", new = 12), lines[4])
        val c = DiffText.unified(diff)
        assertEquals(2, c.adds)
        assertEquals(1, c.dels)
    }

    @Test fun splitsAShowPerFileKeepingSpaces() {
        val show = "diff --git a/a.txt b/a.txt\n@@ -1 +1 @@\n-x\n+y\ndiff --git a/my dir/b c.txt b/my dir/b c.txt\n@@ -0,0 +1 @@\n+z"
        val parts = DiffText.split(show)
        assertEquals(setOf("a.txt", "my dir/b c.txt"), parts.keys)
        assertTrue(parts.getValue("my dir/b c.txt").endsWith("+z"))
        assertEquals(listOf(DiffLine.Kind.HUNK, DiffLine.Kind.DEL, DiffLine.Kind.ADD), DiffText.parse(parts.getValue("a.txt")).map { it.kind })
    }

    @Test fun editDiffIsAllRemovedThenAllAdded() {
        val c = DiffText.edit("a\nb", "c")
        assertEquals(listOf(DiffLine.Kind.DEL, DiffLine.Kind.DEL, DiffLine.Kind.ADD), c.lines.map { it.kind })
        assertEquals(1, c.adds)
        assertEquals(2, c.dels)
    }

    @Test fun routesRoundTripAsJson() {
        val routes = listOf(
            FilesRoute.File("/r/a.kt"),
            FilesRoute.Diff(GitFile("a.kt", index = "M"), staged = true),
            FilesRoute.Commit(GitCommit(hash = "abc", subject = "s")),
            FilesRoute.CommitFile("abc", GitFile("b.kt")),
        )
        for (r in routes) {
            val s = JsonCodec.encodeToString(FilesRoute.serializer(), r)
            assertEquals(r, JsonCodec.decodeFromString(FilesRoute.serializer(), s))
        }
    }

    @Test fun monoColumnsCountWideCharactersTwice() {
        assertEquals(3, monoColumns("abc"))
        assertEquals(4, monoColumns("中文"))
        assertEquals(5, monoColumns("\ta"))
    }

    @Test fun readmeHtmlBecomesMarkdown() {
        val md = "<p align=\"center\"><img src=\"logo.png\" alt=\"Logo\"></p>\n<a href=\"https://example.com\">Site</a>\n```\n<keep>\n```\ntext"
        assertEquals("![Logo](logo.png)\n[Site](https://example.com)\n```\n<keep>\n```\ntext", simplifyHtml(md))
        assertEquals("plain", simplifyHtml("plain"))
    }

    @Test fun nodeAnswersDecode() {
        // Shapes as a node answers (a throwaway repository), times with the machine's offset.
        val st = JsonCodec.decodeFromString<GitStatus>(
            """{"is_repo":true,"root":"/tmp/t","branch":"master","head":{"hash":"321e4a2395fe01a647752b26d29581827f01a1bf","short":"321e4a2","subject":"init","author":"t","date":"2026-10-31T13:58:40+08:00","refs":"HEAD -> master"},"ahead":0,"behind":0,"files":[{"path":"src/a.txt","index":".","worktree":"M","added":2,"deleted":1},{"path":"n.txt","index":"?","worktree":"?","added":0,"deleted":0}],"stashes":0,"commits":[]}""",
        )
        assertEquals(java.time.Instant.parse("2026-10-31T05:58:40Z"), st.head?.time)
        assertEquals(listOf("master"), st.head?.refNames)
        assertEquals(listOf("M", "U"), st.files.map { it.letter })
        val fs = JsonCodec.decodeFromString<jarvis.android.ui.files.FsListing>(
            """{"entries":[{"name":"src","dir":true,"size":4096,"mtime":"2026-10-31T13:58:40.205893083+08:00"}],"path":"/tmp/t","truncated":false}""",
        )
        assertEquals(java.time.Instant.parse("2026-10-31T05:58:40.205893083Z"), fs.entries.single().modified)
    }
}
