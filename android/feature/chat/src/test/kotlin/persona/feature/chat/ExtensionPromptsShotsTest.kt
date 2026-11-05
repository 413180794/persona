package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ExtensionQuickPrompt
import persona.core.designsystem.*
import java.io.File

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1000dp-xhdpi")
class ExtensionPromptsShotsTest {
    @get:Rule val rule = createComposeRule()
    @Test fun composerPicksIntoTheBoxWithoutSendingInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        val prompt = ExtensionQuickPrompt("example", "示例扩展", "today", "整理今天的工作并指出需要跟进的事项", "先整理今天的工作，再列出明天需要跟进的事项。")
        val state = ChatComposerState(ChatComposerDraft("/"))
        var sent = 0
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                CompositionLocalProvider(LocalExtensionPrompts provides listOf(prompt)) {
                    Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                        ChatComposer(SessionKey("example", "chat"), state, working = false, sending = false, canClear = false, service = null,
                            onSend = { sent++ }, onCommand = { sent++ }, onNotice = {})
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            val dir = File("build/extension-prompt-shots").also { it.mkdirs() }
            File(dir, "prompts-${s.key}-${if (d) "dark" else "light"}.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        rule.onNodeWithText(prompt.title).performClick()
        rule.waitForIdle()
        assertEquals(prompt.text, state.value.text)
        assertEquals(0, sent)
        state.value = androidx.compose.ui.text.input.TextFieldValue()
        rule.onNodeWithContentDescription("添加附件和快捷指令").performClick()
        rule.onNodeWithText(prompt.title).performClick()
        rule.waitForIdle()
        assertEquals(prompt.text, state.value.text)
        assertEquals(0, sent)
    }
}
