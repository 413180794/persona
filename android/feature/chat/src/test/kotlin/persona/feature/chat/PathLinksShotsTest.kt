package persona.feature.chat

import android.graphics.Bitmap
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
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
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
import java.io.File

/**
 * Paths in messages (PathLinks.kt) drawn on the JVM: a user's message and a reply, the paths there on the machine as
 * links, a missing one as text, in every style, light and dark. PNGs in `build/path-link-shots/` (or
 * `-Dpersona.shots=<dir>`). A tap on a link opens the path.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
class PathLinksShotsTest {
    @get:Rule val rule = createComposeRule()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val there = mapOf("/tmp/persona-tasks/1664-lark-test.md" to "file", "web/src/lib/api.ts" to "file", "docs/web/web.md" to "file")
    private val presence = PathPresence(scope, { paths -> SessionPathStats(paths.map { SessionPathStat(it, there[it] ?: "missing") }) })
    private val opened = mutableListOf<String>()
    private val actions = ChatRowActions(SessionKey("mac", "s1"), fullText = { it.text.orEmpty() }, openPath = { path, _ -> opened += path },
        photos = null, pictures = { emptyList() }, openImages = { _, _ -> }, menu = null, retryLineId = null, onRetry = {}, paths = presence)

    private val user = "读 /tmp/persona-tasks/1664-lark-test.md 并完整执行这个任务，别动 /tmp/other/notes.md"
    private val reply = "改好了：`web/src/lib/api.ts:34` 加了重试，说明写在 docs/web/web.md 里；/tmp/other/notes.md 没动。"

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/path-link-shots").also { it.mkdirs() }

    @After fun stop() = scope.cancel()

    @Test fun linksThePathsThereInEveryStyle() {
        presence.ask(PathLinks.asked(user) + PathLinks.asked(reply))
        val deadline = System.currentTimeMillis() + 5_000
        while (presence.kind("docs/web/web.md") == null && System.currentTimeMillis() < deadline) Thread.sleep(20)
        assertTrue(presence.linkable("/tmp/persona-tasks/1664-lark-test.md") && !presence.linkable("/tmp/other/notes.md"))

        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var annotated: AnnotatedString? = null
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalChatRowActions provides actions) {
                    annotated = messageLinkAnnotations(user)
                    Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l),
                        verticalArrangement = Arrangement.spacedBy(Tokens.Space.l)) {
                        MergedUserBubble(listOf(Item("u1", role = "user", kind = "text", text = user, ts = "2026-11-04T10:32:00Z")))
                        MessageContent(Item("a1", role = "assistant", kind = "text", text = reply), user = false)
                    }
                }
            }
        }
        rule.waitForIdle()
        // The user's text: one link, the path that is there; a tap opens it.
        val links = annotated!!.getLinkAnnotations(0, user.length)
        assertEquals(listOf("/tmp/persona-tasks/1664-lark-test.md"), links.map { user.substring(it.start, it.end) })
        val link = links.single().item as LinkAnnotation.Clickable
        link.linkInteractionListener!!.onClick(link)
        assertEquals(listOf("/tmp/persona-tasks/1664-lark-test.md"), opened)

        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "path-links-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
