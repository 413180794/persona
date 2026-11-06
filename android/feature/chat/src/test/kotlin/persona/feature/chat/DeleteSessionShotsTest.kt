package persona.feature.chat

import persona.core.designsystem.text
import android.graphics.Bitmap
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.designsystem.*
import persona.core.domain.sort.deleteConfirmation
import java.io.File

/**
 * "Delete chat"'s confirmation (issue #1749), as the chat's menu and the Agents list show it, drawn on the JVM with
 * Robolectric's native graphics in every style, light and dark. PNGs in `build/delete-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
class DeleteSessionShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/delete-shots").also { it.mkdirs() }

    @Test fun drawsTheConfirmationInEveryStyle() {
        val session = Session(machineId = "box", sessionId = "s", title = "补齐网络层的单元测试", cwd = "/srv/app", status = "offline")
        val confirmation = session.deleteConfirmation("工作站")
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                JDialog(onDismiss = {}, title = confirmation.title.text(), text = confirmation.message.text(), confirm = "删除" to {}, destructive = true)
            }
        }
        rule.onNodeWithText("删除这个对话？").assertExists()
        rule.onNodeWithText("删除").assertExists()
        rule.onNodeWithText("取消").assertExists()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNode(isDialog()).captureToImage().asAndroidBitmap()
            val background = bitmap.getPixel(bitmap.width / 2, 4)
            var other = 0
            for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
            assertTrue("drew nothing", other > 100)
            File(out(), "delete-session-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
