package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.*
import kotlinx.serialization.json.*
import java.io.File

/**
 * Settings › Extensions › GitHub (persona#3178): the connector it contributes gets its "Send to Inbox" switch (the hub's own) and a link to
 * its page; the extension's inbox group (repositories, kinds) is its schema form, drawn as any other.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h2400dp-xhdpi")
class ExtensionConnectorInboxShotsTest {
    @get:Rule val rule = createComposeRule()

    private val kinds = listOf("review_requested" to "请你 review", "mention" to "@我", "ci_failed" to "CI 失败")
    private val repo = ExtensionItems(type = "string")
    private val detail = ExtensionDetail(
        HubExtension("github", "GitHub", "connectors", "running", source = "installed", enabled = true,
            contributes = mapOf("connectors" to listOf("github"), "ui" to listOf("github"))),
        form = listOf(
            ExtensionField("triage_rules", title = "分拣规则"),
            ExtensionField("inbox", type = "object", title = "收件箱", description = "哪些 GitHub 通知进收件箱。改了以后只影响之后新来的。", fields = listOf(
                ExtensionField("repos", type = "array", title = "只收这些仓库", description = "写 owner/name，或 owner/*", items = repo),
                ExtensionField("exclude_repos", type = "array", title = "不收这些仓库", items = repo),
                ExtensionField("skip_kinds", type = "array", title = "不收这些类型",
                    items = ExtensionItems(enum = kinds.map { (v, t) -> ExtensionChoice(JsonPrimitive(v), t) })),
            )),
        ),
        config = buildJsonObject {
            putJsonObject("inbox") {
                putJsonArray("exclude_repos") { add("example-org/noisy") }
                putJsonArray("skip_kinds") { add("ci_failed") }
            }
        },
    )

    private fun capture(name: String) {
        rule.waitForIdle()
        val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val dir = File("build/extension-connector-inbox-shots").also { it.mkdirs() }
        File(dir, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun connectorInboxSwitchAndLinkInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var github = HubConnector("github", "GitHub", status = "ok", capabilities = listOf("inbox"), enabled = true, archive = true, inbox = true)
        val feeds = mutableListOf<Pair<String, Boolean>>()
        var opened: String? = null
        val extensions = object : ExtensionApiStub() {
            override suspend fun extension(id: String) = detail
        }
        val connectors = object : ConnectorsApiStub() {
            override suspend fun connectors(all: Boolean) = listOf(github, HubConnector("telegram", "Telegram", inbox = true))
            override suspend fun setConnectorFeed(id: String, feed: String, on: Boolean): HubConnector {
                feeds += feed to on
                github = github.copy(inbox = on)
                return github
            }
        }
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) {
                    ExtensionPage(extensions, "github", {}, connectors = connectors, onConnector = { opened = it })
                }
            }
        }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("连接器 GitHub").fetchSemanticsNodes().isNotEmpty() }
        // Only the connectors it contributes.
        rule.onNodeWithText("连接器 Telegram").assertDoesNotExist()
        rule.onNodeWithText("GitHub 的新条目进收件箱。和连接器页上的「进收件箱」是同一个开关。").assertExists()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            capture("on-${s.key}-${if (d) "dark" else "light"}")
        }

        rule.onNodeWithText("进收件箱").performClick()
        rule.waitUntil(5_000) { rule.onAllNodesWithText("不进收件箱：不再收新的，已有的条目留着。").fetchSemanticsNodes().isNotEmpty() }
        assertEquals(listOf("inbox" to false), feeds)
        style = AppStyle.Glass
        for (d in listOf(false, true)) { dark = d; capture("off-glass-${if (d) "dark" else "light"}") }

        rule.onNodeWithText("连接器设置").performClick()
        rule.waitForIdle()
        assertEquals("github", opened)
    }

    @Test fun noConnectorsServiceLeavesTheGroupOut() {
        val extensions = object : ExtensionApiStub() {
            override suspend fun extension(id: String) = detail
        }
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = true) { ExtensionPage(extensions, "github", {}) }
        }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("分拣规则").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("连接器 GitHub").assertDoesNotExist()
    }
}
