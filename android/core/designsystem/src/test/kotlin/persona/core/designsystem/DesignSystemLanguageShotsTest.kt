package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.AgentModel
import persona.core.designsystem.icons.PhIcons
import java.io.File

/**
 * The design system's own words in each of the app's languages (docs/protocol/i18n.md): a page with its back button, the
 * Agents tree's headings and rows, a request card, the status line, a pending badge, step progress, an empty state
 * and the model picker, in every style, light and dark. Written as PNGs to `build/i18n-shots/` (or
 * `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class DesignSystemLanguageShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    private class Words(val back: String, val offline: String, val needsYou: String, val allModels: String, val noMatches: String)

    @Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
    @Test fun hubCardUsesPersonaBrandInitial() {
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = AppStyle.Brief) {
                Box(Modifier.testTag("hub").width(400.dp).height(160.dp).background(PersonaTheme.palette.background).padding(16.dp)) {
                    HubCard("Persona hub", connected = true, statusText = "Connected")
                }
            }
        }
        for (d in listOf(false, true)) {
            dark = d
            rule.waitForIdle()
            rule.onNodeWithText("P").assertExists()
            save(rule.onNodeWithTag("hub").captureToImage().asAndroidBitmap(), "hub-card-persona-brief-${if (d) "dark" else "light"}.png")
        }
    }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", Words("返回", "离线", "等你处理 3 件", "全部模型", "没有找到"))

    @Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
    @Test fun english() = shots("en", Words("Back", "Offline", "3 things need you", "All models", "No matches"))

    @Config(sdk = [35], qualifiers = "ja-w400dp-h1200dp-xhdpi")
    @Test fun japanese() = shots("ja", Words("戻る", "オフライン", "要対応3件", "すべてのモデル", "見つかりません"))

    private fun shots(language: String, words: Words) {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var sheet by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(1160.dp).background(PersonaTheme.palette.background)) {
                    JListPage(title = "Persona", onBack = {}) {
                        item { AgentTreeHeader("MacBook", folded = false, onToggle = {}, first = true, icon = PhIcons.Laptop, online = false,
                            counts = listOf(Status.NeedsYou to 1, Status.Working to 2), onMonitor = {}) }
                        item { AgentSpaceHeader("photo-sync", folded = true, onToggle = {}, quiet = true, sessions = 3) }
                        item { AgentSpaceHeader("weather", folded = false, onToggle = {}, counts = listOf(Status.Unread to 1)) }
                        Status.entries.take(4).forEachIndexed { index, status ->
                            item { AgentRow("Session ${index + 1}", status, onClick = {}, depth = 2, position = GroupPosition.of(index, 4),
                                time = "12:30", statusText = status.localizedLabel) }
                        }
                        item { AgentIdleRow(2, onClick = {}) }
                        item { DecisionCard(DecisionKind.Plan, "HEIC", Modifier.padding(16.dp), source = "MacBook / photo-sync") }
                        item { StatusLine(StatusLineState(pendingRequests = 3)) }
                        item { PendingBadge(PendingStatus.Refused, Modifier.padding(16.dp), reason = "input_draft") }
                        item {
                            StepProgress(listOf(ProgressStep("hub", StepState.Complete), ProgressStep("TLS", StepState.Active), ProgressStep("sync")),
                                Modifier.padding(16.dp))
                        }
                        item { JEmptyState(stringResourceForTest(R.string.designsystem_model_none), stringResourceForTest(R.string.designsystem_model_none_detail),
                            icon = PhIcons.MagnifyingGlass) }
                    }
                }
                if (sheet) JModelSheet("gpt", listOf(AgentModel("gpt-6.1", "GPT-6.1"), AgentModel("claude-opus-5-5", "Claude Opus")),
                    selected = "gpt-4.1", defaultDetail = null, loading = false, error = "timeout", onPick = {}, onRefresh = {}, onDismiss = {},
                    custom = true, initialQuery = "")
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            rule.onNodeWithContentDescription(words.back).assertExists()
            rule.onNodeWithText(words.offline).assertExists()
            rule.onNodeWithText(words.needsYou).assertExists()
            rule.onNodeWithText(words.noMatches).assertExists()
            save(rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap(), "designsystem-$language-${s.name.lowercase()}-${if (d) "dark" else "light"}.png")
        }
        sheet = true
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.mainClock.advanceTimeBy(1_000)
            rule.waitForIdle()
            rule.onNodeWithText(words.allModels).assertExists()
            save(rule.onNode(isDialog()).captureToImage().asAndroidBitmap(), "models-$language-${s.name.lowercase()}-${if (d) "dark" else "light"}.png")
        }
    }

    private fun save(bitmap: Bitmap, name: String) =
        File(out(), name).outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
}

@androidx.compose.runtime.Composable
private fun stringResourceForTest(id: Int) = androidx.compose.ui.res.stringResource(id)
