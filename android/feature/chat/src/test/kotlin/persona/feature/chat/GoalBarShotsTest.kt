package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.GoalInfo
import persona.core.designsystem.*
import java.io.File

/**
 * The goal bar under the chat's header (docs/integrations/agent-goal.md): one in progress, one met, one out of budget, and the
 * detail of the first. Every style, light and dark; PNGs in `build/goal-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
class GoalBarShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/goal-shots").also { it.mkdirs() }

    @Test fun drawsTheBarInEveryStyleAndAppearance() {
        val active = GoalInfo(objective = "给天气 App 加主屏幕小组件，深浅色都对，UI 测试全部通过\n然后发一版 TestFlight", status = "active",
            reason = "小组件的深色模式还没做，UI 测试有 2 个失败", tokensUsed = 182_400, seconds = 2580)
        val met = GoalInfo(objective = "检查 PR #42 的改动", status = "complete", reason = "所有评论都已回复")
        val stopped = GoalInfo(objective = "把收件箱的同步改成增量", status = "budget_limited", tokensUsed = 500_000, tokenBudget = 500_000)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background)) {
                    ChatGoalBar(active)
                    ChatGoalBar(met)
                    ChatGoalBar(stopped)
                    ChatGoalBar(null)
                    ChatGoalDetail(active)
                }
            }
        }
        for (nextStyle in AppStyle.entries) for (nextDark in listOf(false, true)) {
            rule.runOnIdle { style = nextStyle; dark = nextDark }
            rule.onNodeWithText("已达成").assertIsDisplayed()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "chat-goal-${nextStyle.name.lowercase()}-${if (nextDark) "dark" else "light"}.png").outputStream().use {
                assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
            }
        }
    }
}
