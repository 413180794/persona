package persona.feature.machines

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.JsonElement
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import persona.core.contracts.Machine
import persona.core.contracts.api.*
import java.time.Instant

/** 这台电脑能做什么 as features (web lib/node-health.ts) and 订阅额度's arithmetic (web lib/usage-windows.ts). */
@OptIn(ExperimentalCoroutinesApi::class)
class MachineHealthTest {
    @Before fun main() = kotlinx.coroutines.Dispatchers.setMain(UnconfinedTestDispatcher())
    @After fun reset() = kotlinx.coroutines.Dispatchers.resetMain()

    private fun check(name: String, status: String = "ok", state: String = "", hint: String = "") = DoctorCheck(name, status, state, "found $name", hint)

    @Test fun featuresTakeTheWorstOfTheirChecksAndOptionalOnesAreOff() {
        val report = DoctorReport(checks = listOf(check("node.sock"), check("claude-settings.json", "warn", "stale"), check("command-paths"),
            check("herdr", "warn"), check("runtime.sock", "warn"), check("mcp.json"), check("odd-one", "fail", hint = "do this")))
        val features = deriveFeatures(report, engine = "orca").associateBy { it.id }
        assertEquals(FeatureState.Partial, features.getValue("approvals").state)
        assertEquals(HealthNote.Res(R.string.machines_health_note_stale), features.getValue("approvals").note)
        assertEquals(FeatureState.Ready, features.getValue("status").state)
        assertEquals("not this computer's engine", FeatureState.Off, features.getValue("terminal").state)
        assertEquals(FeatureState.Broken, deriveFeatures(report, engine = "herdr").first { it.id == "terminal" }.state)
        assertEquals(FeatureState.Off, features.getValue("runtimes").state)
        assertFalse("no skill checks reported", "skills" in features)
        assertEquals(FeatureState.Broken, features.getValue("other").state)
        assertEquals(HealthNote.Plain("do this"), features.getValue("other").note)
        assertEquals(2, attentionCount(features.values.toList()))
        assertEquals(listOf("claude-settings.json"), fixableChecks(report).map { it.name })
    }

    @Test fun unavailableSelfChecksSayWhy() {
        assertEquals(DoctorUnavailable.OldNode, DoctorUnavailable.of(502, "unknown method node.doctor", true))
        assertEquals(DoctorUnavailable.OldHub, DoctorUnavailable.of(404, "HTTP 404", false))
        assertNull("an unknown machine is JSON", DoctorUnavailable.of(404, "unknown machine", true))
        assertEquals(DoctorUnavailable.Offline, DoctorUnavailable.of(503, "machine is offline", true))
        assertEquals(DoctorUnavailable.Forbidden, DoctorUnavailable.of(403, "forbidden", true))
    }

    @Test fun windowsLengthPaceResetAndLiveAccounts() {
        val now = Instant.parse("2026-11-05T12:00:00Z")
        assertEquals(300, windowMinutes(UsageWindow("5h", 0)))
        assertEquals(10080, windowMinutes(UsageWindow("7d", 0)))
        assertEquals(45, windowMinutes(UsageWindow("weekly", 45)))
        // 5h window, 1h left: 80% elapsed
        val w = UsageWindow("5h", 300, 95.0, "2026-11-05T13:00:00Z")
        assertEquals(PaceState.Over, pace(w, now)!!.state)
        assertEquals(PaceState.Under, pace(w.copy(usedPercent = 50.0), now)!!.state)
        assertNull("quiet start", pace(w.copy(resetsAt = "2026-11-05T16:59:00Z"), now))
        assertEquals(ResetIn.Minutes(30), resetIn("2026-11-05T12:30:00Z", now, java.time.ZoneOffset.UTC))
        assertEquals(ResetIn.Clock(Instant.parse("2026-11-05T15:00:00Z")), resetIn("2026-11-05T15:00:00Z", now, java.time.ZoneOffset.UTC))
        assertEquals(ResetIn.DaysHours(2, 3), resetIn("2026-11-07T15:00:00Z", now, java.time.ZoneOffset.UTC))
        assertEquals(ResetIn.Due, resetIn("2026-11-05T11:00:00Z", now))
        val old = UsageAccount("old", sampledAt = "2026-11-01T00:00:00Z", windows = listOf(UsageWindow("7d", 0, 10.0)))
        val reset = UsageAccount("reset", sampledAt = "2026-11-05T11:59:00Z", windows = listOf(UsageWindow("5h", 0, 10.0, "2026-11-05T11:00:00Z")))
        val live = UsageAccount("live", name = "Codex Pro", plan = "pro", sampledAt = "2026-11-05T11:59:00Z",
            windows = listOf(UsageWindow("5h", 0, 10.0), UsageWindow("7d", 0, 99.0, model = "gpt-6")))
        assertEquals(listOf("live"), liveAccounts(listOf(old, reset, live), now).map { it.id })
        assertEquals("a model's allowance is not the account's", 10.0, fullest(live), 0.0)
        assertEquals("", planNote(live))
        assertEquals("max", planNote(live.copy(name = "Claude", plan = "max")))
        assertEquals(listOf("full", "live", "stale"), orderedAccounts(listOf(live, live.copy(id = "stale", stale = true),
            live.copy(id = "full", windows = listOf(UsageWindow("5h", 0, 70.0))))).map { it.id })
    }

    private open class Api : MachineManagement {
        var report = DoctorReport(checks = listOf(DoctorCheck("node.sock")))
        var fixed = DoctorReport(checks = listOf(DoctorCheck("node.sock")), fixed = listOf("a", "b"))
        override suspend fun launchers(machineId: String) = emptyList<AgentLauncher>()
        override suspend fun launcherQuota(machineId: String, name: String, refresh: Boolean) = LauncherQuotaAnswer()
        override suspend fun reconnect(machineId: String) = Unit
        override suspend fun deploy(machineId: String) = Unit
        override suspend fun herdr(machineId: String): HerdrState = error("unused")
        override suspend fun startHerdr(machineId: String): HerdrState = error("unused")
        override suspend fun setEngine(machineId: String, engine: String) = Machine(machineId)
        override suspend fun claudeSettings(machineId: String) = emptyList<ClaudeSetting>()
        override suspend fun setClaudeSetting(machineId: String, key: String, value: JsonElement) = emptyList<ClaudeSetting>()
        override suspend fun codexSettings(machineId: String) = emptyList<ClaudeSetting>()
        override suspend fun setCodexSetting(machineId: String, key: String, value: JsonElement) = emptyList<ClaudeSetting>()
        override suspend fun doctor(machineId: String) = report
        override suspend fun fixDoctor(machineId: String) = fixed
    }

    @Test fun openChecksFixRewritesAndSaysHowMany() = runTest {
        val api = Api()
        val model = MachineHealthViewModel(api)
        model.open("m", online = false)
        assertEquals(DoctorLoad.Loading, model.state.value.doctor)
        model.open("m", online = true)
        assertEquals(DoctorLoad.Report(api.report), model.state.value.doctor)
        val notices = mutableListOf<HealthNotice>()
        val collecting = backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { model.notices.collect(notices::add) }
        model.fix("m")
        assertEquals(DoctorLoad.Report(api.fixed), model.state.value.doctor)
        assertEquals(listOf<HealthNotice>(HealthNotice.Fixed(2)), notices)
        collecting.cancel()
    }

    @Test fun anOldNodeIsUnavailableNotAFailure() = runTest {
        val api = object : Api() {
            override suspend fun doctor(machineId: String): DoctorReport =
                throw persona.core.hub.HubFailure(persona.core.hub.HubError.Server, "unknown method node.doctor", 502, body = "{}")
        }
        val model = MachineHealthViewModel(api)
        model.open("m", online = true)
        assertEquals(DoctorLoad.Unavailable(DoctorUnavailable.OldNode), model.state.value.doctor)
    }

    @Test fun aBrowserGoneFromTheListIsNotKeptFromTheNode() = runTest {
        val api = object : Api() {
            override suspend fun browser(machineId: String) = persona.core.contracts.MachineBrowser(enabled = true, installed = true)
        }
        val model = MachineHealthViewModel(api)
        model.browserFromList(persona.core.contracts.MachineBrowser(enabled = true))
        model.refreshBrowser("m")
        assertEquals(true, model.state.value.browser?.installed)
        model.browserFromList(persona.core.contracts.MachineBrowser(enabled = true))
        assertEquals("the node's newer answer stays", true, model.state.value.browser?.installed)
        model.browserFromList(null)
        assertNull(model.state.value.browser)
        assertFalse(model.watchesBrowser(online = true))
    }
}
