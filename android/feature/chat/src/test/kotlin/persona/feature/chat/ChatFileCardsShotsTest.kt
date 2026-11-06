package persona.feature.chat

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.api.SessionPathStat
import persona.core.contracts.api.SessionPathStats
import persona.core.designsystem.*
import java.io.ByteArrayOutputStream
import java.io.File

/**
 * A sent video and recording (`<persona-file>`) as cards above the bubble, and an answer's documents as cards with their
 * first page (`file/thumb`), in every style, light and dark. PNGs in `build/file-card-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
class ChatFileCardsShotsTest {
    @get:Rule val rule = createComposeRule()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val there = mapOf("/Users/me/report/plan.docx" to "file", "/Users/me/report/q3.xlsx" to "file",
        "/Users/me/report/deck.pptx" to "file", "/Users/me/report/run.log" to "file")
    private val presence = PathPresence(scope, { paths -> SessionPathStats(paths.map { SessionPathStat(it, there[it] ?: "missing") }) })
    private val opened = mutableListOf<String>()
    private val thumbs = mutableListOf<String>()
    private fun page(): ByteArray {
        val bitmap = Bitmap.createBitmap(72, 96, Bitmap.Config.ARGB_8888)
        Canvas(bitmap).apply {
            drawColor(0xFFF7F5F0.toInt())
            val ink = Paint().apply { color = 0xFF9AA3B5.toInt() }
            for (i in 0 until 6) drawRect(10f, 14f + i * 12f, if (i % 3 == 2) 44f else 62f, 18f + i * 12f, ink)
        }
        return ByteArrayOutputStream().also { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }.toByteArray()
    }
    private val actions = ChatRowActions(SessionKey("mac", "s1"), fullText = { it.text.orEmpty() }, openPath = { path, _ -> opened += path },
        photos = null, pictures = { emptyList() }, openImages = { _, _ -> }, menu = null, retryLineId = null, onRetry = {}, paths = presence,
        thumbnails = { path -> "mac\n$path" to { _: Int -> thumbs += path; if (path.endsWith(".pptx")) null else page() } })

    private val user = "看看这段录屏，哪里卡了\n\n<persona-file kind=\"video\" path=\"/Users/me/.persona/uploads/screen.mov\" name=\"录屏 11-05.mov\" size=\"24100000\" duration=\"72.4\"/>\n" +
        "<persona-file kind=\"audio\" path=\"/Users/me/.persona/uploads/note.m4a\" name=\"语音备忘.m4a\" size=\"480000\" duration=\"9\"/>"
    private val reply = "整理好了：/Users/me/report/plan.docx 、/Users/me/report/q3.xlsx 和 /Users/me/report/deck.pptx，日志在 /Users/me/report/run.log。"

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/file-card-shots").also { it.mkdirs() }

    @Before fun forget() = FileThumbnails.clear()
    @After fun stop() = scope.cancel()

    @Test fun cardsInEveryStyle() {
        presence.ask(there.keys.toList())
        val deadline = System.currentTimeMillis() + 5_000
        while (presence.kind("/Users/me/report/run.log") == null && System.currentTimeMillis() < deadline) Thread.sleep(20)

        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalChatRowActions provides actions) {
                    Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l),
                        verticalArrangement = Arrangement.spacedBy(Tokens.Space.l)) {
                        MergedUserBubble(listOf(Item("u1", role = "user", kind = "text", text = user, ts = "2026-11-05T10:32:00Z")))
                        MessageContent(Item("a1", role = "assistant", kind = "text", text = reply), user = false)
                    }
                }
            }
        }
        rule.waitForIdle()
        // No tag text left in the bubble; the cards say name and "size · length".
        rule.onNodeWithText("persona-file", substring = true).assertDoesNotExist()
        rule.onNodeWithText("24.1 MB · 1:12").assertExists()
        rule.onNodeWithText("录屏 11-05.mov").performClick()
        assertEquals(listOf("/Users/me/.persona/uploads/screen.mov"), opened)
        // Documents as cards with their kind, the log stays a chip.
        rule.onNodeWithText("Word 文档").assertExists()
        rule.onNodeWithText("run.log").assertExists()

        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            Thread.sleep(200)
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "file-cards-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        // Each document asked once; the one the machine can't draw keeps its icon and isn't asked again.
        assertEquals(listOf("/Users/me/report/deck.pptx", "/Users/me/report/plan.docx", "/Users/me/report/q3.xlsx"), thumbs.sorted())
    }
}
