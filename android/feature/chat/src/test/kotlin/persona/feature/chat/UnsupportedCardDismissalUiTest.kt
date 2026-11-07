package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
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
import persona.core.contracts.*
import persona.core.designsystem.*
import java.io.File

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi", application = QuestionTestApplication::class)
class UnsupportedCardDismissalUiTest {
    @get:Rule val rule = createComposeRule()
    private val question = Interaction("old", kind = "question", canCancel = true,
        questions = listOf(Question("Choose a format", options = listOf(QuestionOption("JSON"), QuestionOption("CSV")))))
    private val close = "Dismiss card on this device"

    @Test fun unsupportedCardsDiscardLocallyWithoutAnswersOrApproval() {
        var request by mutableStateOf<Interaction?>(question)
        var blocked by mutableStateOf<String?>("This source does not support answering")
        val discarded = mutableListOf<String>()
        val answers = mutableListOf<Resolution>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOfNotNull(request), null, 600.dp, null, emptySet(),
                    { _, answer -> answers += answer }, null, {}, blockedReason = blocked,
                    onDiscardLocal = { discarded += it.id; request = null })
            }
        }
        for (kind in listOf("question", "permission", "plan", "screen", "elicitation")) {
            request = question.copy(id = kind, kind = kind)
            rule.onNodeWithContentDescription(close).assertIsEnabled().performClick()
            rule.onNodeWithContentDescription(close).assertDoesNotExist()
        }
        assertEquals(listOf("question", "permission", "plan", "screen", "elicitation"), discarded)
        assertTrue(answers.isEmpty())
        blocked = null
        request = question.copy(id = "legacy", canCancel = null)
        rule.onNodeWithContentDescription(close).performClick()
        assertEquals("legacy", discarded.last())
        request = question.copy(id = "new")
        rule.onNodeWithText("1. JSON").performClick()
        assertEquals(listOf(Resolution(answers = mapOf("Choose a format" to "JSON"))), answers)
    }

    @Test fun unsupportedCardScreenshotsAndBusyGuardAcrossStyles() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var busy by mutableStateOf<String?>(null)
        val discarded = mutableListOf<String>()
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Column(Modifier.width(400.dp).background(PersonaTheme.palette.background).testTag("shot")) {
                    InteractionPanel(listOf(question), null, 600.dp, busy, emptySet(),
                        { _, _ -> fail("No remote resolution") }, null, {},
                        blockedReason = "This source does not support answering",
                        onDiscardLocal = { discarded += it.id; busy = it.id })
                }
            }
        }
        val output = File("build/unsupported-card-shots").also { check(it.mkdirs() || it.isDirectory) }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d; rule.waitForIdle()
            rule.onNodeWithContentDescription(close).assertIsDisplayed().assertIsEnabled()
            rule.onNodeWithText("1. JSON").assertIsNotEnabled()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(output, "${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
            }
        }
        rule.onNodeWithContentDescription(close).performClick()
        rule.onNodeWithContentDescription(close).assertIsNotEnabled().performClick()
        assertEquals(listOf(question.id), discarded)
    }
}
