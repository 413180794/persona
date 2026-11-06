package persona.feature.chat

import android.content.Context
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
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.designsystem.*
import persona.core.domain.answer.*
import java.io.File

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi", application = QuestionTestApplication::class)
class QuestionCancellationUiTest {
    @get:Rule val rule = createComposeRule()
    private val question = Interaction("q", kind = "question", canCancel = true,
        questions = listOf(Question("Choose a format", options = listOf(QuestionOption("JSON"), QuestionOption("CSV")))))
    private val close = "Close question without answering"

    @Test fun closeWithdrawsOnlySelectedQuestionAndCannotSubmitTwice() {
        var requests by mutableStateOf(listOf(question))
        val sent = mutableListOf<Resolution>()
        var busy by mutableStateOf<String?>(null)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(requests, null, 500.dp, busy, emptySet(), { ix, resolution ->
                    sent += resolution; busy = ix.id
                }, null, {}, collapsed = true)
            }
        }
        rule.onNodeWithContentDescription(close).performClick()
        rule.onNodeWithContentDescription(close).assertIsNotEnabled().performClick()
        assertEquals(listOf(Resolution(cancel = true)), sent)
        requests = emptyList()
        rule.onNodeWithContentDescription(close).assertDoesNotExist()
    }

    @Test fun oldRuntimeAndApprovalNeverShowCloseAndBlockedRequestCannotCancel() {
        var request by mutableStateOf(question.copy(canCancel = null))
        var blocked by mutableStateOf<String?>(null)
        var uncertain by mutableStateOf(emptySet<String>())
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOf(request), null, 500.dp, null, uncertain,
                    { _, _ -> fail("Must not resolve") }, null, {}, collapsed = true, blockedReason = blocked)
            }
        }
        rule.onNodeWithContentDescription(close).assertDoesNotExist()
        request = question.copy(kind = "permission")
        rule.onNodeWithContentDescription(close).assertDoesNotExist()
        request = question; blocked = "Read only"
        rule.onNodeWithContentDescription(close).assertIsNotEnabled()
        blocked = null; uncertain = setOf(question.id)
        rule.onNodeWithContentDescription(close).assertIsNotEnabled()
        uncertain = emptySet(); request = question.copy(expiresAt = "2000-01-01T00:00:00Z")
        rule.onNodeWithContentDescription(close).assertIsNotEnabled()
    }

    @Test fun collapseDoesNotAnswerAndNormalOptionStillAnswers() {
        var collapsed by mutableStateOf(false)
        val sent = mutableListOf<Resolution>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOf(question), null, 600.dp, null, emptySet(), { _, resolution -> sent += resolution },
                    null, {}, collapsed = collapsed, onCollapsedChange = { collapsed = it })
            }
        }
        rule.onNodeWithContentDescription("Collapse").performClick()
        assertTrue(sent.isEmpty())
        rule.onNodeWithContentDescription("Expand").performClick()
        rule.onNodeWithText("1. JSON").performClick()
        assertEquals(listOf(Resolution(answers = mapOf("Choose a format" to "JSON"))), sent)
    }

    @Test fun fullSheetCloseDiscardsQuestionWithoutFoldingOrSelecting() {
        var requests by mutableStateOf(listOf(question))
        val sent = mutableListOf<Resolution>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(requests, null, 600.dp, null, emptySet(), { _, resolution ->
                    sent += resolution; requests = emptyList()
                }, null, {})
            }
        }
        val resources = ApplicationProvider.getApplicationContext<Context>().resources
        rule.onNodeWithContentDescription(resources.getString(persona.core.designsystem.R.string.designsystem_decision_open_full)).performClick()
        rule.onNode(hasContentDescription(close) and hasAnyAncestor(isDialog())).performClick()
        assertEquals(listOf(Resolution(cancel = true)), sent)
        rule.onNodeWithContentDescription(close).assertDoesNotExist()
    }

    @Test fun screenshotsAcrossStylesAndReceiptNeverClaimsAnAnswer() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Column(Modifier.width(400.dp).background(PersonaTheme.palette.background).testTag("shot")) {
                    InteractionPanel(listOf(question), null, 600.dp, null, emptySet(), { _, _ -> }, null, {})
                }
            }
        }
        val output = File("build/question-cancel-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d; rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(output, "${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
            }
        }
        val res = ApplicationProvider.getApplicationContext<Context>().resources
        val reply = InteractionReply(InteractionSubmission.Answered)
        assertEquals("Question closed without an answer", reply.notice(res, question, Resolution(cancel = true)))
        assertFalse(reply.offersNext(Resolution(cancel = true)))
        assertFalse(InteractionReply(InteractionSubmission.AlreadyAnswered, Resolution(cancel = true)).offersNext(Resolution()))
    }
}
