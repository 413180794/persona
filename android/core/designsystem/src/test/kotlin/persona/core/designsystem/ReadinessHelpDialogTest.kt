package persona.core.designsystem

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/**
 * Without a model providers page to open (the first checklist after pairing), the help never offers "Connect a model" / "Use an API key instead":
 * those buttons used to call back into the same help and reopen it.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class ReadinessHelpDialogTest {
    @get:Rule val rule = createComposeRule()
    private fun text(id: Int) = ApplicationProvider.getApplicationContext<android.content.Context>().getString(id)

    @Test fun withoutAPageTheModelHelpOnlyCloses() {
        var dismissed = 0
        val actions = mutableListOf<ReadinessActionId>()
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                ReadinessHelpDialog(ReadinessHelp.HowToConnectModel, null, onDismiss = { dismissed++ }, onAction = { actions += it }, canConnectModel = false)
            }
        }
        rule.onAllNodesWithText(text(R.string.designsystem_readiness_action_connect_model)).assertCountEquals(0)
        rule.onNodeWithText(text(R.string.designsystem_readiness_got_it)).performClick()
        assertEquals(1, dismissed)
        assertEquals(emptyList<ReadinessActionId>(), actions)
    }

    @Test fun withoutAPageTheInstallHelpOffersNoApiKey() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                ReadinessHelpDialog(ReadinessHelp.InstallClaude, null, onDismiss = {}, onAction = {}, canConnectModel = false)
            }
        }
        rule.onAllNodesWithText(text(R.string.designsystem_readiness_use_api_key)).assertCountEquals(0)
    }

    @Test fun withAPageTheModelHelpOpensIt() {
        val actions = mutableListOf<ReadinessActionId>()
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                ReadinessHelpDialog(ReadinessHelp.HowToConnectModel, null, onDismiss = {}, onAction = { actions += it })
            }
        }
        rule.onNodeWithText(text(R.string.designsystem_readiness_action_connect_model)).performClick()
        assertEquals(listOf(ReadinessActionId.ConnectModel), actions)
    }
}
