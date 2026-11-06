package persona.feature.inbox

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
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
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * A document's comments (#2964) in every style, light and dark: the open threads with "Reply" / "Resolve", the box answering
 * one; the resolved tab with "Reopen"; and read only, where the plugin cannot write. Written as PNGs to
 * `build/doc-comments-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class LarkDocCommentsShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/doc-comments-shots").also { it.mkdirs() }

    @Test fun drawsTheCommentsInEveryStyleAndReplies() {
        val api = LarkDocCommentsTest.FakeApi()
        val model = LarkDocCommentsModel("https://example.feishu.cn/docx/D1", api, unnamed = "飞书用户")
        runBlocking { model.load() }
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var writes by mutableStateOf(true)
        var resolved by mutableStateOf(false)
        var run by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                key(run) {
                    val scope = rememberCoroutineScope()
                    Box(Modifier.testTag("shot").width(400.dp).height(900.dp).background(
                        // The sheet's own container (JSheet): Brief's sheets are paper like its pages.
                        if (style == AppStyle.Brief) PersonaTheme.palette.background else PersonaTheme.palette.surfaceOpaque)) {
                        Column { LarkDocCommentsContent(model, writes, resolved, { resolved = it }, Modifier.weight(1f)) { scope.launch { it() } } }
                    }
                }
            }
        }
        rule.onNodeWithTag("docComments.reply.t-old").performClick()
        rule.waitForIdle()
        assertEquals("t-old", (model.target as LarkDocCommentTarget.Thread).id)
        model.draft = "已经按这个改了"
        fun shot(name: String) {
            run++
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            shot("doc-comments-${s.name.lowercase()}-${if (d) "dark" else "light"}")
            rule.onNodeWithText("回复 李雷").assertExists()
        }
        style = AppStyle.Glass; dark = false; resolved = true
        shot("doc-comments-resolved-glass-light")
        rule.onNodeWithText("重新打开").assertExists()
        resolved = false; writes = false
        shot("doc-comments-readonly-glass-dark".also { dark = true })
        rule.onNodeWithTag("docComments.send").assertDoesNotExist()
    }
}
