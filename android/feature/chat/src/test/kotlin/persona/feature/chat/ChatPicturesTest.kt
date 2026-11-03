package persona.feature.chat

import persona.core.contracts.ImagePayload
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import persona.core.designsystem.icons.PhIcons
import java.util.Base64
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

/** Photos in messages come from the session's machine; pictures of the chat page through the viewer (R3, R5). */
class ChatPicturesTest {
    /** A machine that has one photo per path it is given, and records what was asked. */
    private class FakeMachine(private val files: Map<String, SessionFileContent>) : SessionFiles {
        val asked = mutableListOf<String>()
        override suspend fun file(machineId: String, cwd: String, path: String): SessionFileContent {
            asked += "$machineId $cwd $path"
            return files[path] ?: error("no such file")
        }
        override suspend fun directory(machineId: String, cwd: String, path: String) = SessionDirectory(path)
        override suspend fun status(machineId: String, cwd: String) = SessionGitStatus(false)
        override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean) = SessionGitDiff()
        override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int) = SessionGitLog()
        override suspend fun show(machineId: String, cwd: String, hash: String) = SessionGitShow(SessionGitCommit(hash))
    }

    private val png = byteArrayOf(0x89.toByte(), 0x50, 0x4e, 0x47, 1, 2, 3)
    private fun photo(path: String, mime: String = "image/png", truncated: Boolean = false) =
        SessionFileContent(path, path.substringAfterLast('/'), png.size.toLong(), mime, data = Base64.getEncoder().encodeToString(png), truncated = truncated)

    @Test fun photosAreReadFromTheSessionsMachineOnce() = runTest {
        val machine = FakeMachine(mapOf("/u/pictures-test/a.png" to photo("/u/pictures-test/a.png")))
        val photos = AttachedPhotos(machine, "macbook", "")
        assertArrayEquals(png, photos.bytes("/u/pictures-test/a.png"))
        assertArrayEquals(png, photos.bytes("/u/pictures-test/a.png"))
        // The hub needs a folder: the session has none, so the photo's absolute path is read from home.
        assertEquals(listOf("macbook ~ /u/pictures-test/a.png"), machine.asked)
    }

    @Test fun whatIsNotAPhotoFails() = runTest {
        val machine = FakeMachine(mapOf(
            "/u/pictures-test/notes.txt" to photo("/u/pictures-test/notes.txt", mime = "text/plain"),
            "/u/pictures-test/big.png" to photo("/u/pictures-test/big.png", truncated = true),
        ))
        val photos = AttachedPhotos(machine, "macbook", "/u")
        for (path in listOf("/u/pictures-test/notes.txt", "/u/pictures-test/big.png", "/u/pictures-test/gone.png")) {
            try {
                photos.bytes(path)
                fail("$path is not a photo")
            } catch (_: IllegalStateException) {
            }
        }
        assertTrue(machine.asked.all { it.startsWith("macbook /u ") })
    }

    @Test fun fileChipsNameAndMarkThePath() {
        assertEquals("Main.kt", fileLabel("/w/app/Main.kt"))
        assertEquals("app", fileLabel("/w/app/"))
        assertEquals(PhIcons.FolderSimple, fileIcon("/w/app/"))
        assertEquals(PhIcons.Image, fileIcon("docs/icon.PNG"))
        assertEquals(PhIcons.Code, fileIcon("src/a.kt"))
        assertEquals(PhIcons.FileText, fileIcon("README.md"))
        assertEquals(PhIcons.File, fileIcon("bin/tool"))
    }

    private class FakeChats : ChatRepository {
        val loaded = mutableListOf<String>()
        override fun observe(session: SessionKey): Flow<ChatSnapshot> = emptyFlow()
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int): List<Item> = emptyList()
        override suspend fun loadImage(session: SessionKey, itemId: String): ByteArray {
            loaded += itemId
            return byteArrayOf(9)
        }
    }

    @Test fun theViewerPagesThroughEveryPictureOfTheConversation() = runTest {
        val key = SessionKey("m", "s")
        val items = listOf(
            Item("u#0", 1, role = "user", kind = "text", text = "看图"),
            Item("i#0", 2, role = "user", kind = "image", image = ImagePayload(data = Base64.getEncoder().encodeToString(png), width = 4, height = 3)),
            Item("t#0", 3, role = "assistant", kind = "tool_use", toolUseId = "shot"),
            Item("t#1", 4, role = "user", kind = "image", toolUseId = "shot", image = ImagePayload(lazy = true, width = 10, height = 20)),
            Item("a#0", 5, role = "assistant", kind = "text", text = "好"),
        )
        val chats = FakeChats()
        val pictures = chatViewerImages(key, items, chats)
        assertEquals(listOf("i#0", "t#1"), pictures.map { it.key })
        assertEquals(4, pictures[0].width)
        assertArrayEquals(png, pictures[0].load())
        assertTrue(chats.loaded.isEmpty())
        assertArrayEquals(byteArrayOf(9), pictures[1].load())
        assertEquals(listOf("t#1"), chats.loaded)
    }
}
