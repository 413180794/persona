package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File
import java.time.Duration
import java.time.Instant

/**
 * "Quota" on a launcher's page drawn on the JVM with Robolectric's native graphics: a full report (a budget, an account
 * cooling down with a used-up window, a provider's models unfolded, the key's use) and the other states (reading, no
 * extension knows the gateway, the read failed), in every style, light and dark. Written as PNGs to
 * `build/launcher-quota-shots/` (or `-Dpersona.shots=<dir>`) to look at. The gateways are the mock hub's made-up ones.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h2400dp-xhdpi")
class LauncherQuotaShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun later(hours: Long) = Instant.now().plus(Duration.ofHours(hours)).toString()
    private fun w(label: String, ratio: Double?, used: Double? = null, limit: Double? = null, unit: String? = null, resets: String? = null, exhausted: Boolean = false) =
        QuotaWindow(label, ratio, used, limit, unit, resets, exhausted)

    private val report = LauncherQuotaAnswer(machine = "mac", checkedAt = Instant.now().minusSeconds(120).toString(), quota = LauncherQuota(
        gateway = "example-gateway", gatewayName = "示例网关", keyName = "cc_main",
        budget = listOf(w("每周预算", 0.62, 496.0, 800.0, "usd", later(60))),
        pools = listOf(
            QuotaPool("账号 A", "available", "可用", listOf(w("每月", 0.64, 6401.0, 10000.0, "credits", later(11)))),
            QuotaPool("账号 B · 订阅", "cooldown", "冷却中，23:00 恢复", listOf(w("5 小时", 1.0, resets = later(2), exhausted = true), w("每周", 0.77, resets = later(70)))),
            QuotaPool("供应商 A · 编码套餐", "ok", windows = listOf(w("5 小时额度", 0.42, 42.0, 100.0, "percent", later(3)), w("每周额度", 0.86, 86.0, 100.0, "percent", later(50))),
                models = listOf(QuotaModelWindows("example-flash", listOf(w("TPM", 0.02, 8400.0, 500000.0, "tokens"))))),
            QuotaPool("供应商 B", "unsupported", "上游没有额度接口，只有网关统计"),
        ),
        usage = listOf(QuotaUsage("今日", 287, 210552323, 123.12), QuotaUsage("本周", 10432, 3359748900, 1109.02), QuotaUsage("本月", 20004, 6012000000, 2190.4)),
    ))

    private val others = listOf(
        LauncherQuotaState(loading = true),
        LauncherQuotaState(LauncherQuotaAnswer(machine = "mac", error = "这个网关没有能查的额度：没有装能读它的额度扩展", unsupported = true)),
        LauncherQuotaState(failed = ResText.of(R.string.settings_src_quota_read_failed, "网关超时")),
        LauncherQuotaState(LauncherQuotaAnswer(machine = "mac", quota = LauncherQuota("example-relay", gatewayName = "示例中转",
            usage = listOf(QuotaUsage("今日", 3, 1200))))),
    )

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/launcher-quota-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun drawsTheReportAndTheOtherStatesInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var full by mutableStateOf(true)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background)) {
                    LazyColumn(Modifier.fillMaxWidth(), contentPadding = PaddingValues(horizontal = Tokens.Space.l, vertical = Tokens.Space.l)) {
                        val name = { id: String -> if (id == "mac") "我的 Mac" else id }
                        if (full) launcherQuotaItems(LauncherQuotaState(report), name, onRefresh = {})
                        else others.forEachIndexed { i, state -> launcherQuotaItems(state, name, onRefresh = {}, key = "q$i") }
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            full = true
            rule.waitForIdle()
            // the provider's models unfolded
            if (rule.onAllNodesWithText("example-flash").fetchSemanticsNodes().isEmpty()) rule.onNodeWithText("按模型 1").performClick()
            capture("launcher-quota-${s.name.lowercase()}-${if (d) "dark" else "light"}")
            full = false
            capture("launcher-quota-states-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }
}
