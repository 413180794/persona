package persona.feature.settings

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.ExtensionListing
import persona.core.contracts.api.HubExtension
import persona.core.designsystem.PersonaTheme

/**
 * A hub with several managers merges their extension lists, and every manager has the built-ins (docs/assistant/managers.md), so the
 * same id comes once per manager. Settings › Extensions draws each row instead of crashing on a repeated list key.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class ExtensionsDuplicateIdsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun theSameIdUnderTwoManagersDrawsBothRows() {
        val api = object : ExtensionApiStub() {
            override suspend fun listing() = ExtensionListing(listOf(
                HubExtension("example-voice", "Example voice", "voice", "disabled", source = "builtin"),
                HubExtension("example-notes", "Example notes", "connectors", "running", source = "installed", enabled = true),
                HubExtension("example-voice", "Example voice", "voice", "disabled", source = "builtin"),
            ))
        }
        rule.setContent { PersonaTheme(reduceMotion = true) { ExtensionsPage(api, onBack = {}, onExtension = { _, _ -> }) } }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("Example notes").fetchSemanticsNodes().isNotEmpty() }
        assertEquals(2, rule.onAllNodesWithText("Example voice").fetchSemanticsNodes().size)
    }
}
