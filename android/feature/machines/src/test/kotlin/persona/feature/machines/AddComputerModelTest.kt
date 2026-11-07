package persona.feature.machines

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Machine
import persona.core.contracts.api.ComputerCode
import persona.core.contracts.api.ComputerOs
import persona.core.contracts.api.ComputerPairing
import persona.core.contracts.api.HubCodedFailure
import persona.core.designsystem.UiText

/** Machines › Add a computer (docs/assistant/onboarding.md 4.2): the code, the wait, "It's mine" on the same screen, and the refusals in words. */
class AddComputerModelTest {
    private class Refused(override val status: Int?, override val code: String?) : Exception("refused"), HubCodedFailure

    private class Hub : ComputerPairing {
        override val canAdd = flowOf(true)
        val calls = mutableListOf<String>()
        var listed = emptyList<Machine>()
        var refuse: Exception? = null
        override suspend fun add(name: String, os: ComputerOs): ComputerCode {
            calls += "add $name ${os.wire}"
            refuse?.let { throw it }
            return ComputerCode("macbook", name, "persona://link?machine=macbook", 600_000)
        }
        override suspend fun again(machineId: String): ComputerCode { calls += "again $machineId"; return ComputerCode(machineId, "", "persona://link?2", 1_200_000) }
        override suspend fun machines(machineId: String) = listed.also { calls += "machines $machineId" }
        override suspend fun confirm(machineId: String) { calls += "confirm $machineId" }
        override suspend fun reject(machineId: String) { calls += "reject $machineId" }
    }

    @Test fun addWaitConfirm() = runBlocking {
        val hub = Hub()
        val list = MutableStateFlow(emptyList<Machine>())
        var now = 0L
        val model = AddComputerModel(hub, list) { now }
        assertEquals(AddStage.Form, model.state.value.stage(list.value))
        model.add()
        assertTrue("no name, nothing asked", hub.calls.isEmpty())
        model.setOs(ComputerOs.Linux)
        model.setName("小林的 MacBook" + "很长".repeat(30))
        assertEquals(ComputerNameMax, model.state.value.name.length)
        model.setName(" 小林的 MacBook ")
        model.add()
        assertEquals("add 小林的 MacBook linux", hub.calls.single())
        assertEquals(AddStage.Waiting, model.state.value.stage(list.value))
        assertEquals("10:00", clockText(model.state.value.secondsLeft))

        // The poll finds it online, waiting to be confirmed: the question is on the same screen.
        hub.listed = listOf(Machine("macbook", "小林的 MacBook", online = true, trust = "pending", os = "linux"))
        model.poll()
        assertEquals(AddStage.Joined, model.state.value.stage(list.value))
        model.answer(true)
        assertEquals("confirm macbook", hub.calls.last())
        assertEquals(AddStage.Connected, model.state.value.stage(list.value))
    }

    @Test fun anExpiredLinkGetsANewOneAndARemovedComputerIsGone() = runBlocking {
        val hub = Hub()
        val list = MutableStateFlow(emptyList<Machine>())
        var now = 0L
        val model = AddComputerModel(hub, list) { now }
        model.setName("家里的服务器"); model.add()
        now = 600_000; model.tick()
        assertEquals(AddStage.Expired, model.state.value.stage(list.value))
        model.again()
        assertEquals("again macbook", hub.calls.last())
        assertEquals(AddStage.Waiting, model.state.value.stage(list.value))
        // In the WebSocket's list, then removed (rejected elsewhere, or it waited too long).
        list.value = listOf(Machine("macbook", online = false, trust = "pending"))
        model.tick()
        list.value = emptyList()
        assertEquals(AddStage.Gone, model.state.value.stage(list.value))
    }

    @Test fun rejectedWhileAskedIsGone() = runTest {
        // #5080: found by the poll, then rejected on the web (or removed): the poll keeps reading and it is gone.
        val hub = Hub()
        val list = MutableStateFlow(emptyList<Machine>())
        val model = AddComputerModel(hub, list) { 0L }
        model.setName("小林的 MacBook"); model.add()
        backgroundScope.launch { model.run() }
        hub.listed = listOf(Machine("macbook", online = true, trust = "pending"))
        advanceTimeBy(3_000); runCurrent()
        assertEquals(AddStage.Joined, model.state.value.stage(list.value))
        val polls = hub.calls.count { it.startsWith("machines") }
        hub.listed = emptyList()
        advanceTimeBy(3_000); runCurrent()
        assertTrue("still polling while asked", hub.calls.count { it.startsWith("machines") } > polls)
        assertEquals(AddStage.Gone, model.state.value.stage(list.value))
    }

    @Test fun joinedThenOfflinePastExpiryIsOfflineNotExpired() = runTest {
        // #5307: it joined, nobody answered, it went to sleep and the code ran out: not Expired (a new code would revoke
        // the pairing of a computer that works), but Offline, and Joined again when it wakes.
        val hub = Hub()
        val list = MutableStateFlow(emptyList<Machine>())
        var now = 0L
        val model = AddComputerModel(hub, list) { now }
        model.setName("小林的 MacBook"); model.add()
        backgroundScope.launch { model.run() }
        hub.listed = listOf(Machine("macbook", online = true, trust = "pending"))
        advanceTimeBy(3_000); runCurrent()
        assertEquals(AddStage.Joined, model.state.value.stage(list.value))
        now = 600_000
        hub.listed = listOf(Machine("macbook", online = false, trust = "pending", errorCode = "link_offline"))
        advanceTimeBy(3_000); runCurrent()
        assertEquals(AddStage.Offline, model.state.value.stage(list.value))
        hub.listed = listOf(Machine("macbook", online = false, trust = "pending"))
        advanceTimeBy(3_000); runCurrent()
        assertEquals("joined, even with no error code", AddStage.Offline, model.state.value.stage(list.value))
        hub.listed = listOf(Machine("macbook", online = true, trust = "pending"))
        advanceTimeBy(3_000); runCurrent()
        assertEquals(AddStage.Joined, model.state.value.stage(list.value))
        assertTrue(hub.calls.none { it.startsWith("again") })
    }

    @Test fun confirmingOneThatWaits() = runBlocking {
        val hub = Hub()
        val list = MutableStateFlow(listOf(Machine("old", online = true), Machine("a", online = false, trust = "pending", errorCode = "never_connected"),
            Machine("b", online = true, trust = "pending", os = "darwin")))
        // "": the first one waiting that is online.
        val first = AddComputerModel(hub, list, confirm = "")
        assertEquals("b", first.state.value.confirming)
        assertEquals(ComputerOs.Mac, first.state.value.os)
        assertEquals(AddStage.Joined, first.state.value.stage(list.value))
        val offline = AddComputerModel(hub, list, confirm = "a") { 0L }
        assertEquals(AddStage.Offline, offline.state.value.stage(list.value))
        assertEquals(R.string.machines_onb_code_never_connected, machineErrorText(list.value[1].errorCode))
        offline.again()
        assertEquals("again a", hub.calls.last())
        assertEquals(AddStage.Waiting, offline.state.value.stage(list.value))
        offline.answer(false)
        assertEquals("reject a", hub.calls.last())
        assertEquals(AddStage.Gone, offline.state.value.stage(list.value))
        // Confirmed elsewhere meanwhile.
        list.value = listOf(Machine("b", online = true))
        assertEquals(AddStage.Connected, first.state.value.stage(list.value))
        assertNull(machineErrorText("something_new"))
    }

    @Test fun refusalsAreInPlainWords() = runBlocking {
        val hub = Hub().apply { refuse = Refused(429, "too_many_pending") }
        val model = AddComputerModel(hub, MutableStateFlow(emptyList()))
        model.setName("x"); model.add()
        assertEquals(UiText.Res(R.string.machines_onb_err_too_many_pending), model.state.value.error)
        assertEquals(AddStage.Form, model.state.value.stage(emptyList()))
        assertEquals(UiText.Res(R.string.machines_onb_err_tls_required), addErrorText(Refused(409, "tls_required")))
        assertEquals(UiText.Res(R.string.machines_onb_err_mesh_member), addErrorText(Refused(409, "mesh_member")))
    }

    @Test fun commands() {
        val code = ComputerCode("m", "n", "persona://link?a=1&b='x'", 0)
        assertEquals("persona-link pair 'persona://link?a=1&b=%27x%27'", pairCommand(code))
        assertEquals(".\\persona-link.exe install --personad .\\personad.exe --pair 'persona://link?a=1&b=%27x%27'", windowsCommand(code.pairUrl))
        assertEquals("persona-link pair 'given'", pairCommand(code.copy(command = "persona-link pair 'given'")))
    }

    @Test fun anUnreadableOrSkewedExpiryCountsTenMinutesFromNow() {
        assertEquals(5_000 + ComputerCodeLifeMs, localExpiry(0, 5_000))
        assertEquals("phone clock ahead of the hub", 5_000 + ComputerCodeLifeMs, localExpiry(4_000, 5_000))
        assertEquals("phone clock behind the hub", 5_000 + ComputerCodeLifeMs, localExpiry(5_000 + 3 * ComputerCodeLifeMs, 5_000))
        assertEquals(305_000L, localExpiry(305_000, 5_000))
    }

    @Test fun confirmWithNoComputerNamedNeverShowsTheAddForm() = runBlocking {
        val hub = Hub()
        val list = MutableStateFlow(emptyList<Machine>())
        val model = AddComputerModel(hub, list, confirm = "") { 0L }
        assertEquals(AddStage.Gone, model.state.value.stage(list.value))
        assertNull(model.state.value.machineId)
        // The list arrives after the page opened.
        list.value = listOf(Machine("lab", online = true, trust = "pending", os = "linux"))
        model.tick()
        assertEquals("lab", model.state.value.machineId)
        assertEquals(AddStage.Joined, model.state.value.stage(list.value))
    }
}
