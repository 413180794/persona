package persona.feature.files

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.AppStyle
import persona.core.designsystem.FileThumbnails
import persona.core.designsystem.PersonaTheme
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import persona.core.navigation.ChatRoute
import java.io.ByteArrayOutputStream
import java.io.File
import java.util.Base64

/** A PNG of [w] × [h] in [color] with a darker band, as the machine's thumbnails and pictures. */
private fun png(w: Int, h: Int, color: Int): ByteArray {
    val bitmap = Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888)
    Canvas(bitmap).apply {
        drawColor(color)
        drawRect(0f, h * 0.6f, w.toFloat(), h.toFloat(), Paint().apply { this.color = 0xFF33415C.toInt() })
    }
    return ByteArrayOutputStream().also { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }.toByteArray()
}

/**
 * A machine with pictures, documents and a video: thumbnails for all but the zip, a big picture, no LibreOffice.
 * [streams]: a hub that answers the viewer's stream=1 (binaries described, no base64); else an older one's base64.
 */
private class MediaMachine(private val streams: Boolean = true) : SessionFiles {
    val root = "/Users/me/report"
    val asked = mutableListOf<String>()
    private val picture = png(1200, 800, 0xFF8FB3FF.toInt())

    override suspend fun directory(machineId: String, cwd: String, path: String) = SessionDirectory(root, listOf(
        SessionFileEntry("photo.png", size = 6L shl 20),
        SessionFileEntry("plan.pdf", size = 320_000),
        SessionFileEntry("slides.pptx", size = 2_400_000),
        SessionFileEntry("demo.mp4", size = 24_000_000),
        SessionFileEntry("notes.md", size = 1200),
        SessionFileEntry("bundle.zip", size = 9_000),
    ))

    override suspend fun file(machineId: String, cwd: String, path: String): SessionFileContent {
        val name = FilesPaths.name(path)
        return when {
            name.endsWith(".png") -> SessionFileContent(path, name, size = 6L shl 20, mime = "image/png",
                data = Base64.getEncoder().encodeToString(picture.copyOf(64)), truncated = true)
            name.endsWith(".pptx") -> SessionFileContent(path, name, size = 2_400_000,
                mime = "application/vnd.openxmlformats-officedocument.presentationml.presentation", data = "AA==", truncated = true)
            name.endsWith(".mp4") -> SessionFileContent(path, name, size = 24_000_000, mime = "video/mp4", data = "AA==", truncated = true)
            else -> SessionFileContent(path, name, size = 10, text = "# notes")
        }
    }

    override suspend fun filePreview(machineId: String, cwd: String, path: String): SessionFileContent {
        asked += "preview ${FilesPaths.name(path)}"
        val old = file(machineId, cwd, path)
        return if (streams && old.text == null) old.copy(data = null, truncated = false, stream = true) else old
    }

    override suspend fun download(machineId: String, cwd: String, path: String, into: File): Long {
        asked += "download $path"
        if (path.endsWith(".mp4")) throw HubFailure(HubError.Server, "outdated", status = 409, code = "node_outdated")
        into.parentFile?.mkdirs()
        into.writeBytes(picture)
        return picture.size.toLong()
    }

    override suspend fun thumbnail(machineId: String, cwd: String, path: String, size: Int): ByteArray? {
        asked += "thumb ${FilesPaths.name(path)}"
        return when (FilesPaths.extension(path)) {
            "png" -> png(96, 64, 0xFF8FB3FF.toInt())
            "pdf", "pptx" -> png(48, 64, 0xFFF4F1EA.toInt())
            "mp4" -> png(96, 54, 0xFF2D3A4F.toInt())
            else -> null
        }
    }

    override suspend fun convertToPdf(machineId: String, cwd: String, path: String): SessionConvertedFile {
        asked += "convert ${FilesPaths.name(path)}"
        throw HubFailure(HubError.Server, "no LibreOffice", status = 404, code = "no_converter")
    }

    override suspend fun status(machineId: String, cwd: String) = SessionGitStatus(false)
    override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean) = SessionGitDiff(path, "")
    override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int) = SessionGitLog()
    override suspend fun show(machineId: String, cwd: String, hash: String) = error("unused")
}

/**
 * The file list with thumbnails (#1370), a picture over 2 MB fetched whole, a video whose node is too old and a
 * PowerPoint on a machine without LibreOffice, in each style, light and dark. PNGs in `build/i18n-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class FilesMediaShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    @Before fun forget() = FileThumbnails.clear()

    @Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
    @Test fun media() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var open by mutableStateOf<String?>(null)
        val machine = MediaMachine()
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").size(400.dp, 800.dp).background(PersonaTheme.palette.background)) {
                    key(open) {
                        SessionFilesScreen(ChatRoute("mac", "s1"), machine.root, machine, onBackToChat = {}, onInsertPath = {},
                            initialPane = FilesPane.Files, initialPath = open?.let { "${machine.root}/$it" })
                    }
                }
            }
        }
        fun capture(name: String) {
            rule.waitForIdle()
            Thread.sleep(300)
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "files-media-$name-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            open = null; capture("list")
            open = "photo.png"; capture("image")
            rule.onNodeWithText("6 MB").assertExists()
            rule.onNodeWithText("图片无法显示").assertDoesNotExist()
            open = "demo.mp4"; capture("video")
            rule.onNodeWithText("放不了这个视频").assertExists()
            open = "slides.pptx"; capture("office")
            rule.onNodeWithText("LibreOffice", substring = true).assertExists()
        }
        // Thumbnails asked once per file for the launch (the zip and the Markdown not at all), a failure not again.
        assertEquals(listOf("thumb demo.mp4", "thumb photo.png", "thumb plan.pdf", "thumb slides.pptx"),
            machine.asked.filter { it.startsWith("thumb") }.sorted())
        assertTrue("download ${machine.root}/photo.png" in machine.asked)
        // The viewer asks with stream=1: no base64 of the 6 MB picture before its download.
        assertTrue("preview photo.png" in machine.asked)
        assertTrue("convert slides.pptx" in machine.asked)
    }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
    @Test fun anOlderHubsCutPictureIsFetchedWholeToo() {
        val machine = MediaMachine(streams = false)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                SessionFilesScreen(ChatRoute("mac", "s1"), machine.root, machine, onBackToChat = {}, onInsertPath = {},
                    initialPane = FilesPane.Files, initialPath = "${machine.root}/photo.png")
            }
        }
        repeat(60) { if ("download ${machine.root}/photo.png" !in machine.asked) { rule.waitForIdle(); Thread.sleep(50) } }
        assertTrue("download ${machine.root}/photo.png" in machine.asked)
        repeat(3) { rule.waitForIdle(); Thread.sleep(200) }
        rule.onNodeWithText("图片无法显示").assertDoesNotExist()
    }

    @Config(sdk = [35])
    @Test fun whatShowsWhole() = runBlocking {
        val streamed = SessionFileContent("/a.png", "a.png", size = 5L shl 20, mime = "image/png", stream = true)
        assertTrue(WholeFiles.hasBytes(streamed))
        assertFalse(WholeFiles.carried(streamed))
        assertTrue(isTable(SessionFileContent("/b.xlsx", "b.xlsx", stream = true)))
        assertTrue(FileShare.canShare(streamed))
        assertFalse(WholeFiles.hasBytes(SessionFileContent("/a.png", "a.png", mime = "image/png")))
        for (ext in listOf("docx", "doc", "pptx", "ppt", "potx", "odt", "odp", "pages", "key", "numbers"))
            assertTrue(ext, WholeFiles.isOffice(SessionFileContent("/a.$ext", "a.$ext", stream = true)))
        // RTF comes as text and shows as text; xlsx opens in the table viewer.
        assertFalse(WholeFiles.isOffice(SessionFileContent("/a.rtf", "a.rtf", stream = true)))
        assertFalse(WholeFiles.isOffice(SessionFileContent("/a.xlsx", "a.xlsx", stream = true)))
        val big = SessionFileContent("/a.png", "a.png", size = 5L shl 20, mime = "image/png", data = "AA==", truncated = true)
        assertTrue(fetchedWhole(big))
        assertFalse(WholeFiles.carried(big))
        assertTrue(WholeFiles.carried(big.copy(truncated = false)))
        assertTrue(WholeFiles.isOffice(SessionFileContent("/a.docx", "a.docx", data = "AA==")))
        assertFalse(WholeFiles.isOffice(SessionFileContent("/k.key", "k.key", text = "-----BEGIN")))
        assertTrue(WholeFiles.isVideo(SessionFileContent("/v.bin", "v.bin", mime = "video/quicktime")))
        assertTrue(WholeFiles.isPdf(SessionFileContent("/p", "p", mime = "application/pdf; x=1")))
        assertFalse(fetchedWhole(SessionFileContent("/a.zip", "a.zip", mime = "application/zip", data = "AA==", truncated = true)))
    }
}
