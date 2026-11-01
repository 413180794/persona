package jarvis.android

import jarvis.android.data.ApiClient
import jarvis.android.data.DetectedEngine
import jarvis.android.data.EngineSelection
import jarvis.android.data.Engines
import jarvis.android.data.HubException
import jarvis.android.data.JsonCodec
import jarvis.android.data.Machine
import jarvis.android.settings.setEngine
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class EnginesDecodeTest {
    @Test fun missingFieldsDefaultToHerdrAndNoDetection() {
        val m = JsonCodec.decodeFromString<Machine>("""{"id":"m1","online":true}""")
        assertEquals("herdr", m.engine)
        assertTrue(m.engines.isEmpty())
        val n = JsonCodec.decodeFromString<Machine>("""{"id":"m1","engine":null,"engines":null}""")
        assertEquals("herdr", n.engine)
        assertTrue(n.engines.isEmpty())
    }

    @Test fun keepsUnknownEnginesAndFieldsAsGiven() {
        val m = JsonCodec.decodeFromString<Machine>(
            """{"id":"m1","engine":"orca","engines":[
              {"id":"herdr","name":"herdr","available":true,"version":"0.9.3","extra":1},
              {"id":"orca","name":"Orca","available":false,"note":"这个版本的 Orca 和 Jarvis 不兼容"},
              {"id":"future","name":"Future"},
              {"id":"bare"}
            ],"surprise":{"a":1}}""",
        )
        assertEquals("orca", m.engine)
        assertEquals(listOf("herdr", "orca", "future", "bare"), m.engines.map { it.id })
        assertEquals("0.9.3", m.engines[0].version)
        assertEquals("这个版本的 Orca 和 Jarvis 不兼容", m.engines[1].note)
        assertFalse(m.engines[2].available)
        assertEquals("", m.engines[3].name)
    }
}

class EnginesLogicTest {
    private fun machine(
        engine: String = "herdr",
        engines: List<DetectedEngine> = emptyList(),
        online: Boolean = true,
        kind: String = "ssh",
        id: String = "m1",
    ) = Machine(id = id, kind = kind, online = online, engine = engine, engines = engines)

    private val herdr = DetectedEngine("herdr", "herdr", available = true, version = "0.9.3")
    private val core = DetectedEngine("herdr-core", "Herdr App", available = false)
    private val orca = DetectedEngine("orca", "Orca", available = false, note = "打开 Orca")

    @Test fun pairingMachineIsTheLocalOneElseTheFirst() {
        val a = machine(id = "a")
        val local = machine(id = "b", kind = "local")
        assertEquals("b", Engines.pairingMachine(listOf(a, local))?.id)
        assertEquals("a", Engines.pairingMachine(listOf(a, machine(id = "c")))?.id)
        assertNull(Engines.pairingMachine(emptyList()))
    }

    @Test fun pairingPreselectsTheOnlyAvailableEngine() {
        val m = machine(engine = "herdr", engines = listOf(herdr.copy(available = false), core.copy(available = true), orca))
        assertEquals("herdr-core", Engines.initialChoice(m, pairing = true))
        // Outside pairing the current choice stays.
        assertEquals("herdr", Engines.initialChoice(m, pairing = false))
    }

    @Test fun severalOrNoneAvailableKeepsTheCurrentEngine() {
        val two = machine(engine = "orca", engines = listOf(herdr, core.copy(available = true), orca))
        assertEquals("orca", Engines.initialChoice(two, pairing = true))
        val none = machine(engine = "herdr-core", engines = listOf(herdr.copy(available = false)))
        assertEquals("herdr-core", Engines.initialChoice(none, pairing = true))
        assertEquals("herdr", Engines.initialChoice(machine(engine = ""), pairing = true))
    }

    @Test fun optionsAreTheHubsListElseTheKnownEngines() {
        val future = DetectedEngine("future", "Future", available = true)
        assertEquals(listOf("herdr", "future"), Engines.options(machine(engines = listOf(herdr, future))).map { it.id })
        val fallback = Engines.options(machine())
        assertEquals(listOf("herdr", "herdr-core", "orca"), fallback.map { it.id })
        assertEquals(listOf("herdr", "Herdr App", "Orca"), fallback.map { it.name })
        assertTrue(fallback.none { it.available })
    }

    @Test fun unknownEnginesOnlyWhenAvailable() {
        val m = machine(engines = listOf(DetectedEngine("future", "Future", available = true), DetectedEngine("gone", "Gone")))
        assertTrue(Engines.canSelect("future", m))
        assertFalse(Engines.canSelect("gone", m))
        assertFalse(Engines.canSelect("nothing", m))
        // Known engines stay selectable offline and without detection (applied on connect).
        assertTrue(Engines.canSelect("orca", machine(online = false)))
    }

    @Test fun detectionAndNamesAndFooter() {
        val m = machine(engines = listOf(herdr, core))
        assertEquals("检测到可用 · 0.9.3", Engines.detection("herdr", m))
        assertEquals("没检测到", Engines.detection("herdr-core", m))
        assertEquals("暂无检测信息", Engines.detection("orca", m))
        assertEquals("离线，暂无检测信息", Engines.detection("herdr", machine(online = false, engines = listOf(herdr))))
        assertEquals("Herdr App", Engines.currentName(machine(engine = "herdr-core")))
        assertEquals("Mine", Engines.currentName(machine(engine = "x", engines = listOf(DetectedEngine("x", "Mine")))))
        assertEquals("x", Engines.currentName(machine(engine = "x")))
        assertTrue(Engines.footer(machine(online = false)).startsWith("机器离线，选择会在连接后生效"))
        assertTrue(Engines.footer(machine()).startsWith("只显示所选引擎里的会话"))
    }
}

class EngineSelectionTest {
    private val m = Machine(id = "m1", online = true, engine = "herdr",
        engines = listOf(DetectedEngine("herdr", "herdr", true), DetectedEngine("orca", "Orca", true)))

    @Test fun saveAppliesTheHubsMachine() = runBlocking {
        val applied = mutableListOf<Machine>()
        var sent: Pair<String, String>? = null
        val s = EngineSelection(m, pairing = false, put = { id, e -> sent = id to e; m.copy(engine = e) }, updated = { applied += it })
        s.choose("orca")
        assertTrue(s.save("m1"))
        assertEquals("m1" to "orca", sent)
        assertEquals("orca", applied.single().engine)
        assertEquals("orca", s.selected)
        assertNull(s.error)
        assertFalse(s.saving)
    }

    @Test fun failedSaveKeepsTheChoiceAndSaysWhy() = runBlocking {
        val applied = mutableListOf<Machine>()
        val s = EngineSelection(m, pairing = true, put = { _, _ -> throw HubException(400, "unknown engine") }, updated = { applied += it })
        s.choose("orca")
        assertFalse(s.save("m1"))
        assertEquals("保存失败：unknown engine", s.error)
        assertEquals("orca", s.selected)
        assertTrue(applied.isEmpty())
        assertFalse(s.saving)
        // Choosing again clears the error.
        s.choose("herdr")
        assertNull(s.error)
        val io = EngineSelection(m, pairing = true, put = { _, _ -> throw java.io.IOException("reset") }, updated = {})
        assertFalse(io.save("m1"))
        assertEquals("保存失败：连不上 hub", io.error)
    }

    @Test fun offlineMachineSavesToo() = runBlocking {
        val offline = m.copy(online = false, engines = emptyList())
        val s = EngineSelection(offline, pairing = false, put = { _, e -> offline.copy(engine = e) }, updated = {})
        s.choose("herdr-core")
        assertTrue(s.save("m1"))
        assertEquals("herdr-core", s.selected)
    }

    @Test fun livePushesMoveThePickerUntilEdited() {
        val s = EngineSelection(m, pairing = false, put = { _, _ -> m }, updated = {})
        s.synchronize(m.copy(engine = "orca"))
        assertEquals("orca", s.selected)
        s.choose("herdr")
        s.synchronize(m.copy(engine = "orca"))
        assertEquals("herdr", s.selected)
    }

    @Test fun noSecondSaveWhileOneIsRunning() = runBlocking {
        val gate = CompletableDeferred<Unit>()
        var calls = 0
        val s = EngineSelection(m, pairing = false, put = { _, e -> calls++; gate.await(); m.copy(engine = e) }, updated = {})
        val first = async(start = kotlinx.coroutines.CoroutineStart.UNDISPATCHED) { s.save("m1") }
        assertTrue(s.saving)
        assertFalse(s.save("m1"))
        gate.complete(Unit)
        assertTrue(first.await())
        assertEquals(1, calls)
    }
}

class SetEngineApiTest {
    private val server = MockWebServer().apply { start() }
    private val api = ApiClient(server.url("/").toString(), "tok")

    @After fun tearDown() { runCatching { server.shutdown() } }

    @Test fun putsTheEngineAndDecodesTheMachine() = runBlocking {
        server.enqueue(MockResponse().setBody("""{"id":"m1","engine":"orca","engines":[{"id":"orca","name":"Orca","available":true}]}"""))
        val saved = api.setEngine("m1", "orca")
        val r = server.takeRequest()
        assertEquals("PUT", r.method)
        assertEquals("/api/v1/machines/m1/engine", r.path)
        assertEquals("""{"engine":"orca"}""", r.body.readUtf8())
        assertEquals("orca", saved.engine)
        assertTrue(saved.engines.single().available)
    }

    @Test fun errorsBecomeHubExceptions() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"unknown engine"}"""))
        val e = runCatching { api.setEngine("m1", "nope") }.exceptionOrNull() as HubException
        assertEquals(400, e.status)
        assertEquals("unknown engine", e.message)
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"no such machine"}"""))
        assertEquals(404, (runCatching { api.setEngine("x", "herdr") }.exceptionOrNull() as HubException).status)
    }
}
