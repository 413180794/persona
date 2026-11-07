package persona.platform.shortcuts

import android.content.Context
import android.graphics.Bitmap
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.FleetState
import persona.core.contracts.Interaction
import persona.core.contracts.Question
import persona.core.designsystem.AppStyle
import java.io.File

/**
 * "Approve first pending" in each language (docs/protocol/i18n.md), every state, Glass light and dark; the widget and the tile are drawn by
 * the launcher and the shade, so their words are checked instead. PNGs go to `build/i18n-shots/` (or
 * `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ApproveFirstShotsTest {
    @get:Rule val rule = createComposeRule()
    private val context: Context get() = ApplicationProvider.getApplicationContext()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
    @Test fun simplifiedChinese() {
        shots("zh", "批准第一个待处理", "允许", "补齐网络层的单元测试 · MacBook")
        surfaces("还没配对", "都处理完了", "打开 Persona 扫 hub 的二维码", "问 Persona", "批准待处理")
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
    @Test fun english() {
        shots("en", "Approve first pending", "Allow", "Add network layer unit tests · MacBook")
        surfaces("Not paired", "All caught up", "Open Persona and scan the hub's QR code", "Ask Persona", "Approve pending")
    }

    @Config(sdk = [35], qualifiers = "ja-w400dp-h800dp-xhdpi")
    @Test fun japanese() {
        shots("ja", "最初の保留中を承認", "許可", "ネットワーク層の単体テストを追加 · MacBook")
        surfaces("未ペアリング", "すべて対応済み", "Personaを開いてハブのQRコードをスキャン", "Personaに質問", "保留中を承認")
    }

    private fun shots(language: String, title: String, allow: String, where: String) {
        val permission = Interaction("permission", "macbook", "tests", kind = "permission", toolName = "Bash",
            summary = "Bash: swift test --filter NetworkTests")
        val plan = Interaction("plan", "studio", "photos", kind = "plan", plan = "## HEIC\n\n1. sips HEIC → JPEG\n2. EXIF")
        val states = listOf(
            "loading" to ApproveState.Loading, "unpaired" to ApproveState.NotPaired, "nothing" to ApproveState.Nothing,
            "question" to ApproveState.NeedsApp(permission.copy(kind = "question", questions = listOf(Question("?"))), where),
            "permission" to ApproveState.Confirm(permission, where), "plan" to ApproveState.Confirm(plan, where),
            "error" to ApproveState.Failed(ShortcutText.loadFailure(context.resources, java.net.ConnectException("refused"))),
        )
        var state by mutableStateOf<ApproveState>(ApproveState.Loading)
        var dark by mutableStateOf(false)
        rule.setContent { ShortcutTheme(AppStyle.Glass, dark) { ApproveFirstDialog(state, {}, {}, {}) } }
        for ((name, value) in states) for (d in listOf(false, true)) {
            state = value; dark = d
            rule.waitForIdle()
            rule.onNodeWithText(title).assertExists()
            if (name == "permission") rule.onNodeWithText(allow).assertExists()
            val bitmap = rule.onNode(isDialog()).captureToImage().asAndroidBitmap()
            File(out(), "approve-first-$language-$name-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    /** The tile's second line, the widget's unpaired hint and the launcher shortcuts' labels. */
    private fun surfaces(notPaired: String, allDone: String, scan: String, ask: String, approve: String) {
        assertEquals(notPaired, ShortcutText.tileSubtitle(context.resources, false, FleetState()))
        assertEquals(allDone, ShortcutText.tileSubtitle(context.resources, true, FleetState()))
        assertEquals(scan, context.getString(R.string.shortcuts_widget_scan))
        assertEquals(ask, context.getString(R.string.shortcuts_ask_label))
        assertEquals(approve, context.getString(R.string.shortcuts_approve_label))
    }
}
