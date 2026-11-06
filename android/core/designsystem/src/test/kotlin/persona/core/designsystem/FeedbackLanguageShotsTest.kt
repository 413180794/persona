package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.ConnectionState
import persona.core.designsystem.icons.PhIcons
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import java.io.File

/**
 * The fixed words code outside composition hands to a page (#2453): every agent status, every pending state, an
 * operation's failure notice with its button, and the same failure through [FeedbackGate] (the app's snackbar text),
 * in each of the app's languages, every style, light and dark. English holds no Chinese. Written as PNGs to
 * `build/i18n-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
class FeedbackLanguageShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    private class Words(val draft: String, val openTerminal: String, val sending: String, val needsYou: String)

    @Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", Words("电脑上有草稿，请先到终端处理；手机输入已保留", "打开终端", "发送中", "需要你"))

    @Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
    @Test fun english() = shots("en", Words("There's a draft on the computer. Deal with it in the terminal first; what you typed on the phone is kept.",
        "Open terminal", "Sending", "Needs you"))

    @Config(sdk = [35], qualifiers = "ja-w400dp-h900dp-xhdpi")
    @Test fun japanese() = shots("ja", Words("コンピューターに下書きがあります。先にターミナルで対応してください。スマートフォンの入力は保持されています",
        "ターミナルを開く", "送信中", "要対応"))

    private fun shots(language: String, words: Words) {
        val context = RuntimeEnvironment.getApplication()
        val draft = operationFeedback(HubFailure(HubError.Server, "raw detail", status = 409, code = "input_draft", delivered = "none", retryable = true))
        val noMachine = operationFeedback(HubFailure(HubError.Server, "HTTP 409", status = 409, code = "no_machine", retryable = false))
        // The gate is what the app's snackbar shows: it resolves the feedback in the app's language.
        val gate = FeedbackGate({ ConnectionState.Online }, localize = { it.resolve(context) })
        val seen = mutableListOf<String>()
        val collector = CoroutineScope(Dispatchers.Unconfined).launch { gate.messages.collect { seen += it } }
        gate.failure(HubFailure(HubError.Server, "raw detail", status = 409, code = "input_draft", delivered = "none", retryable = true))
        collector.cancel()
        assertEquals(listOf(words.draft), seen)
        if (language == "en") {
            val shown = Status.entries.map { it.label(context) } + PendingStatus.entries.map { it.label(context) } +
                listOf(draft.text.resolve(context), noMachine.text.resolve(context), context.getString(draft.actionLabelRes!!))
            shown.forEach { assertFalse("English shows Chinese: $it", it.any { c -> c in '一'..'鿿' }) }
        }

        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    JNotice(draft.localizedMessage, Modifier, status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                        actionLabel = draft.localizedActionLabel, onAction = {})
                    JNotice(noMachine.localizedMessage, Modifier, status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                        actionLabel = noMachine.localizedActionLabel, onAction = {})
                    JNotice(seen.single(), Modifier, status = Status.Unknown)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Status.entries.forEach { status ->
                            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                JStatusDot(status); StatusLabel(status)
                            }
                        }
                    }
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        PendingStatus.entries.forEach { PendingBadge(it) }
                    }
                }
            }
        }
        for (s in AppStyle.entries) {
            style = s
            var light: Bitmap? = null
            for (isDark in listOf(false, true)) {
                dark = isDark
                androidx.compose.runtime.snapshots.Snapshot.sendApplyNotifications()
                rule.waitForIdle()
                rule.onNodeWithText(words.openTerminal).assertExists()
                rule.onNodeWithText(words.sending).assertExists()
                rule.onNodeWithText(words.needsYou).assertExists()
                val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                if (isDark) assertFalse("Dark must differ from light for $language/$s", bitmap.sameAs(light))
                else light = bitmap
                val appearance = if (isDark) "dark" else "light"
                save(bitmap, "feedback-$language-${s.name.lowercase()}-$appearance.png")
            }
        }
    }

    private fun save(bitmap: Bitmap, name: String) =
        File(out(), name).outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
}
