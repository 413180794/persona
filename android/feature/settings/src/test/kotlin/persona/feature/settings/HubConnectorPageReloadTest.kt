package persona.feature.settings

import android.content.Context
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.*
import persona.core.designsystem.PersonaTheme
import java.io.IOException

/** A connector's page whose list cannot be read says so, and a failed reload keeps the connector on screen (#4814). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "w400dp-h1600dp")
class HubConnectorPageReloadTest {
    @get:Rule val rule = createComposeRule()

    private val github = HubConnector("github", "GitHub", status = "ok", capabilities = listOf("archive"))

    private class Hub(var list: List<HubConnector>?) : ConnectorsApiStub() {
        override suspend fun connectors(all: Boolean) = list ?: throw IOException("hub unreachable")
        override suspend fun deleteConnectorData(id: String) = ConnectorDataDeleted().also { list = null }
    }

    @Test
    fun aFailedFirstReadShowsTheError() {
        rule.setContent { PersonaTheme(reduceMotion = true) { HubConnectorPage(Hub(null), "github", onBack = {}) } }
        rule.waitForIdle()
        rule.onNodeWithText("hub unreachable").assertExists()
    }

    @Test
    fun aFailedReloadKeepsTheConnectorAndShowsTheError() {
        val res = ApplicationProvider.getApplicationContext<Context>().resources
        rule.setContent { PersonaTheme(reduceMotion = true) { HubConnectorPage(Hub(listOf(github)), "github", onBack = {}) } }
        rule.waitForIdle()
        rule.onNodeWithText(res.getString(R.string.settings_src_delete_data)).performClick()
        rule.onNodeWithText(res.getString(R.string.settings_src_delete)).performClick()
        rule.waitForIdle()
        rule.onNodeWithText("hub unreachable").assertExists()
        rule.onAllNodesWithText("GitHub")[0].assertExists()
        rule.onNodeWithText(res.getString(R.string.settings_src_delete_data)).assertExists()
    }
}
