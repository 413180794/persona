package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.ExtensionAuthEvent
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File

/**
 * An extension's command-line tools and sign-in (#4507): the install's "Install command-line tools" step, the detail's "Sign in" section signed
 * out and signed in, and the device-code sign-in sheet. Every style, light and dark; the PNGs are in
 * build/extension-auth-shots.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1100dp-xhdpi")
class ExtensionAuthShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun capture(name: String, node: SemanticsNodeInteraction = rule.onNodeWithTag("shot")) {
        rule.waitForIdle()
        val image = node.captureToImage().asAndroidBitmap()
        assertTrue(image.width > 0 && image.height > 0)
        val dir = File("build/extension-auth-shots").also { it.mkdirs() }
        File(dir, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private fun everyStyle(name: String, set: (AppStyle, Boolean) -> Unit, node: () -> SemanticsNodeInteraction = { rule.onNodeWithTag("shot") }) {
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            set(s, d)
            capture("$name-${s.key}-${if (d) "dark" else "light"}", node())
        }
        set(AppStyle.Glass, false)
    }

    private val tools = op("running", "tools", 4, "download" to "done", "verify" to "done", "unpack" to "done", "check" to "done",
        "consent" to "done", "tools" to "running", "install" to "pending", "start" to "pending")
        .let { o -> o.copy(steps = o.steps.map {
            if (it.id == "tools") it.copy(bytes = 12_582_912, total = 31_457_280, tools = listOf(
                ExtensionOpTool("node", "done", reused = true), ExtensionOpTool("example-cli", "running"), ExtensionOpTool("jq", "pending"))) else it
        }) }

    @Test fun theToolsStep() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        val api = object : FakeOperations() {
            val latest = MutableSharedFlow<ExtensionOperation?>(replay = 1)
            override val operationUpdates: Flow<ExtensionOperation?> get() = latest
        }
        api.started = tools
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) {
                    ExtensionCatalogPage(api, "m1", "example-notes", onBack = {}, onInstalled = {})
                }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("安装").fetchSemanticsNodes().isNotEmpty() }
        rule.onAllNodesWithText("安装").filterToOne(hasClickAction()).performClick()
        rule.waitUntil { rule.onAllNodesWithText("12 MB / 30 MB").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("用这台机器上的").assertExists()
        everyStyle("tools", { s, d -> style = s; dark = d })
        val failed = tools.copy(state = "failed", updatedAt = "2026-11-06T08:00:30Z", errorCode = "tools_failed", retryable = true,
            error = "example-cli 0.59.0：校验不对（sha512 不一致）",
            steps = tools.steps.map { s -> if (s.id == "tools") s.copy(state = "failed", tools = s.tools!!.map { if (it.name == "example-cli") it.copy(state = "failed") else it }) else s })
        api.latest.tryEmit(failed)
        rule.waitUntil { rule.onAllNodesWithText("命令行没装上：example-cli 0.59.0：校验不对（sha512 不一致）").fetchSemanticsNodes().isNotEmpty() }
        capture("tools-failed-glass-light")
    }

    private class AuthHub(var auth: ExtensionAuth) : ExtensionApiStub() {
        val pushes = MutableSharedFlow<ExtensionAuthEvent>(extraBufferCapacity = 4)
        val steps = mutableListOf<String>()
        override val authUpdates: Flow<ExtensionAuthEvent> get() = pushes
        override suspend fun extension(id: String) = ExtensionDetail(
            HubExtension(id, "CIS 查询", "ui", "running", version = "1.4.0", source = "installed", enabled = true, auth = auth,
                tools = listOf(ExtensionToolView("node", "Node.js", "22.23.4", reused = true), ExtensionToolView("example-cli", null, "0.59.0"))),
            publisher = "Example", ui = listOf(ExtensionUiPlace(id, "login", "登录", "extension_page"), ExtensionUiPlace(id, "search", "查询", "extension_page")))
        override suspend fun authStep(id: String, step: String, values: Map<String, String>): BindPrompt {
            steps += step
            return BindPrompt(step = "wait", kind = "device_code", url = "https://login.example.com/device?code=WDJB-MJHT", userCode = "WDJB-MJHT",
                interval = 30, qr = BindQrCode(25, qrRows(25)), hint = "用飞书确认身份后才能查")
        }
        override suspend fun authLogout(id: String) = auth.copy(state = "signed_out", account = null)
    }

    @Test fun theSignInSectionAndTheSheet() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        val api = AuthHub(ExtensionAuth("飞书登录", "用飞书确认身份后才能查 CIS。", page = "login", state = "signed_out"))
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) {
                    ExtensionPage(api, "example-cis", onBack = {}, onPage = {})
                }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("未登录").fetchSemanticsNodes().isNotEmpty() }
        // The extension's own sign-in page is for older apps.
        rule.onNodeWithText("查询").assertExists()
        rule.onAllNodesWithText("登录").filterToOne(hasClickAction()).assertExists()
        everyStyle("signed-out", { s, d -> style = s; dark = d })

        api.pushes.tryEmit(ExtensionAuthEvent("example-cis", api.auth.copy(state = "signed_in", account = "me@example.com")))
        rule.waitUntil { rule.onAllNodesWithText("已登录：me@example.com").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("重新登录").assertExists()
        rule.onNodeWithText("退出登录").assertExists()
        everyStyle("signed-in", { s, d -> style = s; dark = d })

        rule.onNodeWithText("重新登录").performClick()
        rule.waitUntil { rule.onAllNodesWithText("WDJB-MJHT").fetchSemanticsNodes().isNotEmpty() }
        assertTrue(api.steps == listOf(""))
        everyStyle("device-code", { s, d -> style = s; dark = d }, node = { rule.onNode(isDialog()) })
    }
}

/** A QR-like pattern (finder squares in three corners, noise elsewhere): enough to see how the sheet draws one. */
internal fun qrRows(n: Int): List<String> = List(n) { y ->
    buildString {
        for (x in 0 until n) {
            fun finder(fx: Int, fy: Int): Boolean? {
                val dx = x - fx; val dy = y - fy
                if (dx !in 0..6 || dy !in 0..6) return null
                return dx == 0 || dx == 6 || dy == 0 || dy == 6 || (dx in 2..4 && dy in 2..4)
            }
            val on = finder(0, 0) ?: finder(n - 7, 0) ?: finder(0, n - 7) ?: (((x * 31 + y * 17 + x * y) % 7) < 3)
            append(if (on) '1' else '0')
        }
    }
}
