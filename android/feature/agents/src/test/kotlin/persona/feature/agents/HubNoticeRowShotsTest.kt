package persona.feature.agents

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
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.HubNotice
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import java.io.File

/** The loading row and its neighboring notice actions, in all three styles and both appearances. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h700dp-xhdpi")
class HubNoticeRowShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/hub-notice-shots").also { it.mkdirs() }

    @Test
    fun drawsLoadingAndActionRowsInEveryStyleAndAppearance() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Column(
                    Modifier.testTag("shot").width(400.dp)
                        .background(PersonaTheme.palette.background).padding(Tokens.Space.l),
                    verticalArrangement = Arrangement.spacedBy(Tokens.Space.m),
                ) {
                    for (notice in listOf(HubNotice.Updating(persona.core.contracts.CoreText.plain("正在下载")), HubNotice.AutoOn, HubNotice.Available("1.5.0", 150))) {
                        HubNoticeRow(notice, {}, {}, {}, {}, {})
                    }
                }
            }
        }
        for (nextStyle in AppStyle.entries) for (nextDark in listOf(false, true)) {
            rule.runOnIdle { style = nextStyle; dark = nextDark }
            rule.onNodeWithText("hub 正在更新：正在下载").assertIsDisplayed()
            rule.onNodeWithContentDescription("加载中").assertIsDisplayed()
            rule.onNodeWithText("关闭").assertIsDisplayed()
            rule.onNodeWithText("知道了").assertIsDisplayed()
            rule.onNodeWithText("更新").assertIsDisplayed()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            val background = bitmap.getPixel(1, 1)
            var drawn = 0
            for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) {
                if (bitmap.getPixel(x, y) != background) drawn++
            }
            assertTrue("The loading and action rows must draw nonempty content", drawn > 100)
            val suffix = "${nextStyle.name.lowercase()}-${if (nextDark) "dark" else "light"}"
            File(out(), "hub-notice-$suffix.png").outputStream().use {
                assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
            }
        }
    }

    @Test
    fun inlineActionsDoNotTriggerTheOuterRowNavigation() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var notice by mutableStateOf<HubNotice>(HubNotice.AutoOn)
        val calls = mutableListOf<String>()
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Column(Modifier.width(400.dp).padding(Tokens.Space.l)) {
                    HubNoticeRow(
                        notice = notice,
                        onClick = { calls += "open" },
                        onAutoOff = { calls += "off" },
                        onNoticeSeen = { calls += "seen" },
                        onUpdate = { calls += "update" },
                        onDismiss = { calls += "dismiss:${it.dismissKey}" },
                    )
                }
            }
        }
        for (nextStyle in AppStyle.entries) for (nextDark in listOf(false, true)) {
            rule.runOnIdle { style = nextStyle; dark = nextDark; notice = HubNotice.AutoOn; calls.clear() }
            rule.onNodeWithText("关闭").performClick()
            rule.onNodeWithText("知道了").performClick()
            rule.runOnIdle { notice = HubNotice.Available("1.5.0", 150) }
            rule.onNodeWithText("更新").performClick()
            rule.onNodeWithContentDescription("不再提示").performClick()
            rule.runOnIdle { assertEquals(listOf("off", "seen", "update", "dismiss:build:150"), calls) }
            rule.onNodeWithTag("agents.hubNotice").performClick()
            rule.runOnIdle { assertEquals(listOf("off", "seen", "update", "dismiss:build:150", "open"), calls) }
        }
    }
}
