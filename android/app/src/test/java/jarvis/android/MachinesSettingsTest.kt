package jarvis.android

import jarvis.android.data.ApiClient
import jarvis.android.data.JsonCodec
import jarvis.android.data.Machine
import jarvis.android.data.MachineSystem
import jarvis.android.settings.ChartMath
import jarvis.android.settings.ClaudeSettingsBody
import jarvis.android.settings.DeviceTokens
import jarvis.android.settings.DoneMode
import jarvis.android.settings.MachineMetricsBody
import jarvis.android.settings.MetricFormat
import jarvis.android.settings.MetricLevel
import jarvis.android.settings.MetricPoint
import jarvis.android.settings.MetricRange
import jarvis.android.settings.NotifyPrefs
import jarvis.android.settings.OSFamily
import jarvis.android.settings.SettingsPrefs
import jarvis.android.settings.StorageKind
import jarvis.android.settings.StorageUsage
import jarvis.android.settings.claudeSettings
import jarvis.android.settings.machineMetrics
import jarvis.android.settings.nearestPoint
import jarvis.android.settings.osName
import jarvis.android.settings.revokeDeviceToken
import jarvis.android.settings.segments
import jarvis.android.settings.setClaudeSetting
import jarvis.android.settings.setNotifyPrefs
import jarvis.android.settings.storageText
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.ZoneOffset

class ChartGapTest {
    private fun p(t: Long, cpu: Double? = 1.0) = MetricPoint(t = t, cpu = cpu)
    private fun gap(t: Long) = MetricPoint(t = t, gap = true)

    @Test fun splitsAtEveryGapMarker() {
        val points = listOf(p(0), p(10), gap(20), p(60), p(70), p(80), gap(90), p(200))
        val segs = segments(points) { it.cpu }
        assertEquals(3, segs.size)
        assertEquals(listOf(0L, 10L), segs[0].times.toList())
        assertEquals(listOf(60L, 70L, 80L), segs[1].times.toList())
        assertEquals(listOf(200L), segs[2].times.toList())
    }

    @Test fun missingValuesAreSkippedWithoutBreakingTheLine() {
        val segs = segments(listOf(p(0), p(10, cpu = null), p(20))) { it.cpu }
        assertEquals(1, segs.size)
        assertEquals(listOf(0L, 20L), segs[0].times.toList())
    }

    @Test fun leadingTrailingAndDoubleGapsMakeNoEmptySegments() {
        val segs = segments(listOf(gap(0), gap(5), p(10), gap(20), gap(30))) { it.cpu }
        assertEquals(1, segs.size)
        assertTrue(segments(listOf(gap(0))) { it.cpu }.isEmpty())
        assertTrue(segments(emptyList()) { it.cpu }.isEmpty())
    }

    @Test fun scrubFindsTheNearestPointButNotAcrossAGap() {
        val points = listOf(p(0), p(10), gap(20), p(100))
        assertEquals(10L, nearestPoint(points, 13, step = 10)?.t)
        assertEquals(10L, nearestPoint(points, 30, step = 10)?.t) // two steps away still counts
        assertNull(nearestPoint(points, 55, step = 10)) // in the gap: nothing
        assertNull(nearestPoint(listOf(gap(0)), 0, step = 10))
    }

    @Test fun decodesTheHubsGapMarkers() {
        val body = """{"metrics":{"machine_id":"m","online":true,"points":[{"t":1,"cpu":5.5,"mem":1024},{"t":2,"gap":true},
            {"t":3,"cpu":7}],"latest":{"t":3,"cpu":7,"mem_used":50,"mem_total":200,"disk_used":1,"disk_total":4}}}"""
        val m = JsonCodec.decodeFromString(MachineMetricsBody.serializer(), body).metrics
        assertEquals(3, m.points.size)
        assertTrue(m.points[1].isGap)
        assertEquals(2, segments(m.points) { it.cpu }.size)
        assertEquals(25.0, m.latest!!.memPercent!!, 1e-9)
        assertEquals(25.0, m.latest!!.diskPercent!!, 1e-9)
        assertNull(m.latest!!.swapPercent)
    }

    @Test fun yTicksAreRoundAndBelowTheTop() {
        assertEquals(listOf(0.0, 50.0, 100.0), ChartMath.yTicks(100.0, 2))
        val t = ChartMath.yTicks(7.3)
        assertEquals(0.0, t.first(), 0.0)
        assertTrue(t.all { it <= 7.3 })
        assertEquals(listOf(0.0), ChartMath.yTicks(0.0))
        assertTrue(ChartMath.niceMax(10.0) > 10.0)
    }

    @Test fun byteTicksAreRoundInTheirUnit() {
        val mb = 1024.0 * 1024
        assertEquals(listOf(0.0, 2 * mb, 4 * mb), ChartMath.yTicks(4.4 * mb, binary = true))
        assertEquals(listOf("0", "2 MB/s", "4 MB/s"), ChartMath.yTicks(4.4 * mb, binary = true).map { MetricFormat.axisRate(it) })
        assertEquals("1.5 KB/s", MetricFormat.axisRate(1536.0))
        assertEquals("512 B/s", MetricFormat.axisRate(512.0))
    }

    @Test fun timeTicksAreRoundLocalTimes() {
        val start = 1_700_000_000L
        val ticks = ChartMath.timeTicks(start, start + 3600, zone = ZoneOffset.ofHours(8))
        assertTrue(ticks.size in 1..4)
        assertTrue(ticks.all { it in start..start + 3600 })
        assertTrue(ticks.all { (it + 8 * 3600) % 900 == 0L }) // whole quarter hours at least
        assertTrue(ChartMath.timeTicks(10, 10).isEmpty())
    }

    @Test fun monotoneCurveDoesNotOvershoot() {
        val xs = doubleArrayOf(0.0, 1.0, 2.0, 3.0)
        val ys = doubleArrayOf(0.0, 100.0, 100.0, 0.0)
        val m = ChartMath.monotoneTangents(xs, ys)
        assertEquals(0.0, m[1], 0.0) // flat between two equal values: no bump over 100
        assertEquals(0.0, m[2], 0.0)
    }

    @Test fun rangesStepsAndLevels() {
        assertEquals(listOf("1h", "6h", "24h"), MetricRange.entries.map { it.wire })
        assertEquals(240L, MetricRange.DAY.step)
        assertEquals(MetricLevel.CRITICAL, MetricLevel.of(95.0, 75.0, 90.0))
        assertEquals(MetricLevel.WARNING, MetricLevel.of(80.0, 75.0, 90.0))
        assertEquals(MetricLevel.NORMAL, MetricLevel.of(null, 75.0, 90.0))
    }

    @Test fun formats() {
        assertEquals("1.00 GB", MetricFormat.bytes(1024.0 * 1024 * 1024))
        assertEquals("512 B", MetricFormat.bytes(512.0))
        assertEquals("4.5%", MetricFormat.percent(4.5))
        assertEquals("37%", MetricFormat.percent(37.2))
        assertEquals("—", MetricFormat.rate(null))
        assertEquals("3 天 4 小时", MetricFormat.uptime(0, (3 * 86400 + 4 * 3600 + 5) * 1000L))
        assertEquals("1.2 MB", storageText(1_200_000))
    }
}

class SettingsPrefsTest {
    @After fun reset() = SettingsPrefs.setNotify(true, DoneMode.LONG)

    @Test fun parsesDevicePrefsIgnoringTheIphoneOnlyFields() {
        val p = JsonCodec.decodeFromString(
            NotifyPrefs.serializer(),
            """{"input":false,"done":"always","live_activity":true,"lock_details":false,"island":{"preset":"a"},"long_turn_seconds":90,"done_delay_seconds":60}""",
        )
        assertFalse(p.input)
        assertEquals(DoneMode.ALWAYS, p.doneMode)
        assertEquals(90, p.longTurnSeconds)
        assertEquals("90 秒", NotifyPrefs.duration(p.longTurnSeconds))
        assertEquals("1 分钟", NotifyPrefs.duration(null))
    }

    @Test fun unknownOrMissingDoneIsLongLikeTheHub() {
        assertEquals(DoneMode.LONG, JsonCodec.decodeFromString(NotifyPrefs.serializer(), "{}").doneMode)
        assertEquals(DoneMode.LONG, DoneMode.of("sometimes"))
        assertTrue(JsonCodec.decodeFromString(NotifyPrefs.serializer(), "{}").input)
    }

    @Test fun doneNotificationsFollowTheChoice() {
        SettingsPrefs.setNotify(true, DoneMode.OFF)
        assertFalse(SettingsPrefs.wantsDone(10 * 60_000L))
        SettingsPrefs.setNotify(true, DoneMode.ALWAYS)
        assertTrue(SettingsPrefs.wantsDone(1_000L))
        SettingsPrefs.setNotify(true, DoneMode.LONG)
        assertFalse(SettingsPrefs.wantsDone(5_000L))
        assertTrue(SettingsPrefs.wantsDone(SettingsPrefs.longTurnSeconds * 1000L))
        assertTrue(SettingsPrefs.wantsDone(null)) // a turn not seen from its start counts as long
    }

    @Test fun claudeSettingsDefaultsAndValues() {
        val list = JsonCodec.decodeFromString(
            ClaudeSettingsBody.serializer(),
            """{"settings":[{"key":"awaySummaryEnabled","title":"离开回顾","type":"bool","default":true,"value":null},
              {"key":"effortLevel","type":"enum","options":["low","high"],"default":"high","value":"low"},
              {"key":"language","type":"string","default":""}]}""",
        ).settings
        assertTrue(list[0].isDefault)
        assertTrue(list[0].boolValue)
        assertFalse(list[1].isDefault)
        assertEquals("low", list[1].stringValue)
        assertEquals("未设置", list[2].placeholder)
    }

    @Test fun devicesDecodeWithRevokedOnes() {
        val d = JsonCodec.decodeFromString(
            DeviceTokens.serializer(),
            """{"tokens":[{"id":"dev_a","name":"","kind":"phone","created_at":"2026-10-01T00:00:00Z","revoked_at":"0001-01-01T00:00:00Z"},
              {"id":"dev_b","name":"Watch","kind":"watch","role":"answer","revoked_at":"2026-10-02T00:00:00Z"}],"current":"dev_a"}""",
        )
        assertEquals("dev_a", d.current)
        assertFalse(d.tokens[0].revoked) // Go's zero time is "not revoked"
        assertEquals("dev_a", d.tokens[0].title)
        assertTrue(d.tokens[1].revoked)
    }

    @Test fun osFamiliesAndNames() {
        assertEquals(OSFamily.UBUNTU, OSFamily.of(MachineSystem(os = "linux", distro = "ubuntu")))
        assertEquals(OSFamily.DEBIAN, OSFamily.of(MachineSystem(os = "linux", distro = "raspbian")))
        assertEquals(OSFamily.UBUNTU, OSFamily.of(MachineSystem(os = "linux", distro = "foo", like = "ubuntu debian")))
        assertEquals(OSFamily.MACOS, OSFamily.of(null, "darwin"))
        assertEquals(OSFamily.LINUX, OSFamily.of(MachineSystem(os = "linux")))
        assertEquals("macOS", osName(Machine(id = "m", os = "darwin")))
        assertEquals("Ubuntu 24.04", osName(Machine(id = "m", os = "linux", system = MachineSystem(name = "Ubuntu 24.04"))))
    }

    @Test fun storageKindsByFolder() {
        assertEquals(StorageKind.NETWORK, StorageUsage.kindOfCacheEntry("http-cache"))
        assertEquals(StorageKind.WEB, StorageUsage.kindOfCacheEntry("WebView"))
        assertEquals(StorageKind.OTHER, StorageUsage.kindOfCacheEntry("tmp123"))
        assertFalse(StorageKind.APP_DATA.clearable)
    }
}

class SettingsApiTest {
    private val server = MockWebServer().apply { start() }
    private val api = ApiClient(server.url("/").toString(), "tok")

    @After fun tearDown() { runCatching { server.shutdown() } }

    private fun json(body: String) = MockResponse().setHeader("Content-Type", "application/json").setBody(body)

    @Test fun notifyPrefsSendOnlyInputAndDone() = runBlocking {
        server.enqueue(json("""{"input":true,"done":"off","live_activity":true}"""))
        val p = api.setNotifyPrefs("android-1", true, DoneMode.OFF)
        assertEquals(DoneMode.OFF, p.doneMode)
        val r = server.takeRequest()
        assertEquals("PUT", r.method)
        assertEquals("/api/v1/devices/android-1/prefs", r.path)
        assertEquals("""{"input":true,"done":"off"}""", r.body.readUtf8())
    }

    @Test fun claudeSettingBackToDefaultSendsNull() = runBlocking {
        server.enqueue(json("""{"settings":[]}"""))
        api.setClaudeSetting("m1", "language", JsonNull)
        assertEquals("""{"key":"language","value":null}""", server.takeRequest().body.readUtf8())
        server.enqueue(json("""{"settings":[{"key":"alwaysThinkingEnabled","type":"bool","default":false,"value":true}]}"""))
        val list = api.setClaudeSetting("m1", "alwaysThinkingEnabled", JsonPrimitive(true))
        assertEquals("""{"key":"alwaysThinkingEnabled","value":true}""", server.takeRequest().body.readUtf8())
        assertTrue(list.single().boolValue)
        server.enqueue(json("""{"settings":[]}"""))
        api.claudeSettings("m1")
        assertEquals("/api/v1/machines/m1/claude-settings", server.takeRequest().path)
    }

    @Test fun metricsAskForTheRange() = runBlocking {
        server.enqueue(json("""{"metrics":{"machine_id":"m1","points":[]},"range":"6h","step":60}"""))
        assertEquals("m1", api.machineMetrics("m1", MetricRange.SIX_HOURS).machineId)
        assertEquals("/api/v1/machines/m1/metrics?range=6h", server.takeRequest().path)
    }

    @Test fun connectReadsTheHubsPrefsAndTheNotifierFollowsThem() = runBlocking {
        try {
            server.enqueue(json("""{"input":false,"done":"off","live_activity":true,"long_turn_seconds":120}"""))
            SettingsPrefs.sync(api)
            val r = server.takeRequest()
            assertEquals("GET", r.method)
            assertEquals("/api/v1/devices/android/prefs", r.path) // before init: the fallback id
            assertFalse(SettingsPrefs.notifyInput.value)
            assertEquals(DoneMode.OFF, SettingsPrefs.doneMode.value)
            assertEquals(120, SettingsPrefs.longTurnSeconds)
            assertFalse(SettingsPrefs.wantsDone(null))
        } finally {
            SettingsPrefs.apply(NotifyPrefs(longTurnSeconds = 60))
        }
    }

    @Test fun aChangeAppliesAtOnceAndGoesToTheHub() = runBlocking {
        server.enqueue(json("""{"input":true,"done":"always"}"""))
        SettingsPrefs.change(api, true, DoneMode.ALWAYS)
        assertEquals(DoneMode.ALWAYS, SettingsPrefs.doneMode.value)
        assertEquals("""{"input":true,"done":"always"}""", server.takeRequest().body.readUtf8())
        SettingsPrefs.setNotify(true, DoneMode.LONG)
    }

    @Test fun aHubOutOfReachLeavesTheSwitchesAlone() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(500).setBody("""{"error":"boom"}"""))
        SettingsPrefs.setNotify(false, DoneMode.ALWAYS)
        SettingsPrefs.sync(api)
        assertFalse(SettingsPrefs.notifyInput.value)
        assertEquals(DoneMode.ALWAYS, SettingsPrefs.doneMode.value)
        SettingsPrefs.setNotify(true, DoneMode.LONG)
    }

    @Test fun revokeDeletesTheToken() = runBlocking {
        server.enqueue(json("""{"ok":true}"""))
        api.revokeDeviceToken("dev_x")
        val r = server.takeRequest()
        assertEquals("DELETE", r.method)
        assertEquals("/api/v1/auth/tokens/dev_x", r.path)
    }
}

class SettingsRouteTest {
    @Test fun pathsRoundTrip() {
        val all = listOf(
            jarvis.android.ui.settings.SettingsRoute.Machines,
            jarvis.android.ui.settings.SettingsRoute.Machine("mac-1"),
            jarvis.android.ui.settings.SettingsRoute.Monitor("mac-1"),
            jarvis.android.ui.settings.SettingsRoute.Claude,
            jarvis.android.ui.settings.SettingsRoute.Devices,
            jarvis.android.ui.settings.SettingsRoute.Notifications,
            jarvis.android.ui.settings.SettingsRoute.Storage,
            jarvis.android.ui.settings.SettingsRoute.Style,
        )
        all.forEach { assertEquals(it, jarvis.android.ui.settings.SettingsRoute.of(it.path)) }
        assertNull(jarvis.android.ui.settings.SettingsRoute.of("machine/"))
        assertNull(jarvis.android.ui.settings.SettingsRoute.of("nope"))
    }

    @Test fun machinesDecodeTheirSystem() {
        val m = JsonCodec.decodeFromString(
            Machine.serializer(),
            """{"id":"m","online":true,"arch":"arm64","node_version":"v1","system":{"os":"darwin","distro":"macos","name":"macOS 26","cores":20,"memory":68719476736,"boot_time":1}}""",
        )
        assertEquals("arm64", m.arch)
        assertEquals(20, m.system?.cores)
        assertEquals(OSFamily.MACOS, OSFamily.of(m.system))
    }
}
