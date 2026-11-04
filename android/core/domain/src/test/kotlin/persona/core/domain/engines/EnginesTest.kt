package persona.core.domain.engines

import persona.core.contracts.DetectedEngine
import persona.core.contracts.Machine
import org.junit.Assert.*
import org.junit.Test

class EnginesTest {
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

    @Test fun pairingAsksAboutTheLocalMachineElseTheFirstAndOnlyWhenEnginesAreReported() {
        val a = machine(id = "a")
        val local = machine(id = "b", kind = "local", engines = listOf(herdr))
        assertEquals("b", Engines.pairingMachine(listOf(a, local))?.id)
        assertEquals("a", Engines.pairingMachine(listOf(a, machine(id = "c")))?.id)
        assertNull(Engines.pairingMachine(emptyList()))
        assertEquals("b", Engines.pairingStep(listOf(a, local))?.id)
        // An older hub reports no engines: no step that could not be saved.
        assertNull(Engines.pairingStep(listOf(a, local.copy(engines = emptyList()))))
        assertNull(Engines.pairingStep(emptyList()))
    }

    @Test fun pickerStartsOnTheCurrentEngineHerdrByDefault() {
        // Never chosen: herdr, even when only another engine is detected.
        assertEquals("herdr", Engines.initialChoice(machine(engines = listOf(herdr.copy(available = false), core.copy(available = true), orca))))
        assertEquals("herdr", Engines.initialChoice(machine(engine = "")))
        assertEquals("orca", Engines.initialChoice(machine(engine = "orca", engines = listOf(herdr, core.copy(available = true), orca))))
        assertEquals("herdr-core", Engines.initialChoice(machine(engine = "herdr-core", engines = listOf(herdr.copy(available = false)))))
    }

    @Test fun optionsAreTheHubsListElseTheKnownEngines() {
        val future = DetectedEngine("future", "Future", available = true)
        assertEquals(listOf("herdr", "future"), Engines.options(machine(engines = listOf(herdr, future))).map { it.id })
        val fallback = Engines.options(machine())
        assertEquals(listOf("herdr", "herdr-core", "orca", "paseo"), fallback.map { it.id })
        assertEquals(listOf("herdr", "Herdr App", "Orca", "Paseo"), fallback.map { it.name })
        assertTrue(fallback.none { it.available })
    }

    @Test fun unknownEnginesOnlyWhenAvailable() {
        val m = machine(engines = listOf(DetectedEngine("future", "Future", available = true), DetectedEngine("gone", "Gone")))
        assertTrue(Engines.canSelect("future", m))
        assertFalse(Engines.canSelect("gone", m))
        assertFalse(Engines.canSelect("nothing", m))
        // Known engines stay selectable offline and without detection (applied on connect).
        assertTrue(Engines.canSelect("orca", machine(online = false)))
        assertTrue(Engines.canSelect("paseo", machine(online = false)))
        assertEquals("Paseo 里的 Agent", Engines.detail("paseo"))
    }

    @Test fun detectionNamesAndNotes() {
        val m = machine(engines = listOf(herdr, core))
        assertEquals("检测到可用，版本 0.9.3", Engines.detection("herdr", m))
        assertEquals("没检测到", Engines.detection("herdr-core", m))
        assertEquals("暂无检测信息", Engines.detection("orca", m))
        assertEquals("离线，暂无检测信息", Engines.detection("herdr", machine(online = false, engines = listOf(herdr))))
        assertEquals("Herdr App", Engines.currentName(machine(engine = "herdr-core")))
        assertEquals("Mine", Engines.currentName(machine(engine = "x", engines = listOf(DetectedEngine("x", "Mine")))))
        assertEquals("x", Engines.currentName(machine(engine = "x")))
        assertEquals("桌面 App 里开的终端", Engines.detail("herdr-core"))
        assertNull(Engines.detail("x"))
        assertTrue(Engines.footer(machine(online = false)).startsWith("机器离线，选择会在连接后生效"))
        assertTrue(Engines.footer(machine()).startsWith("只显示所选引擎里的会话"))
        assertEquals("已切换引擎", Engines.saved(machine()))
        assertEquals("已保存，连上后生效", Engines.saved(machine(online = false)))
    }
}
