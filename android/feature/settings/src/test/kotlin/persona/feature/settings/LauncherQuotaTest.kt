package persona.feature.settings

import android.content.Context
import android.content.res.Resources
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonElement
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Machine
import persona.core.contracts.api.*
import persona.core.designsystem.Tone
import java.io.IOException
import java.time.Instant
import java.time.ZoneId

/** "Quota" on a launcher's page (docs/integrations/launchers.md "Quota"): numbers read as on iOS and the web; reading, ↻ and a failed ↻. */
// Robolectric: the words come from the app's resources, in Simplified Chinese (robolectric.properties).
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class LauncherQuotaTest {
    private val res: Resources get() = ApplicationProvider.getApplicationContext<Context>().resources

    private fun w(label: String = "w", ratio: Double? = null, used: Double? = null, limit: Double? = null, unit: String? = null, exhausted: Boolean = false) =
        QuotaWindow(label, ratio, used, limit, unit, exhausted = exhausted)

    @Test fun amountsReadByTheirUnit() {
        assertEquals("$496 / $800", QuotaText.amount(res, w(ratio = 0.62, used = 496.0, limit = 800.0, unit = "usd")))
        assertEquals("$12.50 / $50.00", QuotaText.amount(res, w(used = 12.5, limit = 50.0, unit = "usd")))
        assertEquals("6401 / 1 万", QuotaText.amount(res, w(ratio = 0.64, used = 6401.0, limit = 10000.0, unit = "credits")))
        assertEquals("8400 / 50 万 token", QuotaText.amount(res, w(used = 8400.0, limit = 500000.0, unit = "tokens")))
        assertEquals("3 / 100 次", QuotaText.amount(res, w(used = 3.0, limit = 100.0, unit = "requests")))
        // percent, or no numbers: the ratio
        assertEquals("已用 42%", QuotaText.amount(res, w(ratio = 0.42, used = 42.0, limit = 100.0, unit = "percent")))
        assertEquals("已用完", QuotaText.amount(res, w(ratio = 1.0, exhausted = true)))
        assertEquals("已用完", QuotaText.amount(res, w(exhausted = true)))
        // exhausted below the top (a cooldown) still shows how much
        assertEquals("已用 80%", QuotaText.amount(res, w(ratio = 0.8, exhausted = true)))
        assertEquals("—", QuotaText.amount(res, w()))
    }

    @Test fun countsMoneyAndUsage() {
        assertEquals("287", QuotaText.count(res, 287.0))
        assertEquals("1.2 万", QuotaText.count(res, 12000.0))
        assertEquals("2.1 亿", QuotaText.count(res, 210552323.0))
        assertEquals("60.1 亿", QuotaText.count(res, 6.012e9))
        assertEquals("123", QuotaText.money(123.12))
        assertEquals("9.90", QuotaText.money(9.9))
        assertEquals("287 次 · 2.1 亿 token · $123", QuotaText.usage(res, QuotaUsage("今日", 287, 210552323, 123.12)))
        assertEquals("3 次 · 1200 token", QuotaText.usage(res, QuotaUsage("今日", 3, 1200)))
    }

    @Test fun tonesTitlesAndTrouble() {
        assertEquals(Tone.Neutral, QuotaText.tone(0.69, false))
        assertEquals(Tone.Caution, QuotaText.tone(0.7, false))
        assertEquals(Tone.Caution, QuotaText.tone(0.9, false))
        assertEquals(Tone.Caution, QuotaText.tone(1.0, false))
        assertEquals(Tone.Danger, QuotaText.tone(0.1, true))
        assertEquals(0, QuotaText.percent(-0.2))
        assertEquals(100, QuotaText.percent(1.4))
        assertEquals("额度", QuotaText.title(res, null))
        assertEquals("额度 · 示例网关", QuotaText.title(res, LauncherQuota("example-gateway", gatewayName = "示例网关")))
        // an older hub sends no name: the id
        assertEquals("额度 · example-gateway", QuotaText.title(res, LauncherQuota("example-gateway", gatewayName = " ")))
        assertFalse(QuotaText.trouble(QuotaPool("a")))
        assertFalse(QuotaText.trouble(QuotaPool("a", state = "unsupported")))
        assertTrue(QuotaText.trouble(QuotaPool("a", state = "cooldown")))
        assertTrue(QuotaText.unlimited(LauncherQuota("g", usage = listOf(QuotaUsage("今日")))))
        assertFalse(QuotaText.unlimited(LauncherQuota("g", note = "按次计费")))
    }

    @Test fun resetsAndTheFooter() {
        val now = Instant.parse("2026-11-04T06:00:00Z")
        val zone = ZoneId.of("Asia/Shanghai")
        assertEquals("11月6日 15:00 重置", QuotaText.reset(res, "2026-11-06T07:00:00Z", now, zone))
        assertEquals("11月6日 15:00 重置", QuotaText.reset(res, "2026-11-06T15:00:00+08:00", now, zone))
        assertEquals("已重置", QuotaText.reset(res, "2026-11-04T05:00:00Z", now, zone))
        assertEquals("", QuotaText.reset(res, "soon", now, zone))
        val answer = LauncherQuotaAnswer(machine = "mac", checkedAt = "2026-11-04T05:56:59.123456Z", quota = LauncherQuota("g", keyName = "cc_main"))
        assertEquals("网关上的 Key 名：cc_main；由 我的 Mac 读取，Key 不离开那台电脑；3 分钟前读的，一分钟内再打开用这次的。",
            QuotaText.footer(res, answer, { if (it == "mac") "我的 Mac" else it }, now))
        assertEquals("", QuotaText.footer(res, LauncherQuotaAnswer(quota = LauncherQuota("g")), { it }, now))
    }

    private class Api : MachineManagement {
        val calls = mutableListOf<String>()
        var fail = false
        override suspend fun launcherQuota(machineId: String, name: String, refresh: Boolean): LauncherQuotaAnswer {
            calls += "$machineId $name $refresh"
            if (fail) throw IOException("网关超时")
            return LauncherQuotaAnswer(machine = machineId, quota = LauncherQuota("g", note = "第 ${calls.size} 次"))
        }
        override suspend fun launchers(machineId: String) = emptyList<AgentLauncher>()
        override suspend fun reconnect(machineId: String) = Unit
        override suspend fun deploy(machineId: String) = Unit
        override suspend fun herdr(machineId: String): persona.core.contracts.api.HerdrState = error("unused")
        override suspend fun startHerdr(machineId: String): persona.core.contracts.api.HerdrState = error("unused")
        override suspend fun setEngine(machineId: String, engine: String) = Machine(machineId)
        override suspend fun claudeSettings(machineId: String) = emptyList<ClaudeSetting>()
        override suspend fun setClaudeSetting(machineId: String, key: String, value: JsonElement) = emptyList<ClaudeSetting>()
        override suspend fun codexSettings(machineId: String) = emptyList<ClaudeSetting>()
        override suspend fun setCodexSetting(machineId: String, key: String, value: JsonElement) = emptyList<ClaudeSetting>()
    }

    @Test fun readsThenReadsAgainAndKeepsTheAnswerWhenARefreshFails() = runBlocking {
        val api = Api()
        val model = LauncherQuotaModel(api, "mac~g1", "cc_main")
        model.load()
        assertEquals("第 1 次", model.state.value.answer?.quota?.note)
        assertFalse(model.state.value.loading)
        model.load(refresh = true)
        assertEquals(listOf("mac~g1 cc_main false", "mac~g1 cc_main true"), api.calls)
        api.fail = true
        model.load(refresh = true)
        assertEquals("第 2 次", model.state.value.answer?.quota?.note)
        assertTrue(model.state.value.failed!!.resolve(res).startsWith("读不到额度："))
        api.fail = false
        model.load(refresh = true)
        assertNull(model.state.value.failed?.resolve(res))
    }

    // A ↻ while the first read is still out is not lost: it reads again once that one is back.
    @Test fun aRefreshDuringARead() = runBlocking {
        val gate = kotlinx.coroutines.CompletableDeferred<Unit>()
        val calls = mutableListOf<Boolean>()
        val api = object : MachineManagement by Api() {
            override suspend fun launcherQuota(machineId: String, name: String, refresh: Boolean): LauncherQuotaAnswer {
                calls += refresh
                if (calls.size == 1) gate.await()
                return LauncherQuotaAnswer(quota = LauncherQuota("g", note = "${calls.size}"))
            }
        }
        val model = LauncherQuotaModel(api, "mac", "cc_main")
        val first = launch { model.load() }
        kotlinx.coroutines.yield()
        assertTrue(model.state.value.loading)
        model.load(refresh = true)
        gate.complete(Unit)
        first.join()
        assertEquals(listOf(false, true), calls)
        assertEquals("2", model.state.value.answer?.quota?.note)
        assertFalse(model.state.value.loading)
    }
}
