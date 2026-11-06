package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.designsystem.*
import java.io.File

/**
 * The user's own messages in the pale blue, another session's and another conversation's in the style's tint (#2643),
 * in every style, light and dark. PNGs in `build/user-bubble-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
class UserBubbleShotsTest {
    @get:Rule val rule = createComposeRule()

    private val relay = "<persona-context kind=\"context\" title=\"来自对话「写周报」\" url=\"\">\n这条消息不是用户本人发的。\n</persona-context>\n周报写好了，放在 docs/weekly.md"

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/user-bubble-shots").also { it.mkdirs() }

    @Test fun ownBlueOthersAsTheyWere() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l),
                    verticalArrangement = Arrangement.spacedBy(Tokens.Space.l)) {
                    MergedUserBubble(listOf(Item("u1", role = "user", kind = "text", text = "帮我看一下 CI 为什么挂了", clientMsgId = "c1", ts = "2026-11-05T10:32:00Z")))
                    MessageContent(Item("a1", role = "assistant", kind = "text", text = "是 lint 报错，已经修好。"), user = false)
                    MergedUserBubble(listOf(Item("u2", role = "user", kind = "text", text = "build passed on main", from = "build-bot", ts = "2026-11-05T10:33:00Z")))
                    MergedUserBubble(listOf(Item("u3", role = "user", kind = "text", text = relay, ts = "2026-11-05T10:34:00Z")))
                    UserBubble("正在发送的一条")
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "user-bubble-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
