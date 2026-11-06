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
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File

/**
 * A market extension's install in steps (#3277): downloading with its bytes, stuck at the download with the reason and
 * "Retry", syncing to the computers, and the permissions it waits for. Every style, light and dark; the PNGs are in
 * build/extension-operation-shots.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1100dp-xhdpi")
class ExtensionOperationShotsTest {
    @get:Rule val rule = createComposeRule()

    private class Hub : FakeOperations() {
        // Kept for a page that subscribes after the push.
        val latest = MutableSharedFlow<ExtensionOperation?>(replay = 1)
        override val operationUpdates: Flow<ExtensionOperation?> get() = latest
    }

    private fun capture(name: String, node: SemanticsNodeInteraction = rule.onNodeWithTag("shot")) {
        rule.waitForIdle()
        val image = node.captureToImage().asAndroidBitmap()
        assertTrue(image.width > 0 && image.height > 0)
        val dir = File(System.getProperty("persona.shots") ?: "build/extension-operation-shots").also { it.mkdirs() }
        File(dir, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private val downloading = op("running", "download", 2, "download" to "running", "verify" to "pending", "unpack" to "pending",
        "check" to "pending", "consent" to "pending", "install" to "pending", "start" to "pending", "nodes" to "pending")
        .let { o -> o.copy(steps = o.steps.map { if (it.id == "download") it.copy(bytes = 6_291_456, total = 16_777_216) else it }) }
    private val stuck = downloading.copy(state = "failed", updatedAt = "2026-11-06T08:00:40Z", error = "下载 example-notes-1.2.0.tar.gz：30 秒没有收到数据",
        errorCode = "download_stalled", retryable = true, steps = downloading.steps.map { if (it.id == "download") it.copy(state = "failed") else it })
    private val syncing = op("running", "nodes", 50, "download" to "done", "verify" to "done", "unpack" to "done", "check" to "done",
        "consent" to "done", "install" to "done", "start" to "done", "nodes" to "running")
        .let { o -> o.copy(steps = o.steps.map {
            when (it.id) {
                "download" -> it.copy(cached = true)
                "nodes" -> it.copy(machines = ExtensionOpMachines(1, 3, mapOf("mac-b" to "离线，上线后会自动同步")))
                else -> it
            }
        }) }

    @Test fun theStepsInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        val api = Hub()
        api.started = downloading
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) {
                    ExtensionCatalogPage(api, "m1", "example-notes", onBack = {}, onInstalled = {})
                }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("安装").fetchSemanticsNodes().isNotEmpty() }
        rule.onAllNodesWithText("安装").filterToOne(hasClickAction()).performClick()
        rule.waitUntil { rule.onAllNodesWithText("6.0 MB / 16 MB").fetchSemanticsNodes().isNotEmpty() }
        for ((name, o) in listOf("downloading" to downloading, "stuck" to stuck, "syncing" to syncing)) {
            api.latest.tryEmit(o)
            rule.waitUntil { rule.onAllNodesWithText(OperationText.line(androidx.test.core.app.ApplicationProvider
                .getApplicationContext<android.content.Context>().resources, o)).fetchSemanticsNodes().isNotEmpty() }
            if (o === stuck) rule.onNodeWithText("重试").assertExists()
            for (s in AppStyle.entries) for (d in listOf(false, true)) {
                style = s; dark = d
                capture("$name-${s.key}-${if (d) "dark" else "light"}")
            }
            style = AppStyle.Glass; dark = false
        }
        // "Retry" goes on from the download.
        api.latest.tryEmit(stuck.copy(updatedAt = "2026-11-06T08:00:59Z"))
        rule.waitUntil { rule.onAllNodesWithText("重试").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("重试").performClick()
        rule.waitUntil { "retry op1" in api.calls }
    }

    @Test fun thePermissionsItWaitsFor() {
        var dark by mutableStateOf(false)
        val api = Hub()
        api.started = op("needs_consent", "consent", 3, "download" to "done", "verify" to "done", "unpack" to "done", "check" to "done",
            "consent" to "pending", "install" to "pending")
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) {
                    ExtensionCatalogPage(api, "m1", "example-notes", onBack = {}, onInstalled = {})
                }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("安装").fetchSemanticsNodes().isNotEmpty() }
        rule.onAllNodesWithText("安装").filterToOne(hasClickAction()).performClick()
        rule.waitUntil { rule.onAllNodesWithText("同意并安装").fetchSemanticsNodes().isNotEmpty() }
        for (d in listOf(false, true)) {
            dark = d
            capture("consent-glass-${if (d) "dark" else "light"}", rule.onNode(isDialog()))
        }
        rule.onNodeWithText("同意并安装").performClick()
        rule.waitUntil { api.calls.any { it.startsWith("agree pv1") } }
    }
}
